// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package workspace

import (
	"context"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	workspacev1alpha1 "github.com/keese-ai/keese/api/workspace/v1alpha1"
)

// WorkspaceSessionReconciler reconciles a WorkspaceSession object.
// SSA fieldOwner: keese-workspacesession-controller (rule 04.7)
//
// Finalizers managed:
//   - finalizers.workspacesession.operator.keese.ai/cleanup
//     Steps: Draining → AgentRuntime.Drain(ctx, 90s) → delete Pod →
//            remove session-scoped OpenFGA tuples → remove finalizer.
//
// NOTE: this file is SEPARATE from workspace_controller.go to avoid overlap with the
// parallel controller-author agent working on Workspace reconciler. Both live in
// package workspace but are distinct Reconciler types with distinct Named() controllers.
//
// TODO(post-gate-controller-author): implement reconciler per
// docs/specs/workspace.operator.keese.ai-v1alpha1-ii-session.md
// Design: docs/designs/08b-goose-acp-stdio-k8s.md + docs/designs/08b-ii-session-crd-spec.md
type WorkspaceSessionReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=workspace.operator.keese.ai,resources=workspacesessions,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=workspace.operator.keese.ai,resources=workspacesessions/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=workspace.operator.keese.ai,resources=workspacesessions/finalizers,verbs=update
// +kubebuilder:rbac:groups=workspace.operator.keese.ai,resources=workspaces,verbs=get;list;watch
// +kubebuilder:rbac:groups=core,resources=pods,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=events,verbs=create;patch

// Reconcile is part of the main kubernetes reconciliation loop.
func (r *WorkspaceSessionReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	_ = logf.FromContext(ctx)

	// TODO(post-gate-controller-author): implement — no-op stub
	// All writes must use SSA: client.Apply with client.FieldOwner("keese-workspacesession-controller")
	// No panic, no log.Fatal, no os.Exit (rule 04.8)

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *WorkspaceSessionReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&workspacev1alpha1.WorkspaceSession{}).
		Named("workspace-workspacesession").
		Complete(r)
}
