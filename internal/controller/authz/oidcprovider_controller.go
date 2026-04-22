// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package authz

import (
	"context"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	authzv1alpha1 "github.com/keese-ai/keese/api/authz/v1alpha1"
)

// OIDCProviderReconciler reconciles an OIDCProvider object.
// SSA fieldOwner: keese-oidcprovider-controller (rule 04.7)
//
// Finalizers managed:
//   - finalizers.oidcprovider.operator.keese.ai/cache-flush
//     Sends gRPC cache-flush to all gateway pods (max 60s drain) before deletion.
//
// TODO(post-gate-controller-author): implement reconciler per
// docs/specs/authz.operator.keese.ai-v1alpha1.md
// Design: docs/designs/04b-projected-sa-identity.md + docs/designs/04b-ii-oidc-trust.md
//
// tenant.uses_oidc_provider OpenFGA relation lives in model.fga as of 04a
// iter-6 (2026-04-21); Tenant controller writes tuples per
// Tenant.spec.oidc.allowedProviders[].
type OIDCProviderReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=authz.operator.keese.ai,resources=oidcproviders,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=authz.operator.keese.ai,resources=oidcproviders/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=authz.operator.keese.ai,resources=oidcproviders/finalizers,verbs=update
// +kubebuilder:rbac:groups=core,resources=events,verbs=create;patch

// Reconcile is part of the main kubernetes reconciliation loop.
func (r *OIDCProviderReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	_ = logf.FromContext(ctx)

	// TODO(post-gate-controller-author): implement — no-op stub
	// All writes must use SSA: client.Apply with client.FieldOwner("keese-oidcprovider-controller")
	// No panic, no log.Fatal, no os.Exit (rule 04.8)

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *OIDCProviderReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&authzv1alpha1.OIDCProvider{}).
		Named("authz-oidcprovider").
		Complete(r)
}
