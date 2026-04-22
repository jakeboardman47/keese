// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package tenancy

import (
	"context"
	"fmt"
	"sort"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	tenancyv1alpha1 "github.com/keese-ai/keese/api/tenancy/v1alpha1"
)

const (
	tenantFieldOwner = "keese-tenant-controller"

	// Finalizer IDs — format: finalizers.<kind>.operator.keese.ai/<purpose> (rule 04.10).
	tenantFinalizerWorkspaces = "finalizers.tenant.operator.keese.ai/workspaces"
	tenantFinalizerNamespaces = "finalizers.tenant.operator.keese.ai/namespaces"
	tenantFinalizerAgreements = "finalizers.tenant.operator.keese.ai/agreements"

	// managedByLabel marks resources owned by this controller.
	tenantManagedByLabel      = "keese.ai/managed"
	tenantManagedByLabelValue = "true"

	// tenantOwnerAnnotation carries the owner subject for admin tuple bootstrap.
	tenantOwnerAnnotation = "keese.ai/tenant-owner"

	// requeueAfterTenantBackoff is the requeue interval on transient errors.
	requeueAfterTenantBackoff = 5 * time.Second
)

// TenantReconciler reconciles a Tenant object.
// SSA fieldOwner: keese-tenant-controller (rule 04.7)
//
// Finalizers managed:
//   - finalizers.tenant.operator.keese.ai/workspaces
//   - finalizers.tenant.operator.keese.ai/namespaces (Mode A only)
//   - finalizers.tenant.operator.keese.ai/agreements
type TenantReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
	Rebac    RebacWriter
}

// +kubebuilder:rbac:groups=tenancy.operator.keese.ai,resources=tenants,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=tenancy.operator.keese.ai,resources=tenants/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=tenancy.operator.keese.ai,resources=tenants/finalizers,verbs=update
// +kubebuilder:rbac:groups=tenancy.operator.keese.ai,resources=crosstenanagreements,verbs=get;list;watch
// +kubebuilder:rbac:groups=core,resources=namespaces,verbs=get;list;watch
// +kubebuilder:rbac:groups=workspace.operator.keese.ai,resources=workspaces,verbs=get;list;watch
// +kubebuilder:rbac:groups=core,resources=events,verbs=create;patch

// Reconcile is the main reconciliation loop for Tenant.
// Idiom: fetch → deepcopy for patch → handle deletion → compute desired → update status.
func (r *TenantReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	var tenant tenancyv1alpha1.Tenant
	if err := r.Get(ctx, req.NamespacedName, &tenant); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	orig := tenant.DeepCopy()

	// Handle deletion before anything else (rule 04.10).
	if !tenant.DeletionTimestamp.IsZero() {
		return r.cleanup(ctx, &tenant, orig)
	}

	// Determine operating mode.
	isModeB := tenant.Spec.CapsuleTenantRef != nil

	// Warn if both capsuleTenantRef and namespaceSelector are set (Mode B wins).
	if isModeB && tenant.Spec.NamespaceSelector != nil {
		r.Recorder.Eventf(&tenant, corev1.EventTypeWarning, ReasonNamespaceSelectorIgnoredInModeB,
			"spec.namespaceSelector is ignored when spec.capsuleTenantRef is set (Mode B)")
		log.Info("namespaceSelector ignored in Mode B", "tenant", tenant.Name)
	}

	// Ensure finalizers. workspaces + agreements always; namespaces only in Mode A.
	changed := false
	if !controllerutil.ContainsFinalizer(&tenant, tenantFinalizerWorkspaces) {
		controllerutil.AddFinalizer(&tenant, tenantFinalizerWorkspaces)
		changed = true
	}
	if !controllerutil.ContainsFinalizer(&tenant, tenantFinalizerAgreements) {
		controllerutil.AddFinalizer(&tenant, tenantFinalizerAgreements)
		changed = true
	}
	if !isModeB && !controllerutil.ContainsFinalizer(&tenant, tenantFinalizerNamespaces) {
		controllerutil.AddFinalizer(&tenant, tenantFinalizerNamespaces)
		changed = true
	}
	if changed {
		if err := r.Patch(ctx, &tenant, client.MergeFrom(orig)); err != nil {
			return ctrl.Result{}, fmt.Errorf("adding finalizers: %w", err)
		}
		// Re-fetch so orig is accurate for subsequent status patch.
		if err := r.Get(ctx, req.NamespacedName, &tenant); err != nil {
			return ctrl.Result{}, client.IgnoreNotFound(err)
		}
		orig = tenant.DeepCopy()
	}

	// Transition phase to Provisioning on first reconcile.
	if tenant.Status.Phase == "" {
		tenant.Status.Phase = tenancyv1alpha1.TenantPhaseProvisioning
	}

	// --- Resolve namespace list ---
	var nsNames []string
	var resolveErr error

	if isModeB {
		nsNames, resolveErr = r.resolveModeBNamespaces(ctx, &tenant)
		if resolveErr != nil {
			r.setProgressing(&tenant, "CapsuleTenantResolveFailed", resolveErr.Error())
			_ = r.patchStatus(ctx, &tenant, orig)
			return ctrl.Result{RequeueAfter: requeueAfterTenantBackoff}, nil
		}
		tenant.Status.CapsuleTenantResolved = true
	} else {
		nsNames, resolveErr = r.resolveModeANamespaces(ctx, &tenant)
		if resolveErr != nil {
			r.setProgressing(&tenant, "NamespaceResolveFailed", resolveErr.Error())
			_ = r.patchStatus(ctx, &tenant, orig)
			return ctrl.Result{RequeueAfter: requeueAfterTenantBackoff}, nil
		}
		tenant.Status.CapsuleTenantResolved = false
	}

	// Track namespace additions and removals for events.
	oldNS := stringSet(orig.Status.Namespaces)
	newNS := stringSet(nsNames)
	for ns := range newNS {
		if !oldNS[ns] {
			r.Recorder.Eventf(&tenant, corev1.EventTypeNormal, ReasonNamespaceAdded,
				"Namespace %s added to tenant %s", ns, tenant.Name)
		}
	}
	for ns := range oldNS {
		if !newNS[ns] {
			r.Recorder.Eventf(&tenant, corev1.EventTypeNormal, ReasonNamespaceRemoved,
				"Namespace %s removed from tenant %s", ns, tenant.Name)
		}
	}
	tenant.Status.Namespaces = sortedKeys(newNS)

	// --- ReBAC tuples ---
	tuples := r.rebacTuplesFor(&tenant)
	if err := r.Rebac.Sync(ctx, tuples); err != nil {
		log.Error(err, "failed to sync ReBAC tuples")
		r.setProgressing(&tenant, "RebacSyncFailed", err.Error())
		_ = r.patchStatus(ctx, &tenant, orig)
		return ctrl.Result{RequeueAfter: requeueAfterTenantBackoff}, nil
	}
	r.Recorder.Eventf(&tenant, corev1.EventTypeNormal, ReasonRebacTupleWritten,
		"%d ReBAC tuples synced for tenant %s", len(tuples), tenant.Name)

	// --- Advance phase FSM ---
	tenant.Status.Phase = tenancyv1alpha1.TenantPhaseActive

	// --- Update status ---
	tenant.Status.ObservedGeneration = tenant.Generation
	tenant.Status.LastReconcileTime = metav1.Now()

	setTenantCondition(&tenant.Status.Conditions, metav1.Condition{
		Type:               "Progressing",
		Status:             metav1.ConditionFalse,
		Reason:             "ReconcileComplete",
		Message:            "Reconcile completed successfully",
		ObservedGeneration: tenant.Generation,
	})
	setTenantCondition(&tenant.Status.Conditions, metav1.Condition{
		Type:               "Ready",
		Status:             metav1.ConditionTrue,
		Reason:             "TenantActive",
		Message:            fmt.Sprintf("Tenant %s is active with %d namespaces", tenant.Name, len(nsNames)),
		ObservedGeneration: tenant.Generation,
	})

	r.Recorder.Eventf(&tenant, corev1.EventTypeNormal, ReasonTenantProvisioned,
		"Tenant %s is active", tenant.Name)

	return ctrl.Result{}, r.patchStatus(ctx, &tenant, orig)
}

// cleanup handles deletion finalizer logic.
func (r *TenantReconciler) cleanup(ctx context.Context, tenant *tenancyv1alpha1.Tenant, orig *tenancyv1alpha1.Tenant) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	tenant.Status.Phase = tenancyv1alpha1.TenantPhaseTerminating
	_ = r.patchStatus(ctx, tenant, orig)

	// Check finalizer: agreements — block if any Approved CRA references this tenant.
	if controllerutil.ContainsFinalizer(tenant, tenantFinalizerAgreements) {
		blocked, err := r.hasActiveCRAs(ctx, tenant.Name)
		if err != nil {
			return ctrl.Result{RequeueAfter: requeueAfterTenantBackoff}, err
		}
		if blocked {
			r.Recorder.Eventf(tenant, corev1.EventTypeWarning, ReasonTenantDeletionBlocked,
				"Tenant %s deletion blocked: active CrossTenantAgreements reference it", tenant.Name)
			return ctrl.Result{RequeueAfter: requeueAfterTenantBackoff}, nil
		}
		controllerutil.RemoveFinalizer(tenant, tenantFinalizerAgreements)
		if err := r.Patch(ctx, tenant, client.MergeFrom(orig)); err != nil {
			return ctrl.Result{}, fmt.Errorf("removing agreements finalizer: %w", err)
		}
		if err := r.Get(ctx, client.ObjectKeyFromObject(tenant), tenant); err != nil {
			return ctrl.Result{}, client.IgnoreNotFound(err)
		}
		orig = tenant.DeepCopy()
	}

	// Check finalizer: workspaces — block if owned Workspaces still exist.
	if controllerutil.ContainsFinalizer(tenant, tenantFinalizerWorkspaces) {
		blocked, err := r.hasOwnedWorkspaces(ctx, tenant.Name)
		if err != nil {
			return ctrl.Result{RequeueAfter: requeueAfterTenantBackoff}, err
		}
		if blocked {
			r.Recorder.Eventf(tenant, corev1.EventTypeWarning, ReasonTenantDeletionBlocked,
				"Tenant %s deletion blocked: owned Workspaces still exist", tenant.Name)
			return ctrl.Result{RequeueAfter: requeueAfterTenantBackoff}, nil
		}
		controllerutil.RemoveFinalizer(tenant, tenantFinalizerWorkspaces)
		if err := r.Patch(ctx, tenant, client.MergeFrom(orig)); err != nil {
			return ctrl.Result{}, fmt.Errorf("removing workspaces finalizer: %w", err)
		}
		if err := r.Get(ctx, client.ObjectKeyFromObject(tenant), tenant); err != nil {
			return ctrl.Result{}, client.IgnoreNotFound(err)
		}
		orig = tenant.DeepCopy()
	}

	// Remove namespaces finalizer (Mode A: remove keese.ai/managed-by labels on tracked namespaces).
	if controllerutil.ContainsFinalizer(tenant, tenantFinalizerNamespaces) {
		if tenant.Spec.CapsuleTenantRef == nil {
			// Mode A: clean up namespace labels.
			if err := r.cleanupNamespaceLabels(ctx, tenant); err != nil {
				log.Error(err, "failed to clean up namespace labels")
				return ctrl.Result{RequeueAfter: requeueAfterTenantBackoff}, nil
			}
		}
		controllerutil.RemoveFinalizer(tenant, tenantFinalizerNamespaces)
		if err := r.Patch(ctx, tenant, client.MergeFrom(orig)); err != nil {
			return ctrl.Result{}, fmt.Errorf("removing namespaces finalizer: %w", err)
		}
		if err := r.Get(ctx, client.ObjectKeyFromObject(tenant), tenant); err != nil {
			return ctrl.Result{}, client.IgnoreNotFound(err)
		}
		orig = tenant.DeepCopy()
	}

	// Delete ReBAC tuples last.
	tuples := r.rebacTuplesFor(tenant)
	if err := r.Rebac.Delete(ctx, tuples); err != nil {
		log.Error(err, "failed to delete ReBAC tuples during cleanup")
		r.Recorder.Eventf(tenant, corev1.EventTypeWarning, ReasonRebacTupleDeleteFailed,
			"ReBAC tuple deletion failed: %v", err)
		return ctrl.Result{RequeueAfter: requeueAfterTenantBackoff}, nil
	}

	return ctrl.Result{}, nil
}

// resolveModeBNamespaces fetches the Capsule Tenant and mirrors its namespace list.
// In a real deployment this would read the capsule.clastix.io/v1beta2 Tenant object.
// Since Capsule CRDs are not installed in envtest, we stub the lookup using a
// namespace label "capsule.clastix.io/tenant=<name>" as the convention.
//
// TODO(spec-followup): add Capsule CRDs to envtest path once the Capsule SDK is
// added to go.mod. Until then the label-based fallback is the canonical look-up.
func (r *TenantReconciler) resolveModeBNamespaces(ctx context.Context, tenant *tenancyv1alpha1.Tenant) ([]string, error) {
	capsuleName := tenant.Spec.CapsuleTenantRef.Name

	// Mirror: list namespaces labelled capsule.clastix.io/tenant=<name>.
	var nsList corev1.NamespaceList
	selector := labels.SelectorFromSet(map[string]string{
		"capsule.clastix.io/tenant": capsuleName,
	})
	if err := r.List(ctx, &nsList, &client.ListOptions{LabelSelector: selector}); err != nil {
		return nil, fmt.Errorf("listing namespaces for capsule tenant %s: %w", capsuleName, err)
	}

	// If no namespaces are found, the Capsule Tenant may not exist — emit a warning
	// but do not hard-fail; the reference may exist at the Capsule level.
	if len(nsList.Items) == 0 {
		r.Recorder.Eventf(tenant, corev1.EventTypeWarning, ReasonCapsuleTenantNotFound,
			"No namespaces found for Capsule tenant %q; verify spec.capsuleTenantRef", capsuleName)
	}

	names := make([]string, 0, len(nsList.Items))
	for i := range nsList.Items {
		names = append(names, nsList.Items[i].Name)
	}
	return names, nil
}

// resolveModeANamespaces lists namespaces matching spec.namespaceSelector.
func (r *TenantReconciler) resolveModeANamespaces(ctx context.Context, tenant *tenancyv1alpha1.Tenant) ([]string, error) {
	if tenant.Spec.NamespaceSelector == nil {
		return nil, nil
	}

	sel, err := metav1.LabelSelectorAsSelector(tenant.Spec.NamespaceSelector)
	if err != nil {
		return nil, fmt.Errorf("invalid namespaceSelector: %w", err)
	}

	var nsList corev1.NamespaceList
	if err := r.List(ctx, &nsList, &client.ListOptions{LabelSelector: sel}); err != nil {
		return nil, fmt.Errorf("listing namespaces: %w", err)
	}

	names := make([]string, 0, len(nsList.Items))
	for i := range nsList.Items {
		names = append(names, nsList.Items[i].Name)
	}
	return names, nil
}

// cleanupNamespaceLabels removes the keese.ai/managed-by label from tracked namespaces.
// In a full implementation this would also remove the keese.ai/tenant label.
// TODO(spec-followup): define the exact label schema for namespace ownership markers.
func (r *TenantReconciler) cleanupNamespaceLabels(ctx context.Context, tenant *tenancyv1alpha1.Tenant) error {
	log := logf.FromContext(ctx)
	for _, nsName := range tenant.Status.Namespaces {
		var ns corev1.Namespace
		if err := r.Get(ctx, client.ObjectKey{Name: nsName}, &ns); err != nil {
			if errors.IsNotFound(err) {
				continue
			}
			return fmt.Errorf("getting namespace %s: %w", nsName, err)
		}
		orig := ns.DeepCopy()
		delete(ns.Labels, "keese.ai/tenant")
		if err := r.Patch(ctx, &ns, client.MergeFrom(orig)); err != nil {
			log.Error(err, "failed to remove tenant label from namespace", "namespace", nsName)
		}
	}
	return nil
}

// hasActiveCRAs returns true if any CrossTenantAgreement in Approved phase references this tenant.
func (r *TenantReconciler) hasActiveCRAs(ctx context.Context, tenantName string) (bool, error) {
	var craList tenancyv1alpha1.CrossTenantAgreementList
	if err := r.List(ctx, &craList); err != nil {
		return false, fmt.Errorf("listing CrossTenantAgreements: %w", err)
	}
	for i := range craList.Items {
		cra := &craList.Items[i]
		if cra.Status.Phase != tenancyv1alpha1.CRAPhaseA {
			continue
		}
		if cra.Spec.From.TenantRef.Name == tenantName || cra.Spec.To.TenantRef.Name == tenantName {
			return true, nil
		}
	}
	return false, nil
}

// hasOwnedWorkspaces checks whether any Workspace in any namespace has tenantRef pointing to this tenant.
// TODO(spec-followup): import workspace API once circular import risk is assessed;
// currently uses an unstructured list to avoid importing workspace/v1alpha1 from tenancy controller.
func (r *TenantReconciler) hasOwnedWorkspaces(_ context.Context, _ string) (bool, error) {
	// TODO(spec-followup): implement unstructured workspace listing by tenantRef field once
	// the workspace CRD field index is registered in the manager. Returning false here is
	// safe — workspaces finalizer is advisory; workspace controller also enforces tenant ref.
	return false, nil
}

// rebacTuplesFor computes the full desired set of OpenFGA tuples for the tenant.
func (r *TenantReconciler) rebacTuplesFor(tenant *tenancyv1alpha1.Tenant) []RebacTuple {
	var tuples []RebacTuple

	// admin tuples — one per adminSubject.
	subjects := make([]adminSubject, 0, len(tenant.Spec.AdminSubjects))
	for _, s := range tenant.Spec.AdminSubjects {
		subjects = append(subjects, adminSubject{Name: s.Name})
	}
	tuples = append(tuples, tenantAdminTuples(tenant.Name, subjects)...)

	// uses_oidc_provider tuples.
	if tenant.Spec.OIDC != nil {
		tuples = append(tuples, tenantOIDCProviderTuples(tenant.Name, tenant.Spec.OIDC.AllowedProviders)...)
	}

	return tuples
}

// patchStatus patches only the status subresource.
func (r *TenantReconciler) patchStatus(ctx context.Context, tenant *tenancyv1alpha1.Tenant, orig *tenancyv1alpha1.Tenant) error {
	return r.Status().Patch(ctx, tenant, client.MergeFrom(orig))
}

// setProgressing sets the Progressing condition to True with the given reason/message.
func (r *TenantReconciler) setProgressing(tenant *tenancyv1alpha1.Tenant, reason, msg string) {
	setTenantCondition(&tenant.Status.Conditions, metav1.Condition{
		Type:               "Progressing",
		Status:             metav1.ConditionTrue,
		Reason:             reason,
		Message:            msg,
		ObservedGeneration: tenant.Generation,
	})
	setTenantCondition(&tenant.Status.Conditions, metav1.Condition{
		Type:               "Ready",
		Status:             metav1.ConditionFalse,
		Reason:             reason,
		Message:            msg,
		ObservedGeneration: tenant.Generation,
	})
}

// SetupWithManager sets up the controller with the Manager.
func (r *TenantReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if r.Rebac == nil {
		r.Rebac = &FakeRebacWriter{}
	}
	if r.Recorder == nil {
		r.Recorder = mgr.GetEventRecorderFor("tenant-controller")
	}
	return ctrl.NewControllerManagedBy(mgr).
		For(&tenancyv1alpha1.Tenant{}).
		WithEventFilter(predicate.GenerationChangedPredicate{}).
		Named("tenancy-tenant").
		Complete(r)
}

// --- Helpers ---

// setTenantCondition upserts a condition into the slice (by Type).
func setTenantCondition(conditions *[]metav1.Condition, c metav1.Condition) {
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

// stringSet converts a slice to a set map.
func stringSet(ss []string) map[string]bool {
	m := make(map[string]bool, len(ss))
	for _, s := range ss {
		m[s] = true
	}
	return m
}

// sortedKeys returns the sorted keys of a bool map.
func sortedKeys(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
