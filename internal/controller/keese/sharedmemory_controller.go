// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package keese

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	keesev1alpha1 "github.com/keese-ai/keese/api/keese/v1alpha1"
)

const (
	sharedMemoryFinalizer  = "finalizers.sharedmemory.operator.keese.ai/cleanup"
	sharedMemoryFieldOwner = "keese-sharedmemory-controller"
)

// SharedMemoryReconciler reconciles a SharedMemory object.
//
// +kubebuilder:rbac:groups=memory.operator.keese.ai,resources=sharedmemories,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=memory.operator.keese.ai,resources=sharedmemories/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=memory.operator.keese.ai,resources=sharedmemories/finalizers,verbs=update
// +kubebuilder:rbac:groups=core,resources=events,verbs=create;patch
type SharedMemoryReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
	Backend  BackendProvisioner
	Rebac    MemoryRebacWriter
}

// Reconcile implements the SharedMemory reconciliation loop.
//
// On every sharedWith[] update: writes memory.reader or memory.writer tuples per
// workspace. On deletion: purges all shared tuples before backend deprovision.
//
// The SharedMemoryMutationAuthz VAP check (≤15ms OpenFGA 1-hop per design 04a) is
// enforced at admission; the controller trusts the API server's admission result.
func (r *SharedMemoryReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	sm := &keesev1alpha1.SharedMemory{}
	if err := r.Get(ctx, req.NamespacedName, sm); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("get SharedMemory: %w", err)
	}

	orig := sm.DeepCopy()

	// Deletion path — purge shared tuples BEFORE backend deprovision.
	if !sm.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, sm, orig)
	}

	// Ensure finalizer present before any external work.
	if !controllerutil.ContainsFinalizer(sm, sharedMemoryFinalizer) {
		controllerutil.AddFinalizer(sm, sharedMemoryFinalizer)
		if err := r.Update(ctx, sm); err != nil {
			return ctrl.Result{}, fmt.Errorf("add finalizer: %w", err)
		}
		return ctrl.Result{Requeue: true}, nil
	}

	// HA validation.
	if err := validateHA(sm.Spec.Provider, sm.Namespace); err != nil {
		log.Info("HA violation detected on SharedMemory", "reason", err.Error())
		r.Recorder.Eventf(sm, corev1.EventTypeWarning, ReasonHAViolation, "%s", err.Error())
		return r.setSharedStatus(ctx, sm, orig, keesev1alpha1.MemoryPhaseDegraded,
			metav1.Condition{
				Type:    "Ready",
				Status:  metav1.ConditionFalse,
				Reason:  ReasonHAViolation,
				Message: err.Error(),
			})
	}

	// Backend provisioning.
	sm.Status.Phase = keesev1alpha1.MemoryPhaseProvisioning
	created, err := r.Backend.Provision(ctx, sm.Spec.Provider, sm.Name, sm.Namespace)
	if err != nil {
		log.Error(err, "backend provision failed")
		r.Recorder.Eventf(sm, corev1.EventTypeWarning, ReasonProvisioningFailed, "provision failed: %v", err)
		return r.setSharedStatus(ctx, sm, orig, keesev1alpha1.MemoryPhaseDegraded,
			metav1.Condition{
				Type:    "Ready",
				Status:  metav1.ConditionFalse,
				Reason:  ReasonProvisioningFailed,
				Message: err.Error(),
			})
	}
	if created {
		r.Recorder.Eventf(sm, corev1.EventTypeNormal, ReasonProvisioningStarted, "backend provisioning started")
	}

	// Health check.
	healthy, err := r.Backend.Healthy(ctx, sm.Spec.Provider, sm.Name, sm.Namespace)
	if err != nil || !healthy {
		msg := "backend unhealthy"
		if err != nil {
			msg = err.Error()
		}
		r.Recorder.Eventf(sm, corev1.EventTypeWarning, ReasonDegraded, "backend unhealthy: %s", msg)
		return r.setSharedStatus(ctx, sm, orig, keesev1alpha1.MemoryPhaseDegraded,
			metav1.Condition{
				Type:    "Ready",
				Status:  metav1.ConditionFalse,
				Reason:  ReasonDegraded,
				Message: msg,
			})
	}
	sm.Status.BackendProvisioned = true

	// Sync sharedWith[] tuples.
	tuples, err := r.buildSharedTuples(sm)
	if err != nil {
		return ctrl.Result{}, err
	}
	if err := r.Rebac.Write(ctx, tuples); err != nil {
		log.Error(err, "rebac write failed")
		r.Recorder.Eventf(sm, corev1.EventTypeWarning, ReasonRebacSyncFailed, "rebac sync failed: %v", err)
		return r.setSharedStatus(ctx, sm, orig, keesev1alpha1.MemoryPhaseDegraded,
			metav1.Condition{
				Type:    "Ready",
				Status:  metav1.ConditionFalse,
				Reason:  ReasonRebacSyncFailed,
				Message: err.Error(),
			})
	}
	sm.Status.RebacTupleCount = int32(len(tuples))
	r.Recorder.Eventf(sm, corev1.EventTypeNormal, ReasonRebacSyncSucceeded, "rebac tuples synced: %d", len(tuples))

	r.Recorder.Eventf(sm, corev1.EventTypeNormal, ReasonProvisioningSucceeded, "backend ready")
	return r.setSharedStatus(ctx, sm, orig, keesev1alpha1.MemoryPhaseReady,
		metav1.Condition{
			Type:    "Ready",
			Status:  metav1.ConditionTrue,
			Reason:  ReasonReady,
			Message: "backend provisioned and healthy",
		})
}

// buildSharedTuples constructs the full set of reader/writer tuples for the
// current sharedWith[] list. Returns an empty slice when sharedWith is empty.
func (r *SharedMemoryReconciler) buildSharedTuples(sm *keesev1alpha1.SharedMemory) ([]MemoryTuple, error) {
	id := string(sm.UID)
	tuples := make([]MemoryTuple, 0, len(sm.Spec.SharedWith))
	for _, ws := range sm.Spec.SharedWith {
		wsID := ws.Namespace + "/" + ws.Name
		switch ws.Access {
		case "writer":
			tuples = append(tuples, MemoryWriterTuple(id, wsID))
		default:
			// Default to reader (also covers "reader" explicitly and empty string).
			tuples = append(tuples, MemoryReaderTuple(id, wsID))
		}
	}
	return tuples, nil
}

// reconcileDelete purges all shared tuples before backend deprovision.
// Ordered to prevent orphaned grants on forced delete.
func (r *SharedMemoryReconciler) reconcileDelete(ctx context.Context, sm, orig *keesev1alpha1.SharedMemory) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	if !controllerutil.ContainsFinalizer(sm, sharedMemoryFinalizer) {
		return ctrl.Result{}, nil
	}

	sm.Status.Phase = keesev1alpha1.MemoryPhaseTerminating
	r.Recorder.Eventf(sm, corev1.EventTypeNormal, ReasonDeprovisioningStarted, "cleanup started")

	// Purge all shared tuples FIRST.
	tuples, _ := r.buildSharedTuples(sm)
	if len(tuples) > 0 {
		if err := r.Rebac.Delete(ctx, tuples); err != nil {
			log.Error(err, "rebac purge failed")
			r.Recorder.Eventf(sm, corev1.EventTypeWarning, ReasonRebacPurgeFailed, "rebac purge failed: %v", err)
			return r.setSharedStatus(ctx, sm, orig, keesev1alpha1.MemoryPhaseTerminating,
				metav1.Condition{
					Type:    "Ready",
					Status:  metav1.ConditionFalse,
					Reason:  ReasonRebacPurgeFailed,
					Message: err.Error(),
				})
		}
	}

	// Deprovision backend.
	if err := r.Backend.Deprovision(ctx, sm.Spec.Provider, sm.Name, sm.Namespace); err != nil {
		log.Error(err, "backend deprovision failed")
		r.Recorder.Eventf(sm, corev1.EventTypeWarning, ReasonDeprovisioningFailed, "deprovision failed: %v", err)
		return r.setSharedStatus(ctx, sm, orig, keesev1alpha1.MemoryPhaseTerminating,
			metav1.Condition{
				Type:    "Ready",
				Status:  metav1.ConditionFalse,
				Reason:  ReasonDeprovisioningFailed,
				Message: err.Error(),
			})
	}

	r.Recorder.Eventf(sm, corev1.EventTypeNormal, ReasonDeprovisioningSucceeded, "cleanup complete")

	controllerutil.RemoveFinalizer(sm, sharedMemoryFinalizer)
	if err := r.Update(ctx, sm); err != nil {
		return ctrl.Result{}, fmt.Errorf("remove finalizer: %w", err)
	}
	return ctrl.Result{}, nil
}

// setSharedStatus patches the SharedMemory status subresource.
func (r *SharedMemoryReconciler) setSharedStatus(
	ctx context.Context,
	sm, orig *keesev1alpha1.SharedMemory,
	phase keesev1alpha1.MemoryPhase,
	cond metav1.Condition,
) (ctrl.Result, error) {
	sm.Status.Phase = phase
	sm.Status.ObservedGeneration = sm.Generation
	cond.ObservedGeneration = sm.Generation
	meta.SetStatusCondition(&sm.Status.Conditions, cond)

	if err := r.Status().Patch(ctx, sm, client.MergeFrom(orig)); err != nil {
		return ctrl.Result{}, fmt.Errorf("patch status: %w", err)
	}
	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *SharedMemoryReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&keesev1alpha1.SharedMemory{}).
		WithEventFilter(predicate.GenerationChangedPredicate{}).
		Named("memory-sharedmemory").
		Complete(r)
}
