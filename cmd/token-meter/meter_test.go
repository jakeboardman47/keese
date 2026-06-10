// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package main

import (
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func newTestMeter(t *testing.T, ttl time.Duration) (*Meter, *prometheus.Registry) {
	t.Helper()
	reg := prometheus.NewRegistry()
	return NewMeter(reg, ttl), reg
}

// TestRelabel asserts the core ADR-30 contract: a single gateway usage record is
// renamed to keese_token_budget_consumed_total and split into one input + one
// output series carrying exactly {tenant,workspace,model,direction}.
func TestRelabel(t *testing.T) {
	m, reg := newTestMeter(t, time.Minute)
	now := time.Unix(1000, 0)

	emitted := m.Process(usageEvent{
		RequestID: "req-1",
		Tenant:    "acme",
		Workspace: "ws-blue",
		Model:     "claude-sonnet",
		TokensIn:  100,
		TokensOut: 40,
		Final:     true,
	}, now)
	if !emitted {
		t.Fatal("expected event to emit")
	}

	gotIn := testutil.ToFloat64(m.consumed.WithLabelValues("acme", "ws-blue", "claude-sonnet", directionInput))
	if gotIn != 100 {
		t.Errorf("input series = %v, want 100", gotIn)
	}
	gotOut := testutil.ToFloat64(m.consumed.WithLabelValues("acme", "ws-blue", "claude-sonnet", directionOutput))
	if gotOut != 40 {
		t.Errorf("output series = %v, want 40", gotOut)
	}
	if got := testutil.ToFloat64(m.translated.WithLabelValues(resultOK)); got != 1 {
		t.Errorf("translated{ok} = %v, want 1", got)
	}

	// Exactly the contract label set, no more, no less.
	assertSeriesLabels(t, reg, "keese_token_budget_consumed_total",
		[]string{labelDirection, labelModel, labelTenant, labelWorkspace})
}

// TestDedupOnRequestID asserts a redelivered final response for the same Envoy
// x-request-id never double-counts (ADR 30 §Failure modes, retry row).
func TestDedupOnRequestID(t *testing.T) {
	m, _ := newTestMeter(t, time.Minute)
	now := time.Unix(2000, 0)
	ev := usageEvent{
		RequestID: "req-retry",
		Tenant:    "acme",
		Model:     "m1",
		TokensIn:  10,
		TokensOut: 5,
		Final:     true,
	}

	if !m.Process(ev, now) {
		t.Fatal("first delivery should emit")
	}
	if m.Process(ev, now.Add(time.Second)) {
		t.Fatal("duplicate request-id should NOT emit")
	}
	if m.Process(ev, now.Add(2*time.Second)) {
		t.Fatal("second duplicate should NOT emit")
	}

	// Counts reflect exactly one emission despite three deliveries.
	if got := testutil.ToFloat64(m.consumed.WithLabelValues("acme", "", "m1", directionInput)); got != 10 {
		t.Errorf("input = %v, want 10 (no double count)", got)
	}
	if got := testutil.ToFloat64(m.dropped.WithLabelValues(reasonDuplicate)); got != 2 {
		t.Errorf("dropped{duplicate} = %v, want 2", got)
	}
}

// TestDedupTTLExpiry asserts that once the dedup window passes, the same
// request-id is treated as a fresh request (bounded memory; ADR 30 stateless).
func TestDedupTTLExpiry(t *testing.T) {
	m, _ := newTestMeter(t, 30*time.Second)
	base := time.Unix(3000, 0)
	ev := usageEvent{RequestID: "req-ttl", Tenant: "t", Model: "m", TokensIn: 1, Final: true}

	if !m.Process(ev, base) {
		t.Fatal("first should emit")
	}
	if m.Process(ev, base.Add(29*time.Second)) {
		t.Fatal("within TTL should be deduped")
	}
	if !m.Process(ev, base.Add(31*time.Second)) {
		t.Fatal("after TTL should emit again")
	}
}

// TestNonFinalDropped asserts intermediate (non-final) responses never count.
func TestNonFinalDropped(t *testing.T) {
	m, _ := newTestMeter(t, time.Minute)
	now := time.Unix(4000, 0)
	if m.Process(usageEvent{RequestID: "r", Tenant: "t", Model: "m", TokensIn: 9, Final: false}, now) {
		t.Fatal("non-final must not emit")
	}
	if got := testutil.ToFloat64(m.dropped.WithLabelValues(reasonNonFinal)); got != 1 {
		t.Errorf("dropped{non_final} = %v, want 1", got)
	}
}

// TestMissingLabelsDropped asserts events missing the required tenant/model
// labels are dropped and self-reported, never emitting a partial contract series.
func TestMissingLabelsDropped(t *testing.T) {
	m, _ := newTestMeter(t, time.Minute)
	now := time.Unix(5000, 0)

	if m.Process(usageEvent{RequestID: "a", Model: "m", TokensIn: 1, Final: true}, now) {
		t.Fatal("missing tenant must not emit")
	}
	if got := testutil.ToFloat64(m.translated.WithLabelValues(resultNoTenant)); got != 1 {
		t.Errorf("translated{no_tenant} = %v, want 1", got)
	}

	if m.Process(usageEvent{RequestID: "b", Tenant: "t", TokensIn: 1, Final: true}, now) {
		t.Fatal("missing model must not emit")
	}
	if got := testutil.ToFloat64(m.translated.WithLabelValues(resultNoModel)); got != 1 {
		t.Errorf("translated{no_model} = %v, want 1", got)
	}
}

// TestMissingRequestIDDropped asserts an event with no x-request-id is dropped,
// since dedup cannot be guaranteed (fail-open favors under-counting).
func TestMissingRequestIDDropped(t *testing.T) {
	m, _ := newTestMeter(t, time.Minute)
	now := time.Unix(6000, 0)
	if m.Process(usageEvent{Tenant: "t", Model: "m", TokensIn: 1, Final: true}, now) {
		t.Fatal("missing request-id must not emit")
	}
	if got := testutil.ToFloat64(m.dropped.WithLabelValues(reasonMissingReqID)); got != 1 {
		t.Errorf("dropped{missing_reqid} = %v, want 1", got)
	}
}

// assertSeriesLabels checks the metric exposes exactly wantLabels (sorted),
// guarding the locked contract series against accidental label drift.
func assertSeriesLabels(t *testing.T, reg *prometheus.Registry, metric string, wantLabels []string) {
	t.Helper()
	mfs, err := reg.Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}
	for _, mf := range mfs {
		if mf.GetName() != metric {
			continue
		}
		for _, mm := range mf.GetMetric() {
			var got []string
			for _, lp := range mm.GetLabel() {
				got = append(got, lp.GetName())
			}
			if !equalSorted(got, wantLabels) {
				t.Errorf("%s labels = %v, want %v", metric, got, wantLabels)
			}
		}
		return
	}
	t.Fatalf("metric %q not found", metric)
}

func equalSorted(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
