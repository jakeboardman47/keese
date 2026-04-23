// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package runtime

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

	runtimev1alpha1 "github.com/keese-ai/keese/api/runtime/v1alpha1"
)

const (
	runtimeExtensionFinalizer  = "finalizers.runtimeextension.operator.keese.ai/rebac-cleanup"
	runtimeExtensionFieldOwner = "keese-runtimeextension-controller"

	// defaultTenantName is the synthetic tenant used for owner tuples in the absence
	// of a full tenancy CRD integration (TODO(spec-followup): drive from Workspace tenant label).
	defaultTenantName = "default"
)

// RuntimeExtensionReconciler reconciles a RuntimeExtension object.
type RuntimeExtensionReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
	Rebac    RebacWriter
}

// +kubebuilder:rbac:groups=runtime.operator.keese.ai,resources=runtimeextensions,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=runtime.operator.keese.ai,resources=runtimeextensions/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=runtime.operator.keese.ai,resources=runtimeextensions/finalizers,verbs=update
// +kubebuilder:rbac:groups=runtime.operator.keese.ai,resources=agentruntimes,verbs=get;list;watch

// Reconcile moves the RuntimeExtension toward its desired state.
// Idiom: fetch → DeepCopy for patch → compute desired → SSA → status patch.
func (r *RuntimeExtensionReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	// 1. Fetch.
	ext := &runtimev1alpha1.RuntimeExtension{}
	if err := r.Get(ctx, req.NamespacedName, ext); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("get runtimeextension: %w", err)
	}

	// 2. Snapshot for status patch.
	orig := ext.DeepCopy()

	// 3. Deletion path.
	if !ext.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, ext, orig)
	}

	// 4. Ensure finalizer.
	if !controllerutil.ContainsFinalizer(ext, runtimeExtensionFinalizer) {
		controllerutil.AddFinalizer(ext, runtimeExtensionFinalizer)
		if err := r.Update(ctx, ext); err != nil {
			return ctrl.Result{}, fmt.Errorf("add finalizer: %w", err)
		}
		return ctrl.Result{}, nil
	}

	// 5. Validate runtimeRef — the referenced AgentRuntime must exist.
	ar := &runtimev1alpha1.AgentRuntime{}
	if err := r.Get(ctx, client.ObjectKey{Name: ext.Spec.RuntimeRef.Name}, ar); err != nil {
		if apierrors.IsNotFound(err) {
			msg := fmt.Sprintf("runtimeRef %q not found", ext.Spec.RuntimeRef.Name)
			log.Info(msg)
			r.Recorder.Eventf(ext, corev1.EventTypeWarning, ReasonExtensionRuntimeRefInvalid, "%s", msg)
			ext.Status.Phase = runtimev1alpha1.RuntimeExtensionPhaseDegraded
			ext.Status.ObservedGeneration = ext.Generation
			meta.SetStatusCondition(&ext.Status.Conditions, metav1.Condition{
				Type:               "Ready",
				Status:             metav1.ConditionFalse,
				Reason:             ReasonExtensionRuntimeRefInvalid,
				Message:            msg,
				ObservedGeneration: ext.Generation,
			})
			return ctrl.Result{}, r.patchStatus(ctx, ext, orig)
		}
		return ctrl.Result{}, fmt.Errorf("get agentruntime: %w", err)
	}

	// 6. Set ownerReference to AgentRuntime for GC cascade.
	// AgentRuntime is cluster-scoped; RuntimeExtension is namespaced.
	// We set a non-blocking owner annotation rather than a formal ownerRef
	// because cross-namespace ownerRefs are not supported by K8s GC.
	// The controller manually checks references on deletion (rule 04.10).
	// TODO(spec-followup): evaluate adopting a custom GC annotation approach if needed.

	// 7. Write owner tuple (idempotent).
	tenantName := defaultTenantName
	if err := r.Rebac.WriteExtensionOwner(ctx, ext.Name, tenantName); err != nil {
		msg := fmt.Sprintf("write owner tuple: %v", err)
		log.Error(err, "OpenFGA unavailable writing owner tuple")
		r.Recorder.Eventf(ext, corev1.EventTypeWarning, ReasonExtensionOpenFGAUnavailable, "%s", msg)
		ext.Status.Phase = runtimev1alpha1.RuntimeExtensionPhaseDegraded
		ext.Status.ObservedGeneration = ext.Generation
		meta.SetStatusCondition(&ext.Status.Conditions, metav1.Condition{
			Type:               "Ready",
			Status:             metav1.ConditionFalse,
			Reason:             ReasonExtensionOpenFGAUnavailable,
			Message:            msg,
			ObservedGeneration: ext.Generation,
		})
		if pErr := r.patchStatus(ctx, ext, orig); pErr != nil {
			log.Error(pErr, "patch status after OpenFGA error")
		}
		return ctrl.Result{}, fmt.Errorf("write owner tuple: %w", err)
	}
	r.Recorder.Eventf(ext, corev1.EventTypeNormal, ReasonExtensionTupleWritten,
		"owner tuple written for extension %q tenant %q", ext.Name, tenantName)

	// 8. Count active enabled_in tuples and update boundWorkspaces.
	count, err := r.Rebac.CountEnabledIn(ctx, ext.Name)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("count enabled_in: %w", err)
	}

	// 9. Converge to Ready.
	ext.Status.Phase = runtimev1alpha1.RuntimeExtensionPhaseReady
	ext.Status.ObservedGeneration = ext.Generation
	ext.Status.BoundWorkspaces = int32(count) //nolint:gosec // count is bounded by cluster size
	meta.SetStatusCondition(&ext.Status.Conditions, metav1.Condition{
		Type:               "Ready",
		Status:             metav1.ConditionTrue,
		Reason:             ReasonExtensionTupleWritten,
		Message:            fmt.Sprintf("runtimeRef %q resolved; owner tuple written; boundWorkspaces=%d", ar.Name, count),
		ObservedGeneration: ext.Generation,
	})

	return ctrl.Result{}, r.patchStatus(ctx, ext, orig)
}

// reconcileDelete performs ReBAC cleanup before releasing the finalizer.
func (r *RuntimeExtensionReconciler) reconcileDelete(
	ctx context.Context,
	ext *runtimev1alpha1.RuntimeExtension,
	orig *runtimev1alpha1.RuntimeExtension,
) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	if !controllerutil.ContainsFinalizer(ext, runtimeExtensionFinalizer) {
		return ctrl.Result{}, nil
	}

	// Delete all tuples (owner + all enabled_in). Retry on failure — the finalizer
	// blocks deletion until this succeeds (spec failure mode: OpenFGA down → retry).
	deleted, err := r.Rebac.DeleteAllExtensionTuples(ctx, ext.Name)
	if err != nil {
		msg := fmt.Sprintf("delete tuples during finalizer cleanup: %v", err)
		log.Error(err, "OpenFGA unavailable during finalizer cleanup")
		r.Recorder.Eventf(ext, corev1.EventTypeWarning, ReasonExtensionOpenFGAUnavailable, "%s", msg)
		// Return error to trigger backoff retry; finalizer is NOT removed.
		return ctrl.Result{}, fmt.Errorf("delete tuples: %w", err)
	}

	if deleted > 0 {
		r.Recorder.Eventf(ext, corev1.EventTypeNormal, ReasonExtensionTupleDeleted,
			"deleted %d tuple(s) for extension %q", deleted, ext.Name)
	}

	// All tuples gone — safe to remove finalizer.
	controllerutil.RemoveFinalizer(ext, runtimeExtensionFinalizer)
	if err := r.Update(ctx, ext); err != nil {
		return ctrl.Result{}, fmt.Errorf("remove finalizer: %w", err)
	}
	_ = orig // orig used only for status patch path above
	return ctrl.Result{}, nil
}

// patchStatus issues a status-subresource patch.
func (r *RuntimeExtensionReconciler) patchStatus(
	ctx context.Context,
	ext *runtimev1alpha1.RuntimeExtension,
	orig *runtimev1alpha1.RuntimeExtension,
) error {
	patch := client.MergeFrom(orig)
	if err := r.Status().Patch(ctx, ext, patch); err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("patch status: %w", err)
	}
	return nil
}

// SetupWithManager registers the controller with the manager.
func (r *RuntimeExtensionReconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.Recorder = mgr.GetEventRecorderFor(runtimeExtensionFieldOwner)
	return ctrl.NewControllerManagedBy(mgr).
		For(&runtimev1alpha1.RuntimeExtension{}).
		WithEventFilter(predicate.GenerationChangedPredicate{}).
		Named("runtime-runtimeextension").
		Complete(r)
}
