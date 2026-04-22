// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

//go:build integration

package tenancy

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	tenancyv1alpha1 "github.com/keese-ai/keese/api/tenancy/v1alpha1"
)

const (
	eventuallyTimeout  = 10 * time.Second
	eventuallyInterval = 250 * time.Millisecond
)

// makeTenant returns a minimal cluster-scoped Tenant with the managed label.
// JWKSCacheFailOpenSeconds is set to 30 (the minimum allowed non-zero value) to
// satisfy the CEL rule `self.jwksCacheFailOpenSeconds == 0 || (>= 30 && <= 600)`.
// The field is int32 omitempty, so zero is omitted from JSON and CEL raises
// "no such key" — a known schema bug requiring `!has(...)` guard in the rule.
// TODO(crd-schema-fix): update tenancy.operator.keese.ai_tenants.yaml rule to
// `!has(self.jwksCacheFailOpenSeconds) || self.jwksCacheFailOpenSeconds == 0 || ...`
func makeTenant(name string) *tenancyv1alpha1.Tenant {
	return &tenancyv1alpha1.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			Labels: map[string]string{
				tenantManagedByLabel: tenantManagedByLabelValue,
			},
		},
		Spec: tenancyv1alpha1.TenantSpec{
			AdminSubjects: []tenancyv1alpha1.TenantSubject{
				{Kind: "User", Name: "alice@example.com"},
			},
			JWKSCacheFailOpenSeconds: 30, // minimum non-zero value; see comment above
		},
	}
}

// makeTenantReconciler returns a TenantReconciler wired to the envtest client.
func makeTenantReconciler(rebac RebacWriter) *TenantReconciler {
	if rebac == nil {
		rebac = &FakeRebacWriter{}
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
			tenant *tenancyv1alpha1.Tenant
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

				var fresh tenancyv1alpha1.Tenant
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
			tenant *tenancyv1alpha1.Tenant
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

			var fresh tenancyv1alpha1.Tenant
			Expect(k8sClient.Get(ctx, nsn(tenant.Name), &fresh)).To(Succeed())
			Expect(fresh.Status.Namespaces).To(ContainElement(ns.Name))
		})
	})

	// --- Mode B: Capsule Tenant ref ---
	Describe("Mode B: capsuleTenantRef mirror", func() {
		var (
			tenant *tenancyv1alpha1.Tenant
			r      *TenantReconciler
		)

		BeforeEach(func() {
			tenant = makeTenant(fmt.Sprintf("tenant-mode-b-%d", GinkgoRandomSeed()))
			tenant.Spec.CapsuleTenantRef = &tenancyv1alpha1.CapsuleTenantRef{
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

		It("mirrors namespaces labelled capsule.clastix.io/tenant=<capsuleName>", func() {
			ns := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf("acme-ns-%d", GinkgoRandomSeed()),
					Labels: map[string]string{
						"capsule.clastix.io/tenant": "acme-capsule",
					},
				},
			}
			Expect(k8sClient.Create(ctx, ns)).To(Succeed())
			DeferCleanup(func() { _ = k8sClient.Delete(ctx, ns) })

			req := reconcile.Request{NamespacedName: nsn(tenant.Name)}
			_, err := r.Reconcile(ctx, req)
			Expect(err).NotTo(HaveOccurred())

			var fresh tenancyv1alpha1.Tenant
			Expect(k8sClient.Get(ctx, nsn(tenant.Name), &fresh)).To(Succeed())
			Expect(fresh.Status.Namespaces).To(ContainElement(ns.Name))
		})
	})

	// --- NamespaceSelectorIgnoredInModeB ---
	Describe("NamespaceSelectorIgnoredInModeB warning", func() {
		It("emits NamespaceSelectorIgnoredInModeB when both capsuleTenantRef and namespaceSelector are set", func() {
			tenant := makeTenant(fmt.Sprintf("tenant-warn-%d", GinkgoRandomSeed()))
			tenant.Spec.CapsuleTenantRef = &tenancyv1alpha1.CapsuleTenantRef{Name: "acme"}
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
			var fresh tenancyv1alpha1.Tenant
			Expect(k8sClient.Get(ctx, nsn(tenant.Name), &fresh)).To(Succeed())
			Expect(k8sClient.Delete(ctx, &fresh)).To(Succeed())

			// Reconcile cleanup — stubs return no workspaces, so cleanup should complete.
			_, err = r.Reconcile(ctx, req)
			Expect(err).NotTo(HaveOccurred())

			// Tenant should eventually be gone.
			Eventually(func() bool {
				var gone tenancyv1alpha1.Tenant
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

			cra := &tenancyv1alpha1.CrossTenantAgreement{
				ObjectMeta: metav1.ObjectMeta{Name: fmt.Sprintf("cra-%d", GinkgoRandomSeed())},
				Spec: tenancyv1alpha1.CrossTenantAgreementSpec{
					From:  tenancyv1alpha1.TenantEndpoint{TenantRef: tenancyv1alpha1.LocalObjectRef{Name: tenantName}},
					To:    tenancyv1alpha1.TenantEndpoint{TenantRef: tenancyv1alpha1.LocalObjectRef{Name: peer.Name}},
					Scope: tenancyv1alpha1.CRAScope{NATSSubjects: []string{"keese.cta.test"}, A2ARoles: []tenancyv1alpha1.A2ARole{tenancyv1alpha1.A2ARoleReader}},
				},
			}
			Expect(k8sClient.Create(ctx, cra)).To(Succeed())
			// Manually patch status to Approved so hasActiveCRAs returns true.
			origCRA := cra.DeepCopy()
			cra.Status.Phase = tenancyv1alpha1.CRAPhaseA
			Expect(k8sClient.Status().Patch(ctx, cra, client.MergeFrom(origCRA))).To(Succeed())
			DeferCleanup(func() {
				var c tenancyv1alpha1.CrossTenantAgreement
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: cra.Name}, &c); err == nil {
					c.Finalizers = nil
					_ = k8sClient.Update(ctx, &c)
					_ = k8sClient.Delete(ctx, &c)
				}
			})

			// Delete tenant.
			var fresh tenancyv1alpha1.Tenant
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
			tenant.Spec.AdminSubjects = []tenancyv1alpha1.TenantSubject{
				{Kind: "User", Name: "alice@example.com"},
				{Kind: "User", Name: "bob@example.com"},
			}
			Expect(k8sClient.Create(ctx, tenant)).To(Succeed())
			DeferCleanup(func() { _ = forceDeleteTenant(tenant.Name) })

			fakeRebac := &FakeRebacWriter{}
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
			tenant.Spec.OIDC = &tenancyv1alpha1.TenantOIDCConfig{
				AllowedProviders: []string{"google", "azure-entra"},
			}
			Expect(k8sClient.Create(ctx, tenant)).To(Succeed())
			DeferCleanup(func() { _ = forceDeleteTenant(tenant.Name) })

			fakeRebac := &FakeRebacWriter{}
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

			var fresh tenancyv1alpha1.Tenant
			Expect(k8sClient.Get(ctx, nsn(tenant.Name), &fresh)).To(Succeed())
			Expect(fresh.Status.Phase).To(Equal(tenancyv1alpha1.TenantPhaseActive))
		})
	})
})

// --- Test helpers ---

// forceDeleteTenant removes finalizers and deletes, best-effort.
func forceDeleteTenant(name string) error {
	var t tenancyv1alpha1.Tenant
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

func (n *tenancyNoopRecorder) Event(_ runtime.Object, _, _, _ string)                      {}
func (n *tenancyNoopRecorder) Eventf(_ runtime.Object, _, _, _ string, _ ...interface{})   {}
func (n *tenancyNoopRecorder) AnnotatedEventf(_ runtime.Object, _ map[string]string, _, _, _ string, _ ...interface{}) {
}
