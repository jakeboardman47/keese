// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

// Package main is the keese A2A bridge sidecar (E1 task T3).
//
// The bridge runs as a second container in the ADK Python runtime pod
// (see internal/runtime/providers/adkpython). It accepts inbound A2A
// (agent-to-agent) traffic from peer workspaces on :8081 and forwards it to
// the ADK Python server listening on localhost:8080 in the same pod. Keeping
// the inbound listener in a separate, minimal Go process means the peer-facing
// surface area is a tiny, audited binary rather than the full ADK runtime, and
// it lets E1c's NetworkPolicy gate ingress at a single well-known port.
//
// Responsibilities:
//   - Listen on :8081 for inbound A2A requests from peer workspace pods.
//   - Reverse-proxy each request to http://localhost:8080 (the ADK server),
//     preserving W3C trace context (traceparent/tracestate) so a peer-initiated
//     call shares one distributed trace end to end (OTEL propagation).
//   - Load the MCP server list from a projected ConfigMap at
//     /var/run/keese/mcp-config/config.json. The file is empty / absent until
//     E6 wires the GuardrailBinding reconciler, so a missing or empty file is
//     non-fatal: the bridge starts with an empty tool list and logs it.
//
// Security invariants (rule 05): the bridge carries NO upstream API keys and
// NO kubeconfig — it only proxies in-pod localhost traffic and reads a
// read-only projected ConfigMap. It runs under the same hardened
// SecurityContext as the ADK container (runAsNonRoot, readOnlyRootFilesystem,
// drop ALL, allowPrivilegeEscalation:false).
//
// Signal handling (rule 06): main installs signal.NotifyContext for SIGTERM
// and SIGINT, drains in-flight forwards within the grace budget, emits a
// structured shutdown event, and exits 0.
//
// NOTE: enforcement of the workspace:W#a2a_callable_by@workspace:caller ReBAC
// relation is NOT done here — E2 adds ext_authz enforcement of A2A peer calls.
// At E1b the bridge forwards unconditionally; the relation is documented as a
// stub in docs/specs/egress-authz-protocol.md.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"time"
)

const (
	// listenAddr is the inbound A2A port (peer ingress on :8081).
	listenAddr = ":8081"

	// upstreamAddr is the in-pod ADK Python server (T2 --a2a-port 8080).
	upstreamAddr = "http://localhost:8080"

	// mcpConfigPath is the projected ConfigMap rendered by the GuardrailBinding
	// reconciler (E6). Absent/empty until then — non-fatal.
	mcpConfigPath = "/var/run/keese/mcp-config/config.json"

	// shutdownBudget bounds the SIGTERM drain (rule 06.3: infra sidecars 30s).
	shutdownBudget = 30 * time.Second

	// readHeaderTimeout guards against slow-loris on the peer-facing listener.
	readHeaderTimeout = 10 * time.Second
)

// traceHeaders are the W3C trace-context headers the bridge always preserves
// when forwarding, so a peer-originated A2A call joins one distributed trace
// across the bridge→ADK hop (OTEL trace context propagation).
var traceHeaders = []string{"traceparent", "tracestate", "baggage"}

// mcpConfig is the projected MCP server list. Empty until E6.
type mcpConfig struct {
	Servers []mcpServer `json:"servers"`
}

type mcpServer struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

// loadMCPConfig reads the projected MCP ConfigMap. A missing file, an empty
// file, or a file with an empty server list is non-fatal (returns an empty
// config, nil error) — the bridge starts with no tools until E6 populates it.
// Only a present-but-malformed JSON file is an error worth surfacing.
func loadMCPConfig(path string) (mcpConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return mcpConfig{}, nil
		}
		return mcpConfig{}, fmt.Errorf("read mcp config %s: %w", path, err)
	}
	if len(data) == 0 {
		return mcpConfig{}, nil
	}
	var cfg mcpConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return mcpConfig{}, fmt.Errorf("parse mcp config %s: %w", path, err)
	}
	return cfg, nil
}

// newProxy builds the reverse proxy that forwards :8081 inbound A2A traffic to
// the ADK server at upstream. It preserves W3C trace-context headers explicitly
// (httputil.ReverseProxy already forwards inbound headers, but we re-stamp them
// defensively so the trace survives even if a future Director rewrites headers).
func newProxy(upstream string) (*httputil.ReverseProxy, error) {
	target, err := url.Parse(upstream)
	if err != nil {
		return nil, fmt.Errorf("parse upstream %q: %w", upstream, err)
	}
	proxy := httputil.NewSingleHostReverseProxy(target)

	base := proxy.Director
	proxy.Director = func(req *http.Request) {
		// Capture inbound trace context before Director mutates the request.
		carried := make(map[string]string, len(traceHeaders))
		for _, h := range traceHeaders {
			if v := req.Header.Get(h); v != "" {
				carried[h] = v
			}
		}
		base(req)
		// Re-stamp the trace headers onto the outbound request so the
		// bridge→ADK hop joins the peer's distributed trace (OTEL propagation).
		for h, v := range carried {
			req.Header.Set(h, v)
		}
	}

	// Fail-closed on upstream error: a 502 (not a panic) so the bridge stays up
	// and the peer sees a clear gateway error (rule 06.6: restart idempotent).
	proxy.ErrorHandler = func(w http.ResponseWriter, _ *http.Request, err error) {
		fmt.Fprintf(os.Stderr, `{"event":"forward_error","upstream":%q,"error":%q}`+"\n", upstream, err.Error())
		w.WriteHeader(http.StatusBadGateway)
	}
	return proxy, nil
}

// run wires the listener, MCP config load, and graceful shutdown. It is
// extracted from main so tests can drive the full forward + SIGTERM-drain path
// without spawning a process. ctx cancellation (SIGTERM/SIGINT) triggers a
// bounded graceful drain of in-flight forwards, then a structured shutdown
// event, then return. out/errOut receive structured JSON events.
func run(ctx context.Context, lis net.Listener, upstream, mcpPath string, out, errOut io.Writer) error {
	cfg, err := loadMCPConfig(mcpPath)
	if err != nil {
		// Malformed (not missing) config: log and continue with empty tools —
		// non-fatal so a bad ConfigMap never wedges the runtime pod.
		fmt.Fprintf(errOut, `{"event":"mcp_config_error","error":%q}`+"\n", err.Error())
		cfg = mcpConfig{}
	}
	fmt.Fprintf(out, `{"event":"startup","listen":%q,"upstream":%q,"mcp_servers":%d}`+"\n",
		lis.Addr().String(), upstream, len(cfg.Servers))

	proxy, err := newProxy(upstream)
	if err != nil {
		return err
	}

	srv := &http.Server{
		Handler:           proxy,
		ReadHeaderTimeout: readHeaderTimeout,
		BaseContext:       func(net.Listener) context.Context { return context.Background() },
	}

	serveErr := make(chan error, 1)
	go func() {
		err := srv.Serve(lis)
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		serveErr <- err
	}()

	select {
	case err := <-serveErr:
		// Listener died on its own (not a signal) — surface it.
		return err
	case <-ctx.Done():
		// Rule 06.2: drain in-flight forwards within the budget.
		start := time.Now()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownBudget)
		defer cancel()

		shutdownErr := srv.Shutdown(shutdownCtx)

		// Drain server-loop result so the goroutine doesn't leak.
		<-serveErr

		// Rule 06.4: structured shutdown event with drain duration.
		fmt.Fprintf(out, `{"event":"shutdown","reason":"SIGTERM","drain_duration_ms":%d,"checkpoint_location":"none"}`+"\n",
			time.Since(start).Milliseconds())
		return shutdownErr
	}
}

func main() {
	// Rule 06.1: install SIGTERM + SIGINT handler before serving.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	lis, err := net.Listen("tcp", listenAddr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "a2a-bridge: listen %s: %v\n", listenAddr, err)
		os.Exit(1)
	}

	if err := run(ctx, lis, upstreamAddr, mcpConfigPath, os.Stdout, os.Stderr); err != nil {
		fmt.Fprintf(os.Stderr, "a2a-bridge: %v\n", err)
		os.Exit(1)
	}
	// Rule 06.3: exit 0 within budget.
}
