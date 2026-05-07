// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package authz

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	authzv1alpha1 "github.com/keese-ai/keese/api/authz/v1alpha1"
)

const (
	craFieldOwner = "keese-crosstenanagreement-controller"

	// Finalizer ID — format: finalizers.<kind>.keese.ai/<purpose> (rule 04.10).
	craFinalizerNATS = "finalizers.crosstenanagreement.keese.ai/nats"

	// craApproveAnnotation is checked on each reconcile to drive the approval flow.
	// An admission webhook (stubbed) validates the annotator has can_approve_cra permission.
	craApproveAnnotation = "keese.ai/cra-approve"

	// requeueAfterCRABackoff is the requeue interval on transient errors.
	requeueAfterCRABackoff = 5 * time.Second

	// requeueAfterExpiryCheck is the period at which the controller checks expiry.
	requeueAfterExpiryCheck = 1 * time.Minute
)

// CrossTenantAgreementReconciler reconciles a CrossTenantAgreement object.
// SSA fieldOwner: keese-crosstenanagreement-controller (rule 04.7)
//
// Finalizers managed:
//   - finalizers.crosstenanagreement.keese.ai/nats
type CrossTenantAgreementReconciler struct {
	client.Client
	Scheme          *runtime.Scheme
	Recorder        record.EventRecorder
	Rebac           CTARebacWriter
	CosignVerifier  CosignVerifier
	SATokenVerifier SATokenHmacVerifier
	NatsDeleter     NatsStreamDeleter
}

// +kubebuilder:rbac:groups=authz.keese.ai,resources=crosstenantagreements,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=authz.keese.ai,resources=crosstenantagreements/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=authz.keese.ai,resources=crosstenantagreements/finalizers,verbs=update
// +kubebuilder:rbac:groups=keese.ai,resources=tenants,verbs=get;list;watch
// +kubebuilder:rbac:groups=keese.ai,resources=workspaces,verbs=get;list;watch
// +kubebuilder:rbac:groups=core,resources=events,verbs=create;patch

// Reconcile is the main reconciliation loop for CrossTenantAgreement.
func (r *CrossTenantAgreementReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	var cra authzv1alpha1.CrossTenantAgreement
	if err := r.Get(ctx, req.NamespacedName, &cra); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	orig := cra.DeepCopy()

	// Handle deletion before anything else.
	if !cra.DeletionTimestamp.IsZero() {
		return r.cleanup(ctx, &cra, orig)
	}

	// Ensure NATS finalizer.
	if !controllerutil.ContainsFinalizer(&cra, craFinalizerNATS) {
		controllerutil.AddFinalizer(&cra, craFinalizerNATS)
		if err := r.Patch(ctx, &cra, client.MergeFrom(orig)); err != nil {
			return ctrl.Result{}, fmt.Errorf("adding nats finalizer: %w", err)
		}
		if err := r.Get(ctx, req.NamespacedName, &cra); err != nil {
			return ctrl.Result{}, client.IgnoreNotFound(err)
		}
		orig = cra.DeepCopy()
	}

	// Initialize phase if needed.
	if cra.Status.Phase == "" {
		cra.Status.Phase = authzv1alpha1.CRAPhaseP
	}

	// Terminal phases: Rejected and Expired are append-only; no further state changes.
	if cra.Status.Phase == authzv1alpha1.CRAPhaseR || cra.Status.Phase == authzv1alpha1.CRAPhaseE {
		cra.Status.ObservedGeneration = cra.Generation
		return ctrl.Result{}, r.patchCRAStatus(ctx, &cra, orig)
	}

	// --- Check expiry on Approved CRAs ---
	if cra.Status.Phase == authzv1alpha1.CRAPhaseA && cra.Spec.ExpiresAt != "" {
		expired, err := r.isExpired(&cra)
		if err != nil {
			log.Error(err, "failed to parse expiresAt")
			return ctrl.Result{RequeueAfter: requeueAfterCRABackoff}, nil
		}
		if expired {
			return r.transitionToExpired(ctx, &cra, orig)
		}
	}

	// --- Process approval annotation (Pending only) ---
	// BUG-FIX(batch-controller-approval): processApprovalAnnotation previously called
	// r.Patch (spec/metadata patch) while holding the in-memory approval in
	// cra.Status.Approvals. client.Patch updates the local object pointer with the
	// server's response, which carries the old (pre-approval) status, discarding the
	// appended approval before patchCRAStatus could persist it.
	// Fix: validateApprovalAnnotation returns the verified approval without patching.
	// We remove the annotations via a spec patch here, re-fetch so orig reflects server
	// state, then append the approval to the fresh copy — exactly the pattern used by
	// WorkspaceReconciler after its finalizer patch (workspace_controller.go §90-99).
	if cra.Status.Phase == authzv1alpha1.CRAPhaseP {
		if val := cra.Annotations[craApproveAnnotation]; val == "true" {
			if approval := r.validateApprovalAnnotation(ctx, &cra); approval != nil {
				// Remove approval annotations from metadata (spec patch, no status touch).
				delete(cra.Annotations, craApproveAnnotation)
				delete(cra.Annotations, "keese.ai/cra-approving-tenant")
				delete(cra.Annotations, "keese.ai/cra-approver")
				delete(cra.Annotations, "keese.ai/cra-signature")
				delete(cra.Annotations, "keese.ai/cra-signature-type")
				if err := r.Patch(ctx, &cra, client.MergeFrom(orig)); err != nil {
					log.Error(err, "failed to remove approval annotations")
					return ctrl.Result{RequeueAfter: requeueAfterCRABackoff}, nil
				}
				// Re-fetch so orig matches server state (mirrors workspace controller
				// finalizer pattern). The approval is appended to the fresh in-memory
				// copy; patchCRAStatus at end of reconcile persists it via the status
				// subresource.
				if err := r.Get(ctx, req.NamespacedName, &cra); err != nil {
					return ctrl.Result{}, client.IgnoreNotFound(err)
				}
				orig = cra.DeepCopy()
				cra.Status.Approvals = append(cra.Status.Approvals, *approval)
			}
		}
	}

	// --- Check if both tenants have approved → transition to Approved ---
	if cra.Status.Phase == authzv1alpha1.CRAPhaseP && r.bothTenantsApproved(&cra) {
		if result, err := r.transitionToApproved(ctx, &cra, orig); err != nil {
			return result, err
		} else if result.Requeue || result.RequeueAfter > 0 {
			return result, nil
		}
	}

	// --- Snapshot drift detection (Approved only) ---
	if cra.Status.Phase == authzv1alpha1.CRAPhaseA {
		r.checkSnapshotDrift(ctx, &cra)
	}

	// --- Conflict detection on create (Pending phase, no approvals yet) ---
	if cra.Status.Phase == authzv1alpha1.CRAPhaseP && len(cra.Status.Approvals) == 0 {
		if conflict, err := r.detectConflict(ctx, &cra); err != nil {
			return ctrl.Result{RequeueAfter: requeueAfterCRABackoff}, err
		} else if conflict {
			r.Recorder.Eventf(&cra, corev1.EventTypeWarning, ReasonCRAConflict,
				"CrossTenantAgreement conflicts with an existing Approved CRA for the same tenant pair")
			cra.Status.Phase = authzv1alpha1.CRAPhaseR
			r.setCRACondition(&cra.Status.Conditions, metav1.Condition{
				Type:               "Ready",
				Status:             metav1.ConditionFalse,
				Reason:             "Conflict",
				Message:            "An Approved CRA already covers this tenant pair",
				ObservedGeneration: cra.Generation,
			})
		}
	}

	cra.Status.ObservedGeneration = cra.Generation
	cra.Status.LastReconcileTime = metav1.Now()

	// Re-queue to catch expiry if expiresAt is set.
	result := ctrl.Result{}
	if cra.Spec.ExpiresAt != "" && cra.Status.Phase == authzv1alpha1.CRAPhaseA {
		result.RequeueAfter = requeueAfterExpiryCheck
	}

	return result, r.patchCRAStatus(ctx, &cra, orig)
}

// validateApprovalAnnotation reads and validates the cra-approve annotation, verifies
// the signature, and returns a CRAApproval ready to append — or nil if validation fails.
// It does NOT mutate cra or call Patch; all writes are the caller's responsibility.
// The real webhook handles admission-time permission checks (can_approve_cra).
func (r *CrossTenantAgreementReconciler) validateApprovalAnnotation(ctx context.Context, cra *authzv1alpha1.CrossTenantAgreement) *authzv1alpha1.CRAApproval {
	log := logf.FromContext(ctx)

	// Determine which tenant is approving from secondary annotations.
	approvingTenant := cra.Annotations["keese.ai/cra-approving-tenant"]
	approver := cra.Annotations["keese.ai/cra-approver"]
	signature := cra.Annotations["keese.ai/cra-signature"]
	sigType := cra.Annotations["keese.ai/cra-signature-type"]

	if approvingTenant == "" || approver == "" || signature == "" {
		log.Info("incomplete approval annotation, ignoring", "cra", cra.Name)
		return nil
	}

	// Validate the approving tenant is a participant.
	if approvingTenant != cra.Spec.From.TenantRef.Name && approvingTenant != cra.Spec.To.TenantRef.Name {
		r.Recorder.Eventf(cra, corev1.EventTypeWarning, ReasonCRAApprovalInvalid,
			"Approving tenant %q is not a participant in this CRA", approvingTenant)
		return nil
	}

	// Check this tenant hasn't already approved.
	for _, existing := range cra.Status.Approvals {
		if existing.Tenant == approvingTenant {
			log.Info("tenant already approved, ignoring duplicate", "tenant", approvingTenant)
			return nil
		}
	}

	// Verify signature.
	var verifyErr error
	st := authzv1alpha1.SignatureTypeSAToken
	if sigType == string(authzv1alpha1.SignatureTypeOIDCKeyless) {
		st = authzv1alpha1.SignatureTypeOIDCKeyless
		verifyErr = r.CosignVerifier.Verify(signature, approver)
	} else {
		verifyErr = r.SATokenVerifier.Verify(signature, "keese-egress-"+approvingTenant)
	}

	if verifyErr != nil {
		r.Recorder.Eventf(cra, corev1.EventTypeWarning, ReasonSignatureVerificationFailed,
			"Signature verification failed for approver %q on tenant %q: %v", approver, approvingTenant, verifyErr)
		r.Recorder.Eventf(cra, corev1.EventTypeWarning, ReasonCRAApprovalInvalid,
			"Approval from tenant %q rejected: signature invalid", approvingTenant)
		return nil
	}

	return &authzv1alpha1.CRAApproval{
		Tenant:        approvingTenant,
		ApprovedBy:    approver,
		ApprovedAt:    metav1.Now(),
		Signature:     signature,
		SignatureType: st,
	}
}

// bothTenantsApproved returns true when both from-tenant and to-tenant have approved.
func (r *CrossTenantAgreementReconciler) bothTenantsApproved(cra *authzv1alpha1.CrossTenantAgreement) bool {
	fromApproved, toApproved := false, false
	for _, a := range cra.Status.Approvals {
		if a.Tenant == cra.Spec.From.TenantRef.Name {
			fromApproved = true
		}
		if a.Tenant == cra.Spec.To.TenantRef.Name {
			toApproved = true
		}
	}
	return fromApproved && toApproved
}

// transitionToApproved moves the CRA to Approved, freezes the workspace snapshot,
// and writes ReBAC tuples.
func (r *CrossTenantAgreementReconciler) transitionToApproved(ctx context.Context, cra *authzv1alpha1.CrossTenantAgreement, orig *authzv1alpha1.CrossTenantAgreement) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	// Check expiresAt is still in the future.
	if cra.Spec.ExpiresAt != "" {
		expired, err := r.isExpired(cra)
		if err != nil {
			return ctrl.Result{RequeueAfter: requeueAfterCRABackoff}, err
		}
		if expired {
			return r.transitionToExpired(ctx, cra, orig)
		}
	}

	// Freeze workspace snapshot (TOFU).
	fromWorkspaces, err := r.resolveWorkspaces(ctx, cra.Spec.From.TenantRef.Name, cra.Spec.From.WorkspaceSelector)
	if err != nil {
		return ctrl.Result{RequeueAfter: requeueAfterCRABackoff}, fmt.Errorf("resolving from-workspaces: %w", err)
	}
	toWorkspaces, err := r.resolveWorkspaces(ctx, cra.Spec.To.TenantRef.Name, cra.Spec.To.WorkspaceSelector)
	if err != nil {
		return ctrl.Result{RequeueAfter: requeueAfterCRABackoff}, fmt.Errorf("resolving to-workspaces: %w", err)
	}

	now := metav1.Now()
	snapshot := make([]authzv1alpha1.WorkspaceSnapshotEntry, 0, len(fromWorkspaces)*len(toWorkspaces))
	for _, from := range fromWorkspaces {
		for _, to := range toWorkspaces {
			snapshot = append(snapshot, authzv1alpha1.WorkspaceSnapshotEntry{
				FromWorkspace: from,
				ToWorkspace:   to,
				SnapshotAt:    now,
			})
		}
	}
	cra.Status.WorkspaceSnapshot = snapshot

	// Write ReBAC tuples.
	tuples := []CTARebacTuple{craAllowsMessagingTuple(cra.Spec.From.TenantRef.Name, cra.Spec.To.TenantRef.Name)}
	tuples = append(tuples, craMessageableFromTuples(fromWorkspaces, toWorkspaces)...)

	if err := r.Rebac.Sync(ctx, tuples); err != nil {
		log.Error(err, "failed to sync ReBAC tuples on CRA approval")
		r.Recorder.Eventf(cra, corev1.EventTypeWarning, ReasonTupleSyncFailed,
			"ReBAC tuple sync failed: %v", err)
		return ctrl.Result{RequeueAfter: requeueAfterCRABackoff}, nil
	}

	cra.Status.Phase = authzv1alpha1.CRAPhaseA
	r.setCRACondition(&cra.Status.Conditions, metav1.Condition{
		Type:               "Ready",
		Status:             metav1.ConditionTrue,
		Reason:             "Approved",
		Message:            fmt.Sprintf("CRA approved by both tenants; %d workspace pairs frozen", len(snapshot)),
		ObservedGeneration: cra.Generation,
	})
	r.Recorder.Eventf(cra, corev1.EventTypeNormal, ReasonCRAApproved,
		"CrossTenantAgreement approved; %d workspace pairs frozen in snapshot", len(snapshot))

	return ctrl.Result{}, nil
}

// transitionToExpired moves the CRA to Expired and deletes synced ReBAC tuples.
func (r *CrossTenantAgreementReconciler) transitionToExpired(ctx context.Context, cra *authzv1alpha1.CrossTenantAgreement, orig *authzv1alpha1.CrossTenantAgreement) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	// Build the tuples that were written at approval time.
	fromWorkspaces := make([]string, 0)
	toWorkspaces := make([]string, 0)
	seen := map[string]struct{}{}
	for _, entry := range cra.Status.WorkspaceSnapshot {
		if _, ok := seen[entry.FromWorkspace]; !ok {
			seen[entry.FromWorkspace] = struct{}{}
			fromWorkspaces = append(fromWorkspaces, entry.FromWorkspace)
		}
	}
	seenTo := map[string]struct{}{}
	for _, entry := range cra.Status.WorkspaceSnapshot {
		if _, ok := seenTo[entry.ToWorkspace]; !ok {
			seenTo[entry.ToWorkspace] = struct{}{}
			toWorkspaces = append(toWorkspaces, entry.ToWorkspace)
		}
	}

	tuples := []CTARebacTuple{craAllowsMessagingTuple(cra.Spec.From.TenantRef.Name, cra.Spec.To.TenantRef.Name)}
	tuples = append(tuples, craMessageableFromTuples(fromWorkspaces, toWorkspaces)...)

	if err := r.Rebac.Delete(ctx, tuples); err != nil {
		log.Error(err, "failed to delete ReBAC tuples on expiry")
		r.Recorder.Eventf(cra, corev1.EventTypeWarning, ReasonTupleSyncFailed,
			"ReBAC tuple deletion failed on expiry: %v", err)
		return ctrl.Result{RequeueAfter: requeueAfterCRABackoff}, nil
	}

	cra.Status.Phase = authzv1alpha1.CRAPhaseE
	r.setCRACondition(&cra.Status.Conditions, metav1.Condition{
		Type:               "Ready",
		Status:             metav1.ConditionFalse,
		Reason:             "Expired",
		Message:            fmt.Sprintf("CRA expired at %s; tuples deleted", cra.Spec.ExpiresAt),
		ObservedGeneration: cra.Generation,
	})
	r.Recorder.Eventf(cra, corev1.EventTypeNormal, ReasonCRAExpired,
		"CrossTenantAgreement expired; ReBAC tuples deleted")

	cra.Status.ObservedGeneration = cra.Generation
	cra.Status.LastReconcileTime = metav1.Now()
	return ctrl.Result{}, r.patchCRAStatus(ctx, cra, orig)
}

// checkSnapshotDrift compares current selector results to the frozen snapshot.
// Emits WorkspaceSnapshotDrift if they diverge (does NOT auto-extend — TOFU).
func (r *CrossTenantAgreementReconciler) checkSnapshotDrift(ctx context.Context, cra *authzv1alpha1.CrossTenantAgreement) {
	fromCurrent, err := r.resolveWorkspaces(ctx, cra.Spec.From.TenantRef.Name, cra.Spec.From.WorkspaceSelector)
	if err != nil {
		return
	}
	toCurrent, err := r.resolveWorkspaces(ctx, cra.Spec.To.TenantRef.Name, cra.Spec.To.WorkspaceSelector)
	if err != nil {
		return
	}

	// Build current pair set.
	current := map[string]bool{}
	for _, f := range fromCurrent {
		for _, t := range toCurrent {
			current[f+"/"+t] = true
		}
	}

	// Build snapshot pair set.
	snapshotSet := map[string]bool{}
	for _, entry := range cra.Status.WorkspaceSnapshot {
		snapshotSet[entry.FromWorkspace+"/"+entry.ToWorkspace] = true
	}

	// Check for drift.
	drift := false
	for pair := range current {
		if !snapshotSet[pair] {
			drift = true
			break
		}
	}
	if !drift {
		for pair := range snapshotSet {
			if !current[pair] {
				drift = true
				break
			}
		}
	}

	if drift {
		r.Recorder.Eventf(cra, corev1.EventTypeWarning, ReasonWorkspaceSnapshotDrift,
			"Workspace snapshot drift detected: current selector results differ from frozen snapshot; create a new CRA to extend coverage")
	}
}

// detectConflict returns true if an existing Approved CRA covers the same tenant pair.
func (r *CrossTenantAgreementReconciler) detectConflict(ctx context.Context, cra *authzv1alpha1.CrossTenantAgreement) (bool, error) {
	var craList authzv1alpha1.CrossTenantAgreementList
	if err := r.List(ctx, &craList); err != nil {
		return false, fmt.Errorf("listing CrossTenantAgreements for conflict check: %w", err)
	}
	for i := range craList.Items {
		other := &craList.Items[i]
		if other.Name == cra.Name {
			continue
		}
		if other.Status.Phase != authzv1alpha1.CRAPhaseA {
			continue
		}
		if other.Spec.From.TenantRef.Name == cra.Spec.From.TenantRef.Name &&
			other.Spec.To.TenantRef.Name == cra.Spec.To.TenantRef.Name {
			return true, nil
		}
	}
	return false, nil
}

// cleanup handles deletion finalizer for the NATS stream.
func (r *CrossTenantAgreementReconciler) cleanup(ctx context.Context, cra *authzv1alpha1.CrossTenantAgreement, orig *authzv1alpha1.CrossTenantAgreement) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	if !controllerutil.ContainsFinalizer(cra, craFinalizerNATS) {
		return ctrl.Result{}, nil
	}

	streamName := natsStreamName(string(cra.UID))
	if err := r.NatsDeleter.DeleteStream(ctx, streamName); err != nil {
		log.Error(err, "failed to delete NATS stream", "stream", streamName)
		r.Recorder.Eventf(cra, corev1.EventTypeWarning, ReasonNATSStreamDeleteFailed,
			"NATS stream %s deletion failed: %v", streamName, err)
		return ctrl.Result{RequeueAfter: requeueAfterCRABackoff}, nil
	}

	r.Recorder.Eventf(cra, corev1.EventTypeNormal, ReasonNATSStreamDeleted,
		"NATS stream %s deleted", streamName)

	controllerutil.RemoveFinalizer(cra, craFinalizerNATS)
	return ctrl.Result{}, r.Patch(ctx, cra, client.MergeFrom(orig))
}

// resolveWorkspaces lists workspace names for a tenant filtered by an optional selector.
// TODO(spec-followup): import workspace/v1alpha1 once the import cycle between
// controller packages is resolved. Currently uses a namespace-list approximation via
// namespace labels as a stub. Real implementation lists Workspace CRs by tenantRef.
func (r *CrossTenantAgreementReconciler) resolveWorkspaces(_ context.Context, tenantName string, _ *metav1.LabelSelector) ([]string, error) {
	// TODO(spec-followup): real workspace listing using unstructured + field index
	// on tenantRef.name. Returning the tenant name itself as a single representative
	// workspace name for now, so snapshot and tuple logic has something to work with.
	_ = labels.Everything() // ensure labels import is used
	return []string{"ws-" + tenantName}, nil
}

// isExpired parses expiresAt and returns true if it is in the past.
func (r *CrossTenantAgreementReconciler) isExpired(cra *authzv1alpha1.CrossTenantAgreement) (bool, error) {
	if cra.Spec.ExpiresAt == "" {
		return false, nil
	}
	exp, err := time.Parse(time.RFC3339, cra.Spec.ExpiresAt)
	if err != nil {
		return false, fmt.Errorf("parsing expiresAt %q: %w", cra.Spec.ExpiresAt, err)
	}
	return time.Now().UTC().After(exp), nil
}

// patchCRAStatus patches only the status subresource.
func (r *CrossTenantAgreementReconciler) patchCRAStatus(ctx context.Context, cra *authzv1alpha1.CrossTenantAgreement, orig *authzv1alpha1.CrossTenantAgreement) error {
	return r.Status().Patch(ctx, cra, client.MergeFrom(orig))
}

// setCRACondition upserts a condition into the CRA status conditions slice.
func (r *CrossTenantAgreementReconciler) setCRACondition(conditions *[]metav1.Condition, c metav1.Condition) {
	setCondition(conditions, c)
}

// SetupWithManager sets up the controller with the Manager.
func (r *CrossTenantAgreementReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if r.Rebac == nil {
		r.Rebac = CTANoopRebacWriter{}
	}
	if r.Recorder == nil {
		r.Recorder = mgr.GetEventRecorderFor("crosstenanagreement-controller")
	}
	if r.CosignVerifier == nil {
		r.CosignVerifier = &FakeCosignVerifier{}
	}
	if r.SATokenVerifier == nil {
		r.SATokenVerifier = &FakeSATokenHmacVerifier{}
	}
	if r.NatsDeleter == nil {
		r.NatsDeleter = &FakeNatsStreamDeleter{}
	}
	return ctrl.NewControllerManagedBy(mgr).
		For(&authzv1alpha1.CrossTenantAgreement{}).
		WithEventFilter(predicate.GenerationChangedPredicate{}).
		Named("tenancy-crosstenanagreement").
		Complete(r)
}
