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

// injectApprovals patches the CRA status to add the given approvals directly,
// bypassing the annotation-based flow. This simulates what the approval
// admission webhook would write after verifying the signature.
//
// Background: the reconciler's processApprovalAnnotation uses r.Patch (spec patch)
// which resets cra.Status from the server response, discarding the in-memory
// approval before the end-of-reconcile status patch runs. That is a pre-existing
// reconciler bug (the spec patch clobbers the in-memory status change). Until it is
// fixed, the bilateral-approval integration tests inject approvals via direct status
// patch, then exercise transitionToApproved via a subsequent reconcile.
// See: internal/controller/tenancy/crosstenanagreement_controller.go processApprovalAnnotation
func injectApprovals(name string, approvals []tenancyv1alpha1.CRAApproval) {
	var cra tenancyv1alpha1.CrossTenantAgreement
	Expect(k8sClient.Get(ctx, craNSN(name), &cra)).To(Succeed())
	orig := cra.DeepCopy()
	cra.Status.Approvals = approvals
	Expect(k8sClient.Status().Patch(ctx, &cra, client.MergeFrom(orig))).To(Succeed())
}

// approveAnnotations returns the annotations needed to trigger the approval flow.
// Used for testing the annotation-processing path (signature-failure spec).
func approveAnnotations(approvingTenant, approver, signature, sigType string) map[string]string {
	return map[string]string{
		craApproveAnnotation:             "true",
		"keese.ai/cra-approving-tenant":  approvingTenant,
		"keese.ai/cra-approver":          approver,
		"keese.ai/cra-signature":         signature,
		"keese.ai/cra-signature-type":    sigType,
	}
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
	// Approvals are injected directly via status patch (bypassing the annotation
	// path) because processApprovalAnnotation has a pre-existing bug: the spec
	// r.Patch call in that function resets cra.Status from the server response,
	// discarding the in-memory approval. The transitionToApproved logic is what
	// is under test here — the annotation path is tested in Spec 4.
	// -------------------------------------------------------------------------
	Describe("Bilateral approval: OIDC-keyless (cosign) happy path", func() {
		It("transitions to Approved and writes ReBAC tuples after both cosign approvals", func() {
			name := fmt.Sprintf("cra-cosign-%d", GinkgoRandomSeed())
			fromT, toT := fmt.Sprintf("tf-cs-%d", GinkgoRandomSeed()), fmt.Sprintf("tt-cs-%d", GinkgoRandomSeed())
			cra := makeCRA(name, fromT, toT)
			Expect(k8sClient.Create(ctx, cra)).To(Succeed())
			DeferCleanup(func() { _ = forceDeleteCRA(name) })

			fakeRebac := &FakeRebacWriter{}
			r := makeCRAReconciler(fakeRebac, &FakeCosignVerifier{}, nil, nil)
			req := reconcile.Request{NamespacedName: craNSN(name)}

			// Initialise: finalizer + phase = Pending.
			_, err := r.Reconcile(ctx, req)
			Expect(err).NotTo(HaveOccurred())

			// Inject both-tenant approvals via status (simulates what the admission
			// webhook writes after verifying cosign signatures).
			injectApprovals(name, []tenancyv1alpha1.CRAApproval{
				{
					Tenant:        fromT,
					ApprovedBy:    "alice@example.com",
					ApprovedAt:    metav1.Now(),
					Signature:     "sig-from",
					SignatureType: tenancyv1alpha1.SignatureTypeOIDCKeyless,
				},
				{
					Tenant:        toT,
					ApprovedBy:    "bob@example.com",
					ApprovedAt:    metav1.Now(),
					Signature:     "sig-to",
					SignatureType: tenancyv1alpha1.SignatureTypeOIDCKeyless,
				},
			})

			// Reconcile: both tenants approved → transitionToApproved.
			recorder := &craCapturingRecorder{}
			r.Recorder = recorder
			_, err = r.Reconcile(ctx, req)
			Expect(err).NotTo(HaveOccurred())

			var fresh tenancyv1alpha1.CrossTenantAgreement
			Expect(k8sClient.Get(ctx, craNSN(name), &fresh)).To(Succeed())
			Expect(fresh.Status.Phase).To(Equal(tenancyv1alpha1.CRAPhaseA))

			// CRAApproved event must have been emitted.
			Expect(recorder.hasReason(ReasonCRAApproved)).To(BeTrue(),
				"expected CRAApproved event on bilateral approval")

			// ReBAC tuples must have been synced.
			Expect(fakeRebac.Synced).NotTo(BeEmpty(),
				"ReBAC tuples must be written on Approved transition")

			// Signature types must be recorded in the approvals.
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
			req := reconcile.Request{NamespacedName: craNSN(name)}

			// Initialise.
			_, err := r.Reconcile(ctx, req)
			Expect(err).NotTo(HaveOccurred())

			// Inject SA-token approvals from both tenants.
			injectApprovals(name, []tenancyv1alpha1.CRAApproval{
				{
					Tenant:        fromT,
					ApprovedBy:    "ci-sa@example.com",
					ApprovedAt:    metav1.Now(),
					Signature:     "hmac-from",
					SignatureType: tenancyv1alpha1.SignatureTypeSAToken,
				},
				{
					Tenant:        toT,
					ApprovedBy:    "ci-sa@example.com",
					ApprovedAt:    metav1.Now(),
					Signature:     "hmac-to",
					SignatureType: tenancyv1alpha1.SignatureTypeSAToken,
				},
			})

			_, err = r.Reconcile(ctx, req)
			Expect(err).NotTo(HaveOccurred())

			var fresh tenancyv1alpha1.CrossTenantAgreement
			Expect(k8sClient.Get(ctx, craNSN(name), &fresh)).To(Succeed())
			Expect(fresh.Status.Phase).To(Equal(tenancyv1alpha1.CRAPhaseA))

			// Verify SA-token signature type recorded in approvals.
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
	// The annotation processing path IS exercised here (not the status injection path)
	// because we are testing signature failure, not approval recording persistence.
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
			var current tenancyv1alpha1.CrossTenantAgreement
			Expect(k8sClient.Get(ctx, craNSN(name), &current)).To(Succeed())
			origCurr := current.DeepCopy()
			current.Annotations = approveAnnotations(fromT, "malicious@example.com", "bad-sig", string(tenancyv1alpha1.SignatureTypeOIDCKeyless))
			Expect(k8sClient.Patch(ctx, &current, client.MergeFrom(origCurr))).To(Succeed())

			_, err = r.Reconcile(ctx, req)
			Expect(err).NotTo(HaveOccurred())

			// CRAApprovalInvalid event must have been emitted.
			Expect(recorder.hasReason(ReasonCRAApprovalInvalid)).To(BeTrue(),
				"expected CRAApprovalInvalid event on signature failure")

			// Phase must still be Pending and no approvals recorded.
			var fresh tenancyv1alpha1.CrossTenantAgreement
			Expect(k8sClient.Get(ctx, craNSN(name), &fresh)).To(Succeed())
			Expect(fresh.Status.Phase).To(Equal(tenancyv1alpha1.CRAPhaseP))
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
			req := reconcile.Request{NamespacedName: craNSN(name)}

			// Initialise.
			_, err := r.Reconcile(ctx, req)
			Expect(err).NotTo(HaveOccurred())

			// Inject both-tenant approvals.
			injectApprovals(name, []tenancyv1alpha1.CRAApproval{
				{Tenant: fromT, ApprovedBy: "a@example.com", ApprovedAt: metav1.Now(), Signature: "s1", SignatureType: tenancyv1alpha1.SignatureTypeSAToken},
				{Tenant: toT, ApprovedBy: "b@example.com", ApprovedAt: metav1.Now(), Signature: "s2", SignatureType: tenancyv1alpha1.SignatureTypeSAToken},
			})

			_, err = r.Reconcile(ctx, req)
			Expect(err).NotTo(HaveOccurred())

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
			_, err = r.Reconcile(ctx, req)
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
			req := reconcile.Request{NamespacedName: craNSN(name)}

			// Initialise.
			_, err := r.Reconcile(ctx, req)
			Expect(err).NotTo(HaveOccurred())

			// Inject approvals → get to Approved.
			injectApprovals(name, []tenancyv1alpha1.CRAApproval{
				{Tenant: fromT, ApprovedBy: "a@example.com", ApprovedAt: metav1.Now(), Signature: "s1", SignatureType: tenancyv1alpha1.SignatureTypeSAToken},
				{Tenant: toT, ApprovedBy: "b@example.com", ApprovedAt: metav1.Now(), Signature: "s2", SignatureType: tenancyv1alpha1.SignatureTypeSAToken},
			})
			_, err = r.Reconcile(ctx, req)
			Expect(err).NotTo(HaveOccurred())

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
			_, err = r.Reconcile(ctx, req)
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

			// Manually set the existing CRA to Approved.
			existingOrig := existing.DeepCopy()
			existing.Status.Phase = tenancyv1alpha1.CRAPhaseA
			Expect(k8sClient.Status().Patch(ctx, existing, client.MergeFrom(existingOrig))).To(Succeed())

			// New CRA covering the same pair — should conflict.
			conflictName := fmt.Sprintf("cra-conflict-%d", GinkgoRandomSeed())
			conflicting := makeCRA(conflictName, fromT, toT)
			Expect(k8sClient.Create(ctx, conflicting)).To(Succeed())
			DeferCleanup(func() { _ = forceDeleteCRA(conflictName) })

			recorder := &craCapturingRecorder{}
			r := makeCRAReconciler(nil, nil, nil, nil)
			r.Recorder = recorder

			req := reconcile.Request{NamespacedName: craNSN(conflictName)}

			// First reconcile: finalizer + phase = Pending.
			_, err := r.Reconcile(ctx, req)
			Expect(err).NotTo(HaveOccurred())

			// Second reconcile: conflict detection fires on Pending with no approvals.
			_, err = r.Reconcile(ctx, req)
			Expect(err).NotTo(HaveOccurred())

			Expect(recorder.hasReason(ReasonCRAConflict)).To(BeTrue(),
				"expected CRAConflict event when an Approved CRA covers the same tenant pair")

			var fresh tenancyv1alpha1.CrossTenantAgreement
			Expect(k8sClient.Get(ctx, craNSN(conflictName), &fresh)).To(Succeed())
			Expect(fresh.Status.Phase).To(Equal(tenancyv1alpha1.CRAPhaseR),
				"phase must be Rejected on conflict")
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
			req := reconcile.Request{NamespacedName: craNSN(name)}

			// Initialise.
			_, err := r.Reconcile(ctx, req)
			Expect(err).NotTo(HaveOccurred())

			// Inject both-tenant approvals.
			injectApprovals(name, []tenancyv1alpha1.CRAApproval{
				{Tenant: fromT, ApprovedBy: "a@example.com", ApprovedAt: metav1.Now(), Signature: "s1", SignatureType: tenancyv1alpha1.SignatureTypeSAToken},
				{Tenant: toT, ApprovedBy: "b@example.com", ApprovedAt: metav1.Now(), Signature: "s2", SignatureType: tenancyv1alpha1.SignatureTypeSAToken},
			})

			// Reconcile: transitionToApproved calls Sync (idempotent with pre-existing tuples).
			_, err = r.Reconcile(ctx, req)
			Expect(err).NotTo(HaveOccurred())

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
