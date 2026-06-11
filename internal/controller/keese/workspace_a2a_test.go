// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

//go:build integration

package keese

import (
	"errors"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	keesev1alpha1 "github.com/keese-ai/keese/api/keese/v1alpha1"
)

// makeA2AWorkspace builds a Workspace with spec.a2a populated.
func makeA2AWorkspace(ns, name, tenant string, enabled bool, scope keesev1alpha1.WorkspaceA2AScope) *keesev1alpha1.Workspace {
	ws := &keesev1alpha1.Workspace{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
			Labels:    map[string]string{managedLabel: managedLabelValue},
		},
		Spec: keesev1alpha1.WorkspaceSpec{
			RuntimeRef:        keesev1alpha1.LocalObjectReference{Name: "default-runtime"},
			TenantRef:         corev1.ObjectReference{Name: tenant},
			SessionMode:       keesev1alpha1.WorkspaceSessionModeOnDemand,
			ConcurrencyPolicy: keesev1alpha1.ConcurrencyPolicyAllow,
			A2A: &keesev1alpha1.WorkspaceA2AConfig{
				Enabled: enabled,
				Scope:   scope,
			},
		},
	}
	return ws
}

// hasTuple reports whether the slice contains object#relation@user.
func hasTuple(tuples []WorkspaceRebacTuple, object, relation, user string) bool {
	for _, t := range tuples {
		if t.Object == object && t.Relation == relation && t.User == user {
			return true
		}
	}
	return false
}

// The A2A endpoint authz outcome (allow/deny) is the ext_authz Check over the
// a2a_callable_by tuple. Tuple PRESENT in the FGA store ⇒ Check allows; ABSENT ⇒
// Check denies (and fails closed on FGA error / missing token). Those Check
// semantics are asserted directly in internal/authz/extauth/a2a_endpoint_test.go.
// Here we assert the AUTHORITATIVE input to that Check: which tuples the
// Workspace controller writes (T4). "W1→W2 succeeds with the tuple present /
// fails after removal" is therefore expressed as "the controller wrote / did
// not write the a2a_callable_by tuple".
var _ = Describe("WorkspaceReconciler A2A (E2 T4/T6)", func() {
	var (
		nsn  types.NamespacedName
		rec  *WorkspaceFakeRebacWriter
		cta  *fakeA2ACTAResolver
		r    *WorkspaceReconciler
		name string
	)

	newReconciler := func() *WorkspaceReconciler {
		rec = &WorkspaceFakeRebacWriter{}
		cta = newFakeA2ACTAResolver()
		return &WorkspaceReconciler{
			Client:   k8sClient,
			Scheme:   k8sClient.Scheme(),
			Recorder: &noopRecorder{},
			Rebac:    rec,
			A2ACTA:   cta,
		}
	}

	deleteWS := func() {
		toDelete := &keesev1alpha1.Workspace{}
		if err := k8sClient.Get(ctx, nsn, toDelete); err == nil {
			toDelete.Finalizers = nil
			_ = k8sClient.Update(ctx, toDelete)
			_ = k8sClient.Delete(ctx, toDelete)
		}
	}

	AfterEach(func() { deleteWS() })

	Describe("intra-tenant", func() {
		It("writes the self a2a_callable_by tuple when enabled (W1→W2 same tenant succeeds)", func() {
			name = fmt.Sprintf("ws-a2a-intra-%d", GinkgoRandomSeed())
			ws := makeA2AWorkspace("default", name, "acme-tenant", true, keesev1alpha1.WorkspaceA2AScopeIntraTenant)
			Expect(k8sClient.Create(ctx, ws)).To(Succeed())
			nsn = mustNamespacedName(ws.Namespace, ws.Name)
			r = newReconciler()

			// Two reconciles: pass 1 adds the finalizer, pass 2 syncs tuples.
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: nsn})
			Expect(err).NotTo(HaveOccurred())
			_, err = r.Reconcile(ctx, reconcile.Request{NamespacedName: nsn})
			Expect(err).NotTo(HaveOccurred())

			wsObj := "workspace:" + name
			Expect(hasTuple(rec.Synced, wsObj, "a2a_callable_by", wsObj)).To(BeTrue(),
				"intra-tenant enabled workspace must write the self a2a_callable_by tuple")
		})

		It("does not write the tuple when A2A is disabled (call fails: no grant)", func() {
			name = fmt.Sprintf("ws-a2a-off-%d", GinkgoRandomSeed())
			ws := makeA2AWorkspace("default", name, "acme-tenant", false, keesev1alpha1.WorkspaceA2AScopeIntraTenant)
			Expect(k8sClient.Create(ctx, ws)).To(Succeed())
			nsn = mustNamespacedName(ws.Namespace, ws.Name)
			r = newReconciler()

			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: nsn})
			Expect(err).NotTo(HaveOccurred())
			_, err = r.Reconcile(ctx, reconcile.Request{NamespacedName: nsn})
			Expect(err).NotTo(HaveOccurred())

			wsObj := "workspace:" + name
			Expect(hasTuple(rec.Synced, wsObj, "a2a_callable_by", wsObj)).To(BeFalse(),
				"disabled A2A must not write any a2a_callable_by tuple")
		})

		It("deletes the self tuple on workspace deletion (tuple removed → call fails)", func() {
			name = fmt.Sprintf("ws-a2a-del-%d", GinkgoRandomSeed())
			ws := makeA2AWorkspace("default", name, "acme-tenant", true, keesev1alpha1.WorkspaceA2AScopeIntraTenant)
			Expect(k8sClient.Create(ctx, ws)).To(Succeed())
			nsn = mustNamespacedName(ws.Namespace, ws.Name)
			r = newReconciler()

			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: nsn})
			Expect(err).NotTo(HaveOccurred())

			// Trigger deletion: the finalizer keeps the object for cleanup.
			Expect(k8sClient.Delete(ctx, ws)).To(Succeed())
			_, err = r.Reconcile(ctx, reconcile.Request{NamespacedName: nsn})
			Expect(err).NotTo(HaveOccurred())

			wsObj := "workspace:" + name
			Expect(hasTuple(rec.Deleted, wsObj, "a2a_callable_by", wsObj)).To(BeTrue(),
				"cleanup must delete the self a2a_callable_by tuple")
		})
	})

	Describe("cross-tenant", func() {
		It("writes NO peer tuple without an Approved CTA (cross-tenant call denied)", func() {
			name = fmt.Sprintf("ws-a2a-xt-nocta-%d", GinkgoRandomSeed())
			ws := makeA2AWorkspace("default", name, "acme-tenant", true, keesev1alpha1.WorkspaceA2AScopeCrossTenant)
			Expect(k8sClient.Create(ctx, ws)).To(Succeed())
			nsn = mustNamespacedName(ws.Namespace, ws.Name)
			r = newReconciler()
			// cta has no approved peers → fail-closed.

			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: nsn})
			Expect(err).NotTo(HaveOccurred())
			_, err = r.Reconcile(ctx, reconcile.Request{NamespacedName: nsn})
			Expect(err).NotTo(HaveOccurred())

			wsObj := "workspace:" + name
			// No self tuple (cross-tenant does not self-grant) and no peer tuple.
			for _, tp := range rec.Synced {
				Expect(tp.Relation == "a2a_callable_by").To(BeFalse(),
					"cross-tenant without CTA must write no a2a_callable_by tuple, got %s#%s@%s", tp.Object, tp.Relation, tp.User)
			}
			Expect(hasTuple(rec.Synced, wsObj, "a2a_callable_by", "workspace:peer-ws")).To(BeFalse())
		})

		It("writes the peer tuple once an Approved CTA exists (cross-tenant call allowed)", func() {
			name = fmt.Sprintf("ws-a2a-xt-cta-%d", GinkgoRandomSeed())
			ws := makeA2AWorkspace("default", name, "acme-tenant", true, keesev1alpha1.WorkspaceA2AScopeCrossTenant)
			Expect(k8sClient.Create(ctx, ws)).To(Succeed())
			nsn = mustNamespacedName(ws.Namespace, ws.Name)
			r = newReconciler()
			// Approved CTA: peer workspace "peer-ws" (in another tenant) may call this callee.
			cta.approve("acme-tenant", name, "peer-ws")

			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: nsn})
			Expect(err).NotTo(HaveOccurred())
			_, err = r.Reconcile(ctx, reconcile.Request{NamespacedName: nsn})
			Expect(err).NotTo(HaveOccurred())

			wsObj := "workspace:" + name
			Expect(hasTuple(rec.Synced, wsObj, "a2a_callable_by", "workspace:peer-ws")).To(BeTrue(),
				"approved-CTA peer must get a per-peer a2a_callable_by tuple")
		})

		It("fails the reconcile (fail-closed) when the CTA resolver errors", func() {
			name = fmt.Sprintf("ws-a2a-xt-err-%d", GinkgoRandomSeed())
			ws := makeA2AWorkspace("default", name, "acme-tenant", true, keesev1alpha1.WorkspaceA2AScopeCrossTenant)
			Expect(k8sClient.Create(ctx, ws)).To(Succeed())
			nsn = mustNamespacedName(ws.Namespace, ws.Name)
			r = newReconciler()

			// Add finalizer first (clean pass), then make the resolver error.
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: nsn})
			Expect(err).NotTo(HaveOccurred())
			cta.err = errors.New("cta api unreachable")

			res, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: nsn})
			Expect(err).NotTo(HaveOccurred())
			Expect(res.RequeueAfter).To(BeNumerically(">", 0),
				"resolver error must requeue rather than write a partial grant set")
			wsObj := "workspace:" + name
			for _, tp := range rec.Synced {
				Expect(tp.Object == wsObj && tp.Relation == "a2a_callable_by").To(BeFalse(),
					"no a2a tuple may be synced when the CTA resolver errors")
			}
		})
	})

	Describe("idempotency (rule 04.16 / E2 acceptance)", func() {
		It("converges A2A tuples in ≤3 reconciles", func() {
			name = fmt.Sprintf("ws-a2a-idem-%d", GinkgoRandomSeed())
			ws := makeA2AWorkspace("default", name, "acme-tenant", true, keesev1alpha1.WorkspaceA2AScopeIntraTenant)
			Expect(k8sClient.Create(ctx, ws)).To(Succeed())
			nsn = mustNamespacedName(ws.Namespace, ws.Name)
			r = newReconciler()

			var lastVersion string
			for i := 0; i < 3; i++ {
				_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: nsn})
				Expect(err).NotTo(HaveOccurred())
				var fresh keesev1alpha1.Workspace
				Expect(k8sClient.Get(ctx, nsn, &fresh)).To(Succeed())
				if i == 2 {
					Expect(fresh.ResourceVersion).To(Equal(lastVersion),
						"spec unchanged; ResourceVersion stable on pass 3")
				}
				lastVersion = fresh.ResourceVersion
			}
		})
	})
})
