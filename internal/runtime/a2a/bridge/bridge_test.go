// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// dialReady polls addr until a TCP connect succeeds or the deadline passes, so
// tests never sleep on a fixed timer (rule 06-testing: no sleep in tests).
func dialReady(t *testing.T, addr string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		c, err := net.DialTimeout("tcp", addr, 50*time.Millisecond)
		if err == nil {
			_ = c.Close()
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("listener at %s never became ready", addr)
}

// TestA2ABridgeForward asserts the bridge accepts an inbound A2A request on its
// listener and forwards it to the upstream ADK server, preserving the request
// path, body, and W3C trace context (traceparent) — the core T3 contract.
func TestA2ABridgeForward(t *testing.T) {
	t.Parallel()

	// Stand-in for the in-pod ADK server on "localhost:8080". It echoes the
	// path, body, and the traceparent header it received so the test can assert
	// the bridge forwarded all three faithfully.
	var gotPath, gotTraceparent, gotBody string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotTraceparent = r.Header.Get("traceparent")
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "adk-ok")
	}))
	defer upstream.Close()

	// Bind the bridge to an ephemeral port (the :8081 fixed port is exercised
	// only by main(); run() is port-agnostic by design so tests can parallelize).
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	bridgeAddr := lis.Addr().String()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var out, errOut bytes.Buffer
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		if rerr := run(ctx, lis, upstream.URL, "/nonexistent/mcp/config.json", &out, &errOut); rerr != nil {
			t.Errorf("run returned error: %v", rerr)
		}
	}()

	dialReady(t, bridgeAddr)

	req, err := http.NewRequest(http.MethodPost,
		"http://"+bridgeAddr+"/a2a/messages/send",
		strings.NewReader(`{"jsonrpc":"2.0","method":"message/send"}`))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	const wantTrace = "00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01"
	req.Header.Set("traceparent", wantTrace)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("forward request: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status: got %d, want 200", resp.StatusCode)
	}
	if string(body) != "adk-ok" {
		t.Errorf("upstream body relayed: got %q, want %q", body, "adk-ok")
	}
	if gotPath != "/a2a/messages/send" {
		t.Errorf("forwarded path: got %q, want /a2a/messages/send", gotPath)
	}
	if gotTraceparent != wantTrace {
		t.Errorf("trace context not propagated: got %q, want %q", gotTraceparent, wantTrace)
	}
	if gotBody != `{"jsonrpc":"2.0","method":"message/send"}` {
		t.Errorf("forwarded body: got %q", gotBody)
	}

	cancel()
	wg.Wait()
}

// TestA2ABridgeForward_UpstreamDownFailsClosed asserts that when the ADK server
// is unreachable the bridge returns 502 (not a panic, not a hang) so the pod
// stays up and the peer sees a clear gateway error (rule 06.6 idempotent restart).
func TestA2ABridgeForward_UpstreamDownFailsClosed(t *testing.T) {
	t.Parallel()

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	bridgeAddr := lis.Addr().String()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var out, errOut bytes.Buffer
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		// Upstream points at a port nothing is listening on.
		_ = run(ctx, lis, "http://127.0.0.1:1", "", &out, &errOut)
	}()
	dialReady(t, bridgeAddr)

	resp, err := http.Get("http://" + bridgeAddr + "/a2a/ping")
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusBadGateway {
		t.Errorf("upstream-down status: got %d, want 502", resp.StatusCode)
	}

	cancel()
	wg.Wait()
}

// TestA2ABridgeShutdown_SIGTERMDrain is the rule 06 §10 signal test: it asserts
// that on context cancellation (the signal path), run (a) drains and stops the
// listener, (b) returns nil (exit 0 in budget), and (c) emits the structured
// `shutdown` event with a drain duration. It also asserts an in-flight request
// started before cancellation completes rather than being torn down — the
// "drain non-zero work" guarantee (rule 06 §10a).
func TestA2ABridgeShutdown_SIGTERMDrain(t *testing.T) {
	t.Parallel()

	// Upstream that blocks until released, so we can hold a request in-flight
	// across the shutdown signal and prove it drains rather than aborts.
	release := make(chan struct{})
	inflightStarted := make(chan struct{})
	var once sync.Once
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		once.Do(func() { close(inflightStarted) })
		<-release
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "drained")
	}))
	defer upstream.Close()

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	bridgeAddr := lis.Addr().String()

	ctx, cancel := context.WithCancel(context.Background())

	var out, errOut bytes.Buffer
	runDone := make(chan error, 1)
	go func() { runDone <- run(ctx, lis, upstream.URL, "", &out, &errOut) }()
	dialReady(t, bridgeAddr)

	// Fire an in-flight request; it blocks in the upstream handler.
	respCh := make(chan *http.Response, 1)
	go func() {
		resp, rerr := http.Get("http://" + bridgeAddr + "/a2a/slow")
		if rerr == nil {
			respCh <- resp
		} else {
			respCh <- nil
		}
	}()

	select {
	case <-inflightStarted:
	case <-time.After(3 * time.Second):
		t.Fatal("in-flight request never reached upstream")
	}

	// Signal shutdown while the request is mid-flight.
	cancel()

	// Let the in-flight request complete, simulating a drain within budget.
	close(release)

	resp := <-respCh
	if resp == nil {
		t.Fatal("in-flight request was aborted instead of drained (rule 06 §10a)")
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if string(body) != "drained" {
		t.Errorf("in-flight body: got %q, want drained", body)
	}

	// run must return nil (exit 0 in budget — rule 06 §10b) within the budget.
	select {
	case rerr := <-runDone:
		if rerr != nil {
			t.Errorf("run returned non-nil on drain: %v", rerr)
		}
	case <-time.After(shutdownBudget + 2*time.Second):
		t.Fatal("run did not return within shutdown budget")
	}

	// Rule 06 §10c: structured shutdown event present and well-formed.
	assertShutdownEvent(t, out.String())

	// Listener must be closed after drain.
	if _, derr := net.DialTimeout("tcp", bridgeAddr, 100*time.Millisecond); derr == nil {
		t.Error("listener still accepting connections after shutdown")
	}
}

// assertShutdownEvent parses the run stdout and asserts exactly one structured
// shutdown event with reason SIGTERM and a non-negative drain duration.
func assertShutdownEvent(t *testing.T, stdout string) {
	t.Helper()
	var found bool
	for _, line := range strings.Split(strings.TrimSpace(stdout), "\n") {
		if line == "" {
			continue
		}
		var ev map[string]any
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			continue
		}
		if ev["event"] != "shutdown" {
			continue
		}
		found = true
		if ev["reason"] != "SIGTERM" {
			t.Errorf("shutdown reason: got %v, want SIGTERM", ev["reason"])
		}
		if _, ok := ev["drain_duration_ms"].(float64); !ok {
			t.Errorf("shutdown event missing numeric drain_duration_ms: %v", ev)
		}
	}
	if !found {
		t.Errorf("no structured shutdown event in stdout (rule 06 §4/§10c):\n%s", stdout)
	}
}

// TestLoadMCPConfig covers the projected-ConfigMap contract: a missing file and
// an empty file are non-fatal (empty config, nil error) — the bridge must start
// before E6 populates the ConfigMap — while a populated file parses and a
// malformed file errors.
func TestLoadMCPConfig(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	t.Run("missing file is non-fatal", func(t *testing.T) {
		cfg, err := loadMCPConfig(filepath.Join(dir, "nope.json"))
		if err != nil {
			t.Fatalf("missing file must be non-fatal, got %v", err)
		}
		if len(cfg.Servers) != 0 {
			t.Errorf("missing file: got %d servers, want 0", len(cfg.Servers))
		}
	})

	t.Run("empty file is non-fatal", func(t *testing.T) {
		p := filepath.Join(dir, "empty.json")
		if err := os.WriteFile(p, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		cfg, err := loadMCPConfig(p)
		if err != nil {
			t.Fatalf("empty file must be non-fatal, got %v", err)
		}
		if len(cfg.Servers) != 0 {
			t.Errorf("empty file: got %d servers, want 0", len(cfg.Servers))
		}
	})

	t.Run("populated file parses", func(t *testing.T) {
		p := filepath.Join(dir, "full.json")
		if err := os.WriteFile(p, []byte(`{"servers":[{"name":"fs","url":"http://localhost:9001"}]}`), 0o600); err != nil {
			t.Fatal(err)
		}
		cfg, err := loadMCPConfig(p)
		if err != nil {
			t.Fatalf("populated file: %v", err)
		}
		if len(cfg.Servers) != 1 || cfg.Servers[0].Name != "fs" {
			t.Errorf("populated parse: got %+v", cfg.Servers)
		}
	})

	t.Run("malformed file errors", func(t *testing.T) {
		p := filepath.Join(dir, "bad.json")
		if err := os.WriteFile(p, []byte(`{not json`), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := loadMCPConfig(p); err == nil {
			t.Error("malformed file must error")
		}
	})
}
