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
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	keesev1alpha1 "github.com/keese-ai/keese/api/keese/v1alpha1"
)

const (
	agentRuntimeFinalizer  = "finalizers.agentruntime.keese.ai/drain"
	agentRuntimeFieldOwner = "keese-agentruntime-controller"
)

// AgentRuntimeReconciler reconciles a AgentRuntime object.
type AgentRuntimeReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
}

// +kubebuilder:rbac:groups=keese.ai,resources=agentruntimes,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=keese.ai,resources=agentruntimes/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=keese.ai,resources=agentruntimes/finalizers,verbs=update
// +kubebuilder:rbac:groups=keese.ai,resources=runtimeextensions,verbs=get;list;watch

// Reconcile moves the AgentRuntime toward its desired state.
// Idiom: fetch → DeepCopy for patch → compute desired → SSA → status patch.
func (r *AgentRuntimeReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	// 1. Fetch — cluster-scoped so no namespace needed.
	ar := &keesev1alpha1.AgentRuntime{}
	if err := r.Get(ctx, req.NamespacedName, ar); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("get agentruntime: %w", err)
	}

	// 2. Snapshot original for status patch.
	orig := ar.DeepCopy()

	// 3. Deletion path.
	if !ar.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, ar, orig)
	}

	// 4. Ensure finalizer.
	if !controllerutil.ContainsFinalizer(ar, agentRuntimeFinalizer) {
		controllerutil.AddFinalizer(ar, agentRuntimeFinalizer)
		if err := r.Update(ctx, ar); err != nil {
			return ctrl.Result{}, fmt.Errorf("add finalizer: %w", err)
		}
		return ctrl.Result{}, nil
	}

	// 5. Detect provider name from discriminated one-of.
	provider, err := detectProvider(ar)
	if err != nil {
		log.Info("unknown provider", "error", err)
		r.Recorder.Eventf(ar, corev1.EventTypeWarning, ReasonProviderUnknown, "%v", err)
		ar.Status.Phase = keesev1alpha1.AgentRuntimePhaseDegraded
		ar.Status.Provider = ""
		meta.SetStatusCondition(&ar.Status.Conditions, metav1.Condition{
			Type:               "Ready",
			Status:             metav1.ConditionFalse,
			Reason:             ReasonProviderUnknown,
			Message:            err.Error(),
			ObservedGeneration: ar.Generation,
		})
		return ctrl.Result{}, r.patchStatus(ctx, ar, orig)
	}

	// 6. Check provider is registered.
	if !IsRegistered(provider) {
		msg := fmt.Sprintf("implementation %q is not registered", provider)
		log.Info(msg)
		r.Recorder.Eventf(ar, corev1.EventTypeWarning, ReasonProviderUnknown, "%s", msg)
		ar.Status.Phase = keesev1alpha1.AgentRuntimePhaseDegraded
		ar.Status.Provider = provider
		meta.SetStatusCondition(&ar.Status.Conditions, metav1.Condition{
			Type:               "Ready",
			Status:             metav1.ConditionFalse,
			Reason:             ReasonProviderUnknown,
			Message:            msg,
			ObservedGeneration: ar.Generation,
		})
		return ctrl.Result{}, r.patchStatus(ctx, ar, orig)
	}

	// 7. Converge to Ready.
	ar.Status.Phase = keesev1alpha1.AgentRuntimePhaseReady
	ar.Status.Provider = provider
	ar.Status.ObservedGeneration = ar.Generation
	meta.SetStatusCondition(&ar.Status.Conditions, metav1.Condition{
		Type:               "Ready",
		Status:             metav1.ConditionTrue,
		Reason:             ReasonRuntimeStarted,
		Message:            fmt.Sprintf("provider %q is registered and ready", provider),
		ObservedGeneration: ar.Generation,
	})
	r.Recorder.Eventf(ar, corev1.EventTypeNormal, ReasonRuntimeStarted, "provider %q ready", provider)

	return ctrl.Result{}, r.patchStatus(ctx, ar, orig)
}

// reconcileDelete blocks deletion until no RuntimeExtensions reference this AgentRuntime.
func (r *AgentRuntimeReconciler) reconcileDelete(
	ctx context.Context,
	ar *keesev1alpha1.AgentRuntime,
	orig *keesev1alpha1.AgentRuntime,
) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	if !controllerutil.ContainsFinalizer(ar, agentRuntimeFinalizer) {
		return ctrl.Result{}, nil
	}

	// Check for referencing RuntimeExtensions across all namespaces.
	exList := &keesev1alpha1.RuntimeExtensionList{}
	if err := r.List(ctx, exList); err != nil {
		return ctrl.Result{}, fmt.Errorf("list runtimeextensions: %w", err)
	}
	for i := range exList.Items {
		if exList.Items[i].Spec.RuntimeRef.Name == ar.Name {
			log.Info("drain blocked: RuntimeExtension still references AgentRuntime",
				"extension", types.NamespacedName{Namespace: exList.Items[i].Namespace, Name: exList.Items[i].Name})
			ar.Status.Phase = keesev1alpha1.AgentRuntimePhaseDegraded
			meta.SetStatusCondition(&ar.Status.Conditions, metav1.Condition{
				Type:               "Ready",
				Status:             metav1.ConditionFalse,
				Reason:             ReasonRuntimeStopped,
				Message:            "drain blocked: RuntimeExtensions still reference this AgentRuntime",
				ObservedGeneration: ar.Generation,
			})
			_ = r.patchStatus(ctx, ar, orig)
			// Requeue; do not return an error (avoids exponential backoff for a normal wait).
			return ctrl.Result{RequeueAfter: 0}, nil
		}
	}

	// Safe to drain.
	r.Recorder.Eventf(ar, corev1.EventTypeNormal, ReasonRuntimeStopped, "provider %q drained", ar.Status.Provider)
	controllerutil.RemoveFinalizer(ar, agentRuntimeFinalizer)
	if err := r.Update(ctx, ar); err != nil {
		return ctrl.Result{}, fmt.Errorf("remove finalizer: %w", err)
	}
	return ctrl.Result{}, nil
}

// patchStatus issues a status-subresource patch using the original as the base.
func (r *AgentRuntimeReconciler) patchStatus(
	ctx context.Context,
	ar *keesev1alpha1.AgentRuntime,
	orig *keesev1alpha1.AgentRuntime,
) error {
	patch := client.MergeFrom(orig)
	if err := r.Status().Patch(ctx, ar, patch); err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("patch status: %w", err)
	}
	return nil
}

// detectProvider returns the canonical provider name from spec.implementation.
func detectProvider(ar *keesev1alpha1.AgentRuntime) (string, error) {
	impl := ar.Spec.Implementation
	switch {
	case impl.Goose != nil:
		return "goose", nil
	case impl.ClaudeCode != nil:
		return "claudeCode", nil
	case impl.Aider != nil:
		return "aider", nil
	default:
		return "", fmt.Errorf("spec.implementation: no provider field is set")
	}
}

// SetupWithManager registers the controller with the manager.
func (r *AgentRuntimeReconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.Recorder = mgr.GetEventRecorderFor(agentRuntimeFieldOwner)
	return ctrl.NewControllerManagedBy(mgr).
		For(&keesev1alpha1.AgentRuntime{}).
		WithEventFilter(predicate.GenerationChangedPredicate{}).
		Named("runtime-agentruntime").
		Complete(r)
}
