// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// ingestRequest is the wire shape the Tier-1 collector (CH5b) posts to /ingest:
// one parsed per-response token-usage record. Field names mirror the gateway's
// token-cost output and the ext_authz-stamped headers (ADR 30 §data flow). The
// JSON contract is intentionally minimal so CH5b's collector exporter can map
// directly onto it.
type ingestRequest struct {
	RequestID string `json:"request_id"`
	Tenant    string `json:"tenant"`
	Workspace string `json:"workspace"`
	Model     string `json:"model"`
	TokensIn  int64  `json:"tokens_in"`
	TokensOut int64  `json:"tokens_out"`
	Final     bool   `json:"final"`
}

func (r ingestRequest) toEvent() usageEvent {
	return usageEvent{
		RequestID: r.RequestID,
		Tenant:    r.Tenant,
		Workspace: r.Workspace,
		Model:     r.Model,
		TokensIn:  r.TokensIn,
		TokensOut: r.TokensOut,
		Final:     r.Final,
	}
}

// Processor is the subset of *Meter the server depends on, so tests can inject a
// fake and assert flush ordering without a real registry.
type Processor interface {
	Process(ev usageEvent, now time.Time) bool
}

// maxIngestBody caps a single /ingest payload. Usage records are tiny; a larger
// body is a misuse or attack and is rejected without reading it all into memory.
const maxIngestBody = 64 << 10 // 64 KiB

// Server owns the HTTP surface (ingest + /metrics + health) and a buffered
// in-flight queue between the handler and a single processing worker. The queue
// is what "flush in-flight counts on shutdown" (rule 06 §2) operates on: on
// SIGTERM we stop accepting new events, drain the queue into Prometheus, then
// exit. Prometheus is the durable store (rule 06 §5), so once an event is
// counted there is nothing further to persist.
type Server struct {
	proc  Processor
	reg   *prometheus.Registry
	log   *slog.Logger
	queue chan usageEvent
	now   func() time.Time

	accepting atomic.Bool
	inFlight  atomic.Int64

	wg   sync.WaitGroup
	once sync.Once
}

// NewServer wires the meter into an HTTP server with an in-flight queue of the
// given depth. The worker is not started until Start is called.
func NewServer(proc Processor, reg *prometheus.Registry, log *slog.Logger, queueDepth int, now func() time.Time) *Server {
	if now == nil {
		now = time.Now
	}
	s := &Server{
		proc:  proc,
		reg:   reg,
		log:   log,
		queue: make(chan usageEvent, queueDepth),
		now:   now,
	}
	s.accepting.Store(true)
	return s
}

// Start launches the single processing worker that drains the queue into the
// meter. One worker keeps relabel + dedup serialized without extra locking on
// the hot path.
func (s *Server) Start() {
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		for ev := range s.queue {
			s.proc.Process(ev, s.now())
			s.inFlight.Add(-1)
		}
	}()
}

// Handler returns the mux. /ingest accepts usage records; /metrics exposes the
// contract series + self-telemetry; /healthz is liveness; /readyz flips to 503
// once draining so the Service stops routing before we stop reading (rule 06 §9).
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/ingest", s.handleIngest)
	mux.Handle("/metrics", promhttp.HandlerFor(s.reg, promhttp.HandlerOpts{}))
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, _ *http.Request) {
		if s.accepting.Load() {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusServiceUnavailable)
	})
	return mux
}

// handleIngest parses one usage record and enqueues it. It is deliberately
// forgiving: the meter never gates traffic (ADR 30 fail-open), so the gateway
// path does not depend on this endpoint's success. We still surface malformed
// input as 4xx so CH5b's collector can self-correct, but a full queue yields 202
// (accepted, dropped) rather than backpressure that could stall the collector.
func (s *Server) handleIngest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.accepting.Load() {
		// Draining: do not accept new work, but never error the caller — the
		// gateway must not see this component as a failure.
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, maxIngestBody))
	if err != nil {
		http.Error(w, "read body", http.StatusBadRequest)
		return
	}
	var req ingestRequest
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}

	s.inFlight.Add(1)
	select {
	case s.queue <- req.toEvent():
		w.WriteHeader(http.StatusAccepted)
	default:
		// Queue full: drop rather than block. Fail-open — a brief metering gap is
		// bounded and caught up by the next reconcile (ADR 30 §Failure modes).
		s.inFlight.Add(-1)
		s.log.Warn("ingest queue full, dropping usage event",
			slog.String("request_id", req.RequestID))
		w.WriteHeader(http.StatusAccepted)
	}
}

// Drain stops accepting new events, closes the queue, and waits for the worker
// to finish counting everything already enqueued (rule 06 §2). It is idempotent.
// It returns the number of events that were still in flight when drain began,
// for the structured shutdown event.
func (s *Server) Drain(ctx context.Context) int64 {
	pending := s.inFlight.Load()
	s.once.Do(func() {
		s.accepting.Store(false)
		close(s.queue)
	})

	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-ctx.Done():
		s.log.Warn("drain budget exceeded before queue emptied",
			slog.Int64("remaining", s.inFlight.Load()))
	}
	return pending
}

// errServerClosed mirrors http.ErrServerClosed for the caller to treat clean
// shutdown as success without importing net/http there.
var errServerClosed = http.ErrServerClosed

// isCleanClose reports whether err is the benign "server closed" signal.
func isCleanClose(err error) bool {
	return err == nil || errors.Is(err, errServerClosed)
}
