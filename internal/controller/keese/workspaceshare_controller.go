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
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	keesev1alpha1 "github.com/keese-ai/keese/api/keese/v1alpha1"
)

const (
	workspaceShareFinalizer = "finalizers.workspaceshare.operator.keese.ai/cleanup"
	shareFieldOwner         = "keese-workspaceshare-controller"

	// TODO(spec-followup): ReferenceGrant projection requires gateway.networking.k8s.io CRD.
	// Stubbed as a label annotation until the Gateway API CRD is wired into the scheme.
	// Replace with a real sigs.k8s.io/gateway-api/apis/v1beta1.ReferenceGrant SSA once available.
	referenceGrantAnnotation = "keese.ai/reference-grant-projected"
)

// WorkspaceShareReconciler reconciles a WorkspaceShare object.
type WorkspaceShareReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
	Rebac    WorkspaceRebacWriter
}

// +kubebuilder:rbac:groups=workspace.operator.keese.ai,resources=workspaceshares,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=workspace.operator.keese.ai,resources=workspaceshares/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=workspace.operator.keese.ai,resources=workspaceshares/finalizers,verbs=update
// +kubebuilder:rbac:groups=core,resources=events,verbs=create;patch

// Reconcile implements the main reconciliation loop for WorkspaceShare.
func (r *WorkspaceShareReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	var share keesev1alpha1.WorkspaceShare
	if err := r.Get(ctx, req.NamespacedName, &share); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	orig := share.DeepCopy()

	// Handle deletion first.
	if !share.DeletionTimestamp.IsZero() {
		return r.cleanupShare(ctx, &share, orig)
	}

	// Ensure finalizer (we write OpenFGA tuples that must be cleaned up).
	if !controllerutil.ContainsFinalizer(&share, workspaceShareFinalizer) {
		controllerutil.AddFinalizer(&share, workspaceShareFinalizer)
		if err := r.Patch(ctx, &share, client.MergeFrom(orig)); err != nil {
			return ctrl.Result{}, fmt.Errorf("adding share finalizer: %w", err)
		}
		if err := r.Get(ctx, req.NamespacedName, &share); err != nil {
			return ctrl.Result{}, client.IgnoreNotFound(err)
		}
		orig = share.DeepCopy()
	}

	// Verify the referenced Workspace exists.
	var ws keesev1alpha1.Workspace
	wsKey := client.ObjectKey{Name: share.Spec.WorkspaceRef.Name, Namespace: share.Namespace}
	if err := r.Get(ctx, wsKey, &ws); err != nil {
		if errors.IsNotFound(err) {
			log.Info("referenced Workspace not found; requeuing", "workspace", share.Spec.WorkspaceRef.Name)
			r.setShareProgressing(&share, "WorkspaceNotFound",
				fmt.Sprintf("Workspace %s not found", share.Spec.WorkspaceRef.Name))
			return ctrl.Result{RequeueAfter: 10 * time.Second}, r.patchShareStatus(ctx, &share, orig)
		}
		return ctrl.Result{}, err
	}

	// --- Project ReferenceGrant (stub) ---
	// TODO(spec-followup): Once gateway.networking.k8s.io/v1beta1 is in the scheme,
	// replace this annotation with a real ReferenceGrant SSA:
	//   rg := buildReferenceGrant(&share)
	//   r.Apply(ctx, rg)
	// For now we record it via annotation on the share itself as a placeholder.
	grantName := "keese-share-" + string(share.UID)
	if share.Annotations == nil {
		share.Annotations = map[string]string{}
	}
	share.Annotations[referenceGrantAnnotation] = grantName
	if err := r.Patch(ctx, &share, client.MergeFrom(orig)); err != nil && !errors.IsConflict(err) {
		log.Error(err, "failed to annotate share with reference grant placeholder")
	}
	// Re-fetch after annotation patch.
	if err := r.Get(ctx, req.NamespacedName, &share); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	orig = share.DeepCopy()

	r.Recorder.Eventf(&share, corev1.EventTypeNormal, ReasonShareReferenceGrantEnsured,
		"ReferenceGrant %s projected (stub)", grantName)
	share.Status.ReferenceGrantName = grantName

	// --- ReBAC tuples ---
	tuples := rebacTuplesForShare(&share, &ws)
	if err := r.Rebac.Sync(ctx, tuples); err != nil {
		log.Error(err, "failed to sync share ReBAC tuples")
		r.setShareProgressing(&share, "RebacSyncFailed", err.Error())
		return ctrl.Result{RequeueAfter: requeueAfterBackoff}, r.patchShareStatus(ctx, &share, orig)
	}
	r.Recorder.Eventf(&share, corev1.EventTypeNormal, ReasonShareRebacTupleWritten,
		"%d share ReBAC tuples synced", len(tuples))
	share.Status.RebacTupleCount = int32(len(tuples)) //nolint:gosec

	// --- Update status ---
	share.Status.ObservedGeneration = share.Generation
	setCondition(&share.Status.Conditions, metav1.Condition{
		Type:               keesev1alpha1.WorkspaceShareConditionProgressing,
		Status:             metav1.ConditionFalse,
		Reason:             "ReconcileComplete",
		Message:            "Reconcile completed successfully",
		ObservedGeneration: share.Generation,
	})
	setCondition(&share.Status.Conditions, metav1.Condition{
		Type:               keesev1alpha1.WorkspaceShareConditionReady,
		Status:             metav1.ConditionTrue,
		Reason:             "Ready",
		Message:            "WorkspaceShare is ready",
		ObservedGeneration: share.Generation,
	})

	return ctrl.Result{}, r.patchShareStatus(ctx, &share, orig)
}

// cleanupShare removes ReBAC tuples then strips the finalizer.
func (r *WorkspaceShareReconciler) cleanupShare(ctx context.Context, share *keesev1alpha1.WorkspaceShare, orig *keesev1alpha1.WorkspaceShare) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	if !controllerutil.ContainsFinalizer(share, workspaceShareFinalizer) {
		return ctrl.Result{}, nil
	}

	// Retrieve the workspace for tuple computation (best-effort; if not found, delete all).
	var ws keesev1alpha1.Workspace
	wsKey := client.ObjectKey{Name: share.Spec.WorkspaceRef.Name, Namespace: share.Namespace}
	if err := r.Get(ctx, wsKey, &ws); err != nil && !errors.IsNotFound(err) {
		return ctrl.Result{}, err
	}

	tuples := rebacTuplesForShare(share, &ws)
	if err := r.Rebac.Delete(ctx, tuples); err != nil {
		log.Error(err, "failed to delete share ReBAC tuples; will retry")
		r.Recorder.Eventf(share, corev1.EventTypeWarning, ReasonShareRebacTupleDeleteFailed,
			"Share ReBAC tuple deletion failed: %v", err)
		return ctrl.Result{RequeueAfter: requeueAfterBackoff}, nil
	}

	controllerutil.RemoveFinalizer(share, workspaceShareFinalizer)
	return ctrl.Result{}, r.Patch(ctx, share, client.MergeFrom(orig))
}

func (r *WorkspaceShareReconciler) patchShareStatus(ctx context.Context, share *keesev1alpha1.WorkspaceShare, orig *keesev1alpha1.WorkspaceShare) error {
	return r.Status().Patch(ctx, share, client.MergeFrom(orig))
}

func (r *WorkspaceShareReconciler) setShareProgressing(share *keesev1alpha1.WorkspaceShare, reason, msg string) {
	setCondition(&share.Status.Conditions, metav1.Condition{
		Type:               keesev1alpha1.WorkspaceShareConditionProgressing,
		Status:             metav1.ConditionTrue,
		Reason:             reason,
		Message:            msg,
		ObservedGeneration: share.Generation,
	})
	setCondition(&share.Status.Conditions, metav1.Condition{
		Type:               keesev1alpha1.WorkspaceShareConditionReady,
		Status:             metav1.ConditionFalse,
		Reason:             reason,
		Message:            msg,
		ObservedGeneration: share.Generation,
	})
}

// rebacTuplesForShare computes the ReBAC tuples for a WorkspaceShare.
func rebacTuplesForShare(share *keesev1alpha1.WorkspaceShare, ws *keesev1alpha1.Workspace) []WorkspaceRebacTuple {
	var tuples []WorkspaceRebacTuple
	wsObj := "workspace:" + share.Spec.WorkspaceRef.Name
	relation := "editor"
	if share.Spec.ReadOnly {
		relation = "viewer"
	}
	for _, grantee := range share.Spec.Grantees {
		tuples = append(tuples, WorkspaceRebacTuple{
			Object:   wsObj,
			Relation: relation,
			User:     "user:" + grantee,
		})
	}
	// Cross-namespace share tuple.
	if ws.Name != "" {
		tuples = append(tuples, WorkspaceRebacTuple{
			Object:   wsObj,
			Relation: "shared_with",
			User:     "namespace:" + share.Spec.TargetNamespace,
		})
	}
	return tuples
}

// SetupWithManager sets up the controller with the Manager.
func (r *WorkspaceShareReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if r.Rebac == nil {
		r.Rebac = &WorkspaceFakeRebacWriter{}
	}
	if r.Recorder == nil {
		r.Recorder = mgr.GetEventRecorderFor("workspaceshare-controller")
	}
	return ctrl.NewControllerManagedBy(mgr).
		For(&keesev1alpha1.WorkspaceShare{}).
		WithEventFilter(predicate.GenerationChangedPredicate{}).
		Named("workspaceshare").
		Complete(r)
}
