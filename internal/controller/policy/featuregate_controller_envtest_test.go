// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

//go:build integration

package policy

import (
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	policyv1alpha1 "github.com/keese-ai/keese/api/policy/v1alpha1"
)

// featureGateTestNamespace is the projection target the suite injects
// into FeatureGateReconciler.Namespace. envtest does not create the
// canonical keese-system namespace, so we project into the
// always-present default namespace instead.
const featureGateTestNamespace = "default"

// newFeatureGate returns a minimal valid cluster-scoped FeatureGate for
// envtest. spec.description is required (MinLength=1); a beta gate with no
// override projects effective=true (DefaultEffective(beta)).
func newFeatureGate(name string) *policyv1alpha1.FeatureGate {
	return &policyv1alpha1.FeatureGate{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: policyv1alpha1.FeatureGateSpec{
			Description: "envtest idempotency gate",
			Stage:       policyv1alpha1.FeatureGateStageBeta,
		},
	}
}

// projectionFor reads the keese-features ConfigMap and returns the raw
// gates.json payload plus the ConfigMap's resourceVersion. Empty payload
// signals the CM is absent or not yet written.
func projectionFor() (data, resourceVersion string) {
	cm := &corev1.ConfigMap{}
	if err := k8sClient.Get(ctx, types.NamespacedName{
		Name:      FeatureGateConfigMapName,
		Namespace: featureGateTestNamespace,
	}, cm); err != nil {
		return "", ""
	}
	return cm.Data[FeatureGateConfigMapKey], cm.ResourceVersion
}

var _ = Describe("FeatureGate Controller", func() {
	// Rule 04.16: the idempotency assertion must run in envtest (CRDs
	// from config/crd/bases, >= 3 reconciles, no spec change). The fake
	// client unit test in featuregate_controller_test.go stays as the
	// fast tier; this is the envtest-tier counterpart.
	Context("Idempotency", func() {
		const resourceName = "fg-idempotency"

		var fg *policyv1alpha1.FeatureGate

		BeforeEach(func() {
			fg = newFeatureGate(resourceName)
			Expect(k8sClient.Create(ctx, fg)).To(Succeed())
		})

		AfterEach(func() {
			Expect(k8sClient.Delete(ctx, fg)).To(Succeed())
			Eventually(func() bool {
				var got policyv1alpha1.FeatureGate
				return k8sClient.Get(ctx, types.NamespacedName{Name: resourceName}, &got) != nil
			}, 10*time.Second, 250*time.Millisecond).Should(BeTrue())
		})

		It("should converge to a stable observedGeneration and projection ConfigMap across >= 3 reconciles with no spec change", func() {
			By("waiting for the first reconcile to set status.observedGeneration and project the gate")
			Eventually(func() bool {
				var got policyv1alpha1.FeatureGate
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: resourceName}, &got); err != nil {
					return false
				}
				data, _ := projectionFor()
				return got.Status.ObservedGeneration == got.Generation && data != ""
			}, 20*time.Second, 250*time.Millisecond).Should(BeTrue())

			By("capturing the converged status and projection state")
			var converged policyv1alpha1.FeatureGate
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: resourceName}, &converged)).To(Succeed())
			Expect(converged.Status.Effective).To(BeTrue(), "beta gate with no override must project effective=true")

			firstGen := converged.Status.ObservedGeneration
			firstEffective := converged.Status.Effective
			firstData, firstCMRV := projectionFor()
			Expect(firstData).NotTo(BeEmpty())
			Expect(firstCMRV).NotTo(BeEmpty())

			By("forcing >= 3 additional reconciles via metadata-only updates (no spec change)")
			// A metadata (annotation) update bumps resourceVersion and
			// fires a watch event — re-driving Reconcile — but leaves
			// .spec untouched, so .metadata.generation is unchanged. This
			// is exactly the "reconcile again with no spec change" path
			// rule 04.16 requires.
			for i := 0; i < 3; i++ {
				var latest policyv1alpha1.FeatureGate
				Expect(k8sClient.Get(ctx, types.NamespacedName{Name: resourceName}, &latest)).To(Succeed())
				if latest.Annotations == nil {
					latest.Annotations = map[string]string{}
				}
				latest.Annotations["keese.ai/idempotency-nudge"] = fmt.Sprintf("%d", i)
				Expect(k8sClient.Update(ctx, &latest)).To(Succeed())

				By(fmt.Sprintf("asserting nudge %d left generation, effective value, and projection unchanged", i))
				Consistently(func() bool {
					var got policyv1alpha1.FeatureGate
					if err := k8sClient.Get(ctx, types.NamespacedName{Name: resourceName}, &got); err != nil {
						return false
					}
					// generation must not advance on a metadata-only update,
					// observedGeneration must track it, and effective must hold.
					if got.Generation != firstGen ||
						got.Status.ObservedGeneration != firstGen ||
						got.Status.Effective != firstEffective {
						return false
					}
					// The projection ConfigMap payload must be byte-stable
					// and must not be rewritten (resourceVersion stable) —
					// the reconciler's read-before-write no-op guard.
					data, cmRV := projectionFor()
					return data == firstData && cmRV == firstCMRV
				}, 4*time.Second, 250*time.Millisecond).Should(BeTrue(),
					"reconcile produced a spurious write: status or projection drifted with no spec change")
			}

			By("asserting the converged effective value is still projected into keese-features")
			finalData, _ := projectionFor()
			Expect(finalData).To(Equal(firstData))
		})
	})
})
