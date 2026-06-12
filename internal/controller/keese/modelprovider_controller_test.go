// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

//go:build integration

package keese

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	keesev1alpha1 "github.com/keese-ai/keese/api/keese/v1alpha1"
)

// fakeModelDiscoverer is a per-spec fake ModelDiscoverer. It returns Models (or
// Err) and records the number of Discover calls. It performs no network I/O.
type fakeModelDiscoverer struct {
	Models []string
	Err    error
	Calls  int
}

func (f *fakeModelDiscoverer) Discover(_ context.Context, _ keesev1alpha1.ModelProviderType, _ string) ([]string, error) {
	f.Calls++
	if f.Err != nil {
		return nil, f.Err
	}
	return f.Models, nil
}

// fakeMPRebacWriter records credential-binding tuple writes/deletes.
type fakeMPRebacWriter struct {
	Written []ModelProviderTuple
	Deleted []ModelProviderTuple
}

func (f *fakeMPRebacWriter) Write(_ context.Context, t []ModelProviderTuple) error {
	f.Written = append(f.Written, t...)
	return nil
}

func (f *fakeMPRebacWriter) Delete(_ context.Context, t []ModelProviderTuple) error {
	f.Deleted = append(f.Deleted, t...)
	return nil
}

var _ = Describe("ModelProvider Controller", func() {
	const mpNS = "default"

	newReconciler := func(disc ModelDiscoverer, rebac ModelProviderRebacWriter) *ModelProviderReconciler {
		return &ModelProviderReconciler{
			Client:     k8sClient,
			Scheme:     k8sClient.Scheme(),
			Recorder:   noopRecorder{},
			Rebac:      rebac,
			Discoverer: disc,
		}
	}

	// reconcileUntilStable drives the reconciler to completion: the first call
	// adds the finalizer and requeues, subsequent calls do the real work.
	reconcileUntilStable := func(rec *ModelProviderReconciler, nn types.NamespacedName, n int) {
		for i := 0; i < n; i++ {
			_, err := rec.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())
		}
	}

	Describe("happy path — gemini, no discovery", func() {
		It("reaches Ready and writes the credential ReBAC tuple", func() {
			mp := &keesev1alpha1.ModelProvider{
				ObjectMeta: metav1.ObjectMeta{Name: "mp-gemini", Namespace: mpNS},
				Spec: keesev1alpha1.ModelProviderSpec{
					Provider:            keesev1alpha1.ModelProviderGemini,
					CredentialSecretRef: &corev1.LocalObjectReference{Name: "gemini-cred"},
				},
			}
			Expect(k8sClient.Create(ctx, mp)).To(Succeed())
			nn := types.NamespacedName{Name: mp.Name, Namespace: mpNS}
			DeferCleanup(func() { deleteMP(nn) })

			rebac := &fakeMPRebacWriter{}
			rec := newReconciler(&fakeModelDiscoverer{}, rebac)
			reconcileUntilStable(rec, nn, 3)

			Expect(k8sClient.Get(ctx, nn, mp)).To(Succeed())
			Expect(mp.Status.Phase).To(Equal(keesev1alpha1.ModelProviderPhaseReady))
			Expect(mp.Status.ObservedGeneration).To(Equal(mp.Generation))
			// Write is an idempotent upsert, so the fake accumulates one tuple per
			// reconcile; assert the binding tuple was synced at least once.
			Expect(rebac.Written).NotTo(BeEmpty())
			Expect(rebac.Written[0].Relation).To(Equal("credential"))
			Expect(rebac.Written[0].User).To(Equal("secret:gemini-cred"))
		})

		It("is idempotent across ≥3 reconciles with no spec change", func() {
			mp := &keesev1alpha1.ModelProvider{
				ObjectMeta: metav1.ObjectMeta{Name: "mp-idem", Namespace: mpNS},
				Spec:       keesev1alpha1.ModelProviderSpec{Provider: keesev1alpha1.ModelProviderAnthropic},
			}
			Expect(k8sClient.Create(ctx, mp)).To(Succeed())
			nn := types.NamespacedName{Name: mp.Name, Namespace: mpNS}
			DeferCleanup(func() { deleteMP(nn) })

			rec := newReconciler(&fakeModelDiscoverer{}, &fakeMPRebacWriter{})
			// First reconcile adds finalizer; then converge.
			reconcileUntilStable(rec, nn, 2)
			Expect(k8sClient.Get(ctx, nn, mp)).To(Succeed())
			Expect(mp.Status.Phase).To(Equal(keesev1alpha1.ModelProviderPhaseReady))
			genAfterConverge := mp.Generation

			for i := 0; i < 3; i++ {
				res, err := rec.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
				Expect(err).NotTo(HaveOccurred())
				Expect(res.Requeue).To(BeFalse())
			}
			Expect(k8sClient.Get(ctx, nn, mp)).To(Succeed())
			Expect(mp.Status.Phase).To(Equal(keesev1alpha1.ModelProviderPhaseReady))
			Expect(mp.Generation).To(Equal(genAfterConverge), "no spec churn across reconciles")
		})
	})

	Describe("discovery", func() {
		It("populates status.availableModels from the discoverer", func() {
			mp := &keesev1alpha1.ModelProvider{
				ObjectMeta: metav1.ObjectMeta{Name: "mp-discovery", Namespace: mpNS},
				Spec: keesev1alpha1.ModelProviderSpec{
					Provider:          keesev1alpha1.ModelProviderOpenAI,
					DiscoveryEnabled:  true,
					DiscoveryInterval: "1h",
				},
			}
			Expect(k8sClient.Create(ctx, mp)).To(Succeed())
			nn := types.NamespacedName{Name: mp.Name, Namespace: mpNS}
			DeferCleanup(func() { deleteMP(nn) })

			disc := &fakeModelDiscoverer{Models: []string{"gpt-4o", "gpt-4o-mini"}}
			rec := newReconciler(disc, &fakeMPRebacWriter{})
			reconcileUntilStable(rec, nn, 2)

			Expect(k8sClient.Get(ctx, nn, mp)).To(Succeed())
			Expect(mp.Status.AvailableModels).To(ConsistOf("gpt-4o", "gpt-4o-mini"))
			Expect(mp.Status.LastDiscoveryTime).NotTo(BeNil())
			Expect(disc.Calls).To(BeNumerically(">=", 1))

			// Synced condition is True.
			var synced *metav1.Condition
			for i := range mp.Status.Conditions {
				if mp.Status.Conditions[i].Type == keesev1alpha1.ModelProviderConditionSynced {
					synced = &mp.Status.Conditions[i]
				}
			}
			Expect(synced).NotTo(BeNil())
			Expect(synced.Status).To(Equal(metav1.ConditionTrue))
		})

		It("requeues on a discovery requeue interval", func() {
			mp := &keesev1alpha1.ModelProvider{
				ObjectMeta: metav1.ObjectMeta{Name: "mp-requeue", Namespace: mpNS},
				Spec: keesev1alpha1.ModelProviderSpec{
					Provider:          keesev1alpha1.ModelProviderOpenAI,
					DiscoveryEnabled:  true,
					DiscoveryInterval: "45m",
				},
			}
			Expect(k8sClient.Create(ctx, mp)).To(Succeed())
			nn := types.NamespacedName{Name: mp.Name, Namespace: mpNS}
			DeferCleanup(func() { deleteMP(nn) })

			rec := newReconciler(&fakeModelDiscoverer{Models: []string{"gpt-4o"}}, &fakeMPRebacWriter{})
			// finalizer add
			_, err := rec.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())
			// real work — should requeue after the configured interval
			res, err := rec.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())
			Expect(res.RequeueAfter).To(BeNumerically(">", 0))
		})
	})

	Describe("CEL admission (CRD x-kubernetes-validations)", func() {
		It("rejects ollama without an endpoint (OllamaEndpointRequired)", func() {
			mp := &keesev1alpha1.ModelProvider{
				ObjectMeta: metav1.ObjectMeta{Name: "mp-ollama-bad", Namespace: mpNS},
				Spec:       keesev1alpha1.ModelProviderSpec{Provider: keesev1alpha1.ModelProviderOllama},
			}
			Expect(k8sClient.Create(ctx, mp)).NotTo(Succeed())
		})

		It("accepts ollama with an endpoint (matches the ollama sample)", func() {
			mp := &keesev1alpha1.ModelProvider{
				ObjectMeta: metav1.ObjectMeta{Name: "mp-ollama-ok", Namespace: mpNS},
				Spec: keesev1alpha1.ModelProviderSpec{
					Provider: keesev1alpha1.ModelProviderOllama,
					Endpoint: "http://ollama.keese-system:11434",
				},
			}
			Expect(k8sClient.Create(ctx, mp)).To(Succeed())
			Expect(k8sClient.Delete(ctx, mp)).To(Succeed())
		})

		It("rejects mutating sp.provider (ModelProviderTypeImmutable)", func() {
			mp := &keesev1alpha1.ModelProvider{
				ObjectMeta: metav1.ObjectMeta{Name: "mp-immutable", Namespace: mpNS},
				Spec:       keesev1alpha1.ModelProviderSpec{Provider: keesev1alpha1.ModelProviderOpenAI},
			}
			Expect(k8sClient.Create(ctx, mp)).To(Succeed())
			DeferCleanup(func() { _ = k8sClient.Delete(ctx, mp) })
			mp.Spec.Provider = keesev1alpha1.ModelProviderAnthropic
			Expect(k8sClient.Update(ctx, mp)).NotTo(Succeed())
		})
	})

	Describe("Recipe model one-of (RecipeModelEitherForm)", func() {
		It("rejects a Recipe that sets both literal and modelProviderRef", func() {
			rc := &keesev1alpha1.Recipe{
				ObjectMeta: metav1.ObjectMeta{Name: "recipe-both-forms", Namespace: mpNS},
				Spec: keesev1alpha1.RecipeSpec{
					Instructions: "instructions.md",
					Model: keesev1alpha1.RecipeModel{
						Provider:         "anthropic",
						ModelID:          "claude-sonnet-4-6",
						ModelProviderRef: &corev1.LocalObjectReference{Name: "mp-gemini"},
					},
					SourceRef: keesev1alpha1.RecipeSourceRef{Name: "src"},
				},
			}
			Expect(k8sClient.Create(ctx, rc)).NotTo(Succeed())
		})

		It("accepts a Recipe in the modelProviderRef form", func() {
			rc := &keesev1alpha1.Recipe{
				ObjectMeta: metav1.ObjectMeta{Name: "recipe-ref-form", Namespace: mpNS},
				Spec: keesev1alpha1.RecipeSpec{
					Instructions: "instructions.md",
					Model: keesev1alpha1.RecipeModel{
						ModelProviderRef: &corev1.LocalObjectReference{Name: "mp-gemini"},
					},
					SourceRef: keesev1alpha1.RecipeSourceRef{Name: "src"},
				},
			}
			Expect(k8sClient.Create(ctx, rc)).To(Succeed())
			Expect(k8sClient.Delete(ctx, rc)).To(Succeed())
		})
	})

	Describe("deletion", func() {
		It("purges the credential tuple and removes the finalizer", func() {
			mp := &keesev1alpha1.ModelProvider{
				ObjectMeta: metav1.ObjectMeta{Name: "mp-delete", Namespace: mpNS},
				Spec: keesev1alpha1.ModelProviderSpec{
					Provider:            keesev1alpha1.ModelProviderAnthropic,
					CredentialSecretRef: &corev1.LocalObjectReference{Name: "anthropic-cred"},
				},
			}
			Expect(k8sClient.Create(ctx, mp)).To(Succeed())
			nn := types.NamespacedName{Name: mp.Name, Namespace: mpNS}

			rebac := &fakeMPRebacWriter{}
			rec := newReconciler(&fakeModelDiscoverer{}, rebac)
			reconcileUntilStable(rec, nn, 2)

			Expect(k8sClient.Delete(ctx, mp)).To(Succeed())
			// drive deletion reconcile
			_, err := rec.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())

			Eventually(func() bool {
				return k8sClient.Get(ctx, nn, &keesev1alpha1.ModelProvider{}) != nil
			}, reconcileWait, reconcileTick).Should(BeTrue())
			Expect(rebac.Deleted).To(HaveLen(1))
		})
	})
})

// deleteMP deletes a ModelProvider and reconciles its finalizer away.
func deleteMP(nn types.NamespacedName) {
	mp := &keesev1alpha1.ModelProvider{}
	if err := k8sClient.Get(ctx, nn, mp); err != nil {
		return
	}
	_ = k8sClient.Delete(ctx, mp)
	rec := &ModelProviderReconciler{
		Client:     k8sClient,
		Scheme:     k8sClient.Scheme(),
		Recorder:   noopRecorder{},
		Rebac:      ModelProviderNoopRebacWriter{},
		Discoverer: &fakeModelDiscoverer{},
	}
	Eventually(func() bool {
		_, _ = rec.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
		return k8sClient.Get(ctx, nn, &keesev1alpha1.ModelProvider{}) != nil
	}, reconcileWait, reconcileTick).Should(BeTrue())
}
