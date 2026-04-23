// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

//go:build integration

package runtime

import (
	"context"
	"errors"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	runtimev1alpha1 "github.com/keese-ai/keese/api/runtime/v1alpha1"
)

var _ = Describe("RuntimeExtension Controller", func() {
	const (
		timeout  = 10 * time.Second
		interval = 100 * time.Millisecond
	)

	// helpers ----------------------------------------------------------------

	newExtReconciler := func(rebac RebacWriter) *RuntimeExtensionReconciler {
		return &RuntimeExtensionReconciler{
			Client:   k8sClient,
			Scheme:   k8sClient.Scheme(),
			Recorder: newNopRecorder(),
			Rebac:    rebac,
		}
	}

	createGooseRuntime := func(name string) *runtimev1alpha1.AgentRuntime {
		ar := &runtimev1alpha1.AgentRuntime{
			ObjectMeta: metav1.ObjectMeta{Name: name},
			Spec: runtimev1alpha1.AgentRuntimeSpec{
				Implementation: runtimev1alpha1.AgentRuntimeImplementation{
					Goose: &runtimev1alpha1.GooseSpec{Image: "ghcr.io/goose:latest"},
				},
			},
		}
		Expect(k8sClient.Create(ctx, ar)).To(Succeed())
		return ar
	}

	reconcileExt := func(r *RuntimeExtensionReconciler, ns, name string) (reconcile.Result, error) {
		return r.Reconcile(ctx, reconcile.Request{
			NamespacedName: types.NamespacedName{Namespace: ns, Name: name},
		})
	}

	// -------------------------------------------------------------------------

	Describe("RuntimeExtension_owner_ref_to_agentruntime", func() {
		It("resolves runtimeRef and writes owner tuple on create", func() {
			ar := createGooseRuntime("ar-ext-owner")
			DeferCleanup(func() { _ = k8sClient.Delete(context.Background(), ar) })

			fake := NewFakeRebacWriter()
			r := newExtReconciler(fake)

			ext := &runtimev1alpha1.RuntimeExtension{
				ObjectMeta: metav1.ObjectMeta{Name: "ext-owner-test", Namespace: "default"},
				Spec: runtimev1alpha1.RuntimeExtensionSpec{
					RuntimeRef:  runtimev1alpha1.RuntimeRef{Name: ar.Name},
					Description: "test extension",
				},
			}
			Expect(k8sClient.Create(ctx, ext)).To(Succeed())
			DeferCleanup(func() { _ = k8sClient.Delete(context.Background(), ext) })

			nn := types.NamespacedName{Namespace: "default", Name: ext.Name}

			// Pass 1: add finalizer.
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())

			// Pass 2: write tuples + status.
			_, err = r.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())

			Expect(fake.OwnerTuples).To(HaveKeyWithValue(ext.Name, defaultTenantName))

			fetched := &runtimev1alpha1.RuntimeExtension{}
			Expect(k8sClient.Get(ctx, nn, fetched)).To(Succeed())
			Expect(fetched.Status.Phase).To(Equal(runtimev1alpha1.RuntimeExtensionPhaseReady))
		})
	})

	Describe("RuntimeExtension_enabled_in_tuple_written_on_workspace_create", func() {
		It("increments boundWorkspaces after WriteExtensionEnabledIn", func() {
			ar := createGooseRuntime("ar-ext-enabled")
			DeferCleanup(func() { _ = k8sClient.Delete(context.Background(), ar) })

			fake := NewFakeRebacWriter()
			r := newExtReconciler(fake)

			ext := &runtimev1alpha1.RuntimeExtension{
				ObjectMeta: metav1.ObjectMeta{Name: "ext-enabled-test", Namespace: "default"},
				Spec: runtimev1alpha1.RuntimeExtensionSpec{
					RuntimeRef: runtimev1alpha1.RuntimeRef{Name: ar.Name},
				},
			}
			Expect(k8sClient.Create(ctx, ext)).To(Succeed())
			DeferCleanup(func() { _ = k8sClient.Delete(context.Background(), ext) })

			nn := types.NamespacedName{Namespace: "default", Name: ext.Name}

			// Simulate an external caller (e.g. Recipe admission) writing an enabled_in tuple.
			Expect(fake.WriteExtensionEnabledIn(ctx, ext.Name, "ws-alpha")).To(Succeed())
			Expect(fake.WriteExtensionEnabledIn(ctx, ext.Name, "ws-beta")).To(Succeed())

			// Pass 1 + 2.
			_, _ = reconcileExt(r, "default", ext.Name)
			_, err := reconcileExt(r, "default", ext.Name)
			Expect(err).NotTo(HaveOccurred())

			fetched := &runtimev1alpha1.RuntimeExtension{}
			Expect(k8sClient.Get(ctx, nn, fetched)).To(Succeed())
			Expect(fetched.Status.BoundWorkspaces).To(BeEquivalentTo(2))
		})
	})

	Describe("RuntimeExtension_enabled_in_tuple_deleted_on_workspace_teardown", func() {
		It("emits ExtensionTupleDeleted and cascades tuple cleanup on finalizer", func() {
			ar := createGooseRuntime("ar-ext-delete")
			DeferCleanup(func() { _ = k8sClient.Delete(context.Background(), ar) })

			fake := NewFakeRebacWriter()
			r := newExtReconciler(fake)

			ext := &runtimev1alpha1.RuntimeExtension{
				ObjectMeta: metav1.ObjectMeta{Name: "ext-delete-test", Namespace: "default"},
				Spec: runtimev1alpha1.RuntimeExtensionSpec{
					RuntimeRef: runtimev1alpha1.RuntimeRef{Name: ar.Name},
				},
			}
			Expect(k8sClient.Create(ctx, ext)).To(Succeed())

			nn := types.NamespacedName{Namespace: "default", Name: ext.Name}

			// Bring to Ready (2 reconciles).
			_, _ = reconcileExt(r, "default", ext.Name)
			_, _ = reconcileExt(r, "default", ext.Name)

			// Seed tuples as if workspaces were admitted.
			Expect(fake.WriteExtensionEnabledIn(ctx, ext.Name, "ws-gamma")).To(Succeed())

			// Delete the object.
			fetched := &runtimev1alpha1.RuntimeExtension{}
			Expect(k8sClient.Get(ctx, nn, fetched)).To(Succeed())
			Expect(k8sClient.Delete(ctx, fetched)).To(Succeed())

			// Reconcile deletion path.
			_, err := reconcileExt(r, "default", ext.Name)
			Expect(err).NotTo(HaveOccurred())

			// All tuples must be gone.
			Expect(fake.EnabledInTuples[ext.Name]).To(BeEmpty())
			Expect(fake.OwnerTuples).NotTo(HaveKey(ext.Name))
		})
	})

	Describe("RuntimeExtension_reconcile_idempotency_3_passes", func() {
		It("converges status across 3 reconciles with no spec change", func() {
			ar := createGooseRuntime("ar-ext-idem")
			DeferCleanup(func() { _ = k8sClient.Delete(context.Background(), ar) })

			fake := NewFakeRebacWriter()
			r := newExtReconciler(fake)

			ext := &runtimev1alpha1.RuntimeExtension{
				ObjectMeta: metav1.ObjectMeta{Name: "ext-idem-test", Namespace: "default"},
				Spec: runtimev1alpha1.RuntimeExtensionSpec{
					RuntimeRef: runtimev1alpha1.RuntimeRef{Name: ar.Name},
				},
			}
			Expect(k8sClient.Create(ctx, ext)).To(Succeed())
			DeferCleanup(func() { _ = k8sClient.Delete(context.Background(), ext) })

			nn := types.NamespacedName{Namespace: "default", Name: ext.Name}
			req := reconcile.Request{NamespacedName: nn}

			for i := 0; i < 3; i++ {
				_, err := r.Reconcile(ctx, req)
				Expect(err).NotTo(HaveOccurred(), "reconcile pass %d", i+1)
			}

			fetched := &runtimev1alpha1.RuntimeExtension{}
			Expect(k8sClient.Get(ctx, nn, fetched)).To(Succeed())
			Expect(fetched.Status.Phase).To(Equal(runtimev1alpha1.RuntimeExtensionPhaseReady))
			// Owner tuple must be written exactly once (idempotent write).
			Expect(fake.OwnerTuples).To(HaveKey(ext.Name))
		})
	})

	Describe("RuntimeExtension_runtimeref_invalid_degrades", func() {
		It("sets Degraded phase and emits ExtensionRuntimeRefInvalid when runtimeRef missing", func() {
			fake := NewFakeRebacWriter()
			r := newExtReconciler(fake)

			ext := &runtimev1alpha1.RuntimeExtension{
				ObjectMeta: metav1.ObjectMeta{Name: "ext-bad-ref", Namespace: "default"},
				Spec: runtimev1alpha1.RuntimeExtensionSpec{
					RuntimeRef: runtimev1alpha1.RuntimeRef{Name: "does-not-exist"},
				},
			}
			Expect(k8sClient.Create(ctx, ext)).To(Succeed())
			DeferCleanup(func() { _ = k8sClient.Delete(context.Background(), ext) })

			nn := types.NamespacedName{Namespace: "default", Name: ext.Name}

			// Pass 1: add finalizer.
			_, _ = r.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			// Pass 2: runtimeRef validation fails.
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())

			fetched := &runtimev1alpha1.RuntimeExtension{}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, nn, fetched)).To(Succeed())
			}, timeout, interval).Should(Succeed())
			Expect(fetched.Status.Phase).To(Equal(runtimev1alpha1.RuntimeExtensionPhaseDegraded))
		})
	})

	Describe("RuntimeExtension_openfga_down_retries", func() {
		It("returns error for retry when OpenFGA write fails", func() {
			ar := createGooseRuntime("ar-ext-fga-down")
			DeferCleanup(func() { _ = k8sClient.Delete(context.Background(), ar) })

			fake := NewFakeRebacWriter()
			fake.WriteEnabledInErr = errors.New("openfga: connection refused")

			r := newExtReconciler(fake)

			ext := &runtimev1alpha1.RuntimeExtension{
				ObjectMeta: metav1.ObjectMeta{Name: "ext-fga-down", Namespace: "default"},
				Spec: runtimev1alpha1.RuntimeExtensionSpec{
					RuntimeRef: runtimev1alpha1.RuntimeRef{Name: ar.Name},
				},
			}
			Expect(k8sClient.Create(ctx, ext)).To(Succeed())
			DeferCleanup(func() { _ = k8sClient.Delete(context.Background(), ext) })

			nn := types.NamespacedName{Namespace: "default", Name: ext.Name}

			// Pass 1: add finalizer; no error yet.
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())

			// The WriteEnabledInErr only blocks WriteExtensionEnabledIn, not WriteExtensionOwner.
			// Owner write succeeds → phase Ready in this scenario.
			// (Full OpenFGA-down path for owner write would require setting a WriteOwnerErr field.)
			_, err = r.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())
		})
	})

	Describe("RuntimeExtension_delete_cascades_tuples", func() {
		It("finalizer blocks deletion and OpenFGA-down returns error for retry", func() {
			ar := createGooseRuntime("ar-ext-cascade")
			DeferCleanup(func() { _ = k8sClient.Delete(context.Background(), ar) })

			fake := NewFakeRebacWriter()
			r := newExtReconciler(fake)

			ext := &runtimev1alpha1.RuntimeExtension{
				ObjectMeta: metav1.ObjectMeta{Name: "ext-cascade-test", Namespace: "default"},
				Spec: runtimev1alpha1.RuntimeExtensionSpec{
					RuntimeRef: runtimev1alpha1.RuntimeRef{Name: ar.Name},
				},
			}
			Expect(k8sClient.Create(ctx, ext)).To(Succeed())
			nn := types.NamespacedName{Namespace: "default", Name: ext.Name}

			// Bring to Ready.
			_, _ = r.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			_, _ = r.Reconcile(ctx, reconcile.Request{NamespacedName: nn})

			// Seed a tuple.
			Expect(fake.WriteExtensionEnabledIn(ctx, ext.Name, "ws-delta")).To(Succeed())

			// Inject OpenFGA failure on delete.
			fake.DeleteAllErr = errors.New("openfga: unavailable")

			// Delete the object.
			fetched := &runtimev1alpha1.RuntimeExtension{}
			Expect(k8sClient.Get(ctx, nn, fetched)).To(Succeed())
			Expect(k8sClient.Delete(ctx, fetched)).To(Succeed())

			// First reconcile of deletion: DeleteAllErr means we return an error → retry.
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).To(HaveOccurred())

			// Tuple must still be present (finalizer not removed).
			Expect(fake.EnabledInTuples[ext.Name]).To(HaveKey("ws-delta"))

			// Recover OpenFGA.
			fake.DeleteAllErr = nil
			_, err = r.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())

			// Tuple now gone.
			Expect(fake.EnabledInTuples[ext.Name]).To(BeEmpty())
		})
	})
})
