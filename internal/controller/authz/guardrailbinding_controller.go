// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package authz

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	logf "sigs.k8s.io/controller-runtime/pkg/log"

	authzv1alpha1 "github.com/keese-ai/keese/api/authz/v1alpha1"
)

const (
	bindingFinalizer = "finalizers.guardrailbinding.keese.ai/cleanup"
	fieldOwner       = "keese-guardrailbinding-controller"

	// bindingScopeLabel is set by the user to indicate the scope tier for display.
	bindingScopeLabel = "keese.ai/binding-scope"

	// defaultBindingName is the name of the cluster-scoped default GuardrailBinding
	// that lives in keese-system. Its absence is an error event (not a hard failure).
	defaultBindingName      = "keese.ai-default"
	defaultBindingNamespace = "keese-system"
)

// GuardrailBindingReconciler reconciles a GuardrailBinding object.
type GuardrailBindingReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
	Rebac    GuardrailRebacWriter
	Kyverno  KyvernoPolicyProjector
	// Envoy projects the effective tool policy into an Envoy Gateway SecurityPolicy
	// via SSA. CEL expressions are compiled for structural validation before projection.
	// May be nil in installations that do not use the Envoy AI Gateway integration;
	// the controller skips Envoy projection when nil or when spec.envoy is absent.
	Envoy EnvoySecurityPolicyProjector
}

// +kubebuilder:rbac:groups=authz.keese.ai,resources=guardrailbindings,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=authz.keese.ai,resources=guardrailbindings/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=authz.keese.ai,resources=guardrailbindings/finalizers,verbs=update
// +kubebuilder:rbac:groups=core,resources=events,verbs=create;patch
// +kubebuilder:rbac:groups=gateway.envoyproxy.io,resources=securitypolicies,verbs=get;list;watch;create;update;patch
// +kubebuilder:rbac:groups=coordination.k8s.io,resources=leases,verbs=get;create;update

// Reconcile implements the main reconciliation loop for GuardrailBinding.
// Idiom: fetch → deepcopy for status patch → handle deletion → ensure desired state → update status.
func (r *GuardrailBindingReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	var binding authzv1alpha1.GuardrailBinding
	if err := r.Get(ctx, req.NamespacedName, &binding); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	orig := binding.DeepCopy()

	// Handle deletion before anything else (rule 04.10).
	if !binding.DeletionTimestamp.IsZero() {
		return r.cleanup(ctx, &binding, orig)
	}

	// Ensure finalizer is present.
	if !controllerutil.ContainsFinalizer(&binding, bindingFinalizer) {
		controllerutil.AddFinalizer(&binding, bindingFinalizer)
		if err := r.Patch(ctx, &binding, client.MergeFrom(orig)); err != nil {
			return ctrl.Result{}, fmt.Errorf("adding finalizer: %w", err)
		}
		// Re-fetch after patch.
		if err := r.Get(ctx, req.NamespacedName, &binding); err != nil {
			return ctrl.Result{}, client.IgnoreNotFound(err)
		}
		orig = binding.DeepCopy()
	}

	// --- Resolve scope-chain parents ---
	parents, err := r.resolveParents(ctx, &binding)
	if err != nil {
		log.Error(err, "failed to resolve parent bindings")
		r.setCondition(&binding, authzv1alpha1.ConditionParentReadable, metav1.ConditionFalse, "ParentResolveFailed", err.Error())
		r.setPhase(&binding, authzv1alpha1.BindingPhaseDegraded)
		r.Recorder.Eventf(&binding, corev1.EventTypeWarning, ReasonDefaultBindingReadForbidden, "parent resolution failed: %v", err)
		return ctrl.Result{RequeueAfter: requeueAfterBackoff}, r.patchStatus(ctx, &binding, orig)
	}
	r.setCondition(&binding, authzv1alpha1.ConditionParentReadable, metav1.ConditionTrue, "ParentResolved", "all parent bindings readable")

	// --- Check cluster-default binding presence ---
	if err := r.checkDefaultBinding(ctx, &binding); err != nil {
		// Not fatal — emit event and continue.
		r.Recorder.Eventf(&binding, corev1.EventTypeWarning, ReasonDefaultBindingMissing,
			"cluster-default GuardrailBinding not found in %s: %v", defaultBindingNamespace, err)
	}

	// --- Compute strictest-wins effective policy ---
	chain := append(parents, &binding)
	ep, mergeErr := MergeBindings(chain)
	if mergeErr != nil {
		log.Error(mergeErr, "merge conflict")
		r.setCondition(&binding, authzv1alpha1.ConditionReady, metav1.ConditionFalse, "MergeConflict", mergeErr.Error())
		r.setPhase(&binding, authzv1alpha1.BindingPhaseDegraded)
		r.Recorder.Eventf(&binding, corev1.EventTypeWarning, ReasonMergeConflict, "merge conflict: %v", mergeErr)
		return ctrl.Result{RequeueAfter: requeueAfterBackoff}, r.patchStatus(ctx, &binding, orig)
	}
	r.Recorder.Eventf(&binding, corev1.EventTypeNormal, ReasonBindingMerged, "effective policy computed from %d binding(s)", len(chain))

	// --- Project Envoy SecurityPolicy (compiles + validates CEL, then SSA-applies) ---
	if projErr := r.projectEnvoySecurityPolicy(ctx, &binding, ep); projErr != nil {
		log.Error(projErr, "Envoy SecurityPolicy projection failed")
		r.setCondition(&binding, ConditionCELCompilationFailed, metav1.ConditionTrue, "CELCompileError", projErr.Error())
		r.setCondition(&binding, authzv1alpha1.ConditionReady, metav1.ConditionFalse, "CELCompileError", projErr.Error())
		r.setPhase(&binding, authzv1alpha1.BindingPhaseDegraded)
		r.Recorder.Eventf(&binding, corev1.EventTypeWarning, ReasonCELCompileError, "Envoy SecurityPolicy projection failed: %v", projErr)
		return ctrl.Result{RequeueAfter: requeueAfterBackoff}, r.patchStatus(ctx, &binding, orig)
	}
	// Clear any prior CELCompilationFailed condition on success.
	r.setCondition(&binding, ConditionCELCompilationFailed, metav1.ConditionFalse, "CELCompileOK", "CEL expression compiled and SecurityPolicy projected")

	// --- Project Kyverno ClusterPolicies ---
	for _, kp := range binding.Spec.Kyverno {
		if err := r.Kyverno.Apply(ctx, binding.Namespace, binding.Name, kp.PolicyRef); err != nil {
			log.Error(err, "failed to project Kyverno ClusterPolicy", "policyRef", kp.PolicyRef)
			r.Recorder.Eventf(&binding, corev1.EventTypeWarning, ReasonKyvernoProjectFailed, "kyverno project failed for %s: %v", kp.PolicyRef, err)
			r.setPhase(&binding, authzv1alpha1.BindingPhaseDegraded)
			return ctrl.Result{RequeueAfter: requeueAfterBackoff}, r.patchStatus(ctx, &binding, orig)
		}
	}

	// --- Sync ReBAC tuples ---
	tuples := rebacTuplesFor(&binding)
	if err := r.Rebac.Sync(ctx, tuples); err != nil {
		log.Error(err, "failed to sync ReBAC tuples")
		r.Recorder.Eventf(&binding, corev1.EventTypeWarning, ReasonTupleWriteFailed, "ReBAC sync failed: %v", err)
		r.setPhase(&binding, authzv1alpha1.BindingPhaseDegraded)
		return ctrl.Result{RequeueAfter: requeueAfterBackoff}, r.patchStatus(ctx, &binding, orig)
	}
	binding.Status.RebacTupleCount = int32(len(tuples)) //nolint:gosec

	// --- Update status ---
	now := metav1.Now()
	ep.ObservedGeneration = binding.Generation
	binding.Status.EffectivePolicy = ep
	binding.Status.LastMergeTime = &now
	binding.Status.ObservedGeneration = binding.Generation
	r.setPhase(&binding, authzv1alpha1.BindingPhaseReady)
	r.setCondition(&binding, authzv1alpha1.ConditionReady, metav1.ConditionTrue, "MergeComplete", "effective policy computed and written")
	r.Recorder.Eventf(&binding, corev1.EventTypeNormal, ReasonEffectivePolicyComputed,
		"effectivePolicy written with observedGeneration=%d", binding.Generation)

	return ctrl.Result{}, r.patchStatus(ctx, &binding, orig)
}

// cleanup removes external resources and then removes the finalizer.
func (r *GuardrailBindingReconciler) cleanup(ctx context.Context, binding *authzv1alpha1.GuardrailBinding, orig *authzv1alpha1.GuardrailBinding) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	// Delete Envoy SecurityPolicy projection owned by this binding.
	if r.Envoy != nil {
		if err := r.Envoy.Delete(ctx, binding.Namespace, binding.Name); err != nil {
			log.Error(err, "failed to delete Envoy SecurityPolicy projection")
			return ctrl.Result{RequeueAfter: requeueAfterBackoff}, nil
		}
	}

	// Delete Kyverno ClusterPolicy projections owned by this binding.
	for _, kp := range binding.Spec.Kyverno {
		if err := r.Kyverno.Delete(ctx, binding.Namespace, binding.Name, kp.PolicyRef); err != nil {
			log.Error(err, "failed to delete Kyverno ClusterPolicy projection", "policyRef", kp.PolicyRef)
			return ctrl.Result{RequeueAfter: requeueAfterBackoff}, nil
		}
	}

	// Delete ReBAC tuples for this binding.
	tuples := rebacTuplesFor(binding)
	if err := r.Rebac.Delete(ctx, tuples); err != nil {
		log.Error(err, "failed to delete ReBAC tuples")
		return ctrl.Result{RequeueAfter: requeueAfterBackoff}, nil
	}

	// All external resources cleaned; remove finalizer.
	controllerutil.RemoveFinalizer(binding, bindingFinalizer)
	if err := r.Patch(ctx, binding, client.MergeFrom(orig)); err != nil && !errors.IsNotFound(err) {
		return ctrl.Result{}, fmt.Errorf("removing finalizer: %w", err)
	}
	return ctrl.Result{}, nil
}

// resolveParents fetches all parent GuardrailBindings listed in spec.inherit,
// returning them ordered from broadest to narrowest (i.e. in list order).
func (r *GuardrailBindingReconciler) resolveParents(ctx context.Context, binding *authzv1alpha1.GuardrailBinding) ([]*authzv1alpha1.GuardrailBinding, error) {
	parents := make([]*authzv1alpha1.GuardrailBinding, 0, len(binding.Spec.Inherit))
	for _, ref := range binding.Spec.Inherit {
		var parent authzv1alpha1.GuardrailBinding
		if err := r.Get(ctx, types.NamespacedName{Name: ref.Name, Namespace: ref.Namespace}, &parent); err != nil {
			return nil, fmt.Errorf("fetching parent %s/%s: %w", ref.Namespace, ref.Name, err)
		}
		parents = append(parents, &parent)
	}
	return parents, nil
}

// checkDefaultBinding emits a warning event if the cluster-default binding is absent.
// It does not fail the reconcile.
func (r *GuardrailBindingReconciler) checkDefaultBinding(ctx context.Context, binding *authzv1alpha1.GuardrailBinding) error {
	// Only check for Tenant-scoped and Workspace-scoped bindings; Cluster bindings
	// are (or could be) the default themselves.
	if binding.Spec.Scope.Type == authzv1alpha1.BindingScopeCluster {
		return nil
	}
	var def authzv1alpha1.GuardrailBinding
	err := r.Get(ctx, types.NamespacedName{Name: defaultBindingName, Namespace: defaultBindingNamespace}, &def)
	if errors.IsNotFound(err) {
		return fmt.Errorf("default binding %s/%s not found", defaultBindingNamespace, defaultBindingName)
	}
	return err
}

// projectEnvoySecurityPolicy compiles the effective tool policy into a CEL expression
// for structural validation (per design 06 §CEL compile-error fallback), then
// SSA-applies a SecurityPolicy encoding the allow/deny authorisation rules.
//
// The method is a no-op when:
//   - r.Envoy is nil (gateway integration not configured), or
//   - the effective policy has no tool allow or deny rules.
//
// A non-nil error causes the binding to enter Degraded with condition
// CELCompilationFailed=True. Rule 02 (security): the error message MUST NOT
// contain request-body content or decoded CEL runtime values — only structural
// compile diagnostics are safe to surface.
func (r *GuardrailBindingReconciler) projectEnvoySecurityPolicy(
	ctx context.Context,
	binding *authzv1alpha1.GuardrailBinding,
	ep *authzv1alpha1.EffectivePolicy,
) error {
	if r.Envoy == nil {
		return nil
	}
	return r.Envoy.Apply(ctx, binding, ep)
}

// rebacTuplesFor constructs the OpenFGA tuples for a GuardrailBinding.
// Tuple relations per spec (rule 04.14):
//   - guardrail.inherits  (parent inheritance chain)
//   - guardrail.binds_to_workspace
func rebacTuplesFor(b *authzv1alpha1.GuardrailBinding) []GuardrailRebacTuple {
	// +keese:rebac-tuple=guardrail.inherits
	// +keese:rebac-tuple=guardrail.binds_to_workspace
	var tuples []GuardrailRebacTuple

	obj := fmt.Sprintf("guardrail:%s/%s", b.Namespace, b.Name)

	// Inherit chain tuples.
	for _, ref := range b.Spec.Inherit {
		parentObj := fmt.Sprintf("guardrail:%s/%s", ref.Namespace, ref.Name)
		tuples = append(tuples, GuardrailRebacTuple{
			Object:   obj,
			Relation: "guardrail.inherits",
			User:     parentObj,
		})
	}

	// Workspace-bind tuple (when scope is Workspace).
	if b.Spec.Scope.Type == authzv1alpha1.BindingScopeWorkspace && b.Spec.Scope.WorkspaceRef != nil {
		wsObj := fmt.Sprintf("workspace:%s/%s", b.Spec.Scope.WorkspaceRef.Namespace, b.Spec.Scope.WorkspaceRef.Name)
		tuples = append(tuples, GuardrailRebacTuple{
			Object:   obj,
			Relation: "guardrail.binds_to_workspace",
			User:     wsObj,
		})
	}

	return tuples
}

// patchStatus patches the status subresource using MergeFrom the original snapshot.
func (r *GuardrailBindingReconciler) patchStatus(ctx context.Context, binding *authzv1alpha1.GuardrailBinding, orig *authzv1alpha1.GuardrailBinding) error {
	if err := r.Status().Patch(ctx, binding, client.MergeFrom(orig)); err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("patching status: %w", err)
	}
	return nil
}

// setPhase sets the status phase.
func (r *GuardrailBindingReconciler) setPhase(binding *authzv1alpha1.GuardrailBinding, phase authzv1alpha1.BindingPhase) {
	binding.Status.Phase = phase
}

// setCondition upserts a standard metav1.Condition on the binding status.
func (r *GuardrailBindingReconciler) setCondition(
	binding *authzv1alpha1.GuardrailBinding,
	condType string,
	status metav1.ConditionStatus,
	reason, message string,
) {
	cond := metav1.Condition{
		Type:               condType,
		Status:             status,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: binding.Generation,
		LastTransitionTime: metav1.Now(),
	}
	for i, c := range binding.Status.Conditions {
		if c.Type == condType {
			if c.Status == status {
				// Preserve LastTransitionTime if status hasn't changed.
				cond.LastTransitionTime = c.LastTransitionTime
			}
			binding.Status.Conditions[i] = cond
			return
		}
	}
	binding.Status.Conditions = append(binding.Status.Conditions, cond)
}

// SetupWithManager sets up the controller with the Manager.
func (r *GuardrailBindingReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&authzv1alpha1.GuardrailBinding{}).
		WithEventFilter(predicate.And(
			predicate.NewPredicateFuncs(func(obj client.Object) bool {
				return obj.GetLabels()[managedLabel] == managedLabelValue
			}),
			predicate.GenerationChangedPredicate{},
		)).
		Named("guardrail-guardrailbinding").
		Complete(r)
}
