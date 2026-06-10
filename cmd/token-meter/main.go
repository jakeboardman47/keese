// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

// keese-token-meter is the stateless OTEL/metrics processor ADR 30 specifies as
// the one missing hop in TokenBudget enforcement. It consumes the Envoy AI
// Gateway's per-response token usage (the same value behind
// envoy_ai_gateway_token_cost_total) and relabels it into the contract series
// the TokenBudget reconciler already queries:
//
//	keese_token_budget_consumed_total{tenant,workspace,model,direction}
//
// It adds the workspace + direction labels the gateway's native metric lacks,
// splits each usage record into one input and one output series, and dedups on
// Envoy's x-request-id so an upstream retry never double-counts. It is wired into
// the Tier-1 OTEL collector pipeline by CH5b.
//
// Fail-open posture (ADR 30 §"Fail-open vs fail-closed"): this component NEVER
// gates egress traffic. A meter outage degrades to a bounded metering gap, not a
// cluster-wide outage; the short-window BackendTrafficPolicy and the reconciler's
// no-false-clear rule hold the line.
//
// Rule 06-signal-handling: main installs a SIGTERM/SIGINT handler via
// signal.NotifyContext before any I/O; on signal it stops accepting, flushes the
// in-flight queue into Prometheus, emits a structured shutdown event, and exits 0
// within the termination grace budget.
package main

import (
	"context"
	"errors"
	"flag"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// config holds the resolved runtime flags.
type config struct {
	listenAddr   string
	dedupTTL     time.Duration
	queueDepth   int
	drainTimeout time.Duration
	readTimeout  time.Duration
}

func main() {
	cfg := config{}
	flag.StringVar(&cfg.listenAddr, "listen", ":8080",
		"Address for the ingest + /metrics HTTP server")
	flag.DurationVar(&cfg.dedupTTL, "dedup-ttl", 60*time.Second,
		"How long an Envoy x-request-id is remembered for dedup; must exceed the gateway upstream retry budget")
	flag.IntVar(&cfg.queueDepth, "queue-depth", 4096,
		"In-flight ingest queue depth before events are dropped (fail-open)")
	flag.DurationVar(&cfg.drainTimeout, "drain-timeout", 25*time.Second,
		"Maximum time to flush in-flight counts on SIGTERM before exiting (rule 06 §3; infra-sidecar budget)")
	flag.DurationVar(&cfg.readTimeout, "read-timeout", 5*time.Second,
		"HTTP server read/header timeout")
	flag.Parse()

	// Rule 06-signal-handling §1: install the SIGTERM/SIGINT handler before any
	// I/O, so in-flight work can be drained cleanly rather than SIGKILLed.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	log := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	if err := run(ctx, cfg, log, os.Stdout); err != nil {
		log.Error("token-meter exited with error", slog.String("err", err.Error()))
		os.Exit(1)
	}
}

// run is the full server lifecycle, extracted from main so the SIGTERM →
// flush → exit-0 path is driven directly by tests (rule 06 §10). It blocks until
// ctx is cancelled (signal), then drains the in-flight queue within
// cfg.drainTimeout and writes the structured shutdown event to eventOut. It
// returns nil on a clean drain-and-exit so the caller exits 0.
func run(ctx context.Context, cfg config, log *slog.Logger, eventOut io.Writer) error {
	reg := prometheus.NewRegistry()
	meter := NewMeter(reg, cfg.dedupTTL)
	srv := NewServer(meter, reg, log, cfg.queueDepth, time.Now)
	srv.Start()

	httpSrv := &http.Server{
		Addr:              cfg.listenAddr,
		Handler:           srv.Handler(),
		ReadTimeout:       cfg.readTimeout,
		ReadHeaderTimeout: cfg.readTimeout,
	}

	serveErr := make(chan error, 1)
	go func() {
		log.Info("token-meter listening",
			slog.String("addr", cfg.listenAddr),
			slog.Duration("dedup_ttl", cfg.dedupTTL))
		serveErr <- httpSrv.ListenAndServe()
	}()

	// Block until SIGTERM/SIGINT (ctx cancel) or an unexpected serve error.
	select {
	case <-ctx.Done():
		log.Info("shutdown signal received, draining")
	case err := <-serveErr:
		if !isCleanClose(err) {
			return err
		}
	}

	start := time.Now()

	// Rule 06 §2: drain in-flight work. Stop accepting, flush the queue into
	// Prometheus, and shut the HTTP server down, all within the grace budget.
	drainCtx, cancel := context.WithTimeout(context.Background(), cfg.drainTimeout)
	defer cancel()

	pending := srv.Drain(drainCtx)
	shutdownErr := httpSrv.Shutdown(drainCtx)

	// Rule 06 §4: structured shutdown event with reason + drain_duration_ms.
	emitShutdownEvent(eventOut, log, shutdownEvent{
		Reason:          "SIGTERM",
		DrainDurationMS: time.Since(start).Milliseconds(),
		FlushedInFlight: pending,
	})

	if shutdownErr != nil && !errors.Is(shutdownErr, context.Canceled) {
		// A drain-timeout is not a process failure: Prometheus is the durable
		// store (rule 06 §5) and any lost in-flight event is bounded to ≤ the
		// queue. Log and still exit 0 to honor the grace budget.
		log.Warn("http shutdown did not complete cleanly",
			slog.String("err", shutdownErr.Error()))
	}
	return nil
}

// shutdownEvent is the structured rule-06 §4 shutdown record. checkpoint_location
// is intentionally omitted: the meter holds no durable checkpoint of its own —
// Prometheus is the durable store (rule 06 §5).
type shutdownEvent struct {
	Reason          string `json:"reason"`
	DrainDurationMS int64  `json:"drain_duration_ms"`
	FlushedInFlight int64  `json:"flushed_in_flight"`
}

// emitShutdownEvent writes the shutdown event as a single JSON line to out (the
// rule-06 §4 contract the smoke harness greps) and mirrors it to the structured
// logger.
func emitShutdownEvent(out io.Writer, log *slog.Logger, ev shutdownEvent) {
	// Hand-rolled to keep the exact one-line shape the smoke harness asserts and
	// to avoid a fmt.Print* call (forbidigo).
	line := `{"event":"shutdown","reason":"` + ev.Reason +
		`","drain_duration_ms":` + itoa(ev.DrainDurationMS) +
		`,"flushed_in_flight":` + itoa(ev.FlushedInFlight) + "}\n"
	_, _ = io.WriteString(out, line)
	log.Info("shutdown",
		slog.String("reason", ev.Reason),
		slog.Int64("drain_duration_ms", ev.DrainDurationMS),
		slog.Int64("flushed_in_flight", ev.FlushedInFlight))
}

// itoa renders a non-negative-or-negative int64 without importing strconv twice
// across files; kept tiny and allocation-light for the shutdown path.
func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
