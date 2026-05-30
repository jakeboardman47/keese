// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package keese

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
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
	recipeFinalizer  = "finalizers.recipe.keese.ai/cache-cleanup"
	recipeFieldOwner = "keese-recipe-controller"

	// requeueOnRecipeError is the requeue interval on transient errors.
	requeueOnRecipeError = 5 * time.Second
)

// RecipeReconciler reconciles a Recipe object.
//
// +kubebuilder:rbac:groups=keese.ai,resources=recipes,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=keese.ai,resources=recipes/status,verbs=update;patch
// +kubebuilder:rbac:groups=keese.ai,resources=recipes/finalizers,verbs=update
// +kubebuilder:rbac:groups=keese.ai,resources=recipesources,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=keese.ai,resources=recipesources/status,verbs=update;patch
// +kubebuilder:rbac:groups=authz.keese.ai,resources=guardrailbindings,verbs=get;list;watch
// +kubebuilder:rbac:groups=keese.ai,resources=workspaces,verbs=get;list;watch
// +kubebuilder:rbac:groups=core,resources=events,verbs=create;patch
type RecipeReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
	Rebac    RecipeRebacWriter
	ExtAuthz ExtAuthzChecker
}

// Reconcile implements the Recipe reconciliation loop.
// Idiom: fetch → deepcopy for status patch → deletion → source resolution → status.
func (r *RecipeReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	var recipe keesev1alpha1.Recipe
	if err := r.Get(ctx, req.NamespacedName, &recipe); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	orig := recipe.DeepCopy()

	// Handle deletion before anything else.
	if !recipe.DeletionTimestamp.IsZero() {
		return r.cleanup(ctx, &recipe, orig)
	}

	// Ensure finalizer is present.
	if !controllerutil.ContainsFinalizer(&recipe, recipeFinalizer) {
		controllerutil.AddFinalizer(&recipe, recipeFinalizer)
		if err := r.Patch(ctx, &recipe, client.MergeFrom(orig)); err != nil {
			return ctrl.Result{}, fmt.Errorf("adding finalizer: %w", err)
		}
		if err := r.Get(ctx, req.NamespacedName, &recipe); err != nil {
			return ctrl.Result{}, client.IgnoreNotFound(err)
		}
		orig = recipe.DeepCopy()
	}

	if recipe.Status.Phase == "" {
		recipe.Status.Phase = keesev1alpha1.RecipePhasePending
	}

	// Transition to Pulling.
	recipe.Status.Phase = keesev1alpha1.RecipePhasePulling
	setCondition(&recipe.Status.Conditions, metav1.Condition{
		Type:               keesev1alpha1.RecipeConditionProgressing,
		Status:             metav1.ConditionTrue,
		Reason:             "Pulling",
		Message:            "resolving RecipeSource",
		ObservedGeneration: recipe.Generation,
	})

	// Resolve RecipeSource.
	sourceNS := recipe.Spec.SourceRef.Namespace
	if sourceNS == "" {
		sourceNS = recipe.Namespace
	}
	var rs keesev1alpha1.RecipeSource
	rsKey := types.NamespacedName{Name: recipe.Spec.SourceRef.Name, Namespace: sourceNS}
	if err := r.Get(ctx, rsKey, &rs); err != nil {
		if errors.IsNotFound(err) {
			r.Recorder.Eventf(&recipe, corev1.EventTypeWarning, ReasonRecipeSourceNotFound,
				"RecipeSource %s/%s not found", sourceNS, recipe.Spec.SourceRef.Name)
		}
		log.Error(err, "failed to get RecipeSource", "key", rsKey)
		recipe.Status.Phase = keesev1alpha1.RecipePhaseFailed
		setCondition(&recipe.Status.Conditions, metav1.Condition{
			Type:               keesev1alpha1.RecipeConditionReady,
			Status:             metav1.ConditionFalse,
			Reason:             "SourceNotFound",
			Message:            fmt.Sprintf("RecipeSource %s not found: %v", rsKey.String(), err),
			ObservedGeneration: recipe.Generation,
		})
		recipe.Status.ObservedGeneration = recipe.Generation
		return ctrl.Result{RequeueAfter: requeueOnRecipeError}, r.patchStatus(ctx, &recipe, orig)
	}

	// Wait until the RecipeSource is Synced.
	if rs.Status.Phase != keesev1alpha1.RecipeSourcePhaseSynced || !rs.Status.Cached {
		log.Info("RecipeSource not yet synced; requeuing", "source", rsKey, "phase", rs.Status.Phase)
		recipe.Status.Phase = keesev1alpha1.RecipePhasePulling
		setCondition(&recipe.Status.Conditions, metav1.Condition{
			Type:               keesev1alpha1.RecipeConditionReady,
			Status:             metav1.ConditionFalse,
			Reason:             "SourceNotReady",
			Message:            fmt.Sprintf("RecipeSource %s is not yet Synced (phase=%s)", rsKey.String(), rs.Status.Phase),
			ObservedGeneration: recipe.Generation,
		})
		recipe.Status.ObservedGeneration = recipe.Generation
		return ctrl.Result{RequeueAfter: requeueOnRecipeError}, r.patchStatus(ctx, &recipe, orig)
	}

	// RecipeSource is synced; adopt its resolved digest.
	recipe.Status.ResolvedDigest = rs.Status.ResolvedDigest
	recipe.Status.Phase = keesev1alpha1.RecipePhaseVerified

	r.Recorder.Eventf(&recipe, corev1.EventTypeNormal, ReasonRecipePulled,
		"RecipeSource %s resolved: digest=%s", rsKey.String(), rs.Status.ResolvedDigest)

	// Sync ReBAC tuples.
	tuples := recipeRebacTuples(&recipe)
	if err := r.Rebac.Sync(ctx, tuples); err != nil {
		log.Error(err, "failed to sync ReBAC tuples")
		r.Recorder.Eventf(&recipe, corev1.EventTypeWarning, ReasonRebacTupleDeleteFailed,
			"ReBAC sync failed: %v", err)
		recipe.Status.Phase = keesev1alpha1.RecipePhaseFailed
		setCondition(&recipe.Status.Conditions, metav1.Condition{
			Type:               keesev1alpha1.RecipeConditionReady,
			Status:             metav1.ConditionFalse,
			Reason:             "RebacSyncFailed",
			Message:            fmt.Sprintf("ReBAC sync failed: %v", err),
			ObservedGeneration: recipe.Generation,
		})
		recipe.Status.ObservedGeneration = recipe.Generation
		return ctrl.Result{RequeueAfter: requeueOnRecipeError}, r.patchStatus(ctx, &recipe, orig)
	}
	r.Recorder.Eventf(&recipe, corev1.EventTypeNormal, ReasonRebacTupleWritten,
		"%d ReBAC tuples synced", len(tuples))
	recipe.Status.RebacTupleCount = int32(len(tuples)) //nolint:gosec

	// Advance to Ready.
	recipe.Status.Phase = keesev1alpha1.RecipePhaseReady
	recipe.Status.ObservedGeneration = recipe.Generation

	r.Recorder.Eventf(&recipe, corev1.EventTypeNormal, ReasonRecipeReady,
		"Recipe %s is ready (digest=%s)", recipe.Name, recipe.Status.ResolvedDigest)

	setCondition(&recipe.Status.Conditions, metav1.Condition{
		Type:               keesev1alpha1.RecipeConditionProgressing,
		Status:             metav1.ConditionFalse,
		Reason:             "Ready",
		Message:            "Recipe is ready",
		ObservedGeneration: recipe.Generation,
	})
	setCondition(&recipe.Status.Conditions, metav1.Condition{
		Type:               keesev1alpha1.RecipeConditionVerified,
		Status:             metav1.ConditionTrue,
		Reason:             "Verified",
		Message:            fmt.Sprintf("artifact verified at digest %s", recipe.Status.ResolvedDigest),
		ObservedGeneration: recipe.Generation,
	})
	setCondition(&recipe.Status.Conditions, metav1.Condition{
		Type:               keesev1alpha1.RecipeConditionReady,
		Status:             metav1.ConditionTrue,
		Reason:             "Ready",
		Message:            "Recipe is ready",
		ObservedGeneration: recipe.Generation,
	})

	return ctrl.Result{}, r.patchStatus(ctx, &recipe, orig)
}

// cleanup runs when DeletionTimestamp is set.
func (r *RecipeReconciler) cleanup(ctx context.Context, recipe *keesev1alpha1.Recipe, orig *keesev1alpha1.Recipe) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	if !controllerutil.ContainsFinalizer(recipe, recipeFinalizer) {
		return ctrl.Result{}, nil
	}

	recipe.Status.Phase = keesev1alpha1.RecipePhaseTerminating
	r.Recorder.Eventf(recipe, corev1.EventTypeNormal, ReasonRecipeCacheCleanup,
		"Recipe %s cache cleanup on deletion", recipe.Name)

	// Delete ReBAC tuples.
	tuples := recipeRebacTuples(recipe)
	if err := r.Rebac.Delete(ctx, tuples); err != nil {
		log.Error(err, "failed to delete ReBAC tuples; will retry")
		r.Recorder.Eventf(recipe, corev1.EventTypeWarning, ReasonRebacTupleDeleteFailed,
			"ReBAC tuple deletion failed: %v", err)
		_ = r.patchStatus(ctx, recipe, orig)
		return ctrl.Result{RequeueAfter: requeueOnRecipeError}, nil
	}

	// Recipes have no per-recipe cached state on workspace PVCs — the recipe
	// is materialised as a ConfigMap and mounted into session pods via the
	// recipe volume. The ConfigMap is owned by the Recipe via an owner-ref,
	// so K8s garbage collection removes it when the Recipe is deleted.
	// Any per-session derived artifact (e.g. agent transcripts) lives in
	// goose's sessions dir on the workspace PVC and is owned by the
	// WorkspaceSession lifecycle, not the Recipe.
	_ = log

	controllerutil.RemoveFinalizer(recipe, recipeFinalizer)
	return ctrl.Result{}, r.Patch(ctx, recipe, client.MergeFrom(orig))
}

// patchStatus patches only the status subresource.
func (r *RecipeReconciler) patchStatus(ctx context.Context, recipe, orig *keesev1alpha1.Recipe) error {
	return r.Status().Patch(ctx, recipe, client.MergeFrom(orig))
}

// recipeRebacTuples computes the full desired set of ReBAC tuples for a Recipe.
func recipeRebacTuples(recipe *keesev1alpha1.Recipe) []RecipeRebacTuple {
	var tuples []RecipeRebacTuple
	recipeObj := "recipe:" + recipe.Name

	// readable_by tuples for each tool (one per tool → workspace association placeholder).
	// The workspace association is written at Workspace admit time; here we write
	// the recipe object's own readability tuple for the source namespace.
	tuples = append(tuples, RecipeRebacTuple{
		Object:   recipeObj,
		Relation: "readable_by",
		User:     "namespace:" + recipe.Namespace,
	})

	// uses_extension tuples per extension.
	for _, ext := range recipe.Spec.Extensions {
		tuples = append(tuples, RecipeRebacTuple{
			Object:   recipeObj,
			Relation: "uses_extension",
			User:     "extension:" + ext.Namespace + "/" + ext.Name,
		})
	}

	return tuples
}

// SetupWithManager sets up the controller with the Manager.
func (r *RecipeReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if r.Rebac == nil {
		r.Rebac = RecipeNoopRebacWriter{}
	}
	if r.Recorder == nil {
		r.Recorder = mgr.GetEventRecorderFor("recipe-controller")
	}
	if r.ExtAuthz == nil {
		r.ExtAuthz = &FakeExtAuthzChecker{AllowedExtensions: map[string]bool{}}
	}
	return ctrl.NewControllerManagedBy(mgr).
		For(&keesev1alpha1.Recipe{}).
		WithEventFilter(predicate.And(
			predicate.NewPredicateFuncs(func(obj client.Object) bool {
				return obj.GetLabels()[managedLabel] == managedLabelValue
			}),
			predicate.GenerationChangedPredicate{},
		)).
		Named("recipe-recipe").
		Complete(r)
}
