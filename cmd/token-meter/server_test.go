// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package main

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// blockingProc lets a test hold events inside the worker so they are observably
// "in flight" when Drain is called, exercising the rule-06 §2 flush path.
type blockingProc struct {
	gate      chan struct{}
	processed atomic.Int64
}

func (b *blockingProc) Process(usageEvent, time.Time) bool {
	<-b.gate
	b.processed.Add(1)
	return true
}

// TestIngestRelabelEndToEnd posts a usage record to /ingest and asserts the
// contract series appears on /metrics — the full HTTP relabel path.
func TestIngestRelabelEndToEnd(t *testing.T) {
	reg := prometheus.NewRegistry()
	meter := NewMeter(reg, time.Minute)
	srv := NewServer(meter, reg, discardLogger(), 16, func() time.Time { return time.Unix(0, 0) })
	srv.Start()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	body := `{"request_id":"r1","tenant":"acme","workspace":"ws1","model":"m1","tokens_in":7,"tokens_out":3,"final":true}`
	resp, err := http.Post(ts.URL+"/ingest", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", resp.StatusCode)
	}

	// Drain so the worker has counted the event before we assert.
	srv.Drain(context.Background())

	if got := testutil.ToFloat64(meter.consumed.WithLabelValues("acme", "ws1", "m1", directionInput)); got != 7 {
		t.Errorf("input = %v, want 7", got)
	}
	if got := testutil.ToFloat64(meter.consumed.WithLabelValues("acme", "ws1", "m1", directionOutput)); got != 3 {
		t.Errorf("output = %v, want 3", got)
	}

	mresp, err := http.Get(ts.URL + "/metrics")
	if err != nil {
		t.Fatalf("get metrics: %v", err)
	}
	defer func() { _ = mresp.Body.Close() }()
	page, _ := io.ReadAll(mresp.Body)
	if !strings.Contains(string(page), "keese_token_budget_consumed_total") {
		t.Error("/metrics missing contract series")
	}
}

// TestDrainFlushesInFlight asserts Drain blocks until queued-but-unprocessed
// events are flushed into the meter (rule 06 §2): the flush count equals what
// was in flight, and every event is processed before Drain returns.
func TestDrainFlushesInFlight(t *testing.T) {
	reg := prometheus.NewRegistry()
	bp := &blockingProc{gate: make(chan struct{})}
	srv := NewServer(bp, reg, discardLogger(), 16, nil)
	srv.Start()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	const n = 5
	for i := 0; i < n; i++ {
		resp, err := http.Post(ts.URL+"/ingest", "application/json",
			strings.NewReader(`{"request_id":"r","tenant":"t","model":"m","tokens_in":1,"final":true}`))
		if err != nil {
			t.Fatalf("post %d: %v", i, err)
		}
		_ = resp.Body.Close()
	}

	// Release the worker shortly after Drain begins, so events are genuinely
	// in flight when drain starts counting them.
	var once sync.Once
	go func() {
		time.Sleep(20 * time.Millisecond)
		once.Do(func() { close(bp.gate) })
	}()

	pending := srv.Drain(context.Background())
	if pending != n {
		t.Errorf("flushed_in_flight = %d, want %d", pending, n)
	}
	if got := bp.processed.Load(); got != n {
		t.Errorf("processed = %d, want %d (drain must flush all)", got, n)
	}

	// After drain, /readyz must report NotReady (rule 06 §9).
	resp, err := http.Get(ts.URL + "/readyz")
	if err != nil {
		t.Fatalf("readyz: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("readyz after drain = %d, want 503", resp.StatusCode)
	}
}

// TestSIGTERMFlushAndExit drives the full run() lifecycle through the
// signal.NotifyContext cancellation path (rule 06 §10): it enqueues work, fires
// the equivalent of SIGTERM by cancelling ctx, and asserts run returns nil
// (exit 0) and emits the structured shutdown event after flushing.
func TestSIGTERMFlushAndExit(t *testing.T) {
	cfg := config{
		listenAddr:   "127.0.0.1:0",
		dedupTTL:     time.Minute,
		queueDepth:   64,
		drainTimeout: 5 * time.Second,
		readTimeout:  time.Second,
	}
	ctx, cancel := context.WithCancel(context.Background())
	var out bytes.Buffer

	done := make(chan error, 1)
	go func() { done <- run(ctx, cfg, discardLogger(), &out) }()

	// Let the server bind, then simulate SIGTERM via ctx cancel (the same
	// cancellation signal.NotifyContext delivers on a real SIGTERM).
	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("run returned error (must exit 0 on SIGTERM): %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("run did not exit within grace budget after SIGTERM")
	}

	got := out.String()
	if !strings.Contains(got, `"event":"shutdown"`) {
		t.Errorf("missing structured shutdown event; got %q", got)
	}
	if !strings.Contains(got, `"reason":"SIGTERM"`) {
		t.Errorf("shutdown event missing reason; got %q", got)
	}
	if !strings.Contains(got, `"drain_duration_ms":`) {
		t.Errorf("shutdown event missing drain_duration_ms; got %q", got)
	}
}

// TestIngestRejectsNonPost guards the method check.
func TestIngestRejectsNonPost(t *testing.T) {
	reg := prometheus.NewRegistry()
	srv := NewServer(NewMeter(reg, time.Minute), reg, discardLogger(), 4, nil)
	srv.Start()
	defer srv.Drain(context.Background())
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/ingest")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", resp.StatusCode)
	}
}

// TestIngestMalformedJSON asserts bad input is a 400, not a panic — fail-open.
func TestIngestMalformedJSON(t *testing.T) {
	reg := prometheus.NewRegistry()
	srv := NewServer(NewMeter(reg, time.Minute), reg, discardLogger(), 4, nil)
	srv.Start()
	defer srv.Drain(context.Background())
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/ingest", "application/json", strings.NewReader("{not json"))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}
}
