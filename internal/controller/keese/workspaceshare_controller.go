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
	gatewayv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"

	keesev1alpha1 "github.com/keese-ai/keese/api/keese/v1alpha1"
)

const (
	workspaceShareFinalizer = "finalizers.workspaceshare.keese.ai/cleanup"
	shareFieldOwner         = "keese-workspaceshare-controller"

	// WorkspaceShareConditionReferenceGrantProjected is a condition type that
	// tracks whether the Gateway API ReferenceGrant has been successfully projected
	// into the source (workspace) namespace.
	WorkspaceShareConditionReferenceGrantProjected = "ReferenceGrantProjected"
)

// WorkspaceShareReconciler reconciles a WorkspaceShare object.
type WorkspaceShareReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
	Rebac    WorkspaceRebacWriter
}

// +kubebuilder:rbac:groups=keese.ai,resources=workspaceshares,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=keese.ai,resources=workspaceshares/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=keese.ai,resources=workspaceshares/finalizers,verbs=update
// +kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=referencegrants,verbs=get;list;watch;create;update;patch;delete
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

	// Ensure finalizer (we allocate a ReferenceGrant + OpenFGA tuples that must be cleaned up).
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

	// --- Project ReferenceGrant via SSA ---
	// The ReferenceGrant lives in the *source* (workspace) namespace — not the consumer
	// namespace — because Gateway API §ReferenceGrant semantics: a grant is authored
	// by the namespace that *owns* the resource and grants other namespaces the right to
	// reference it.  Placing it in the consumer namespace would have no effect; only the
	// namespace containing the referenced object may grant cross-namespace access
	// (GW-API spec §5.1 "ReferenceGrant must be in the same namespace as the referent").
	rg := buildReferenceGrant(&share)
	if err := r.Patch(ctx, rg, client.Apply,
		client.FieldOwner(shareFieldOwner),
		client.ForceOwnership,
	); err != nil {
		log.Error(err, "failed to apply ReferenceGrant")
		r.setShareReferenceGrantNotReady(&share, "ReferenceGrantApplyFailed", err.Error())
		return ctrl.Result{}, fmt.Errorf("applying ReferenceGrant: %w", err)
	}
	r.Recorder.Eventf(&share, corev1.EventTypeNormal, ReasonShareReferenceGrantProjected,
		"ReferenceGrant %s/%s projected", rg.Namespace, rg.Name)
	share.Status.ReferenceGrantName = rg.Name
	setCondition(&share.Status.Conditions, metav1.Condition{
		Type:               WorkspaceShareConditionReferenceGrantProjected,
		Status:             metav1.ConditionTrue,
		Reason:             "Projected",
		Message:            fmt.Sprintf("ReferenceGrant %s/%s applied", rg.Namespace, rg.Name),
		ObservedGeneration: share.Generation,
	})

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

// cleanupShare deletes the projected ReferenceGrant, removes ReBAC tuples, then strips the finalizer.
func (r *WorkspaceShareReconciler) cleanupShare(ctx context.Context, share *keesev1alpha1.WorkspaceShare, orig *keesev1alpha1.WorkspaceShare) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	if !controllerutil.ContainsFinalizer(share, workspaceShareFinalizer) {
		return ctrl.Result{}, nil
	}

	// Delete the projected ReferenceGrant (SSA-delete: same fieldOwner).
	// The grant lives in the workspace's namespace (share.Namespace), not TargetNamespace.
	rg := buildReferenceGrant(share)
	if err := r.Delete(ctx, rg); client.IgnoreNotFound(err) != nil {
		log.Error(err, "failed to delete ReferenceGrant; will retry")
		r.Recorder.Eventf(share, corev1.EventTypeWarning, ReasonShareReferenceGrantPruned,
			"ReferenceGrant deletion failed: %v", err)
		return ctrl.Result{RequeueAfter: requeueAfterBackoff}, nil
	}
	r.Recorder.Eventf(share, corev1.EventTypeNormal, ReasonShareReferenceGrantPruned,
		"ReferenceGrant %s/%s pruned", rg.Namespace, rg.Name)

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

// buildReferenceGrant constructs the desired ReferenceGrant for a WorkspaceShare.
// The grant is placed in the *source* namespace (share.Namespace) — the namespace that owns
// the Workspace — because Gateway API §ReferenceGrant requires the grant to live in the same
// namespace as the resource being referenced (the referent), not in the consumer's namespace.
// This allows workloads in TargetNamespace to cross-reference resources in share.Namespace.
func buildReferenceGrant(share *keesev1alpha1.WorkspaceShare) *gatewayv1beta1.ReferenceGrant {
	targetNS := gatewayv1beta1.Namespace(share.Spec.TargetNamespace)
	// Apply forces the full object to be declared; TypeMeta is mandatory for SSA.
	rg := &gatewayv1beta1.ReferenceGrant{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "gateway.networking.k8s.io/v1beta1",
			Kind:       "ReferenceGrant",
		},
		ObjectMeta: metav1.ObjectMeta{
			// Name is deterministic and stable — same WorkspaceShare UID → same grant name.
			Name:      referenceGrantName(share),
			Namespace: share.Namespace,
			Labels: map[string]string{
				"keese.ai/managed-by":      shareFieldOwner,
				"keese.ai/workspace-share": share.Name,
			},
		},
		Spec: gatewayv1beta1.ReferenceGrantSpec{
			// From: workloads in the consumer (TargetNamespace) namespace.
			From: []gatewayv1beta1.ReferenceGrantFrom{
				{
					Group:     gatewayv1beta1.Group(""),
					Kind:      gatewayv1beta1.Kind("ServiceAccount"),
					Namespace: targetNS,
				},
			},
			// To: resources in this (source) namespace that may be referenced.
			// We allow cross-referencing the Workspace kind in this namespace.
			To: []gatewayv1beta1.ReferenceGrantTo{
				{
					Group: gatewayv1beta1.Group("keese.ai"),
					Kind:  gatewayv1beta1.Kind("Workspace"),
				},
			},
		},
	}
	return rg
}

// referenceGrantName produces a deterministic, stable name for the ReferenceGrant
// derived from the WorkspaceShare name (max 253 chars per DNS subdomain rules).
func referenceGrantName(share *keesev1alpha1.WorkspaceShare) string {
	return fmt.Sprintf("keese-share-%s", share.Name)
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

func (r *WorkspaceShareReconciler) setShareReferenceGrantNotReady(share *keesev1alpha1.WorkspaceShare, reason, msg string) {
	setCondition(&share.Status.Conditions, metav1.Condition{
		Type:               WorkspaceShareConditionReferenceGrantProjected,
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
		r.Rebac = WorkspaceNoopRebacWriter{}
	}
	if r.Recorder == nil {
		r.Recorder = mgr.GetEventRecorderFor("workspaceshare-controller")
	}
	return ctrl.NewControllerManagedBy(mgr).
		For(&keesev1alpha1.WorkspaceShare{}).
		Owns(&gatewayv1beta1.ReferenceGrant{}).
		WithEventFilter(predicate.GenerationChangedPredicate{}).
		Named("workspaceshare").
		Complete(r)
}

// referenceGrantNameForUID returns the ReferenceGrant name for a given WorkspaceShare UID.
// Used in tests that need to look up the projected grant by name.
func referenceGrantNameForUID(share *keesev1alpha1.WorkspaceShare) types.NamespacedName {
	return types.NamespacedName{
		Namespace: share.Namespace,
		Name:      referenceGrantName(share),
	}
}
