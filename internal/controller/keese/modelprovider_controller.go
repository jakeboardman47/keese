// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package keese

import (
	"context"
	"fmt"
	"time"

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
	// modelProviderFinalizer purges the credential-binding ReBAC tuple before the
	// object is removed (rule 04.10 — finalizer for externally-allocated state).
	modelProviderFinalizer = "finalizers.modelprovider.keese.ai/cleanup"
	// modelProviderFieldOwner is the Server-Side-Apply field owner for any child
	// object this controller writes (rule 04.7).
	modelProviderFieldOwner = "keese-modelprovider-controller"

	// defaultDiscoveryInterval is used when spec.discoveryInterval is unset or
	// unparseable. discoveryBackoffCap bounds the 2x backoff after a 429.
	defaultDiscoveryInterval = time.Hour
	discoveryBackoffCap      = 30 * time.Minute
)

// ModelProviderReconciler reconciles a ModelProvider object.
//
// +kubebuilder:rbac:groups=keese.ai,resources=modelproviders,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=keese.ai,resources=modelproviders/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=keese.ai,resources=modelproviders/finalizers,verbs=update
// +kubebuilder:rbac:groups=core,resources=events,verbs=create;patch
type ModelProviderReconciler struct {
	client.Client
	Scheme     *runtime.Scheme
	Recorder   record.EventRecorder
	Rebac      ModelProviderRebacWriter
	Discoverer ModelDiscoverer
}

// Reconcile implements the ModelProvider reconciliation loop.
//
// FSM: Pending → Validating → Ready ↔ Degraded; Terminating on deletion.
//
// Idempotency guarantee: converges in ≤3 reconciles with no spec change
// (rule 04.16). Status is never read into the next reconcile decision (rule 04.4)
// — discovery scheduling is driven solely by spec + wall clock via requeue.
func (r *ModelProviderReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	mp := &keesev1alpha1.ModelProvider{}
	if err := r.Get(ctx, req.NamespacedName, mp); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("get ModelProvider: %w", err)
	}

	orig := mp.DeepCopy()

	// Deletion path — purge ReBAC tuples before finalizer removal.
	if !mp.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, mp, orig)
	}

	// Ensure finalizer before any external (ReBAC) work.
	if !controllerutil.ContainsFinalizer(mp, modelProviderFinalizer) {
		controllerutil.AddFinalizer(mp, modelProviderFinalizer)
		if err := r.Update(ctx, mp); err != nil {
			return ctrl.Result{}, fmt.Errorf("add finalizer: %w", err)
		}
		return ctrl.Result{Requeue: true}, nil
	}

	mp.Status.Phase = keesev1alpha1.ModelProviderPhaseValidating
	r.Recorder.Eventf(mp, corev1.EventTypeNormal, ReasonModelProviderValidated, "spec validated")

	// Record the credential-binding ReBAC tuple when a credential secret is set.
	// This is the authz-affecting write the +keese:rebac-tuple marker promises.
	if ref := mp.Spec.CredentialSecretRef; ref != nil && ref.Name != "" {
		tuple := ModelProviderCredentialTuple(string(mp.UID), ref.Name)
		if err := r.Rebac.Write(ctx, []ModelProviderTuple{tuple}); err != nil {
			log.Error(err, "rebac write failed")
			r.Recorder.Eventf(mp, corev1.EventTypeWarning, ReasonModelProviderRebacSyncFailed, "rebac sync failed: %v", err)
			return r.setStatus(ctx, mp, orig, keesev1alpha1.ModelProviderPhaseDegraded,
				readyCond(metav1.ConditionFalse, ReasonModelProviderRebacSyncFailed, err.Error()))
		}
		r.Recorder.Eventf(mp, corev1.EventTypeNormal, ReasonModelProviderRebacSyncSucceeded, "credential binding tuple synced")
	}

	// Discovery (optional, gated). On 429 we back off and requeue without
	// flipping to Degraded; other errors set Synced=False but keep Ready true
	// when the config itself is valid.
	var requeueAfter time.Duration
	if mp.Spec.DiscoveryEnabled {
		var done ctrl.Result
		var handled bool
		requeueAfter, done, handled = r.runDiscovery(ctx, mp, orig)
		if handled {
			return done, nil
		}
	}

	// Happy path — config valid (and discovery, if enabled, succeeded).
	r.Recorder.Eventf(mp, corev1.EventTypeNormal, ReasonModelProviderReady, "provider ready")
	res, err := r.setStatusResult(ctx, mp, orig, keesev1alpha1.ModelProviderPhaseReady,
		readyCond(metav1.ConditionTrue, ReasonModelProviderReady, "model provider configuration valid"), requeueAfter)
	return res, err
}

// runDiscovery polls the provider model-list endpoint and writes status.
// Returns (requeueAfter, result, handled). When handled is true the caller must
// return result directly (the status write already happened for the terminal
// discovery outcome); otherwise it proceeds to the Ready path with requeueAfter.
func (r *ModelProviderReconciler) runDiscovery(
	ctx context.Context,
	mp, orig *keesev1alpha1.ModelProvider,
) (time.Duration, ctrl.Result, bool) {
	log := logf.FromContext(ctx)
	interval := parseDiscoveryInterval(mp.Spec.DiscoveryInterval)

	r.Recorder.Eventf(mp, corev1.EventTypeNormal, ReasonModelProviderDiscoveryStarted, "discovery poll started")
	models, err := r.Discoverer.Discover(ctx, mp.Spec.Provider, mp.Spec.Endpoint)
	switch {
	case isRateLimited(err):
		// Back off 2x from the configured interval, capped.
		backoff := interval * 2
		if backoff > discoveryBackoffCap {
			backoff = discoveryBackoffCap
		}
		r.Recorder.Eventf(mp, corev1.EventTypeWarning, ReasonModelProviderDiscoveryFailed, "rate-limited; backing off %s", backoff)
		setSynced(mp, metav1.ConditionFalse, ReasonModelProviderDiscoveryFailed, "rate-limited (429); backing off")
		res, _ := r.setStatusResult(ctx, mp, orig, keesev1alpha1.ModelProviderPhaseReady,
			readyCond(metav1.ConditionTrue, ReasonModelProviderReady, "config valid; discovery backing off"), backoff)
		return 0, res, true
	case err != nil:
		log.Error(err, "model discovery failed")
		r.Recorder.Eventf(mp, corev1.EventTypeWarning, ReasonModelProviderDiscoveryFailed, "discovery failed: %v", err)
		setSynced(mp, metav1.ConditionFalse, ReasonModelProviderDiscoveryFailed, err.Error())
		// Config is still valid; stay Ready but requeue to retry discovery.
		return interval, ctrl.Result{}, false
	default:
		now := metav1.Now()
		mp.Status.AvailableModels = models
		mp.Status.LastDiscoveryTime = &now
		setSynced(mp, metav1.ConditionTrue, ReasonModelProviderDiscoverySucceeded,
			fmt.Sprintf("discovered %d models", len(models)))
		r.Recorder.Eventf(mp, corev1.EventTypeNormal, ReasonModelProviderDiscoverySucceeded, "discovered %d models", len(models))
		return interval, ctrl.Result{}, false
	}
}

// reconcileDelete purges the credential-binding tuple, then removes the finalizer.
func (r *ModelProviderReconciler) reconcileDelete(ctx context.Context, mp, orig *keesev1alpha1.ModelProvider) (ctrl.Result, error) {
	log := logf.FromContext(ctx)
	if !controllerutil.ContainsFinalizer(mp, modelProviderFinalizer) {
		return ctrl.Result{}, nil
	}

	mp.Status.Phase = keesev1alpha1.ModelProviderPhaseTerminating

	if ref := mp.Spec.CredentialSecretRef; ref != nil && ref.Name != "" {
		tuple := ModelProviderCredentialTuple(string(mp.UID), ref.Name)
		if err := r.Rebac.Delete(ctx, []ModelProviderTuple{tuple}); err != nil {
			log.Error(err, "rebac purge failed")
			r.Recorder.Eventf(mp, corev1.EventTypeWarning, ReasonModelProviderRebacPurgeFailed, "rebac purge failed: %v", err)
			return r.setStatus(ctx, mp, orig, keesev1alpha1.ModelProviderPhaseTerminating,
				readyCond(metav1.ConditionFalse, ReasonModelProviderRebacPurgeFailed, err.Error()))
		}
	}

	controllerutil.RemoveFinalizer(mp, modelProviderFinalizer)
	if err := r.Update(ctx, mp); err != nil {
		return ctrl.Result{}, fmt.Errorf("remove finalizer: %w", err)
	}
	return ctrl.Result{}, nil
}

// setStatus patches status with the original as the merge base and no requeue.
func (r *ModelProviderReconciler) setStatus(
	ctx context.Context,
	mp, orig *keesev1alpha1.ModelProvider,
	phase keesev1alpha1.ModelProviderPhase,
	cond metav1.Condition,
) (ctrl.Result, error) {
	return r.setStatusResult(ctx, mp, orig, phase, cond, 0)
}

// setStatusResult patches the status subresource (MergeFrom orig) and returns a
// result with the given requeue delay. Never reads status into the next decision
// (rule 04.4).
func (r *ModelProviderReconciler) setStatusResult(
	ctx context.Context,
	mp, orig *keesev1alpha1.ModelProvider,
	phase keesev1alpha1.ModelProviderPhase,
	cond metav1.Condition,
	requeueAfter time.Duration,
) (ctrl.Result, error) {
	mp.Status.Phase = phase
	mp.Status.ObservedGeneration = mp.Generation
	cond.ObservedGeneration = mp.Generation
	meta.SetStatusCondition(&mp.Status.Conditions, cond)

	if err := r.Status().Patch(ctx, mp, client.MergeFrom(orig)); err != nil {
		return ctrl.Result{}, fmt.Errorf("patch status: %w", err)
	}
	return ctrl.Result{RequeueAfter: requeueAfter}, nil
}

// readyCond builds a Ready condition. ObservedGeneration is stamped by setStatus*.
func readyCond(status metav1.ConditionStatus, reason, msg string) metav1.Condition {
	return metav1.Condition{
		Type:    keesev1alpha1.ModelProviderConditionReady,
		Status:  status,
		Reason:  reason,
		Message: msg,
	}
}

// setSynced upserts the Synced condition on the in-memory status (the surrounding
// setStatusResult call persists it).
func setSynced(mp *keesev1alpha1.ModelProvider, status metav1.ConditionStatus, reason, msg string) {
	meta.SetStatusCondition(&mp.Status.Conditions, metav1.Condition{
		Type:               keesev1alpha1.ModelProviderConditionSynced,
		Status:             status,
		Reason:             reason,
		Message:            msg,
		ObservedGeneration: mp.Generation,
	})
}

// parseDiscoveryInterval parses a Go duration string, falling back to the default
// on empty or invalid input.
func parseDiscoveryInterval(s string) time.Duration {
	if s == "" {
		return defaultDiscoveryInterval
	}
	d, err := time.ParseDuration(s)
	if err != nil || d <= 0 {
		return defaultDiscoveryInterval
	}
	return d
}

// SetupWithManager wires the controller into the manager, defaulting dependencies.
func (r *ModelProviderReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if r.Recorder == nil {
		r.Recorder = mgr.GetEventRecorderFor("modelprovider-controller")
	}
	if r.Rebac == nil {
		r.Rebac = ModelProviderNoopRebacWriter{}
	}
	if r.Discoverer == nil {
		r.Discoverer = NewHTTPModelDiscoverer()
	}
	return ctrl.NewControllerManagedBy(mgr).
		For(&keesev1alpha1.ModelProvider{}).
		WithEventFilter(predicate.GenerationChangedPredicate{}).
		Named("modelprovider").
		Complete(r)
}
