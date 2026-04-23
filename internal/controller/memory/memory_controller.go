// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package memory

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

	memoryv1alpha1 "github.com/keese-ai/keese/api/memory/v1alpha1"
)

const (
	memoryFinalizer  = "finalizers.memory.operator.keese.ai/cleanup"
	memoryFieldOwner = "keese-memory-controller"
)

// MemoryReconciler reconciles a Memory object.
//
// +kubebuilder:rbac:groups=memory.operator.keese.ai,resources=memories,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=memory.operator.keese.ai,resources=memories/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=memory.operator.keese.ai,resources=memories/finalizers,verbs=update
// +kubebuilder:rbac:groups=core,resources=persistentvolumeclaims,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=events,verbs=create;patch
type MemoryReconciler struct {
	client.Client
	Scheme    *runtime.Scheme
	Recorder  record.EventRecorder
	Backend   BackendProvisioner
	Rebac     RebacWriter
}

// Reconcile implements the Memory reconciliation loop.
//
// FSM: Pending → Provisioning → Ready ↔ Degraded; Terminating on deletion.
//
// Idempotency guarantee: converges in ≤3 reconciles with no spec change (rule 04.16).
// No spec is read from status (rule 04.4).
func (r *MemoryReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	// 1. Fetch — canonical fetch with no assumptions about existence.
	mem := &memoryv1alpha1.Memory{}
	if err := r.Get(ctx, req.NamespacedName, mem); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("get Memory: %w", err)
	}

	// 2. DeepCopy for status patch; compute desired from spec only.
	orig := mem.DeepCopy()

	// 3. Deletion path — finalizer cleanup before backend deprovision.
	if !mem.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, mem, orig)
	}

	// 4. Ensure finalizer is present before any external work.
	if !controllerutil.ContainsFinalizer(mem, memoryFinalizer) {
		controllerutil.AddFinalizer(mem, memoryFinalizer)
		if err := r.Update(ctx, mem); err != nil {
			return ctrl.Result{}, fmt.Errorf("add finalizer: %w", err)
		}
		return ctrl.Result{Requeue: true}, nil
	}

	// 5. HA validation — controller-side defence (VAP is primary).
	if err := validateHA(mem.Spec.Provider, mem.Namespace); err != nil {
		log.Info("HA violation detected", "reason", err.Error())
		r.Recorder.Eventf(mem, corev1.EventTypeWarning, ReasonHAViolation, "%s", err.Error())
		return r.setStatus(ctx, mem, orig, memoryv1alpha1.MemoryPhaseDegraded,
			metav1.Condition{
				Type:    "Ready",
				Status:  metav1.ConditionFalse,
				Reason:  ReasonHAViolation,
				Message: err.Error(),
			})
	}

	// 6. Backend provisioning.
	mem.Status.Phase = memoryv1alpha1.MemoryPhaseProvisioning
	created, err := r.Backend.Provision(ctx, mem.Spec.Provider, mem.Name, mem.Namespace)
	if err != nil {
		log.Error(err, "backend provision failed")
		r.Recorder.Eventf(mem, corev1.EventTypeWarning, ReasonProvisioningFailed, "provision failed: %v", err)
		return r.setStatus(ctx, mem, orig, memoryv1alpha1.MemoryPhaseDegraded,
			metav1.Condition{
				Type:    "Ready",
				Status:  metav1.ConditionFalse,
				Reason:  ReasonProvisioningFailed,
				Message: err.Error(),
			})
	}
	if created {
		r.Recorder.Eventf(mem, corev1.EventTypeNormal, ReasonProvisioningStarted, "backend provisioning started")
	}

	// 7. Health check — drives phase transitions.
	healthy, err := r.Backend.Healthy(ctx, mem.Spec.Provider, mem.Name, mem.Namespace)
	if err != nil || !healthy {
		msg := "backend unhealthy"
		if err != nil {
			msg = err.Error()
		}
		r.Recorder.Eventf(mem, corev1.EventTypeWarning, ReasonDegraded, "backend unhealthy: %s", msg)
		return r.setStatus(ctx, mem, orig, memoryv1alpha1.MemoryPhaseDegraded,
			metav1.Condition{
				Type:    "Ready",
				Status:  metav1.ConditionFalse,
				Reason:  ReasonDegraded,
				Message: msg,
			})
	}
	mem.Status.BackendProvisioned = true

	// 8. ReBAC: write owner tuple. Tuple purge on delete (step 3) is ordered BEFORE
	//    backend deprovision to prevent orphaned grants.
	ownerTuple := MemoryOwnerTuple(string(mem.UID), mem.Spec.WorkspaceRef)
	if err := r.Rebac.Write(ctx, []Tuple{ownerTuple}); err != nil {
		log.Error(err, "rebac write failed")
		r.Recorder.Eventf(mem, corev1.EventTypeWarning, ReasonRebacSyncFailed, "rebac sync failed: %v", err)
		return r.setStatus(ctx, mem, orig, memoryv1alpha1.MemoryPhaseDegraded,
			metav1.Condition{
				Type:    "Ready",
				Status:  metav1.ConditionFalse,
				Reason:  ReasonRebacSyncFailed,
				Message: err.Error(),
			})
	}
	mem.Status.RebacTupleCount = 1
	r.Recorder.Eventf(mem, corev1.EventTypeNormal, ReasonRebacSyncSucceeded, "rebac tuples synced")

	// 9. Happy path.
	r.Recorder.Eventf(mem, corev1.EventTypeNormal, ReasonProvisioningSucceeded, "backend ready")
	return r.setStatus(ctx, mem, orig, memoryv1alpha1.MemoryPhaseReady,
		metav1.Condition{
			Type:    "Ready",
			Status:  metav1.ConditionTrue,
			Reason:  ReasonReady,
			Message: "backend provisioned and healthy",
		})
}

// reconcileDelete handles the cleanup path: purge ReBAC tuples first, then
// deprovision the backend, then remove the finalizer.
func (r *MemoryReconciler) reconcileDelete(ctx context.Context, mem, orig *memoryv1alpha1.Memory) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	if !controllerutil.ContainsFinalizer(mem, memoryFinalizer) {
		return ctrl.Result{}, nil
	}

	mem.Status.Phase = memoryv1alpha1.MemoryPhaseTerminating
	r.Recorder.Eventf(mem, corev1.EventTypeNormal, ReasonDeprovisioningStarted, "cleanup started")

	// Purge ReBAC tuples BEFORE backend deprovision (prevents orphaned grants).
	ownerTuple := MemoryOwnerTuple(string(mem.UID), mem.Spec.WorkspaceRef)
	if err := r.Rebac.Delete(ctx, []Tuple{ownerTuple}); err != nil {
		log.Error(err, "rebac purge failed")
		r.Recorder.Eventf(mem, corev1.EventTypeWarning, ReasonRebacPurgeFailed, "rebac purge failed: %v", err)
		return r.setStatus(ctx, mem, orig, memoryv1alpha1.MemoryPhaseTerminating,
			metav1.Condition{
				Type:    "Ready",
				Status:  metav1.ConditionFalse,
				Reason:  ReasonRebacPurgeFailed,
				Message: err.Error(),
			})
	}

	// Deprovision backend.
	if err := r.Backend.Deprovision(ctx, mem.Spec.Provider, mem.Name, mem.Namespace); err != nil {
		log.Error(err, "backend deprovision failed")
		r.Recorder.Eventf(mem, corev1.EventTypeWarning, ReasonDeprovisioningFailed, "deprovision failed: %v", err)
		return r.setStatus(ctx, mem, orig, memoryv1alpha1.MemoryPhaseTerminating,
			metav1.Condition{
				Type:    "Ready",
				Status:  metav1.ConditionFalse,
				Reason:  ReasonDeprovisioningFailed,
				Message: err.Error(),
			})
	}

	r.Recorder.Eventf(mem, corev1.EventTypeNormal, ReasonDeprovisioningSucceeded, "cleanup complete")

	// Remove finalizer last.
	controllerutil.RemoveFinalizer(mem, memoryFinalizer)
	if err := r.Update(ctx, mem); err != nil {
		return ctrl.Result{}, fmt.Errorf("remove finalizer: %w", err)
	}
	return ctrl.Result{}, nil
}

// setStatus patches the status subresource using the original (pre-mutation) object
// as the patch base. It never reads status into the next reconcile decision (rule 04.4).
func (r *MemoryReconciler) setStatus(
	ctx context.Context,
	mem, orig *memoryv1alpha1.Memory,
	phase memoryv1alpha1.MemoryPhase,
	cond metav1.Condition,
) (ctrl.Result, error) {
	mem.Status.Phase = phase
	mem.Status.ObservedGeneration = mem.Generation
	cond.ObservedGeneration = mem.Generation
	meta.SetStatusCondition(&mem.Status.Conditions, cond)

	if err := r.Status().Patch(ctx, mem, client.MergeFrom(orig)); err != nil {
		return ctrl.Result{}, fmt.Errorf("patch status: %w", err)
	}
	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *MemoryReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&memoryv1alpha1.Memory{}).
		WithEventFilter(predicate.GenerationChangedPredicate{}).
		Named("memory-memory").
		Complete(r)
}
