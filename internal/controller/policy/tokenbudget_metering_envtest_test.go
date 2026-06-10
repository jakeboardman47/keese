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
	"sigs.k8s.io/controller-runtime/pkg/client"

	policyv1alpha1 "github.com/keese-ai/keese/api/policy/v1alpha1"
)

// These specs drive the un-stubbed TokenBudget enforcement loop (CH5c) against a
// MOCK Prometheus (the shared `fakeQuerier`, whose DefaultValue stands in for the
// `increase(keese_token_budget_consumed_total{…}[window])` series CH5a/CH5b now
// feed). They assert the consume→compare→signal crossover, exhaustionMode
// (hard/soft/disabled) routing, the window reset, and — most importantly — the
// fail-open rule from ADR 30 §failure-modes: a Prometheus fetch error must NEVER
// false-clear an already-tripped exceeded signal.

// newScopedBudget builds a tenant-scoped TokenBudget with a single total-token
// limit and the requested exhaustion mode. A distinct tenant name per case keeps
// the NATS KV key (tenant/<name>/<model>) isolated across specs that share the
// process-global fakeNats.
func newScopedBudget(name, ns, tenant string, mode policyv1alpha1.ExhaustionMode, limit int64) *policyv1alpha1.TokenBudget {
	tb := newTokenBudget(name, ns)
	tb.Spec.Scope.Tenant = &policyv1alpha1.TokenBudgetScopeRef{Name: tenant}
	tb.Spec.ExhaustionMode = mode
	tb.Spec.Limits = []policyv1alpha1.TokenLimit{{Model: "gpt-4", TotalTokens: ptr(limit)}}
	return tb
}

func getBudget(name, ns string) *policyv1alpha1.TokenBudget {
	var t policyv1alpha1.TokenBudget
	if err := k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: ns}, &t); err != nil {
		return nil
	}
	return &t
}

var _ = Describe("TokenBudget metering enforcement (CH5c)", func() {
	const ns = "default"
	const model = "gpt-4"

	Describe("consumed vs spec.limits crossover", func() {
		// Table-driven: each case sets the mock-Prom consumed value, the limit, and
		// the exhaustion mode, then asserts the derived phase + whether the NATS KV
		// hard-429 signal was written. "at budget" exercises the >= boundary
		// (consumed == limit must trip, per isExceeded).
		type crossCase struct {
			name        string
			consumed    float64
			limit       int64
			mode        policyv1alpha1.ExhaustionMode
			wantPhase   policyv1alpha1.TokenBudgetPhase
			wantNATSSet bool // expect SetExceeded → hard-429 KV write
		}

		cases := []crossCase{
			{
				name: "under-budget-hard", consumed: 500, limit: 10000,
				mode: policyv1alpha1.ExhaustionModeHard,
				wantPhase: policyv1alpha1.TokenBudgetPhaseReady, wantNATSSet: false,
			},
			{
				name: "at-budget-hard", consumed: 10000, limit: 10000,
				mode: policyv1alpha1.ExhaustionModeHard,
				wantPhase: policyv1alpha1.TokenBudgetPhaseExhausted, wantNATSSet: true,
			},
			{
				name: "over-budget-hard", consumed: 12000, limit: 10000,
				mode: policyv1alpha1.ExhaustionModeHard,
				wantPhase: policyv1alpha1.TokenBudgetPhaseExhausted, wantNATSSet: true,
			},
			{
				name: "over-budget-soft", consumed: 12000, limit: 10000,
				mode: policyv1alpha1.ExhaustionModeSoft,
				wantPhase: policyv1alpha1.TokenBudgetPhaseSoftExhausted, wantNATSSet: false,
			},
			{
				name: "over-budget-disabled", consumed: 12000, limit: 10000,
				mode: policyv1alpha1.ExhaustionModeDisabled,
				wantPhase: policyv1alpha1.TokenBudgetPhaseReady, wantNATSSet: false,
			},
		}

		for _, tc := range cases {
			tc := tc
			It("routes "+tc.name+" correctly", func() {
				tenant := "t-" + tc.name
				key := budgetExceededKey("tenant", tenant, model)

				fakeQuerier.Results = map[string]float64{}
				fakeQuerier.DefaultValue = tc.consumed
				fakeQuerier.FailNext = false
				fakeNats.Reset()

				tb := newScopedBudget("tb-"+tc.name, ns, tenant, tc.mode, tc.limit)
				Expect(k8sClient.Create(ctx, tb)).To(Succeed())
				defer func() { _ = k8sClient.Delete(ctx, tb) }()

				By("waiting for the reconciler to derive the expected phase")
				Eventually(func() policyv1alpha1.TokenBudgetPhase {
					if t := getBudget("tb-"+tc.name, ns); t != nil {
						return t.Status.Phase
					}
					return ""
				}, 20*time.Second, 250*time.Millisecond).Should(Equal(tc.wantPhase))

				By("asserting the NATS hard-429 signal matches the mode")
				if tc.wantNATSSet {
					Eventually(func() bool {
						return fakeNats.IsExceeded(key)
					}, 5*time.Second, 100*time.Millisecond).Should(BeTrue(),
						"hard-mode crossover must write the exceeded KV key")
				} else {
					// soft / disabled / under-budget must never set the hard-429 key.
					Consistently(func() bool {
						return fakeNats.IsExceeded(key)
					}, 2*time.Second, 200*time.Millisecond).Should(BeFalse(),
						"non-hard / under-budget must not write the exceeded KV key")
				}

				By("asserting consumedCurrent is derived from the mock series (rule 04.4)")
				t := getBudget("tb-"+tc.name, ns)
				Expect(t).NotTo(BeNil())
				Expect(t.Status.ConsumedCurrent).NotTo(BeEmpty())
				Expect(t.Status.ConsumedCurrent[0].TotalTokens).To(Equal(int64(tc.consumed)))
			})
		}
	})

	Describe("fail-open on Prometheus fetch error", func() {
		// ADR 30 §failure-modes: budgets fail OPEN, but an already-tripped signal
		// stays CLOSED — the reconciler must NEVER false-clear an existing exceeded
		// key on a fetch error. We trip the budget (consumed > limit), confirm the
		// KV key is set, then make the next query fail and assert the key persists
		// AND the MetricFetchHealthy condition flips False.
		const name = "tb-failopen"
		const tenant = "t-failopen"

		It("never false-clears an existing exceeded signal when the query errors", func() {
			key := budgetExceededKey("tenant", tenant, model)

			fakeQuerier.Results = map[string]float64{}
			fakeQuerier.DefaultValue = 12000 // over the 10000 limit
			fakeQuerier.FailNext = false
			fakeNats.Reset()

			tb := newScopedBudget(name, ns, tenant, policyv1alpha1.ExhaustionModeHard, 10000)
			Expect(k8sClient.Create(ctx, tb)).To(Succeed())
			defer func() { _ = k8sClient.Delete(ctx, tb) }()

			By("waiting for the budget to trip (KV exceeded key written)")
			Eventually(func() bool {
				return fakeNats.IsExceeded(key)
			}, 20*time.Second, 250*time.Millisecond).Should(BeTrue())

			By("arming a Prometheus fetch failure for the next reconcile")
			fakeQuerier.FailNext = true

			By("waiting for the MetricFetchFailed event (fetch error observed)")
			Eventually(func() bool {
				var evs corev1.EventList
				if err := k8sClient.List(ctx, &evs, &client.ListOptions{Namespace: ns}); err != nil {
					return false
				}
				for _, e := range evs.Items {
					if e.Reason == ReasonMetricFetchFailed && e.InvolvedObject.Name == name {
						return true
					}
				}
				return false
			}, 20*time.Second, 500*time.Millisecond).Should(BeTrue())

			By("asserting the exceeded signal PERSISTS across the fetch error (no false-clear)")
			Consistently(func() bool {
				return fakeNats.IsExceeded(key)
			}, 3*time.Second, 200*time.Millisecond).Should(BeTrue(),
				"fail-open must not clear an already-tripped budget signal")

			By("asserting MetricFetchHealthy condition is False")
			t := getBudget(name, ns)
			Expect(t).NotTo(BeNil())
			var cond *metav1.Condition
			for i := range t.Status.Conditions {
				if t.Status.Conditions[i].Type == "MetricFetchHealthy" {
					cond = &t.Status.Conditions[i]
					break
				}
			}
			Expect(cond).NotTo(BeNil())
			Expect(cond.Status).To(Equal(metav1.ConditionFalse))
		})
	})

	Describe("window reset clears the exceeded signal", func() {
		// On window boundary the reconciler archives consumedCurrent→Previous, zeroes
		// current, and DELETES the NATS KV key (10b/ADR 30 §window-accounting). Reset
		// is unconditional — a healthy boundary crossing always clears, distinct from
		// the fail-open path above which must not clear.
		const name = "tb-reset-clears"
		const tenant = "t-reset-clears"

		It("deletes the exceeded key and re-derives phase Ready after a boundary", func() {
			key := budgetExceededKey("tenant", tenant, model)

			fakeQuerier.Results = map[string]float64{}
			fakeQuerier.DefaultValue = 12000 // start over-limit so the key is set
			fakeQuerier.FailNext = false
			fakeNats.Reset()

			tb := newScopedBudget(name, ns, tenant, policyv1alpha1.ExhaustionModeHard, 10000)
			tb.Spec.WindowDuration = "1h"
			Expect(k8sClient.Create(ctx, tb)).To(Succeed())
			defer func() { _ = k8sClient.Delete(ctx, tb) }()

			By("waiting for the budget to trip")
			Eventually(func() bool {
				return fakeNats.IsExceeded(key)
			}, 20*time.Second, 250*time.Millisecond).Should(BeTrue())

			By("dropping consumed below the limit so the new window is Ready")
			fakeQuerier.DefaultValue = 100

			By("forcing the window boundary into the past to trigger reset")
			Eventually(func() error {
				latest := getBudget(name, ns)
				if latest == nil || latest.Status.WindowEnd == "" {
					return fmt.Errorf("window not initialised yet")
				}
				orig := latest.DeepCopy()
				latest.Status.WindowStart = time.Now().UTC().Add(-1*time.Hour - time.Second).Format(time.RFC3339)
				latest.Status.WindowEnd = time.Now().UTC().Add(-time.Second).Format(time.RFC3339)
				return k8sClient.Status().Patch(ctx, latest, client.MergeFrom(orig))
			}, 10*time.Second, 250*time.Millisecond).Should(Succeed())

			By("waiting for the BudgetReset event")
			Eventually(func() bool {
				var evs corev1.EventList
				if err := k8sClient.List(ctx, &evs, &client.ListOptions{Namespace: ns}); err != nil {
					return false
				}
				for _, e := range evs.Items {
					if e.Reason == ReasonBudgetReset && e.InvolvedObject.Name == name {
						return true
					}
				}
				return false
			}, 30*time.Second, 500*time.Millisecond).Should(BeTrue())

			By("asserting the exceeded key was cleared on reset")
			Eventually(func() bool {
				return fakeNats.IsExceeded(key)
			}, 10*time.Second, 250*time.Millisecond).Should(BeFalse())

			By("asserting consumedPrevious carries the archived window")
			Eventually(func() int {
				t := getBudget(name, ns)
				if t == nil {
					return 0
				}
				return len(t.Status.ConsumedPrevious)
			}, 10*time.Second, 250*time.Millisecond).Should(BeNumerically(">", 0))
		})
	})
})
