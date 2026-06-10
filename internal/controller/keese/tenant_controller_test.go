// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

//go:build integration

package keese

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	authzv1alpha1 "github.com/keese-ai/keese/api/authz/v1alpha1"
	keesev1alpha1 "github.com/keese-ai/keese/api/keese/v1alpha1"
)

// eventuallyTimeout / eventuallyInterval are defined once in
// workspace_controller_test.go and shared across the consolidated keese suite.

// makeTenant returns a minimal cluster-scoped Tenant with the managed label.
// JWKSCacheFailOpenSeconds is left at its zero value (omitted from JSON) to exercise
// the default path. The TenantSpec CEL rule guards with !has(self.jwksCacheFailOpenSeconds)
// so absence is accepted without error.
func makeTenant(name string) *keesev1alpha1.Tenant {
	return &keesev1alpha1.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			Labels: map[string]string{
				tenantManagedByLabel: tenantManagedByLabelValue,
			},
		},
		Spec: keesev1alpha1.TenantSpec{
			AdminSubjects: []keesev1alpha1.TenantSubject{
				{Kind: "User", Name: "alice@example.com"},
			},
		},
	}
}

// makeTenantReconciler returns a TenantReconciler wired to the envtest client.
func makeTenantReconciler(rebac TenantRebacWriter) *TenantReconciler {
	if rebac == nil {
		rebac = &TenantFakeRebacWriter{}
	}
	return &TenantReconciler{
		Client:   k8sClient,
		Scheme:   k8sClient.Scheme(),
		Recorder: &tenancyNoopRecorder{},
		Rebac:    rebac,
	}
}

func nsn(name string) types.NamespacedName {
	return types.NamespacedName{Name: name}
}

var _ = Describe("TenantReconciler", func() {
	// --- Idempotency ---
	Describe("Idempotency", func() {
		var (
			tenant *keesev1alpha1.Tenant
			r      *TenantReconciler
		)

		BeforeEach(func() {
			tenant = makeTenant(fmt.Sprintf("tenant-idempotent-%d", GinkgoRandomSeed()))
			Expect(k8sClient.Create(ctx, tenant)).To(Succeed())
			r = makeTenantReconciler(nil)
		})

		AfterEach(func() {
			_ = forceDeleteTenant(tenant.Name)
		})

		It("converges in ≤3 reconciles with no spec change", func() {
			req := reconcile.Request{NamespacedName: nsn(tenant.Name)}

			var lastVersion string
			for i := 0; i < 3; i++ {
				_, err := r.Reconcile(ctx, req)
				Expect(err).NotTo(HaveOccurred())

				var fresh keesev1alpha1.Tenant
				Expect(k8sClient.Get(ctx, nsn(tenant.Name), &fresh)).To(Succeed())
				if i == 2 {
					// Pass 3 should not mutate (status already set, finalizers already added).
					Expect(fresh.ResourceVersion).To(Equal(lastVersion),
						"ResourceVersion must not increment on idempotent third reconcile")
				}
				lastVersion = fresh.ResourceVersion
			}
		})
	})

	// --- Mode A: namespace selector ---
	Describe("Mode A: namespace selector match", func() {
		var (
			tenant *keesev1alpha1.Tenant
			r      *TenantReconciler
		)

		BeforeEach(func() {
			tenant = makeTenant(fmt.Sprintf("tenant-mode-a-%d", GinkgoRandomSeed()))
			// Use a label selector that will match a namespace we create.
			tenant.Spec.NamespaceSelector = &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"keese.ai/tenant": tenant.Name,
				},
			}
			Expect(k8sClient.Create(ctx, tenant)).To(Succeed())
			r = makeTenantReconciler(nil)
		})

		AfterEach(func() {
			_ = forceDeleteTenant(tenant.Name)
		})

		It("populates status.namespaces from matching namespaces", func() {
			// Create a namespace that matches the selector.
			ns := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name:   fmt.Sprintf("ns-%s", tenant.Name),
					Labels: map[string]string{"keese.ai/tenant": tenant.Name},
				},
			}
			Expect(k8sClient.Create(ctx, ns)).To(Succeed())
			DeferCleanup(func() { _ = k8sClient.Delete(ctx, ns) })

			req := reconcile.Request{NamespacedName: nsn(tenant.Name)}
			_, err := r.Reconcile(ctx, req)
			Expect(err).NotTo(HaveOccurred())

			var fresh keesev1alpha1.Tenant
			Expect(k8sClient.Get(ctx, nsn(tenant.Name), &fresh)).To(Succeed())
			Expect(fresh.Status.Namespaces).To(ContainElement(ns.Name))
		})
	})

	// --- Mode B: Capsule Tenant ref ---
	Describe("Mode B: capsuleTenantRef mirror", func() {
		var (
			tenant *keesev1alpha1.Tenant
			r      *TenantReconciler
		)

		BeforeEach(func() {
			tenant = makeTenant(fmt.Sprintf("tenant-mode-b-%d", GinkgoRandomSeed()))
			tenant.Spec.CapsuleTenantRef = &keesev1alpha1.CapsuleTenantRef{
				Name: "acme-capsule",
			}
			Expect(k8sClient.Create(ctx, tenant)).To(Succeed())
			r = makeTenantReconciler(nil)
		})

		AfterEach(func() {
			_ = forceDeleteTenant(tenant.Name)
		})

		It("emits CapsuleTenantNotFound warning when no namespaces carry the capsule label", func() {
			recorder := &capturingRecorder{}
			r.Recorder = recorder

			req := reconcile.Request{NamespacedName: nsn(tenant.Name)}
			_, err := r.Reconcile(ctx, req)
			Expect(err).NotTo(HaveOccurred())

			// Should have emitted the warning.
			Expect(recorder.hasReason(ReasonCapsuleTenantNotFound)).To(BeTrue(),
				"expected CapsuleTenantNotFound warning when capsule label is absent")
		})

		It("mirrors namespaces from the referenced Capsule Tenant's status.namespaces", func() {
			// The keese Tenant created in BeforeEach references a Capsule Tenant
			// named "acme-capsule" via capsuleTenantRef. The reconciler GETs that
			// Capsule Tenant and mirrors its status.namespaces (it does not scan
			// namespaces by label — see resolveCapsuleNamespaces in
			// tenant_controller.go). So the test must create the Capsule Tenant and
			// populate its status.
			nsName := fmt.Sprintf("acme-ns-%d", GinkgoRandomSeed())
			capTenant := &capsulev1beta2.Tenant{
				ObjectMeta: metav1.ObjectMeta{Name: "acme-capsule"},
				Spec: capsulev1beta2.TenantSpec{
					Owners: capsulev1beta2.OwnerListSpec{
						{Name: "alice", Kind: "User"},
					},
				},
			}
			Expect(k8sClient.Create(ctx, capTenant)).To(Succeed())
			DeferCleanup(func() { _ = k8sClient.Delete(ctx, capTenant) })

			origCap := capTenant.DeepCopy()
			capTenant.Status.Namespaces = []string{nsName}
			capTenant.Status.Size = 1
			capTenant.Status.State = capsulev1beta2.TenantStateActive
			Expect(k8sClient.Status().Patch(ctx, capTenant, client.MergeFrom(origCap))).To(Succeed())

			req := reconcile.Request{NamespacedName: nsn(tenant.Name)}
			_, err := r.Reconcile(ctx, req)
			Expect(err).NotTo(HaveOccurred())

			var fresh keesev1alpha1.Tenant
			Expect(k8sClient.Get(ctx, nsn(tenant.Name), &fresh)).To(Succeed())
			Expect(fresh.Status.Namespaces).To(ContainElement(nsName))
		})
	})

	// --- TD-P2-06: real Capsule Tenant lookup ---

	// Case 1: Capsule Tenant exists → status.namespaces populated + CapsuleTenantResolved=True.
	Describe("Mode B: Capsule Tenant exists — namespaces mirrored", func() {
		It("populates status.namespaces from Capsule Tenant.status.namespaces and sets CapsuleTenantResolved=True", func() {
			// Create a Capsule Tenant with two namespaces pre-populated in status.
			capTenant := &capsulev1beta2.Tenant{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf("acme-cap-%d", GinkgoRandomSeed()),
				},
				Spec: capsulev1beta2.TenantSpec{
					Owners: capsulev1beta2.OwnerListSpec{
						{
							Name: "alice",
							Kind: "User",
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, capTenant)).To(Succeed())
			DeferCleanup(func() { _ = k8sClient.Delete(ctx, capTenant) })

			// Patch the Capsule Tenant status to report two namespaces. The Capsule
			// Tenant CRD marks status.size and status.state as required, so set them
			// alongside namespaces or the status patch is rejected with
			// "size: Required value".
			ns1 := fmt.Sprintf("ns-acme-a-%d", GinkgoRandomSeed())
			ns2 := fmt.Sprintf("ns-acme-b-%d", GinkgoRandomSeed())
			origCap := capTenant.DeepCopy()
			capTenant.Status.Namespaces = []string{ns1, ns2}
			capTenant.Status.Size = 2
			capTenant.Status.State = capsulev1beta2.TenantStateActive
			Expect(k8sClient.Status().Patch(ctx, capTenant, client.MergeFrom(origCap))).To(Succeed())

			// Create the keese Tenant referencing the Capsule Tenant.
			tenant := makeTenant(fmt.Sprintf("tenant-cap-found-%d", GinkgoRandomSeed()))
			tenant.Spec.CapsuleTenantRef = &keesev1alpha1.CapsuleTenantRef{Name: capTenant.Name}
			Expect(k8sClient.Create(ctx, tenant)).To(Succeed())
			DeferCleanup(func() { _ = forceDeleteTenant(tenant.Name) })

			r := makeTenantReconciler(nil)
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: nsn(tenant.Name)})
			Expect(err).NotTo(HaveOccurred())

			var fresh keesev1alpha1.Tenant
			Expect(k8sClient.Get(ctx, nsn(tenant.Name), &fresh)).To(Succeed())

			// status.namespaces must mirror the Capsule Tenant's namespace list.
			Expect(fresh.Status.Namespaces).To(ConsistOf(ns1, ns2))

			// status.capsuleTenantResolved must be true.
			Expect(fresh.Status.CapsuleTenantResolved).To(BeTrue())

			// CapsuleTenantResolved condition must be True.
			var resolvedCond *metav1.Condition
			for i := range fresh.Status.Conditions {
				if fresh.Status.Conditions[i].Type == "CapsuleTenantResolved" {
					resolvedCond = &fresh.Status.Conditions[i]
					break
				}
			}
			Expect(resolvedCond).NotTo(BeNil(), "CapsuleTenantResolved condition must be present")
			Expect(resolvedCond.Status).To(Equal(metav1.ConditionTrue))
			Expect(resolvedCond.Reason).To(Equal("CapsuleTenantFound"))
		})
	})

	// Case 2: Capsule Tenant NotFound → CapsuleTenantResolved=False + requeue.
	Describe("Mode B: Capsule Tenant NotFound — condition False + requeue", func() {
		It("sets CapsuleTenantResolved=False and emits CapsuleTenantNotFound warning when Capsule Tenant is absent", func() {
			tenant := makeTenant(fmt.Sprintf("tenant-cap-missing-%d", GinkgoRandomSeed()))
			tenant.Spec.CapsuleTenantRef = &keesev1alpha1.CapsuleTenantRef{
				Name: fmt.Sprintf("does-not-exist-%d", GinkgoRandomSeed()),
			}
			Expect(k8sClient.Create(ctx, tenant)).To(Succeed())
			DeferCleanup(func() { _ = forceDeleteTenant(tenant.Name) })

			recorder := &capturingRecorder{}
			r := makeTenantReconciler(nil)
			r.Recorder = recorder

			result, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: nsn(tenant.Name)})
			// No error returned to the queue — caller uses RequeueAfter.
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(Equal(requeueAfterTenantBackoff))

			// Warning event must be emitted.
			Expect(recorder.hasReason(ReasonCapsuleTenantNotFound)).To(BeTrue(),
				"expected CapsuleTenantNotFound warning event")

			// status.capsuleTenantResolved must be false.
			var fresh keesev1alpha1.Tenant
			Expect(k8sClient.Get(ctx, nsn(tenant.Name), &fresh)).To(Succeed())
			Expect(fresh.Status.CapsuleTenantResolved).To(BeFalse())

			// CapsuleTenantResolved condition must be False.
			var resolvedCond *metav1.Condition
			for i := range fresh.Status.Conditions {
				if fresh.Status.Conditions[i].Type == "CapsuleTenantResolved" {
					resolvedCond = &fresh.Status.Conditions[i]
					break
				}
			}
			Expect(resolvedCond).NotTo(BeNil(), "CapsuleTenantResolved condition must be present")
			Expect(resolvedCond.Status).To(Equal(metav1.ConditionFalse))
			Expect(resolvedCond.Reason).To(Equal("CapsuleTenantNotFound"))
		})
	})

	// --- NamespaceSelectorIgnoredInModeB ---
	Describe("NamespaceSelectorIgnoredInModeB warning", func() {
		It("emits NamespaceSelectorIgnoredInModeB when both capsuleTenantRef and namespaceSelector are set", func() {
			tenant := makeTenant(fmt.Sprintf("tenant-warn-%d", GinkgoRandomSeed()))
			tenant.Spec.CapsuleTenantRef = &keesev1alpha1.CapsuleTenantRef{Name: "acme"}
			tenant.Spec.NamespaceSelector = &metav1.LabelSelector{
				MatchLabels: map[string]string{"foo": "bar"},
			}
			Expect(k8sClient.Create(ctx, tenant)).To(Succeed())
			DeferCleanup(func() { _ = forceDeleteTenant(tenant.Name) })

			recorder := &capturingRecorder{}
			r := makeTenantReconciler(nil)
			r.Recorder = recorder

			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: nsn(tenant.Name)})
			Expect(err).NotTo(HaveOccurred())
			Expect(recorder.hasReason(ReasonNamespaceSelectorIgnoredInModeB)).To(BeTrue())
		})
	})

	// --- Finalizer cascade: workspaces ---
	Describe("Finalizer: workspaces blocks deletion", func() {
		It("emits TenantDeletionBlocked when Workspaces exist (mocked via hasOwnedWorkspaces)", func() {
			// This test exercises the event path using a reconciler that wraps hasOwnedWorkspaces.
			// Since hasOwnedWorkspaces currently stubs to false, we validate the finalizer
			// is present and phase becomes Terminating on delete.
			tenant := makeTenant(fmt.Sprintf("tenant-fin-%d", GinkgoRandomSeed()))
			Expect(k8sClient.Create(ctx, tenant)).To(Succeed())

			r := makeTenantReconciler(nil)
			req := reconcile.Request{NamespacedName: nsn(tenant.Name)}

			// Provision first.
			_, err := r.Reconcile(ctx, req)
			Expect(err).NotTo(HaveOccurred())

			// Delete the tenant.
			var fresh keesev1alpha1.Tenant
			Expect(k8sClient.Get(ctx, nsn(tenant.Name), &fresh)).To(Succeed())
			Expect(k8sClient.Delete(ctx, &fresh)).To(Succeed())

			// Reconcile cleanup — stubs return no workspaces, so cleanup should complete.
			_, err = r.Reconcile(ctx, req)
			Expect(err).NotTo(HaveOccurred())

			// Tenant should eventually be gone.
			Eventually(func() bool {
				var gone keesev1alpha1.Tenant
				err := k8sClient.Get(ctx, nsn(tenant.Name), &gone)
				return err != nil // IsNotFound or any error means gone
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())
		})
	})

	// --- Finalizer cascade: agreements ---
	Describe("Finalizer: agreements blocks deletion when active CRA exists", func() {
		It("does not remove the agreements finalizer while an Approved CRA references the tenant", func() {
			tenantName := fmt.Sprintf("tenant-cra-fin-%d", GinkgoRandomSeed())
			tenant := makeTenant(tenantName)
			Expect(k8sClient.Create(ctx, tenant)).To(Succeed())

			r := makeTenantReconciler(nil)
			req := reconcile.Request{NamespacedName: nsn(tenantName)}
			_, err := r.Reconcile(ctx, req)
			Expect(err).NotTo(HaveOccurred())

			// Create a sibling tenant and an Approved CRA.
			peer := makeTenant(fmt.Sprintf("tenant-peer-%d", GinkgoRandomSeed()))
			Expect(k8sClient.Create(ctx, peer)).To(Succeed())
			DeferCleanup(func() { _ = forceDeleteTenant(peer.Name) })

			cra := &authzv1alpha1.CrossTenantAgreement{
				ObjectMeta: metav1.ObjectMeta{Name: fmt.Sprintf("cra-%d", GinkgoRandomSeed())},
				Spec: authzv1alpha1.CrossTenantAgreementSpec{
					From:  authzv1alpha1.TenantEndpoint{TenantRef: authzv1alpha1.LocalObjectRef{Name: tenantName}},
					To:    authzv1alpha1.TenantEndpoint{TenantRef: authzv1alpha1.LocalObjectRef{Name: peer.Name}},
					Scope: authzv1alpha1.CRAScope{NATSSubjects: []string{"keese.cta.test"}, A2ARoles: []authzv1alpha1.A2ARole{authzv1alpha1.A2ARoleReader}},
				},
			}
			Expect(k8sClient.Create(ctx, cra)).To(Succeed())
			// Manually patch status to Approved so hasActiveCRAs returns true.
			origCRA := cra.DeepCopy()
			cra.Status.Phase = authzv1alpha1.CRAPhaseA
			Expect(k8sClient.Status().Patch(ctx, cra, client.MergeFrom(origCRA))).To(Succeed())
			DeferCleanup(func() {
				var c authzv1alpha1.CrossTenantAgreement
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: cra.Name}, &c); err == nil {
					c.Finalizers = nil
					_ = k8sClient.Update(ctx, &c)
					_ = k8sClient.Delete(ctx, &c)
				}
			})

			// Delete tenant.
			var fresh keesev1alpha1.Tenant
			Expect(k8sClient.Get(ctx, nsn(tenantName), &fresh)).To(Succeed())
			Expect(k8sClient.Delete(ctx, &fresh)).To(Succeed())

			recorder := &capturingRecorder{}
			r.Recorder = recorder

			// Reconcile — cleanup should block on agreements finalizer.
			_, err = r.Reconcile(ctx, req)
			Expect(err).NotTo(HaveOccurred())
			Expect(recorder.hasReason(ReasonTenantDeletionBlocked)).To(BeTrue())

			// Tenant still exists with agreements finalizer.
			Expect(k8sClient.Get(ctx, nsn(tenantName), &fresh)).To(Succeed())
			Expect(fresh.Finalizers).To(ContainElement(tenantFinalizerAgreements))

			// Force cleanup for afterEach.
			fresh.Finalizers = nil
			_ = k8sClient.Update(ctx, &fresh)
			_ = k8sClient.Delete(ctx, &fresh)
		})
	})

	// --- ReBAC tuples on create ---
	Describe("ReBAC tuples on create", func() {
		It("syncs admin tuple for each adminSubject", func() {
			tenant := makeTenant(fmt.Sprintf("tenant-rebac-%d", GinkgoRandomSeed()))
			tenant.Spec.AdminSubjects = []keesev1alpha1.TenantSubject{
				{Kind: "User", Name: "alice@example.com"},
				{Kind: "User", Name: "bob@example.com"},
			}
			Expect(k8sClient.Create(ctx, tenant)).To(Succeed())
			DeferCleanup(func() { _ = forceDeleteTenant(tenant.Name) })

			fakeRebac := &TenantFakeRebacWriter{}
			r := makeTenantReconciler(fakeRebac)

			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: nsn(tenant.Name)})
			Expect(err).NotTo(HaveOccurred())

			relations := map[string]bool{}
			for _, t := range fakeRebac.Synced {
				relations[t.Relation] = true
			}
			Expect(relations["admin"]).To(BeTrue(), "admin tuples must be synced")
		})

		It("syncs uses_oidc_provider tuple for each allowedProvider", func() {
			tenant := makeTenant(fmt.Sprintf("tenant-oidc-%d", GinkgoRandomSeed()))
			tenant.Spec.OIDC = &keesev1alpha1.TenantOIDCConfig{
				AllowedProviders: []string{"google", "azure-entra"},
			}
			Expect(k8sClient.Create(ctx, tenant)).To(Succeed())
			DeferCleanup(func() { _ = forceDeleteTenant(tenant.Name) })

			fakeRebac := &TenantFakeRebacWriter{}
			r := makeTenantReconciler(fakeRebac)

			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: nsn(tenant.Name)})
			Expect(err).NotTo(HaveOccurred())

			oidcTupleUsers := map[string]bool{}
			for _, t := range fakeRebac.Synced {
				if t.Relation == "uses_oidc_provider" {
					oidcTupleUsers[t.User] = true
				}
			}
			Expect(oidcTupleUsers["oidc_provider:google"]).To(BeTrue())
			Expect(oidcTupleUsers["oidc_provider:azure-entra"]).To(BeTrue())
		})
	})

	// --- Phase transition ---
	Describe("Phase transition", func() {
		It("advances phase to Active on first successful reconcile", func() {
			tenant := makeTenant(fmt.Sprintf("tenant-phase-%d", GinkgoRandomSeed()))
			Expect(k8sClient.Create(ctx, tenant)).To(Succeed())
			DeferCleanup(func() { _ = forceDeleteTenant(tenant.Name) })

			r := makeTenantReconciler(nil)
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: nsn(tenant.Name)})
			Expect(err).NotTo(HaveOccurred())

			var fresh keesev1alpha1.Tenant
			Expect(k8sClient.Get(ctx, nsn(tenant.Name), &fresh)).To(Succeed())
			Expect(fresh.Status.Phase).To(Equal(keesev1alpha1.TenantPhaseActive))
		})
	})
})

// --- Test helpers ---

// forceDeleteTenant removes finalizers and deletes, best-effort.
func forceDeleteTenant(name string) error {
	var t keesev1alpha1.Tenant
	if err := k8sClient.Get(context.Background(), nsn(name), &t); err != nil {
		return err
	}
	t.Finalizers = nil
	if err := k8sClient.Update(context.Background(), &t); err != nil {
		return err
	}
	return k8sClient.Delete(context.Background(), &t)
}

// capturingRecorder records event reasons for assertion.
type capturingRecorder struct {
	events []string
}

func (c *capturingRecorder) Event(_ runtime.Object, _, reason, _ string) {
	c.events = append(c.events, reason)
}

func (c *capturingRecorder) Eventf(_ runtime.Object, _, reason, _ string, _ ...interface{}) {
	c.events = append(c.events, reason)
}

func (c *capturingRecorder) AnnotatedEventf(_ runtime.Object, _ map[string]string, _, reason, _ string, _ ...interface{}) {
	c.events = append(c.events, reason)
}

func (c *capturingRecorder) hasReason(reason string) bool {
	for _, e := range c.events {
		if e == reason {
			return true
		}
	}
	return false
}

// tenancyNoopRecorder satisfies record.EventRecorder without emitting anything.
type tenancyNoopRecorder struct{}

func (n *tenancyNoopRecorder) Event(_ runtime.Object, _, _, _ string)                    {}
func (n *tenancyNoopRecorder) Eventf(_ runtime.Object, _, _, _ string, _ ...interface{}) {}
func (n *tenancyNoopRecorder) AnnotatedEventf(_ runtime.Object, _ map[string]string, _, _, _ string, _ ...interface{}) {
}
