// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package tenancy

import (
	"context"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	tenancyv1alpha1 "github.com/keese-ai/keese/api/tenancy/v1alpha1"
)

// CrossTenantAgreementReconciler reconciles a CrossTenantAgreement object.
// SSA fieldOwner: keese-crosstenanagreement-controller (rule 04.7)
//
// Finalizers managed:
//   - finalizers.crosstenanagreement.operator.keese.ai/nats
//
// TODO(post-gate-controller-author): implement reconciler per
// docs/specs/tenancy.operator.keese.ai-v1alpha1-ii-cra.md
// Design: docs/designs/25-cross-tenant-agreement.md + 25-ii-spec-schema.md + 25-iii-approval-flow.md
type CrossTenantAgreementReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=tenancy.operator.keese.ai,resources=crosstenanagreements,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=tenancy.operator.keese.ai,resources=crosstenanagreements/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=tenancy.operator.keese.ai,resources=crosstenanagreements/finalizers,verbs=update
// +kubebuilder:rbac:groups=tenancy.operator.keese.ai,resources=tenants,verbs=get;list;watch
// +kubebuilder:rbac:groups=core,resources=events,verbs=create;patch

// Reconcile is part of the main kubernetes reconciliation loop.
func (r *CrossTenantAgreementReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	_ = logf.FromContext(ctx)

	// TODO(post-gate-controller-author): implement — no-op stub
	// All writes must use SSA: client.Apply with client.FieldOwner("keese-crosstenanagreement-controller")
	// No panic, no log.Fatal, no os.Exit (rule 04.8)

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *CrossTenantAgreementReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&tenancyv1alpha1.CrossTenantAgreement{}).
		Named("tenancy-crosstenanagreement").
		Complete(r)
}
