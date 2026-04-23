// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

//go:build integration

package recipe

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	recipev1alpha1 "github.com/keese-ai/keese/api/recipe/v1alpha1"
)

var _ = Describe("Recipe Controller", func() {
	const (
		timeout  = 10 * time.Second
		interval = 100 * time.Millisecond
	)

	// buildRecipeReconciler creates a RecipeReconciler with fake dependencies.
	buildRecipeReconciler := func(rebac RebacWriter, extAuthz ExtAuthzChecker) *RecipeReconciler {
		return &RecipeReconciler{
			Client:   k8sClient,
			Scheme:   k8sClient.Scheme(),
			Recorder: noopRecorder{},
			Rebac:    rebac,
			ExtAuthz: extAuthz,
		}
	}

	// createSyncedRecipeSource creates a RecipeSource and patches its status to Synced.
	// Uses Eventually to retry the status patch against racing manager reconciles.
	createSyncedRecipeSource := func(name, ns, digest string) {
		rs := &recipev1alpha1.RecipeSource{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: ns,
				Labels:    map[string]string{"keese.ai/managed": "true"},
			},
			Spec: recipev1alpha1.RecipeSourceSpec{
				OCI: &recipev1alpha1.OCISource{
					Registry:   "ghcr.io",
					Repository: "keese-ai/recipes/test",
					Digest:     digest,
				},
			},
		}
		Expect(k8sClient.Create(ctx, rs)).To(Succeed())

		// Retry the status patch until it succeeds; the manager may have updated
		// the object between Create and our patch attempt.
		Eventually(func(g Gomega) {
			var fresh recipev1alpha1.RecipeSource
			g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: ns}, &fresh)).To(Succeed())
			fresh.Status.Phase = recipev1alpha1.RecipeSourcePhaseSynced
			fresh.Status.ResolvedDigest = digest
			fresh.Status.Cached = true
			fresh.Status.SourceType = recipev1alpha1.RecipeSourceTypeOCI
			fresh.Status.ObservedGeneration = fresh.Generation
			g.Expect(k8sClient.Status().Update(ctx, &fresh)).To(Succeed())
		}, timeout, interval).Should(Succeed())
	}

	// Spec 1: Recipe reaches Ready when RecipeSource is Synced.
	Context("Happy path: Recipe with synced OCI RecipeSource", func() {
		const (
			rsName     = "rs-recipe-happy"
			recipeName = "recipe-happy"
			ns         = "default"
			digest     = "sha256:1111111111111111111111111111111111111111111111111111111111111111"
		)

		BeforeEach(func() {
			createSyncedRecipeSource(rsName, ns, digest)

			recipe := &recipev1alpha1.Recipe{
				ObjectMeta: metav1.ObjectMeta{
					Name:      recipeName,
					Namespace: ns,
					Labels:    map[string]string{"keese.ai/managed": "true"},
				},
				Spec: recipev1alpha1.RecipeSpec{
					Instructions: "instructions.md",
					Model: recipev1alpha1.RecipeModel{
						Provider: "anthropic",
						ModelID:  "claude-sonnet-4-6",
					},
					SourceRef: recipev1alpha1.RecipeSourceRef{Name: rsName},
				},
			}
			Expect(k8sClient.Create(ctx, recipe)).To(Succeed())
		})

		AfterEach(func() {
			for _, name := range []string{recipeName} {
				obj := &recipev1alpha1.Recipe{}
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: ns}, obj); err == nil {
					Expect(k8sClient.Delete(ctx, obj)).To(Succeed())
				}
			}
			rs := &recipev1alpha1.RecipeSource{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: rsName, Namespace: ns}, rs); err == nil {
				Expect(k8sClient.Delete(ctx, rs)).To(Succeed())
			}
		})

		It("should reconcile Recipe to Ready and populate resolvedDigest", func() {
			reconciler := buildRecipeReconciler(&FakeRebacWriter{}, &FakeExtAuthzChecker{})
			nsn := types.NamespacedName{Name: recipeName, Namespace: ns}
			req := reconcile.Request{NamespacedName: nsn}

			// First reconcile: adds finalizer.
			_, err := reconciler.Reconcile(ctx, req)
			Expect(err).NotTo(HaveOccurred())

			// Second reconcile: resolves RecipeSource, syncs ReBAC, sets Ready.
			_, err = reconciler.Reconcile(ctx, req)
			Expect(err).NotTo(HaveOccurred())

			var recipe recipev1alpha1.Recipe
			Expect(k8sClient.Get(ctx, nsn, &recipe)).To(Succeed())
			Expect(recipe.Status.Phase).To(Equal(recipev1alpha1.RecipePhaseReady))
			Expect(recipe.Status.ResolvedDigest).To(Equal(digest))
			Expect(recipe.Status.RebacTupleCount).To(BeNumerically(">", 0))

			// Verify Ready condition.
			var readyCond *metav1.Condition
			for i := range recipe.Status.Conditions {
				if recipe.Status.Conditions[i].Type == recipev1alpha1.RecipeConditionReady {
					readyCond = &recipe.Status.Conditions[i]
					break
				}
			}
			Expect(readyCond).NotTo(BeNil())
			Expect(readyCond.Status).To(Equal(metav1.ConditionTrue))
		})
	})

	// Spec 2: Three-gate happy path (tools + model + extension all allowed).
	Context("Three-gate admission: all gates pass", func() {
		const (
			rsName     = "rs-gates-happy"
			recipeName = "recipe-gates-happy"
			ns         = "default"
			digest     = "sha256:2222222222222222222222222222222222222222222222222222222222222222"
		)

		BeforeEach(func() {
			createSyncedRecipeSource(rsName, ns, digest)

			recipe := &recipev1alpha1.Recipe{
				ObjectMeta: metav1.ObjectMeta{
					Name:      recipeName,
					Namespace: ns,
					Labels:    map[string]string{"keese.ai/managed": "true"},
				},
				Spec: recipev1alpha1.RecipeSpec{
					Instructions: "instructions.md",
					Model: recipev1alpha1.RecipeModel{
						Provider: "anthropic",
						ModelID:  "claude-sonnet-4-6",
					},
					Tools: []recipev1alpha1.RecipeTool{
						{Name: "read_file"},
						{Name: "web_search"},
					},
					Extensions: []recipev1alpha1.RecipeExtension{
						{Name: "my-ext", Namespace: ns},
					},
					SourceRef: recipev1alpha1.RecipeSourceRef{Name: rsName},
				},
			}
			Expect(k8sClient.Create(ctx, recipe)).To(Succeed())
		})

		AfterEach(func() {
			for _, name := range []string{recipeName} {
				obj := &recipev1alpha1.Recipe{}
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: ns}, obj); err == nil {
					Expect(k8sClient.Delete(ctx, obj)).To(Succeed())
				}
			}
			rs := &recipev1alpha1.RecipeSource{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: rsName, Namespace: ns}, rs); err == nil {
				Expect(k8sClient.Delete(ctx, rs)).To(Succeed())
			}
		})

		It("should reach Ready when all three gates pass", func() {
			extChecker := &FakeExtAuthzChecker{
				AllowedExtensions: map[string]bool{"my-ext": true},
			}
			policy := &EffectivePolicy{
				AllowedTools:  map[string]bool{"read_file": true, "web_search": true},
				AllowedModels: map[string]bool{"anthropic/claude-sonnet-4-6": true},
			}

			// Verify the three gates all pass.
			recipe := &recipev1alpha1.Recipe{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: recipeName, Namespace: ns}, recipe)).To(Succeed())

			admitReq := AdmissionRequest{
				Recipe:          recipe,
				WorkspaceName:   "ws-test",
				EffectivePolicy: policy,
			}
			Expect(CheckThreeGates(ctx, admitReq, extChecker)).To(Succeed())

			// Full reconcile should also reach Ready.
			reconciler := buildRecipeReconciler(&FakeRebacWriter{}, extChecker)
			nsn := types.NamespacedName{Name: recipeName, Namespace: ns}
			req := reconcile.Request{NamespacedName: nsn}

			_, _ = reconciler.Reconcile(ctx, req)
			_, err := reconciler.Reconcile(ctx, req)
			Expect(err).NotTo(HaveOccurred())

			Expect(k8sClient.Get(ctx, nsn, recipe)).To(Succeed())
			Expect(recipe.Status.Phase).To(Equal(recipev1alpha1.RecipePhaseReady))
		})
	})

	// Spec 3: Three-gate denial — tool not in allowlist.
	Context("Three-gate admission: tool gate denial", func() {
		It("RecipeToolNotAllowed: should deny when tool is not in effectivePolicy.tools.allow", func() {
			recipe := &recipev1alpha1.Recipe{
				ObjectMeta: metav1.ObjectMeta{Name: "denied-recipe", Namespace: "default"},
				Spec: recipev1alpha1.RecipeSpec{
					Instructions: "instructions.md",
					Model:        recipev1alpha1.RecipeModel{Provider: "anthropic", ModelID: "claude-sonnet-4-6"},
					Tools:        []recipev1alpha1.RecipeTool{{Name: "dangerous_tool"}},
					SourceRef:    recipev1alpha1.RecipeSourceRef{Name: "rs"},
				},
			}
			policy := &EffectivePolicy{
				AllowedTools:  map[string]bool{"read_file": true},
				AllowedModels: map[string]bool{"anthropic/claude-sonnet-4-6": true},
			}

			err := CheckThreeGates(ctx, AdmissionRequest{
				Recipe:          recipe,
				WorkspaceName:   "ws",
				EffectivePolicy: policy,
			}, &FakeExtAuthzChecker{})

			Expect(err).To(HaveOccurred())
			admitErr, ok := err.(*AdmissionError)
			Expect(ok).To(BeTrue())
			Expect(admitErr.Gate).To(Equal("tool"))
			Expect(admitErr.Reason).To(Equal(ReasonRecipeToolNotAllowed))
		})
	})

	// Spec 4: Three-gate denial — model not in allowlist.
	Context("Three-gate admission: model gate denial", func() {
		It("RecipeModelNotAllowed: should deny when model is not in effectivePolicy allowed-model list", func() {
			recipe := &recipev1alpha1.Recipe{
				ObjectMeta: metav1.ObjectMeta{Name: "denied-recipe", Namespace: "default"},
				Spec: recipev1alpha1.RecipeSpec{
					Instructions: "instructions.md",
					Model:        recipev1alpha1.RecipeModel{Provider: "anthropic", ModelID: "claude-opus-not-allowed"},
					SourceRef:    recipev1alpha1.RecipeSourceRef{Name: "rs"},
				},
			}
			policy := &EffectivePolicy{
				AllowedTools:  map[string]bool{},
				AllowedModels: map[string]bool{"anthropic/claude-sonnet-4-6": true},
			}

			err := CheckThreeGates(ctx, AdmissionRequest{
				Recipe:          recipe,
				WorkspaceName:   "ws",
				EffectivePolicy: policy,
			}, &FakeExtAuthzChecker{})

			Expect(err).To(HaveOccurred())
			admitErr, ok := err.(*AdmissionError)
			Expect(ok).To(BeTrue())
			Expect(admitErr.Gate).To(Equal("model"))
			Expect(admitErr.Reason).To(Equal(ReasonRecipeModelNotAllowed))
		})
	})

	// Spec 5: Three-gate denial — extension not enabled via OpenFGA.
	Context("Three-gate admission: extension gate denial", func() {
		It("RecipeExtensionNotEnabled: should deny when OpenFGA returns extension not enabled", func() {
			recipe := &recipev1alpha1.Recipe{
				ObjectMeta: metav1.ObjectMeta{Name: "ext-denied-recipe", Namespace: "default"},
				Spec: recipev1alpha1.RecipeSpec{
					Instructions: "instructions.md",
					Model:        recipev1alpha1.RecipeModel{Provider: "anthropic", ModelID: "claude-sonnet-4-6"},
					Extensions: []recipev1alpha1.RecipeExtension{
						{Name: "restricted-ext", Namespace: "default"},
					},
					SourceRef: recipev1alpha1.RecipeSourceRef{Name: "rs"},
				},
			}
			policy := &EffectivePolicy{
				AllowedTools:  map[string]bool{},
				AllowedModels: map[string]bool{"anthropic/claude-sonnet-4-6": true},
			}
			extChecker := &FakeExtAuthzChecker{
				DenyExtensions: map[string]bool{"restricted-ext": true},
			}

			err := CheckThreeGates(ctx, AdmissionRequest{
				Recipe:          recipe,
				WorkspaceName:   "ws",
				EffectivePolicy: policy,
			}, extChecker)

			Expect(err).To(HaveOccurred())
			admitErr, ok := err.(*AdmissionError)
			Expect(ok).To(BeTrue())
			Expect(admitErr.Gate).To(Equal("extension"))
			Expect(admitErr.Reason).To(Equal(ReasonRecipeExtensionNotEnabled))
		})

		It("RecipeAdmitExtAuthzTimeout: should fail-closed when OpenFGA check exceeds 500ms", func() {
			recipe := &recipev1alpha1.Recipe{
				ObjectMeta: metav1.ObjectMeta{Name: "timeout-recipe", Namespace: "default"},
				Spec: recipev1alpha1.RecipeSpec{
					Instructions: "instructions.md",
					Model:        recipev1alpha1.RecipeModel{Provider: "anthropic", ModelID: "claude-sonnet-4-6"},
					Extensions: []recipev1alpha1.RecipeExtension{
						{Name: "slow-ext", Namespace: "default"},
					},
					SourceRef: recipev1alpha1.RecipeSourceRef{Name: "rs"},
				},
			}
			policy := &EffectivePolicy{
				AllowedTools:  map[string]bool{},
				AllowedModels: map[string]bool{"anthropic/claude-sonnet-4-6": true},
			}
			extChecker := &FakeExtAuthzChecker{
				TimeoutOnCheck: true,
			}

			err := CheckThreeGates(ctx, AdmissionRequest{
				Recipe:          recipe,
				WorkspaceName:   "ws",
				EffectivePolicy: policy,
			}, extChecker)

			Expect(err).To(HaveOccurred())
			admitErr, ok := err.(*AdmissionError)
			Expect(ok).To(BeTrue())
			Expect(admitErr.Reason).To(Equal(ReasonRecipeAdmitExtAuthzTimeout))
		})
	})

	// Spec 6: TOCTOU stale parent status rejection.
	Context("TOCTOU: stale GuardrailBinding policy", func() {
		It("StaleParentStatus: should reject when GuardrailBinding generation does not match workspace", func() {
			recipe := &recipev1alpha1.Recipe{
				ObjectMeta: metav1.ObjectMeta{Name: "stale-recipe", Namespace: "default"},
				Spec: recipev1alpha1.RecipeSpec{
					Instructions: "instructions.md",
					Model:        recipev1alpha1.RecipeModel{Provider: "anthropic", ModelID: "claude-sonnet-4-6"},
					SourceRef:    recipev1alpha1.RecipeSourceRef{Name: "rs"},
				},
			}
			policy := &EffectivePolicy{
				AllowedTools:       map[string]bool{},
				AllowedModels:      map[string]bool{"anthropic/claude-sonnet-4-6": true},
				ObservedGeneration: 5, // stale: workspace expects generation 6
			}

			err := CheckThreeGates(ctx, AdmissionRequest{
				Recipe:                       recipe,
				WorkspaceName:                "ws",
				WorkspaceGuardrailGeneration: 6, // workspace's expected generation
				EffectivePolicy:              policy,
			}, &FakeExtAuthzChecker{})

			Expect(err).To(HaveOccurred())
			admitErr, ok := err.(*AdmissionError)
			Expect(ok).To(BeTrue())
			Expect(admitErr.Reason).To(Equal(ReasonStaleParentStatus))
		})
	})

	// Spec 7: Idempotency — three reconciles converge.
	Context("Idempotency: three reconciles with no spec change", func() {
		const (
			rsName     = "rs-recipe-idempotent"
			recipeName = "recipe-idempotent"
			ns         = "default"
			digest     = "sha256:3333333333333333333333333333333333333333333333333333333333333333"
		)

		BeforeEach(func() {
			createSyncedRecipeSource(rsName, ns, digest)

			recipe := &recipev1alpha1.Recipe{
				ObjectMeta: metav1.ObjectMeta{
					Name:      recipeName,
					Namespace: ns,
					Labels:    map[string]string{"keese.ai/managed": "true"},
				},
				Spec: recipev1alpha1.RecipeSpec{
					Instructions: "instructions.md",
					Model: recipev1alpha1.RecipeModel{
						Provider: "anthropic",
						ModelID:  "claude-sonnet-4-6",
					},
					SourceRef: recipev1alpha1.RecipeSourceRef{Name: rsName},
				},
			}
			Expect(k8sClient.Create(ctx, recipe)).To(Succeed())
		})

		AfterEach(func() {
			for _, name := range []string{recipeName} {
				obj := &recipev1alpha1.Recipe{}
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: ns}, obj); err == nil {
					Expect(k8sClient.Delete(ctx, obj)).To(Succeed())
				}
			}
			rs := &recipev1alpha1.RecipeSource{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: rsName, Namespace: ns}, rs); err == nil {
				Expect(k8sClient.Delete(ctx, rs)).To(Succeed())
			}
		})

		It("PullIdempotencyThreeReconciles: should converge in ≤3 reconciles and remain stable", func() {
			reconciler := buildRecipeReconciler(&FakeRebacWriter{}, &FakeExtAuthzChecker{})
			nsn := types.NamespacedName{Name: recipeName, Namespace: ns}
			req := reconcile.Request{NamespacedName: nsn}

			for i := range 3 {
				_, err := reconciler.Reconcile(ctx, req)
				Expect(err).NotTo(HaveOccurred(), "reconcile %d should not error", i+1)
			}

			var recipe recipev1alpha1.Recipe
			Expect(k8sClient.Get(ctx, nsn, &recipe)).To(Succeed())
			Expect(recipe.Status.Phase).To(Equal(recipev1alpha1.RecipePhaseReady))
			Expect(recipe.Status.ObservedGeneration).To(Equal(recipe.Generation))

			// Fourth reconcile must not change status.
			_, err := reconciler.Reconcile(ctx, req)
			Expect(err).NotTo(HaveOccurred())

			Eventually(func(g Gomega) {
				var r2 recipev1alpha1.Recipe
				g.Expect(k8sClient.Get(ctx, nsn, &r2)).To(Succeed())
				g.Expect(r2.Status.Phase).To(Equal(recipev1alpha1.RecipePhaseReady))
			}, timeout, interval).Should(Succeed())
		})
	})

	// Spec 8: Recipe waits when RecipeSource is not yet Synced.
	Context("Recipe waits for RecipeSource to sync", func() {
		const (
			rsName     = "rs-pending"
			recipeName = "recipe-pending-source"
			ns         = "default"
		)

		BeforeEach(func() {
			// Create RecipeSource WITHOUT the managed label so the suite's manager
			// ignores it (predicate filters on keese.ai/managed=true). This lets
			// the test assert the "source not yet synced" path without a race.
			rs := &recipev1alpha1.RecipeSource{
				ObjectMeta: metav1.ObjectMeta{
					Name:      rsName,
					Namespace: ns,
					// No keese.ai/managed label — manager predicate skips this object.
				},
				Spec: recipev1alpha1.RecipeSourceSpec{
					OCI: &recipev1alpha1.OCISource{
						Registry:   "ghcr.io",
						Repository: "keese-ai/recipes/pending",
						Digest:     "sha256:4444444444444444444444444444444444444444444444444444444444444444",
					},
				},
			}
			Expect(k8sClient.Create(ctx, rs)).To(Succeed())

			recipe := &recipev1alpha1.Recipe{
				ObjectMeta: metav1.ObjectMeta{
					Name:      recipeName,
					Namespace: ns,
					Labels:    map[string]string{"keese.ai/managed": "true"},
				},
				Spec: recipev1alpha1.RecipeSpec{
					Instructions: "instructions.md",
					Model:        recipev1alpha1.RecipeModel{Provider: "anthropic", ModelID: "claude-sonnet-4-6"},
					SourceRef:    recipev1alpha1.RecipeSourceRef{Name: rsName},
				},
			}
			Expect(k8sClient.Create(ctx, recipe)).To(Succeed())
		})

		AfterEach(func() {
			for _, name := range []string{recipeName} {
				obj := &recipev1alpha1.Recipe{}
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: ns}, obj); err == nil {
					Expect(k8sClient.Delete(ctx, obj)).To(Succeed())
				}
			}
			rs := &recipev1alpha1.RecipeSource{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: rsName, Namespace: ns}, rs); err == nil {
				Expect(k8sClient.Delete(ctx, rs)).To(Succeed())
			}
		})

		It("should return RequeueAfter when RecipeSource is not yet Synced", func() {
			reconciler := buildRecipeReconciler(&FakeRebacWriter{}, &FakeExtAuthzChecker{})
			nsn := types.NamespacedName{Name: recipeName, Namespace: ns}
			req := reconcile.Request{NamespacedName: nsn}

			_, _ = reconciler.Reconcile(ctx, req) // adds finalizer
			res, err := reconciler.Reconcile(ctx, req)
			Expect(err).NotTo(HaveOccurred())
			// RequeueAfter should be set because RecipeSource is not Synced.
			Expect(res.RequeueAfter).To(BeNumerically(">", 0),
				"expected RequeueAfter when RecipeSource is not yet synced")

			var recipe recipev1alpha1.Recipe
			Expect(k8sClient.Get(ctx, nsn, &recipe)).To(Succeed())
			Expect(recipe.Status.Phase).To(Equal(recipev1alpha1.RecipePhasePulling))
		})
	})

	// Spec 9: UpgradeDigestBump — digest change triggers re-pull.
	Context("UpgradeDigestBump: new digest in RecipeSource triggers update", func() {
		const (
			rsName     = "rs-upgrade"
			recipeName = "recipe-upgrade"
			ns         = "default"
			digest1    = "sha256:5555555555555555555555555555555555555555555555555555555555555555"
			digest2    = "sha256:6666666666666666666666666666666666666666666666666666666666666666"
		)

		BeforeEach(func() {
			createSyncedRecipeSource(rsName, ns, digest1)

			recipe := &recipev1alpha1.Recipe{
				ObjectMeta: metav1.ObjectMeta{
					Name:      recipeName,
					Namespace: ns,
					Labels:    map[string]string{"keese.ai/managed": "true"},
				},
				Spec: recipev1alpha1.RecipeSpec{
					Instructions: "instructions.md",
					Model:        recipev1alpha1.RecipeModel{Provider: "anthropic", ModelID: "claude-sonnet-4-6"},
					SourceRef:    recipev1alpha1.RecipeSourceRef{Name: rsName},
				},
			}
			Expect(k8sClient.Create(ctx, recipe)).To(Succeed())
		})

		AfterEach(func() {
			for _, name := range []string{recipeName} {
				obj := &recipev1alpha1.Recipe{}
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: ns}, obj); err == nil {
					Expect(k8sClient.Delete(ctx, obj)).To(Succeed())
				}
			}
			rs := &recipev1alpha1.RecipeSource{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: rsName, Namespace: ns}, rs); err == nil {
				Expect(k8sClient.Delete(ctx, rs)).To(Succeed())
			}
		})

		It("UpgradeDigestBump: updating RecipeSource digest causes Recipe to reflect new digest", func() {
			reconciler := buildRecipeReconciler(&FakeRebacWriter{}, &FakeExtAuthzChecker{})
			nsn := types.NamespacedName{Name: recipeName, Namespace: ns}
			req := reconcile.Request{NamespacedName: nsn}

			// Reconcile to Ready — RecipeSource may hold any synced digest at this point
			// (manager's FakeOCIFetcher races with createSyncedRecipeSource). Read the
			// actual digest from the source after it is Synced.
			_, _ = reconciler.Reconcile(ctx, req)
			_, err := reconciler.Reconcile(ctx, req)
			Expect(err).NotTo(HaveOccurred())

			var rs recipev1alpha1.RecipeSource
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: rsName, Namespace: ns}, &rs)).To(Succeed())
			initialDigest := rs.Status.ResolvedDigest
			Expect(initialDigest).NotTo(BeEmpty())

			var recipe recipev1alpha1.Recipe
			Expect(k8sClient.Get(ctx, nsn, &recipe)).To(Succeed())
			Expect(recipe.Status.ResolvedDigest).To(Equal(initialDigest))

			// Bump RecipeSource to digest2 (simulate an upgrade). Use Eventually to
			// win any concurrent manager status patch.
			Eventually(func(g Gomega) {
				var freshRS recipev1alpha1.RecipeSource
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: rsName, Namespace: ns}, &freshRS)).To(Succeed())
				freshRS.Status.ResolvedDigest = digest2
				freshRS.Status.Cached = true
				freshRS.Status.Phase = recipev1alpha1.RecipeSourcePhaseSynced
				g.Expect(k8sClient.Status().Update(ctx, &freshRS)).To(Succeed())
			}, timeout, interval).Should(Succeed())

			// Reconcile again — Recipe should pick up digest2.
			_, err = reconciler.Reconcile(ctx, req)
			Expect(err).NotTo(HaveOccurred())

			Eventually(func(g Gomega) {
				var r2 recipev1alpha1.Recipe
				g.Expect(k8sClient.Get(ctx, nsn, &r2)).To(Succeed())
				g.Expect(r2.Status.ResolvedDigest).To(Equal(digest2))
			}, timeout, interval).Should(Succeed())
		})
	})
})
