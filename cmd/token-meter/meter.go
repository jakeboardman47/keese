// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package main

import (
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// Label and metric names form the contract with the TokenBudget reconciler's
// PromQL (ADR 30 §Decision / §Observability). The reconciler queries
// keese_token_budget_consumed_total{tenant,workspace,model,direction}; renaming
// any of these breaks the locked loop.
const (
	labelTenant    = "tenant"
	labelWorkspace = "workspace"
	labelModel     = "model"
	labelDirection = "direction"

	directionInput  = "input"
	directionOutput = "output"

	// Self-telemetry result/reason values (ADR 30 §Observability).
	resultOK       = "ok"
	resultNoTenant = "no_tenant"
	resultNoModel  = "no_model"

	reasonDuplicate    = "duplicate"     // x-request-id already seen
	reasonMissingReqID = "missing_reqid" // no x-request-id to dedup on
	reasonNoTenant     = "no_tenant"     // ext_authz header absent
	reasonNoModel      = "no_model"      // upstream-model-id absent
	reasonNonFinal     = "non_final"     // not the final upstream response
)

// usageEvent is one parsed per-response token-usage record read off the Envoy AI
// Gateway. It mirrors the gateway's token-cost extension output plus the headers
// ext_authz already stamps (ADR 30 §"Why response-usage at the gateway").
type usageEvent struct {
	// RequestID is Envoy's x-request-id — the per-request idempotency key used
	// to dedup retries (ADR 30 §Failure modes, double-count row).
	RequestID string
	// Tenant / Workspace come from the x-keese-tenant / x-keese-workspace headers
	// ext_authz stamps (05a). Model comes from x-envoy-upstream-model-id.
	Tenant    string
	Workspace string
	Model     string
	// TokensIn / TokensOut are the input/output token counts parsed from the
	// upstream LLM response usage block. The meter splits these into two series
	// keyed by direction (ADR 30 data-flow line 2: "+1 series each dir").
	TokensIn  int64
	TokensOut int64
	// Final marks this as the terminal upstream response. The meter emits only on
	// the final response so an Envoy upstream retry never double-counts.
	Final bool
}

// Meter is the stateless relabel + dedup core. "Stateless" in ADR 30's sense:
// it holds no durable accounting — Prometheus is the durable store. The only
// in-memory state is a bounded, TTL-expiring dedup set of recently seen
// x-request-ids, which is pure double-delivery defense and is safe to lose on
// restart (a lost entry can at worst admit one already-counted duplicate; the
// gateway emits final usage once per request).
type Meter struct {
	consumed   *prometheus.CounterVec
	translated *prometheus.CounterVec
	dropped    *prometheus.CounterVec

	dedupTTL time.Duration
	mu       sync.Mutex
	seen     map[string]time.Time
}

// NewMeter registers the contract series and self-telemetry on reg and returns a
// ready Meter. dedupTTL bounds how long an x-request-id is remembered; it should
// exceed the gateway's upstream retry budget so all attempts of one request fall
// inside the window.
func NewMeter(reg prometheus.Registerer, dedupTTL time.Duration) *Meter {
	consumed := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "keese_token_budget_consumed_total",
		Help: "Tokens consumed via the Envoy AI Gateway, relabeled for TokenBudget enforcement (ADR 30 contract series).",
	}, []string{labelTenant, labelWorkspace, labelModel, labelDirection})

	translated := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "keese_token_meter_translated_total",
		Help: "Token-usage events translated into the contract series, by result.",
	}, []string{"result"})

	dropped := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "keese_token_meter_dropped_total",
		Help: "Token-usage events dropped without emitting the contract series, by reason.",
	}, []string{"reason"})

	reg.MustRegister(consumed, translated, dropped)

	return &Meter{
		consumed:   consumed,
		translated: translated,
		dropped:    dropped,
		dedupTTL:   dedupTTL,
		seen:       make(map[string]time.Time),
	}
}

// Process relabels one usage event into the contract series. It returns true
// when the event produced an emission (counters incremented), false when it was
// dropped (dedup, non-final, or missing required labels). It never errors and
// never blocks on external systems — the meter never gates traffic (ADR 30
// fail-open posture). now is injected so dedup expiry is deterministic in tests.
func (m *Meter) Process(ev usageEvent, now time.Time) bool {
	// Only the final upstream response carries the authoritative usage; retries
	// and intermediate responses are ignored so attempts never double-count
	// (ADR 30 §Failure modes).
	if !ev.Final {
		m.dropped.WithLabelValues(reasonNonFinal).Inc()
		return false
	}

	// Dedup on Envoy x-request-id. Without a request-id we cannot guarantee a
	// single emission across redelivery, so we drop rather than risk a
	// double-count (fail-open for budgets favors under- over over-counting).
	if ev.RequestID == "" {
		m.dropped.WithLabelValues(reasonMissingReqID).Inc()
		return false
	}
	if m.alreadySeen(ev.RequestID, now) {
		m.dropped.WithLabelValues(reasonDuplicate).Inc()
		return false
	}

	// Required labels for the contract series. ADR 30 §Observability: events
	// missing tenant/model are self-reported and dropped (10a fail-closed
	// isolation discards spans missing keese.tenant).
	if ev.Tenant == "" {
		m.translated.WithLabelValues(resultNoTenant).Inc()
		m.dropped.WithLabelValues(reasonNoTenant).Inc()
		return false
	}
	if ev.Model == "" {
		m.translated.WithLabelValues(resultNoModel).Inc()
		m.dropped.WithLabelValues(reasonNoModel).Inc()
		return false
	}

	// Mark seen only once we commit to emitting, so a drop above does not
	// suppress a later well-formed redelivery of the same request-id.
	m.markSeen(ev.RequestID, now)

	// Relabel: rename envoy_ai_gateway_token_cost_total →
	// keese_token_budget_consumed_total, add workspace + direction, split the
	// single usage record into one input and one output series.
	if ev.TokensIn > 0 {
		m.consumed.WithLabelValues(ev.Tenant, ev.Workspace, ev.Model, directionInput).
			Add(float64(ev.TokensIn))
	}
	if ev.TokensOut > 0 {
		m.consumed.WithLabelValues(ev.Tenant, ev.Workspace, ev.Model, directionOutput).
			Add(float64(ev.TokensOut))
	}

	m.translated.WithLabelValues(resultOK).Inc()
	return true
}

// alreadySeen reports whether reqID is within the live dedup window, expiring
// stale entries opportunistically so the set stays bounded without a sweeper
// goroutine.
func (m *Meter) alreadySeen(reqID string, now time.Time) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.gcLocked(now)
	seenAt, ok := m.seen[reqID]
	return ok && now.Sub(seenAt) < m.dedupTTL
}

func (m *Meter) markSeen(reqID string, now time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.seen[reqID] = now
}

// gcLocked drops entries older than dedupTTL. Caller holds m.mu.
func (m *Meter) gcLocked(now time.Time) {
	for id, t := range m.seen {
		if now.Sub(t) >= m.dedupTTL {
			delete(m.seen, id)
		}
	}
}
