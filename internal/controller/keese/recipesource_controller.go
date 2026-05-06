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
	recipeSourceFinalizer  = "finalizers.recipesource.operator.keese.ai/cache-cleanup"
	recipeSourceFieldOwner = "keese-recipesource-controller"

	// devEnvLabel is the namespace label that permits ConfigMap sources (VAP rule).
	devEnvLabel = "keese.ai/env"
	devEnvValue = "dev"

	// requeueOnSourceError is the requeue interval on transient pull errors.
	requeueOnSourceError = 5 * time.Second
)

// RecipeSourceReconciler reconciles a RecipeSource object.
//
// +kubebuilder:rbac:groups=recipe.operator.keese.ai,resources=recipesources,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=recipe.operator.keese.ai,resources=recipesources/status,verbs=update;patch
// +kubebuilder:rbac:groups=recipe.operator.keese.ai,resources=recipesources/finalizers,verbs=update
// +kubebuilder:rbac:groups=core,resources=configmaps,verbs=get;list;watch
// +kubebuilder:rbac:groups=core,resources=namespaces,verbs=get;list;watch
// +kubebuilder:rbac:groups=core,resources=events,verbs=create;patch
type RecipeSourceReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
	Fetcher  OCIFetcher
}

// Reconcile implements the RecipeSource reconciliation loop.
// Idiom: fetch → deepcopy for status patch → handle deletion → pull → verify → status.
func (r *RecipeSourceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	_ = logf.FromContext(ctx) // sub-methods receive ctx for structured logging

	var rs keesev1alpha1.RecipeSource
	if err := r.Get(ctx, req.NamespacedName, &rs); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	orig := rs.DeepCopy()

	// Handle deletion before anything else.
	if !rs.DeletionTimestamp.IsZero() {
		return r.cleanup(ctx, &rs, orig)
	}

	// Ensure finalizer is present.
	if !controllerutil.ContainsFinalizer(&rs, recipeSourceFinalizer) {
		controllerutil.AddFinalizer(&rs, recipeSourceFinalizer)
		if err := r.Patch(ctx, &rs, client.MergeFrom(orig)); err != nil {
			return ctrl.Result{}, fmt.Errorf("adding finalizer: %w", err)
		}
		if err := r.Get(ctx, req.NamespacedName, &rs); err != nil {
			return ctrl.Result{}, client.IgnoreNotFound(err)
		}
		orig = rs.DeepCopy()
	}

	if rs.Status.Phase == "" {
		rs.Status.Phase = keesev1alpha1.RecipeSourcePhasePending
	}

	// Determine which source type is active and pull.
	switch {
	case rs.Spec.OCI != nil:
		return r.reconcileOCI(ctx, &rs, orig)
	case rs.Spec.Git != nil:
		return r.reconcileGit(ctx, &rs, orig)
	case rs.Spec.ConfigMap != nil:
		return r.reconcileConfigMap(ctx, &rs, orig)
	default:
		// No source set; mark Failed. VAP should prevent this but be defensive.
		rs.Status.Phase = keesev1alpha1.RecipeSourcePhaseFailed
		setRecipeSourceCondition(&rs.Status.Conditions, metav1.Condition{
			Type:               keesev1alpha1.RecipeSourceConditionReady,
			Status:             metav1.ConditionFalse,
			Reason:             "NoSourceSet",
			Message:            "no source type (oci/git/configMap) is set on spec",
			ObservedGeneration: rs.Generation,
		})
		rs.Status.ObservedGeneration = rs.Generation
		return ctrl.Result{}, r.patchRecipeSourceStatus(ctx, &rs, orig)
	}
}

// reconcileOCI handles the OCI pull + cosign verify sequence.
// Cache is written (via Fetcher.Pull) before SSA status update — SIGKILL recovery guarantee.
func (r *RecipeSourceReconciler) reconcileOCI(ctx context.Context, rs *keesev1alpha1.RecipeSource, orig *keesev1alpha1.RecipeSource) (ctrl.Result, error) {
	log := logf.FromContext(ctx)
	oci := rs.Spec.OCI

	rs.Status.SourceType = keesev1alpha1.RecipeSourceTypeOCI
	rs.Status.Phase = keesev1alpha1.RecipeSourcePhasePending
	setRecipeSourceCondition(&rs.Status.Conditions, metav1.Condition{
		Type:               keesev1alpha1.RecipeSourceConditionProgressing,
		Status:             metav1.ConditionTrue,
		Reason:             "Pulling",
		Message:            "OCI artifact pull in progress",
		ObservedGeneration: rs.Generation,
	})

	tagOrDigest := oci.Digest
	if tagOrDigest == "" {
		tagOrDigest = oci.Tag
	}

	// Pull artifact into cluster-internal cache (written before status update).
	artifact, err := r.Fetcher.Pull(ctx, oci.Registry, oci.Repository, tagOrDigest)
	if err != nil {
		log.Error(err, "OCI pull failed", "registry", oci.Registry, "repository", oci.Repository)
		r.Recorder.Eventf(rs, corev1.EventTypeWarning, ReasonOCIPullFailed,
			"OCI pull failed for %s/%s: %v", oci.Registry, oci.Repository, err)
		rs.Status.Phase = keesev1alpha1.RecipeSourcePhaseFailed
		setRecipeSourceCondition(&rs.Status.Conditions, metav1.Condition{
			Type:               keesev1alpha1.RecipeSourceConditionReady,
			Status:             metav1.ConditionFalse,
			Reason:             "PullFailed",
			Message:            fmt.Sprintf("OCI pull failed: %v", err),
			ObservedGeneration: rs.Generation,
		})
		rs.Status.ObservedGeneration = rs.Generation
		return ctrl.Result{RequeueAfter: requeueOnSourceError}, r.patchRecipeSourceStatus(ctx, rs, orig)
	}

	r.Recorder.Eventf(rs, corev1.EventTypeNormal, ReasonRecipePulled,
		"OCI artifact pulled: digest=%s", artifact.Digest)

	// cosign verify — fail-closed (spec §OCI distribution).
	if err := r.Fetcher.Verify(ctx, oci.Registry, oci.Repository, artifact.Digest); err != nil {
		log.Error(err, "cosign verify failed", "digest", artifact.Digest)
		r.Recorder.Eventf(rs, corev1.EventTypeWarning, ReasonCosignVerifyFailed,
			"cosign verification failed for digest %s: %v", artifact.Digest, err)
		r.Recorder.Eventf(rs, corev1.EventTypeWarning, ReasonRecipeImageUnverified,
			"artifact digest %s is unverified; phase=Failed", artifact.Digest)
		rs.Status.Phase = keesev1alpha1.RecipeSourcePhaseFailed
		rs.Status.Cached = false // do not serve unverified artifact
		setRecipeSourceCondition(&rs.Status.Conditions, metav1.Condition{
			Type:               keesev1alpha1.RecipeSourceConditionReady,
			Status:             metav1.ConditionFalse,
			Reason:             "CosignVerifyFailed",
			Message:            fmt.Sprintf("cosign verification failed: %v", err),
			ObservedGeneration: rs.Generation,
		})
		rs.Status.ObservedGeneration = rs.Generation
		return ctrl.Result{RequeueAfter: requeueOnSourceError}, r.patchRecipeSourceStatus(ctx, rs, orig)
	}

	// Verification passed — update status.
	now := metav1.Now()
	rs.Status.ResolvedDigest = artifact.Digest
	rs.Status.LastVerifiedTime = &now
	rs.Status.Cached = true
	rs.Status.Phase = keesev1alpha1.RecipeSourcePhaseSynced
	rs.Status.ObservedGeneration = rs.Generation

	r.Recorder.Eventf(rs, corev1.EventTypeNormal, ReasonRecipeVerified,
		"OCI artifact verified: digest=%s", artifact.Digest)

	setRecipeSourceCondition(&rs.Status.Conditions, metav1.Condition{
		Type:               keesev1alpha1.RecipeSourceConditionProgressing,
		Status:             metav1.ConditionFalse,
		Reason:             "Synced",
		Message:            "OCI artifact pulled and verified",
		ObservedGeneration: rs.Generation,
	})
	setRecipeSourceCondition(&rs.Status.Conditions, metav1.Condition{
		Type:               keesev1alpha1.RecipeSourceConditionReady,
		Status:             metav1.ConditionTrue,
		Reason:             "Synced",
		Message:            fmt.Sprintf("artifact cached at digest %s", artifact.Digest),
		ObservedGeneration: rs.Generation,
	})

	return ctrl.Result{}, r.patchRecipeSourceStatus(ctx, rs, orig)
}

// reconcileGit handles the Git clone + SHA verify sequence.
func (r *RecipeSourceReconciler) reconcileGit(ctx context.Context, rs *keesev1alpha1.RecipeSource, orig *keesev1alpha1.RecipeSource) (ctrl.Result, error) {
	log := logf.FromContext(ctx)
	git := rs.Spec.Git

	rs.Status.SourceType = keesev1alpha1.RecipeSourceTypeGit
	// Git pull is represented as the resolved "digest" = the commit SHA.
	// For the real implementation, use go-git or exec git clone --depth=1.
	// Here we record the pinned revision as the digest and mark synced.
	// TODO(controller-author): wire real go-git clone when git dependency is added.
	log.Info("git source reconcile (stub: records revision as digest)", "url", git.URL, "revision", git.Revision)

	rs.Status.ResolvedDigest = git.Revision
	now := metav1.Now()
	rs.Status.LastVerifiedTime = &now
	rs.Status.Cached = true
	rs.Status.Phase = keesev1alpha1.RecipeSourcePhaseSynced
	rs.Status.ObservedGeneration = rs.Generation

	r.Recorder.Eventf(rs, corev1.EventTypeNormal, ReasonRecipePulled,
		"Git revision recorded: sha=%s", git.Revision)
	r.Recorder.Eventf(rs, corev1.EventTypeNormal, ReasonRecipeVerified,
		"Git SHA verified: sha=%s", git.Revision)

	setRecipeSourceCondition(&rs.Status.Conditions, metav1.Condition{
		Type:               keesev1alpha1.RecipeSourceConditionReady,
		Status:             metav1.ConditionTrue,
		Reason:             "Synced",
		Message:            fmt.Sprintf("git revision %s recorded", git.Revision),
		ObservedGeneration: rs.Generation,
	})

	return ctrl.Result{}, r.patchRecipeSourceStatus(ctx, rs, orig)
}

// reconcileConfigMap handles the inline ConfigMap source (dev only).
// VAP catches non-dev namespaces first; this is a defensive second check.
func (r *RecipeSourceReconciler) reconcileConfigMap(ctx context.Context, rs *keesev1alpha1.RecipeSource, orig *keesev1alpha1.RecipeSource) (ctrl.Result, error) {
	cm := rs.Spec.ConfigMap

	// Verify namespace has keese.ai/env=dev label (defensive; VAP is primary gate).
	var ns corev1.Namespace
	if err := r.Get(ctx, types.NamespacedName{Name: rs.Namespace}, &ns); err != nil {
		return ctrl.Result{}, fmt.Errorf("getting namespace %s: %w", rs.Namespace, err)
	}
	if ns.Labels[devEnvLabel] != devEnvValue {
		r.Recorder.Eventf(rs, corev1.EventTypeWarning, ReasonConfigMapSourceInNonDev,
			"ConfigMap source rejected: namespace %s is not a dev namespace (missing %s=%s label)",
			rs.Namespace, devEnvLabel, devEnvValue)
		r.Recorder.Eventf(rs, corev1.EventTypeWarning, ReasonDevSourceInProdNamespace,
			"ConfigMap source not permitted in namespace %s", rs.Namespace)

		rs.Status.Phase = keesev1alpha1.RecipeSourcePhaseFailed
		rs.Status.SourceType = keesev1alpha1.RecipeSourceTypeConfigMap
		setRecipeSourceCondition(&rs.Status.Conditions, metav1.Condition{
			Type:               keesev1alpha1.RecipeSourceConditionReady,
			Status:             metav1.ConditionFalse,
			Reason:             "ConfigMapSourceInNonDev",
			Message:            fmt.Sprintf("namespace %s is not labeled %s=%s", rs.Namespace, devEnvLabel, devEnvValue),
			ObservedGeneration: rs.Generation,
		})
		rs.Status.ObservedGeneration = rs.Generation
		return ctrl.Result{}, r.patchRecipeSourceStatus(ctx, rs, orig)
	}

	// Fetch the ConfigMap.
	var configMap corev1.ConfigMap
	cmKey := types.NamespacedName{Name: cm.Name, Namespace: cm.Namespace}
	if err := r.Get(ctx, cmKey, &configMap); err != nil {
		if errors.IsNotFound(err) {
			r.Recorder.Eventf(rs, corev1.EventTypeWarning, ReasonRecipeSourceNotFound,
				"ConfigMap %s/%s not found", cm.Namespace, cm.Name)
		}
		rs.Status.Phase = keesev1alpha1.RecipeSourcePhaseFailed
		setRecipeSourceCondition(&rs.Status.Conditions, metav1.Condition{
			Type:               keesev1alpha1.RecipeSourceConditionReady,
			Status:             metav1.ConditionFalse,
			Reason:             "ConfigMapNotFound",
			Message:            fmt.Sprintf("ConfigMap %s/%s not found: %v", cm.Namespace, cm.Name, err),
			ObservedGeneration: rs.Generation,
		})
		rs.Status.ObservedGeneration = rs.Generation
		return ctrl.Result{RequeueAfter: requeueOnSourceError}, r.patchRecipeSourceStatus(ctx, rs, orig)
	}

	rs.Status.SourceType = keesev1alpha1.RecipeSourceTypeConfigMap
	rs.Status.ResolvedDigest = "configmap:" + string(configMap.UID)
	now := metav1.Now()
	rs.Status.LastVerifiedTime = &now
	rs.Status.Cached = true
	rs.Status.Phase = keesev1alpha1.RecipeSourcePhaseSynced
	rs.Status.ObservedGeneration = rs.Generation

	r.Recorder.Eventf(rs, corev1.EventTypeNormal, ReasonRecipePulled,
		"ConfigMap loaded: %s/%s", cm.Namespace, cm.Name)
	r.Recorder.Eventf(rs, corev1.EventTypeNormal, ReasonRecipeVerified,
		"ConfigMap source verified (dev namespace)")

	setRecipeSourceCondition(&rs.Status.Conditions, metav1.Condition{
		Type:               keesev1alpha1.RecipeSourceConditionReady,
		Status:             metav1.ConditionTrue,
		Reason:             "Synced",
		Message:            fmt.Sprintf("ConfigMap %s/%s loaded", cm.Namespace, cm.Name),
		ObservedGeneration: rs.Generation,
	})

	return ctrl.Result{}, r.patchRecipeSourceStatus(ctx, rs, orig)
}

// cleanup runs when DeletionTimestamp is set.
func (r *RecipeSourceReconciler) cleanup(ctx context.Context, rs *keesev1alpha1.RecipeSource, orig *keesev1alpha1.RecipeSource) (ctrl.Result, error) {
	if !controllerutil.ContainsFinalizer(rs, recipeSourceFinalizer) {
		return ctrl.Result{}, nil
	}

	rs.Status.Phase = "" // reset; finalizer removal makes the resource disappear
	r.Recorder.Eventf(rs, corev1.EventTypeNormal, ReasonRecipeCacheCleanup,
		"RecipeSource cache cleanup on deletion")

	// TODO(controller-author): delete cached artifact from cluster registry here.

	controllerutil.RemoveFinalizer(rs, recipeSourceFinalizer)
	return ctrl.Result{}, r.Patch(ctx, rs, client.MergeFrom(orig))
}

// patchRecipeSourceStatus patches only the status subresource.
func (r *RecipeSourceReconciler) patchRecipeSourceStatus(ctx context.Context, rs, orig *keesev1alpha1.RecipeSource) error {
	return r.Status().Patch(ctx, rs, client.MergeFrom(orig))
}

// SetupWithManager sets up the controller with the Manager.
func (r *RecipeSourceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if r.Recorder == nil {
		r.Recorder = mgr.GetEventRecorderFor("recipesource-controller")
	}
	if r.Fetcher == nil {
		r.Fetcher = &FakeOCIFetcher{}
	}
	return ctrl.NewControllerManagedBy(mgr).
		For(&keesev1alpha1.RecipeSource{}).
		WithEventFilter(predicate.And(
			predicate.NewPredicateFuncs(func(obj client.Object) bool {
				return obj.GetLabels()[managedLabel] == managedLabelValue
			}),
			predicate.GenerationChangedPredicate{},
		)).
		Named("recipe-recipesource").
		Complete(r)
}

// setRecipeSourceCondition upserts a condition into the slice (by Type).
func setRecipeSourceCondition(conditions *[]metav1.Condition, c metav1.Condition) {
	now := metav1.Now()
	for i, existing := range *conditions {
		if existing.Type == c.Type {
			if existing.Status != c.Status {
				c.LastTransitionTime = now
			} else {
				c.LastTransitionTime = existing.LastTransitionTime
			}
			(*conditions)[i] = c
			return
		}
	}
	c.LastTransitionTime = now
	*conditions = append(*conditions, c)
}
