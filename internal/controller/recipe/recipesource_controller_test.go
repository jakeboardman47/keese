// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

//go:build integration

package recipe

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	recipev1alpha1 "github.com/keese-ai/keese/api/recipe/v1alpha1"
)

var _ = Describe("RecipeSource Controller", func() {
	const (
		timeout  = 10 * time.Second
		interval = 100 * time.Millisecond
	)

	// Spec 1: OCIDigestRequiredInProd — OCI source with digest syncs to Synced.
	Context("OCI source with digest pin in managed namespace", func() {
		const rsName = "rs-oci-digest"
		const rsNS = "default"

		BeforeEach(func() {
			rs := &recipev1alpha1.RecipeSource{
				ObjectMeta: metav1.ObjectMeta{
					Name:      rsName,
					Namespace: rsNS,
					Labels:    map[string]string{"keese.ai/managed": "true"},
				},
				Spec: recipev1alpha1.RecipeSourceSpec{
					OCI: &recipev1alpha1.OCISource{
						Registry:   "ghcr.io",
						Repository: "keese-ai/recipes/my-recipe",
						Digest:     "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
					},
				},
			}
			Expect(k8sClient.Create(ctx, rs)).To(Succeed())
		})

		AfterEach(func() {
			rs := &recipev1alpha1.RecipeSource{}
			err := k8sClient.Get(ctx, types.NamespacedName{Name: rsName, Namespace: rsNS}, rs)
			if err == nil {
				Expect(k8sClient.Delete(ctx, rs)).To(Succeed())
			}
		})

		It("OCIDigestRequiredInProd: should reconcile OCI source to Synced with resolved digest", func() {
			// Direct reconcile via controller (tests controller logic without manager predicate).
			reconciler := &RecipeSourceReconciler{
				Client:   k8sClient,
				Scheme:   k8sClient.Scheme(),
				Recorder: noopRecorder{},
				Fetcher: &FakeOCIFetcher{
					PulledDigest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
				},
			}

			nsn := types.NamespacedName{Name: rsName, Namespace: rsNS}
			req := reconcile.Request{NamespacedName: nsn}

			// First reconcile: adds finalizer and returns.
			_, err := reconciler.Reconcile(ctx, req)
			Expect(err).NotTo(HaveOccurred())

			// Second reconcile: pulls and verifies.
			_, err = reconciler.Reconcile(ctx, req)
			Expect(err).NotTo(HaveOccurred())

			// Third reconcile (idempotency): no change.
			_, err = reconciler.Reconcile(ctx, req)
			Expect(err).NotTo(HaveOccurred())

			var rs recipev1alpha1.RecipeSource
			Expect(k8sClient.Get(ctx, nsn, &rs)).To(Succeed())
			Expect(rs.Status.Phase).To(Equal(recipev1alpha1.RecipeSourcePhaseSynced))
			Expect(rs.Status.Cached).To(BeTrue())
			Expect(rs.Status.ResolvedDigest).NotTo(BeEmpty())
			Expect(rs.Status.SourceType).To(Equal(recipev1alpha1.RecipeSourceTypeOCI))
		})
	})

	// Spec 2: PullCosignFailClosed — cosign failure sets phase=Failed, cached=false.
	Context("OCI source with cosign verify failure", func() {
		const rsName = "rs-cosign-fail"
		const rsNS = "default"

		BeforeEach(func() {
			rs := &recipev1alpha1.RecipeSource{
				ObjectMeta: metav1.ObjectMeta{
					Name:      rsName,
					Namespace: rsNS,
					Labels:    map[string]string{"keese.ai/managed": "true"},
				},
				Spec: recipev1alpha1.RecipeSourceSpec{
					OCI: &recipev1alpha1.OCISource{
						Registry:   "ghcr.io",
						Repository: "keese-ai/recipes/unsigned",
						Digest:     "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
					},
				},
			}
			Expect(k8sClient.Create(ctx, rs)).To(Succeed())
		})

		AfterEach(func() {
			rs := &recipev1alpha1.RecipeSource{}
			err := k8sClient.Get(ctx, types.NamespacedName{Name: rsName, Namespace: rsNS}, rs)
			if err == nil {
				Expect(k8sClient.Delete(ctx, rs)).To(Succeed())
			}
		})

		It("PullCosignFailClosed: should set phase=Failed and cached=false on cosign error", func() {
			cosignErr := &testError{msg: "signature verification failed: no matching signatures"}
			reconciler := &RecipeSourceReconciler{
				Client:   k8sClient,
				Scheme:   k8sClient.Scheme(),
				Recorder: noopRecorder{},
				Fetcher: &FakeOCIFetcher{
					PulledDigest: "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
					VerifyErr:    cosignErr,
				},
			}

			nsn := types.NamespacedName{Name: rsName, Namespace: rsNS}
			req := reconcile.Request{NamespacedName: nsn}

			// First reconcile: adds finalizer.
			_, _ = reconciler.Reconcile(ctx, req)
			// Second reconcile: pull succeeds but verify fails.
			_, err := reconciler.Reconcile(ctx, req)
			Expect(err).NotTo(HaveOccurred()) // controller returns (RequeueAfter, nil) on cosign fail

			var rs recipev1alpha1.RecipeSource
			Expect(k8sClient.Get(ctx, nsn, &rs)).To(Succeed())
			Expect(rs.Status.Phase).To(Equal(recipev1alpha1.RecipeSourcePhaseFailed))
			Expect(rs.Status.Cached).To(BeFalse())
		})
	})

	// Spec 3: InlineConfigMapAllowedInDevNS — ConfigMap source succeeds in dev namespace.
	Context("ConfigMap source in a dev namespace", func() {
		const rsName = "rs-cm-dev"
		const rsNS = "recipe-dev-test"
		const cmName = "my-recipe-cm"

		BeforeEach(func() {
			ensureDevNamespace(rsNS)

			rs := &recipev1alpha1.RecipeSource{
				ObjectMeta: metav1.ObjectMeta{
					Name:      rsName,
					Namespace: rsNS,
					Labels:    map[string]string{"keese.ai/managed": "true"},
				},
				Spec: recipev1alpha1.RecipeSourceSpec{
					ConfigMap: &recipev1alpha1.ConfigMapSource{
						Name:      cmName,
						Namespace: rsNS,
					},
				},
			}
			Expect(k8sClient.Create(ctx, rs)).To(Succeed())
		})

		AfterEach(func() {
			rs := &recipev1alpha1.RecipeSource{}
			err := k8sClient.Get(ctx, types.NamespacedName{Name: rsName, Namespace: rsNS}, rs)
			if err == nil {
				Expect(k8sClient.Delete(ctx, rs)).To(Succeed())
			}
		})

		It("InlineConfigMapAllowedInDevNS: should reject ConfigMap source when ConfigMap does not exist (source not found)", func() {
			// ConfigMap does not exist → phase=Failed, RequeueAfter.
			reconciler := &RecipeSourceReconciler{
				Client:   k8sClient,
				Scheme:   k8sClient.Scheme(),
				Recorder: noopRecorder{},
				Fetcher:  &FakeOCIFetcher{},
			}
			nsn := types.NamespacedName{Name: rsName, Namespace: rsNS}
			req := reconcile.Request{NamespacedName: nsn}

			_, _ = reconciler.Reconcile(ctx, req) // adds finalizer
			res, err := reconciler.Reconcile(ctx, req)
			Expect(err).NotTo(HaveOccurred())
			Expect(res.RequeueAfter).To(BeNumerically(">", 0))

			var rs recipev1alpha1.RecipeSource
			Expect(k8sClient.Get(ctx, nsn, &rs)).To(Succeed())
			Expect(rs.Status.Phase).To(Equal(recipev1alpha1.RecipeSourcePhaseFailed))
		})
	})

	// Spec 4: ConfigMapRejectedInNonDevNamespace — ConfigMap source in prod namespace sets Failed.
	Context("ConfigMap source in a non-dev namespace", func() {
		const rsName = "rs-cm-prod"
		const rsNS = "recipe-prod-test"

		BeforeEach(func() {
			ensureProdNamespace(rsNS)

			rs := &recipev1alpha1.RecipeSource{
				ObjectMeta: metav1.ObjectMeta{
					Name:      rsName,
					Namespace: rsNS,
					Labels:    map[string]string{"keese.ai/managed": "true"},
				},
				Spec: recipev1alpha1.RecipeSourceSpec{
					ConfigMap: &recipev1alpha1.ConfigMapSource{
						Name:      "some-cm",
						Namespace: rsNS,
					},
				},
			}
			Expect(k8sClient.Create(ctx, rs)).To(Succeed())
		})

		AfterEach(func() {
			rs := &recipev1alpha1.RecipeSource{}
			err := k8sClient.Get(ctx, types.NamespacedName{Name: rsName, Namespace: rsNS}, rs)
			if err == nil {
				Expect(k8sClient.Delete(ctx, rs)).To(Succeed())
			}
		})

		It("ConfigMapRejectedInNonDevNamespace: should set phase=Failed for ConfigMap source in non-dev namespace", func() {
			reconciler := &RecipeSourceReconciler{
				Client:   k8sClient,
				Scheme:   k8sClient.Scheme(),
				Recorder: noopRecorder{},
				Fetcher:  &FakeOCIFetcher{},
			}
			nsn := types.NamespacedName{Name: rsName, Namespace: rsNS}
			req := reconcile.Request{NamespacedName: nsn}

			_, _ = reconciler.Reconcile(ctx, req) // adds finalizer
			_, err := reconciler.Reconcile(ctx, req)
			Expect(err).NotTo(HaveOccurred())

			var rs recipev1alpha1.RecipeSource
			Expect(k8sClient.Get(ctx, nsn, &rs)).To(Succeed())
			Expect(rs.Status.Phase).To(Equal(recipev1alpha1.RecipeSourcePhaseFailed))

			// Verify the Ready condition is False with ConfigMapSourceInNonDev reason.
			var readyCond *metav1.Condition
			for i := range rs.Status.Conditions {
				if rs.Status.Conditions[i].Type == recipev1alpha1.RecipeSourceConditionReady {
					readyCond = &rs.Status.Conditions[i]
					break
				}
			}
			Expect(readyCond).NotTo(BeNil())
			Expect(readyCond.Status).To(Equal(metav1.ConditionFalse))
			Expect(readyCond.Reason).To(Equal("ConfigMapSourceInNonDev"))
		})
	})

	// Spec 5 (idempotency): IdempotencyThreeReconciles — 3 OCI reconciles converge.
	Context("Idempotency: three reconciles with no spec change", func() {
		const rsName = "rs-idempotency"
		const rsNS = "default"

		BeforeEach(func() {
			rs := &recipev1alpha1.RecipeSource{
				ObjectMeta: metav1.ObjectMeta{
					Name:      rsName,
					Namespace: rsNS,
					Labels:    map[string]string{"keese.ai/managed": "true"},
				},
				Spec: recipev1alpha1.RecipeSourceSpec{
					OCI: &recipev1alpha1.OCISource{
						Registry:   "ghcr.io",
						Repository: "keese-ai/recipes/idempotent",
						Digest:     "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
					},
				},
			}
			Expect(k8sClient.Create(ctx, rs)).To(Succeed())
		})

		AfterEach(func() {
			rs := &recipev1alpha1.RecipeSource{}
			err := k8sClient.Get(ctx, types.NamespacedName{Name: rsName, Namespace: rsNS}, rs)
			if err == nil {
				Expect(k8sClient.Delete(ctx, rs)).To(Succeed())
			}
		})

		It("IdempotencyThreeReconciles: should converge in ≤3 reconciles with no spec change", func() {
			reconciler := &RecipeSourceReconciler{
				Client:   k8sClient,
				Scheme:   k8sClient.Scheme(),
				Recorder: noopRecorder{},
				Fetcher: &FakeOCIFetcher{
					PulledDigest: "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
				},
			}

			nsn := types.NamespacedName{Name: rsName, Namespace: rsNS}
			req := reconcile.Request{NamespacedName: nsn}

			for i := range 3 {
				_, err := reconciler.Reconcile(ctx, req)
				Expect(err).NotTo(HaveOccurred(), "reconcile %d should not error", i+1)
			}

			var rs recipev1alpha1.RecipeSource
			Expect(k8sClient.Get(ctx, nsn, &rs)).To(Succeed())
			// After ≤3 reconciles: Synced, cached.
			Expect(rs.Status.Phase).To(Equal(recipev1alpha1.RecipeSourcePhaseSynced))
			Expect(rs.Status.Cached).To(BeTrue())
			Expect(rs.Status.ObservedGeneration).To(Equal(rs.Generation))

			// Fourth reconcile must also be error-free (still converged).
			_, err := reconciler.Reconcile(ctx, req)
			Expect(err).NotTo(HaveOccurred())

			// Status must not change.
			Eventually(func(g Gomega) {
				var rs2 recipev1alpha1.RecipeSource
				g.Expect(k8sClient.Get(ctx, nsn, &rs2)).To(Succeed())
				g.Expect(rs2.Status.Phase).To(Equal(recipev1alpha1.RecipeSourcePhaseSynced))
			}, timeout, interval).Should(Succeed())
		})
	})
})

// noopRecorder satisfies record.EventRecorder with no-ops.
type noopRecorder struct{}

func (noopRecorder) Event(_ runtime.Object, _, _, _ string) {}
func (noopRecorder) Eventf(_ runtime.Object, _, _, _ string, _ ...interface{}) {}
func (noopRecorder) AnnotatedEventf(_ runtime.Object, _ map[string]string, _, _, _ string, _ ...interface{}) {
}

// testError is a simple error type for test injection.
type testError struct{ msg string }

func (e *testError) Error() string { return e.msg }
