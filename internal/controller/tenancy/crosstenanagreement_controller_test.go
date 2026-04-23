// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

//go:build integration

package tenancy

import (
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	tenancyv1alpha1 "github.com/keese-ai/keese/api/tenancy/v1alpha1"
)

// makeCRA returns a minimal valid CrossTenantAgreement for two named tenants.
func makeCRA(name, fromTenant, toTenant string) *tenancyv1alpha1.CrossTenantAgreement {
	return &tenancyv1alpha1.CrossTenantAgreement{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: tenancyv1alpha1.CrossTenantAgreementSpec{
			From: tenancyv1alpha1.TenantEndpoint{
				TenantRef: tenancyv1alpha1.LocalObjectRef{Name: fromTenant},
			},
			To: tenancyv1alpha1.TenantEndpoint{
				TenantRef: tenancyv1alpha1.LocalObjectRef{Name: toTenant},
			},
			Scope: tenancyv1alpha1.CRAScope{
				NATSSubjects: []string{"keese.cta.test"},
				A2ARoles:     []tenancyv1alpha1.A2ARole{tenancyv1alpha1.A2ARoleReader},
			},
		},
	}
}

// makeCRAReconciler returns a CrossTenantAgreementReconciler wired to envtest.
// Caller may override individual fakes for targeted injection.
func makeCRAReconciler(rebac RebacWriter, cosign CosignVerifier, saToken SATokenHmacVerifier, nats NatsStreamDeleter) *CrossTenantAgreementReconciler {
	if rebac == nil {
		rebac = &FakeRebacWriter{}
	}
	if cosign == nil {
		cosign = &FakeCosignVerifier{}
	}
	if saToken == nil {
		saToken = &FakeSATokenHmacVerifier{}
	}
	if nats == nil {
		nats = &FakeNatsStreamDeleter{}
	}
	return &CrossTenantAgreementReconciler{
		Client:          k8sClient,
		Scheme:          k8sClient.Scheme(),
		Recorder:        &tenancyNoopRecorder{},
		Rebac:           rebac,
		CosignVerifier:  cosign,
		SATokenVerifier: saToken,
		NatsDeleter:     nats,
	}
}

// craNSN returns a NamespacedName for a cluster-scoped CRA.
func craNSN(name string) types.NamespacedName { return types.NamespacedName{Name: name} }

// findCRACondition returns the condition of type t, or nil if absent.
func findCRACondition(conds []metav1.Condition, t string) *metav1.Condition {
	for i := range conds {
		if conds[i].Type == t {
			return &conds[i]
		}
	}
	return nil
}

// forceDeleteCRA removes finalizers then deletes, best-effort.
func forceDeleteCRA(name string) error {
	var c tenancyv1alpha1.CrossTenantAgreement
	if err := k8sClient.Get(ctx, craNSN(name), &c); err != nil {
		return err
	}
	c.Finalizers = nil
	if err := k8sClient.Update(ctx, &c); err != nil {
		return err
	}
	return k8sClient.Delete(ctx, &c)
}

// approveAnnotations returns the annotations needed to trigger the approval flow.
func approveAnnotations(approvingTenant, approver, signature, sigType string) map[string]string {
	return map[string]string{
		craApproveAnnotation:            "true",
		"keese.ai/cra-approving-tenant": approvingTenant,
		"keese.ai/cra-approver":         approver,
		"keese.ai/cra-signature":        signature,
		"keese.ai/cra-signature-type":   sigType,
	}
}

// annotateCRA patches the CR with the given annotations on top of existing metadata.
func annotateCRA(name string, annotations map[string]string) {
	var cra tenancyv1alpha1.CrossTenantAgreement
	Expect(k8sClient.Get(ctx, craNSN(name), &cra)).To(Succeed())
	orig := cra.DeepCopy()
	if cra.Annotations == nil {
		cra.Annotations = make(map[string]string)
	}
	for k, v := range annotations {
		cra.Annotations[k] = v
	}
	Expect(k8sClient.Patch(ctx, &cra, client.MergeFrom(orig))).To(Succeed())
}

// driveToApproved initialises a CRA (finalizer + Pending) then drives both-tenant
// approvals through the real annotation path, returning the reconciler used so
// callers can assert on fakes or swap the recorder.
// sigType should be string(tenancyv1alpha1.SignatureTypeSAToken) or OIDCKeyless.
func driveToApproved(name, fromT, toT, sigType string, r *CrossTenantAgreementReconciler) {
	req := reconcile.Request{NamespacedName: craNSN(name)}

	// Reconcile 1: add finalizer + set phase=Pending.
	_, err := r.Reconcile(ctx, req)
	Expect(err).NotTo(HaveOccurred())

	// Annotate for from-tenant approval.
	annotateCRA(name, approveAnnotations(fromT, "approver-from@example.com", "sig-from", sigType))

	// Reconcile 2: validate annotation, remove it, persist approval in status.
	_, err = r.Reconcile(ctx, req)
	Expect(err).NotTo(HaveOccurred())

	// Assert from-tenant approval was persisted before continuing.
	var mid tenancyv1alpha1.CrossTenantAgreement
	Expect(k8sClient.Get(ctx, craNSN(name), &mid)).To(Succeed())
	Expect(mid.Status.Approvals).To(HaveLen(1), "from-tenant approval must be persisted after reconcile 2")

	// Annotate for to-tenant approval.
	annotateCRA(name, approveAnnotations(toT, "approver-to@example.com", "sig-to", sigType))

	// Reconcile 3: second approval persisted → bothTenantsApproved → transitionToApproved.
	_, err = r.Reconcile(ctx, req)
	Expect(err).NotTo(HaveOccurred())
}

// craCapturingRecorder records events for assertion — mirrors tenancy's capturingRecorder.
type craCapturingRecorder struct {
	events []string
}

func (c *craCapturingRecorder) Event(_ runtime.Object, _, reason, _ string) {
	c.events = append(c.events, reason)
}
func (c *craCapturingRecorder) Eventf(_ runtime.Object, _, reason, _ string, _ ...interface{}) {
	c.events = append(c.events, reason)
}
func (c *craCapturingRecorder) AnnotatedEventf(_ runtime.Object, _ map[string]string, _, reason, _ string, _ ...interface{}) {
	c.events = append(c.events, reason)
}
func (c *craCapturingRecorder) hasReason(reason string) bool {
	for _, e := range c.events {
		if e == reason {
			return true
		}
	}
	return false
}

var _ = Describe("CrossTenantAgreementReconciler", func() {

	// -------------------------------------------------------------------------
	// Spec 1 — Idempotency
	// -------------------------------------------------------------------------
	Describe("TestReconcileIdempotent_CrossTenantAgreement", func() {
		It("converges in ≤3 reconciles with no spec change (rule 04-kubernetes.md §16)", func() {
			name := fmt.Sprintf("cra-idempotent-%d", GinkgoRandomSeed())
			cra := makeCRA(name, "tenant-alpha", "tenant-beta")
			Expect(k8sClient.Create(ctx, cra)).To(Succeed())
			DeferCleanup(func() { _ = forceDeleteCRA(name) })

			r := makeCRAReconciler(nil, nil, nil, nil)
			req := reconcile.Request{NamespacedName: craNSN(name)}

			var lastRV string
			for i := 0; i < 3; i++ {
				_, err := r.Reconcile(ctx, req)
				Expect(err).NotTo(HaveOccurred())

				var fresh tenancyv1alpha1.CrossTenantAgreement
				Expect(k8sClient.Get(ctx, craNSN(name), &fresh)).To(Succeed())
				if i == 2 {
					Expect(fresh.ResourceVersion).To(Equal(lastRV),
						"ResourceVersion must not increment on idempotent third reconcile")
				}
				lastRV = fresh.ResourceVersion
			}
		})
	})

	// -------------------------------------------------------------------------
	// Spec 2 — Bilateral approval happy path: OIDC-keyless (cosign) signatures
	//
	// Drives both approvals through the real annotation path (one reconcile per
	// tenant). The bug fix ensures validateApprovalAnnotation never calls Patch
	// internally, so the in-memory approval survives until patchCRAStatus runs.
	// -------------------------------------------------------------------------
	Describe("Bilateral approval: OIDC-keyless (cosign) happy path", func() {
		It("transitions to Approved and writes ReBAC tuples after both cosign approvals", func() {
			name := fmt.Sprintf("cra-cosign-%d", GinkgoRandomSeed())
			fromT, toT := fmt.Sprintf("tf-cs-%d", GinkgoRandomSeed()), fmt.Sprintf("tt-cs-%d", GinkgoRandomSeed())
			cra := makeCRA(name, fromT, toT)
			Expect(k8sClient.Create(ctx, cra)).To(Succeed())
			DeferCleanup(func() { _ = forceDeleteCRA(name) })

			fakeRebac := &FakeRebacWriter{}
			recorder := &craCapturingRecorder{}
			r := makeCRAReconciler(fakeRebac, &FakeCosignVerifier{}, nil, nil)
			r.Recorder = recorder

			driveToApproved(name, fromT, toT, string(tenancyv1alpha1.SignatureTypeOIDCKeyless), r)

			var fresh tenancyv1alpha1.CrossTenantAgreement
			Expect(k8sClient.Get(ctx, craNSN(name), &fresh)).To(Succeed())
			Expect(fresh.Status.Phase).To(Equal(tenancyv1alpha1.CRAPhaseA))

			// CRAApproved event must have been emitted.
			Expect(recorder.hasReason(ReasonCRAApproved)).To(BeTrue(),
				"expected CRAApproved event on bilateral approval")

			// ReBAC tuples must have been synced.
			Expect(fakeRebac.Synced).NotTo(BeEmpty(),
				"ReBAC tuples must be written on Approved transition")

			// Both approvals must be persisted with the correct signature type.
			Expect(fresh.Status.Approvals).To(HaveLen(2))
			for _, appr := range fresh.Status.Approvals {
				Expect(appr.SignatureType).To(Equal(tenancyv1alpha1.SignatureTypeOIDCKeyless))
			}
		})
	})

	// -------------------------------------------------------------------------
	// Spec 3 — Bilateral approval: SA-token happy path
	// -------------------------------------------------------------------------
	Describe("Bilateral approval: SA-token happy path", func() {
		It("transitions to Approved after both tenants sign with SA-token HMAC", func() {
			name := fmt.Sprintf("cra-satoken-%d", GinkgoRandomSeed())
			fromT, toT := fmt.Sprintf("tf-sa-%d", GinkgoRandomSeed()), fmt.Sprintf("tt-sa-%d", GinkgoRandomSeed())
			cra := makeCRA(name, fromT, toT)
			Expect(k8sClient.Create(ctx, cra)).To(Succeed())
			DeferCleanup(func() { _ = forceDeleteCRA(name) })

			r := makeCRAReconciler(nil, nil, &FakeSATokenHmacVerifier{}, nil)

			driveToApproved(name, fromT, toT, string(tenancyv1alpha1.SignatureTypeSAToken), r)

			var fresh tenancyv1alpha1.CrossTenantAgreement
			Expect(k8sClient.Get(ctx, craNSN(name), &fresh)).To(Succeed())
			Expect(fresh.Status.Phase).To(Equal(tenancyv1alpha1.CRAPhaseA))

			// Verify SA-token signature type recorded in both approvals.
			Expect(fresh.Status.Approvals).To(HaveLen(2))
			for _, appr := range fresh.Status.Approvals {
				Expect(appr.SignatureType).To(Equal(tenancyv1alpha1.SignatureTypeSAToken))
			}
		})
	})

	// -------------------------------------------------------------------------
	// Spec 4 — Signature verification failure
	//
	// Tests the annotation processing path directly: when FakeCosignVerifier.FailNext
	// is true, the reconciler emits CRAApprovalInvalid and keeps phase Pending.
	// -------------------------------------------------------------------------
	Describe("Signature verification failure", func() {
		It("emits CRAApprovalInvalid and keeps phase Pending when cosign verify fails", func() {
			name := fmt.Sprintf("cra-sigfail-%d", GinkgoRandomSeed())
			fromT, toT := fmt.Sprintf("tf-fail-%d", GinkgoRandomSeed()), fmt.Sprintf("tt-fail-%d", GinkgoRandomSeed())
			cra := makeCRA(name, fromT, toT)
			Expect(k8sClient.Create(ctx, cra)).To(Succeed())
			DeferCleanup(func() { _ = forceDeleteCRA(name) })

			failingCosign := &FakeCosignVerifier{FailNext: true}
			recorder := &craCapturingRecorder{}
			r := makeCRAReconciler(nil, failingCosign, nil, nil)
			r.Recorder = recorder

			req := reconcile.Request{NamespacedName: craNSN(name)}

			// Initialise (adds finalizer).
			_, err := r.Reconcile(ctx, req)
			Expect(err).NotTo(HaveOccurred())

			// Annotate for from-tenant approval with a signature that will fail.
			annotateCRA(name, approveAnnotations(fromT, "malicious@example.com", "bad-sig", string(tenancyv1alpha1.SignatureTypeOIDCKeyless)))

			_, err = r.Reconcile(ctx, req)
			Expect(err).NotTo(HaveOccurred())

			// CRAApprovalInvalid event must have been emitted.
			Expect(recorder.hasReason(ReasonCRAApprovalInvalid)).To(BeTrue(),
				"expected CRAApprovalInvalid event on signature failure")

			// Phase must still be Pending and no approvals recorded.
			var fresh tenancyv1alpha1.CrossTenantAgreement
			Expect(k8sClient.Get(ctx, craNSN(name), &fresh)).To(Succeed())
			Expect(fresh.Status.Phase).To(Equal(tenancyv1alpha1.CRAPhaseP))
			Expect(fresh.Status.Approvals).To(BeEmpty(),
				"no approval must be recorded when signature verification fails")
		})
	})

	// -------------------------------------------------------------------------
	// Spec 5 — TOFU snapshot frozen on Approved transition
	// -------------------------------------------------------------------------
	Describe("TOFU snapshot frozen on Approved transition", func() {
		It("freezes workspace snapshot with non-zero SnapshotAt on Approved transition", func() {
			name := fmt.Sprintf("cra-tofu-%d", GinkgoRandomSeed())
			fromT, toT := fmt.Sprintf("tf-tofu-%d", GinkgoRandomSeed()), fmt.Sprintf("tt-tofu-%d", GinkgoRandomSeed())
			cra := makeCRA(name, fromT, toT)
			Expect(k8sClient.Create(ctx, cra)).To(Succeed())
			DeferCleanup(func() { _ = forceDeleteCRA(name) })

			r := makeCRAReconciler(nil, nil, nil, nil)

			driveToApproved(name, fromT, toT, string(tenancyv1alpha1.SignatureTypeSAToken), r)

			var fresh tenancyv1alpha1.CrossTenantAgreement
			Expect(k8sClient.Get(ctx, craNSN(name), &fresh)).To(Succeed())
			Expect(fresh.Status.Phase).To(Equal(tenancyv1alpha1.CRAPhaseA))

			// Snapshot must be non-empty and every entry must have SnapshotAt set (TOFU).
			Expect(fresh.Status.WorkspaceSnapshot).NotTo(BeEmpty(),
				"workspace snapshot must be frozen on Approved transition")
			for _, entry := range fresh.Status.WorkspaceSnapshot {
				Expect(entry.SnapshotAt.IsZero()).To(BeFalse(),
					"SnapshotAt must be recorded for each workspace pair")
			}

			// Snapshot must be stable on a subsequent reconcile (immutable TOFU).
			snapshotLen := len(fresh.Status.WorkspaceSnapshot)
			req := reconcile.Request{NamespacedName: craNSN(name)}
			_, err := r.Reconcile(ctx, req)
			Expect(err).NotTo(HaveOccurred())

			var fresh2 tenancyv1alpha1.CrossTenantAgreement
			Expect(k8sClient.Get(ctx, craNSN(name), &fresh2)).To(Succeed())
			Expect(fresh2.Status.WorkspaceSnapshot).To(HaveLen(snapshotLen),
				"workspace snapshot must not grow on subsequent reconciles (TOFU immutability)")
		})
	})

	// -------------------------------------------------------------------------
	// Spec 6 — Snapshot drift detection
	// -------------------------------------------------------------------------
	Describe("Snapshot drift detection", func() {
		It("emits WorkspaceSnapshotDrift when snapshot diverges from current selector", func() {
			name := fmt.Sprintf("cra-drift-%d", GinkgoRandomSeed())
			fromT, toT := fmt.Sprintf("tf-drift-%d", GinkgoRandomSeed()), fmt.Sprintf("tt-drift-%d", GinkgoRandomSeed())
			cra := makeCRA(name, fromT, toT)
			Expect(k8sClient.Create(ctx, cra)).To(Succeed())
			DeferCleanup(func() { _ = forceDeleteCRA(name) })

			r := makeCRAReconciler(nil, nil, nil, nil)

			driveToApproved(name, fromT, toT, string(tenancyv1alpha1.SignatureTypeSAToken), r)

			var approved tenancyv1alpha1.CrossTenantAgreement
			Expect(k8sClient.Get(ctx, craNSN(name), &approved)).To(Succeed())
			Expect(approved.Status.Phase).To(Equal(tenancyv1alpha1.CRAPhaseA))

			// Inject drift: patch snapshot to reference workspace names the stub
			// resolver never returns (stub returns "ws-<tenant>" only).
			orig := approved.DeepCopy()
			approved.Status.WorkspaceSnapshot = []tenancyv1alpha1.WorkspaceSnapshotEntry{
				{FromWorkspace: "ws-stale-from", ToWorkspace: "ws-stale-to", SnapshotAt: metav1.Now()},
			}
			Expect(k8sClient.Status().Patch(ctx, &approved, client.MergeFrom(orig))).To(Succeed())

			// Reconcile: drift detector fires.
			recorder := &craCapturingRecorder{}
			r.Recorder = recorder
			req := reconcile.Request{NamespacedName: craNSN(name)}
			_, err := r.Reconcile(ctx, req)
			Expect(err).NotTo(HaveOccurred())

			Expect(recorder.hasReason(ReasonWorkspaceSnapshotDrift)).To(BeTrue(),
				"expected WorkspaceSnapshotDrift event when snapshot diverges from selector")
		})
	})

	// -------------------------------------------------------------------------
	// Spec 7 — Expiry transition
	// -------------------------------------------------------------------------
	Describe("Expiry transition", func() {
		It("transitions to Expired and deletes ReBAC tuples when expiresAt is in the past", func() {
			name := fmt.Sprintf("cra-expiry-%d", GinkgoRandomSeed())
			fromT, toT := fmt.Sprintf("tf-exp-%d", GinkgoRandomSeed()), fmt.Sprintf("tt-exp-%d", GinkgoRandomSeed())
			cra := makeCRA(name, fromT, toT)
			// Set expiresAt 1 second in the past.
			cra.Spec.ExpiresAt = time.Now().UTC().Add(-1 * time.Second).Format(time.RFC3339)
			Expect(k8sClient.Create(ctx, cra)).To(Succeed())
			DeferCleanup(func() { _ = forceDeleteCRA(name) })

			fakeRebac := &FakeRebacWriter{}
			r := makeCRAReconciler(fakeRebac, nil, nil, nil)
			req := reconcile.Request{NamespacedName: craNSN(name)}

			// Initialise: phase = Pending. Since phase is Pending, expiry check is
			// skipped (only fires on Approved). We manually advance to Approved with
			// a frozen snapshot, then reconcile to trigger the expiry path.
			_, err := r.Reconcile(ctx, req)
			Expect(err).NotTo(HaveOccurred())

			// Manually set phase = Approved + inject frozen snapshot via status patch.
			var current tenancyv1alpha1.CrossTenantAgreement
			Expect(k8sClient.Get(ctx, craNSN(name), &current)).To(Succeed())
			orig := current.DeepCopy()
			current.Status.Phase = tenancyv1alpha1.CRAPhaseA
			current.Status.WorkspaceSnapshot = []tenancyv1alpha1.WorkspaceSnapshotEntry{
				{FromWorkspace: "ws-" + fromT, ToWorkspace: "ws-" + toT, SnapshotAt: metav1.Now()},
			}
			Expect(k8sClient.Status().Patch(ctx, &current, client.MergeFrom(orig))).To(Succeed())

			recorder := &craCapturingRecorder{}
			r.Recorder = recorder
			_, err = r.Reconcile(ctx, req)
			Expect(err).NotTo(HaveOccurred())

			var fresh tenancyv1alpha1.CrossTenantAgreement
			Expect(k8sClient.Get(ctx, craNSN(name), &fresh)).To(Succeed())
			Expect(fresh.Status.Phase).To(Equal(tenancyv1alpha1.CRAPhaseE),
				"phase must be Expired when expiresAt is in the past")

			Expect(fakeRebac.Deleted).NotTo(BeEmpty(),
				"ReBAC tuples must be deleted on expiry transition")

			Expect(recorder.hasReason(ReasonCRAExpired)).To(BeTrue(),
				"expected CRAExpired event on expiry transition")
		})
	})

	// -------------------------------------------------------------------------
	// Spec 8 — Conflict detection
	// -------------------------------------------------------------------------
	Describe("Conflict detection", func() {
		It("emits CRAConflict and moves to Rejected when an Approved CRA covers the same tenant pair", func() {
			fromT := fmt.Sprintf("tf-conflict-%d", GinkgoRandomSeed())
			toT := fmt.Sprintf("tt-conflict-%d", GinkgoRandomSeed())

			// Pre-existing Approved CRA for the same tenant pair.
			existingName := fmt.Sprintf("cra-existing-%d", GinkgoRandomSeed())
			existing := makeCRA(existingName, fromT, toT)
			Expect(k8sClient.Create(ctx, existing)).To(Succeed())
			DeferCleanup(func() { _ = forceDeleteCRA(existingName) })

			// Wait for the manager's reconciler to settle (status phase set), then
			// atomically force status.phase=Approved via Status().Patch with retry on
			// conflict. Without this Eventually, the manager races with the test's
			// status patch and may overwrite our Approved assertion with its own
			// Pending patch (MergeFrom captures a stale base).
			Eventually(func(g Gomega) {
				var fresh tenancyv1alpha1.CrossTenantAgreement
				g.Expect(k8sClient.Get(ctx, craNSN(existingName), &fresh)).To(Succeed())
				g.Expect(fresh.Status.Phase).NotTo(Equal(tenancyv1alpha1.CrossTenantAgreementPhase("")),
					"manager must have reconciled existing CRA and set an initial phase")
				orig := fresh.DeepCopy()
				fresh.Status.Phase = tenancyv1alpha1.CRAPhaseA
				g.Expect(k8sClient.Status().Patch(ctx, &fresh, client.MergeFrom(orig))).To(Succeed())
				// Re-fetch and assert the patch stuck (manager could re-overwrite; this
				// Eventually keeps retrying until the Approved phase is persisted long
				// enough for the conflict-detection assertion below to see it).
				g.Expect(k8sClient.Get(ctx, craNSN(existingName), &fresh)).To(Succeed())
				g.Expect(fresh.Status.Phase).To(Equal(tenancyv1alpha1.CRAPhaseA))
			}, "5s", "100ms").Should(Succeed())

			// New CRA covering the same pair — should conflict.
			conflictName := fmt.Sprintf("cra-conflict-%d", GinkgoRandomSeed())
			conflicting := makeCRA(conflictName, fromT, toT)
			Expect(k8sClient.Create(ctx, conflicting)).To(Succeed())
			DeferCleanup(func() { _ = forceDeleteCRA(conflictName) })

			// Manager's reconciler runs concurrently against the watch. Wait for
			// it to persist the conflict-rejection transition via status conditions
			// (Ready: False, Reason: Conflict), which the manual reconcile path
			// below also produces idempotently. Assert on persisted status, not on
			// a capturing recorder — because the manager's recorder (set in
			// suite_test.go) may win the race and emit the event first, meaning
			// the test's own recorder never sees it.
			req := reconcile.Request{NamespacedName: craNSN(conflictName)}
			r := makeCRAReconciler(nil, nil, nil, nil)
			// Also drive a manual reconcile so the test exercises the full path
			// even if the manager raced ahead (harmless: conflict detection is
			// idempotent past terminal phase).
			_, _ = r.Reconcile(ctx, req)
			_, _ = r.Reconcile(ctx, req)

			Eventually(func(g Gomega) {
				var fresh tenancyv1alpha1.CrossTenantAgreement
				g.Expect(k8sClient.Get(ctx, craNSN(conflictName), &fresh)).To(Succeed())
				g.Expect(fresh.Status.Phase).To(Equal(tenancyv1alpha1.CRAPhaseR),
					"phase must be Rejected on conflict")
				ready := findCRACondition(fresh.Status.Conditions, "Ready")
				g.Expect(ready).NotTo(BeNil(), "Ready condition must be set")
				g.Expect(ready.Status).To(Equal(metav1.ConditionFalse))
				g.Expect(ready.Reason).To(Equal("Conflict"),
					"Ready condition Reason must be Conflict when an Approved CRA covers the same tenant pair")
			}, "5s", "100ms").Should(Succeed())
		})
	})

	// -------------------------------------------------------------------------
	// Spec 9 — Out-of-band tuple (pre-existing tuple idempotency)
	// -------------------------------------------------------------------------
	Describe("Out-of-band tuple idempotency", func() {
		It("is a no-op and reaches Approved even when ReBAC Sync is called for already-present tuples", func() {
			// The FakeRebacWriter.Sync is idempotent by contract: calling it with
			// tuples that are already present appends duplicates but returns nil.
			// This spec verifies that an Approved CRA reconciles stably when the
			// ReBAC tuples were written out-of-band before the approval transition.
			name := fmt.Sprintf("cra-oob-%d", GinkgoRandomSeed())
			fromT, toT := fmt.Sprintf("tf-oob-%d", GinkgoRandomSeed()), fmt.Sprintf("tt-oob-%d", GinkgoRandomSeed())
			cra := makeCRA(name, fromT, toT)
			Expect(k8sClient.Create(ctx, cra)).To(Succeed())
			DeferCleanup(func() { _ = forceDeleteCRA(name) })

			fakeRebac := &FakeRebacWriter{}
			// Pre-populate tuples to simulate an out-of-band write before approval.
			fakeRebac.Synced = append(fakeRebac.Synced,
				craAllowsMessagingTuple(fromT, toT),
				RebacTuple{Object: "workspace:ws-" + toT, Relation: "messageable_from", User: "workspace:ws-" + fromT},
			)

			r := makeCRAReconciler(fakeRebac, nil, nil, nil)

			driveToApproved(name, fromT, toT, string(tenancyv1alpha1.SignatureTypeSAToken), r)

			var fresh tenancyv1alpha1.CrossTenantAgreement
			Expect(k8sClient.Get(ctx, craNSN(name), &fresh)).To(Succeed())
			Expect(fresh.Status.Phase).To(Equal(tenancyv1alpha1.CRAPhaseA),
				"must reach Approved even when tuples were already present (out-of-band write)")

			// Sync must have been called again (idempotent duplicate is safe).
			// The initial pre-populated tuples + the new sync = len > 2.
			Expect(len(fakeRebac.Synced)).To(BeNumerically(">", 2))
		})
	})
})
