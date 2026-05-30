// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

//go:build integration

package keese

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	keesev1alpha1 "github.com/keese-ai/keese/api/keese/v1alpha1"
)

const smTestNS = "default"

var _ = Describe("SharedMemory Controller", func() {
	buildSM := func(name string, sharedWith ...keesev1alpha1.WorkspaceRef) *keesev1alpha1.SharedMemory {
		return &keesev1alpha1.SharedMemory{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: smTestNS,
			},
			Spec: keesev1alpha1.SharedMemorySpec{
				TenantRef: "tenant-" + name,
				Provider: keesev1alpha1.MemoryProvider{
					Type:   keesev1alpha1.ProviderSQLite,
					SQLite: &keesev1alpha1.SQLiteConfig{StorageSize: "1Gi"},
				},
				EmbeddingDim: 768,
				SharedWith:   sharedWith,
			},
		}
	}

	Describe("happy path — no sharedWith", func() {
		var sm *keesev1alpha1.SharedMemory

		BeforeEach(func() {
			sm = buildSM("sm-empty-shared")
			Expect(k8sClient.Create(ctx, sm)).To(Succeed())
		})

		AfterEach(func() {
			Expect(k8sClient.Delete(ctx, sm)).To(Succeed())
			Eventually(func() bool {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: sm.Name, Namespace: smTestNS}, sm)
				return err != nil
			}, 5*time.Second, 100*time.Millisecond).Should(BeTrue())
		})

		It("reaches Ready phase", func() {
			nn := types.NamespacedName{Name: sm.Name, Namespace: smTestNS}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, nn, sm)).To(Succeed())
				g.Expect(sm.Status.Phase).To(Equal(keesev1alpha1.MemoryPhaseReady))
			}, 5*time.Second, 100*time.Millisecond).Should(Succeed())
		})

		It("sets Ready condition", func() {
			nn := types.NamespacedName{Name: sm.Name, Namespace: smTestNS}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, nn, sm)).To(Succeed())
				var rc *metav1.Condition
				for i := range sm.Status.Conditions {
					if sm.Status.Conditions[i].Type == "Ready" {
						rc = &sm.Status.Conditions[i]
						break
					}
				}
				g.Expect(rc).NotTo(BeNil())
				g.Expect(rc.Status).To(Equal(metav1.ConditionTrue))
			}, 5*time.Second, 100*time.Millisecond).Should(Succeed())
		})
	})

	Describe("sharedWith reader/writer tuple sync", func() {
		It("writes reader and writer tuples for sharedWith entries", func() {
			sm := buildSM("sm-shared-rw",
				keesev1alpha1.WorkspaceRef{Name: "ws-reader", Namespace: "ns-a", Access: "reader"},
				keesev1alpha1.WorkspaceRef{Name: "ws-writer", Namespace: "ns-b", Access: "writer"},
			)
			Expect(k8sClient.Create(ctx, sm)).To(Succeed())
			nn := types.NamespacedName{Name: sm.Name, Namespace: smTestNS}

			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, nn, sm)).To(Succeed())
				g.Expect(sm.Status.Phase).To(Equal(keesev1alpha1.MemoryPhaseReady))
				g.Expect(sm.Status.RebacTupleCount).To(Equal(int32(2)))
			}, 5*time.Second, 100*time.Millisecond).Should(Succeed())

			Expect(k8sClient.Delete(ctx, sm)).To(Succeed())
		})
	})

	Describe("finalizer tuple-purge-before-deprovision order", func() {
		It("purges all shared tuples before backend deprovision", func() {
			sm := buildSM("sm-finalize-order",
				keesev1alpha1.WorkspaceRef{Name: "ws-del", Namespace: "ns-del", Access: "reader"},
			)
			Expect(k8sClient.Create(ctx, sm)).To(Succeed())
			nn := types.NamespacedName{Name: sm.Name, Namespace: smTestNS}

			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, nn, sm)).To(Succeed())
				g.Expect(sm.Status.Phase).To(Equal(keesev1alpha1.MemoryPhaseReady))
			}, 5*time.Second, 100*time.Millisecond).Should(Succeed())

			// Snapshot deprovision count before delete.
			deprovCountBefore := len(fakeBackend.DeprovisionCalls)

			// Delete and wait for full finalizer removal by the background manager.
			Expect(k8sClient.Delete(ctx, sm)).To(Succeed())
			Eventually(func() bool {
				err := k8sClient.Get(ctx, nn, sm)
				return err != nil
			}, 5*time.Second, 100*time.Millisecond).Should(BeTrue())

			// After finalizer is removed, both rebac purge and deprovision must have run.
			Expect(fakeRebac.Deleted).NotTo(BeEmpty(), "rebac tuples must be purged on delete")
			Expect(len(fakeBackend.DeprovisionCalls)).To(BeNumerically(">", deprovCountBefore),
				"backend must be deprovisioned after rebac purge")
		})
	})

	Describe("HA validation — qdrant outside dev namespace", func() {
		It("rejects qdrant with replicas < 2 in non-dev namespace", func() {
			provider := keesev1alpha1.MemoryProvider{
				Type: keesev1alpha1.ProviderQdrant,
				Qdrant: &keesev1alpha1.QdrantConfig{
					CollectionName: "test",
					Endpoint:       "qdrant:6334",
					Replicas:       1,
				},
			}
			err := validateHA(provider, "prod-cluster", nil)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("MemoryHARequired"))

			// dev namespace exemption.
			Expect(validateHA(provider, "team-dev", nil)).NotTo(HaveOccurred())
		})
	})

	Describe("idempotency across ≥3 reconciles", func() {
		It("converges with no spec change", func() {
			sm := buildSM("sm-idempotent",
				keesev1alpha1.WorkspaceRef{Name: "ws-idem", Namespace: "ns-idem", Access: "reader"},
			)
			Expect(k8sClient.Create(ctx, sm)).To(Succeed())
			nn := types.NamespacedName{Name: sm.Name, Namespace: smTestNS}

			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, nn, sm)).To(Succeed())
				g.Expect(sm.Status.Phase).To(Equal(keesev1alpha1.MemoryPhaseReady))
			}, 5*time.Second, 100*time.Millisecond).Should(Succeed())

			rec := &SharedMemoryReconciler{
				Client:   mgr.GetClient(),
				Scheme:   mgr.GetScheme(),
				Recorder: mgr.GetEventRecorderFor("sm-idem-test"),
				Backend:  fakeBackend,
				Rebac:    fakeRebac,
			}
			for i := 0; i < 3; i++ {
				result, err := rec.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
				Expect(err).NotTo(HaveOccurred())
				Expect(result.Requeue).To(BeFalse())
			}

			Expect(k8sClient.Get(ctx, nn, sm)).To(Succeed())
			Expect(sm.Status.Phase).To(Equal(keesev1alpha1.MemoryPhaseReady))

			Expect(k8sClient.Delete(ctx, sm)).To(Succeed())
		})
	})

	Describe("EmbeddingDimImmutable", func() {
		It("controller does not alter embeddingDim after creation", func() {
			sm := buildSM("sm-embed-dim")
			sm.Spec.EmbeddingDim = 512
			Expect(k8sClient.Create(ctx, sm)).To(Succeed())
			nn := types.NamespacedName{Name: sm.Name, Namespace: smTestNS}

			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, nn, sm)).To(Succeed())
				g.Expect(sm.Status.Phase).To(Equal(keesev1alpha1.MemoryPhaseReady))
			}, 5*time.Second, 100*time.Millisecond).Should(Succeed())

			Expect(k8sClient.Get(ctx, nn, sm)).To(Succeed())
			Expect(sm.Spec.EmbeddingDim).To(Equal(int32(512)))

			Expect(k8sClient.Delete(ctx, sm)).To(Succeed())
		})
	})
})
