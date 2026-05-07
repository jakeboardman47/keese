// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package goose

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	spi "github.com/keese-ai/keese/internal/runtime/spi/v1alpha1"
)

// fakeExecutor records every Exec call and returns scripted responses.
type fakeExecutor struct {
	mu       sync.Mutex
	calls    [][]string
	respond  func(argv []string) (stdout, stderr []byte, err error)
	delayPer time.Duration // optional sleep per call
}

func (f *fakeExecutor) Exec(ctx context.Context, _ string, _ string, _ string, argv []string) ([]byte, []byte, error) {
	f.mu.Lock()
	f.calls = append(f.calls, argv)
	f.mu.Unlock()
	if f.delayPer > 0 {
		select {
		case <-ctx.Done():
			return nil, nil, ctx.Err()
		case <-time.After(f.delayPer):
		}
	}
	if f.respond != nil {
		return f.respond(argv)
	}
	return nil, nil, nil
}

func (f *fakeExecutor) callsCopy() [][]string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([][]string, len(f.calls))
	copy(out, f.calls)
	return out
}

func TestCapabilities(t *testing.T) {
	t.Parallel()
	r, err := Factory(map[string]string{"image": "keese-goose:1.33.1"})
	if err != nil {
		t.Fatalf("Factory: %v", err)
	}
	caps := r.Capabilities()
	if caps.ProviderName != "goose" {
		t.Fatalf("ProviderName: got %q", caps.ProviderName)
	}
	if !caps.SupportsACP || !caps.SupportsRecipes || !caps.SupportsStreaming {
		t.Fatalf("expected ACP+Recipes+Streaming, got %+v", caps)
	}
}

func TestBootstrapNoExecutorIsNoOp(t *testing.T) {
	t.Parallel()
	r, _ := Factory(nil)
	if err := r.Bootstrap(context.Background(), spi.Workspace{UID: "abcd1234"}); err != nil {
		t.Fatalf("Bootstrap with nil executor: %v", err)
	}
}

func TestBootstrapIdempotent(t *testing.T) {
	t.Parallel()
	exec := &fakeExecutor{}
	r := FactoryWithExecutor("keese-goose:1.33.1", exec)
	ws := spi.Workspace{UID: "abcd1234", Name: "ws", Namespace: "alpha"}

	for i := 0; i < 3; i++ {
		if err := r.Bootstrap(context.Background(), ws); err != nil {
			t.Fatalf("Bootstrap iter %d: %v", i, err)
		}
	}
	if got := len(exec.callsCopy()); got != 3 {
		t.Fatalf("Bootstrap calls: got %d, want 3", got)
	}
	for _, c := range exec.callsCopy() {
		if len(c) < 3 || c[0] != "/bin/sh" || c[1] != "-c" {
			t.Fatalf("Bootstrap argv: got %v", c)
		}
		if !strings.Contains(c[2], "/var/run/keese/session/keese-checkpoints/abcd1234") {
			t.Fatalf("Bootstrap path: %q missing workspace dir", c[2])
		}
	}
}

func TestDrainEnforcesBudget(t *testing.T) {
	t.Parallel()
	exec := &fakeExecutor{
		// Ensure each Exec sleeps; drainBudget is 50 ms so we blow past.
		delayPer: 30 * time.Millisecond,
	}
	r := FactoryWithExecutor("keese-goose:1.33.1", exec)
	r.drainBudget = 50 * time.Millisecond
	sess := spi.WorkspaceSession{UID: "s1", PodName: "p1", Namespace: "alpha", WorkspaceID: "wsuid"}
	err := r.Drain(context.Background(), sess)
	if !errors.Is(err, spi.ErrBudget) && err == nil {
		// Either ErrBudget directly or context-deadline-exceeded surfaced
		// through one of the steps — both are acceptable; nil is not.
		t.Fatalf("Drain: expected non-nil error on budget breach, got nil")
	}
}

func TestDrainSendsTermAndCheckpoints(t *testing.T) {
	t.Parallel()
	checkpointed := false
	exec := &fakeExecutor{
		respond: func(argv []string) ([]byte, []byte, error) {
			cmd := strings.Join(argv, " ")
			switch {
			case strings.Contains(cmd, "stat -c '%Y'"):
				// Constant mtime so the stable-mtime loop terminates
				// after one extra poll.
				return []byte("1700000000\n"), nil, nil
			case strings.Contains(cmd, "cp -f /var/run/keese/session/home/.local/share/goose/sessions/sessions.db"):
				checkpointed = true
				return nil, nil, nil
			}
			return nil, nil, nil
		},
	}
	r := FactoryWithExecutor("keese-goose:1.33.1", exec)
	// Tighten the drain budget so the test finishes quickly even if
	// the stable-mtime loop polls a few times.
	r.drainBudget = 5 * time.Second
	sess := spi.WorkspaceSession{UID: "s1", PodName: "p1", Namespace: "alpha", WorkspaceID: "wsuid"}
	if err := r.Drain(context.Background(), sess); err != nil {
		t.Fatalf("Drain: %v", err)
	}

	calls := exec.callsCopy()
	var sawTerm bool
	for _, c := range calls {
		if len(c) >= 3 && strings.Contains(c[2], "kill -TERM 1") {
			sawTerm = true
		}
	}
	if !sawTerm {
		t.Fatalf("Drain: expected SIGTERM step, calls=%v", calls)
	}
	if !checkpointed {
		t.Fatalf("Drain: expected checkpoint cp step, calls=%v", calls)
	}
}

func TestResumeNoCheckpointIsNoOp(t *testing.T) {
	t.Parallel()
	exec := &fakeExecutor{}
	r := FactoryWithExecutor("keese-goose:1.33.1", exec)
	if err := r.Resume(context.Background(), spi.Workspace{UID: "abcd"}); err != nil {
		t.Fatalf("Resume no-checkpoint: %v", err)
	}
	if got := len(exec.callsCopy()); got != 0 {
		t.Fatalf("Resume no-checkpoint: expected 0 exec calls, got %d", got)
	}
}

func TestResumeRestoresCheckpoint(t *testing.T) {
	t.Parallel()
	exec := &fakeExecutor{}
	r := FactoryWithExecutor("keese-goose:1.33.1", exec)
	ws := spi.Workspace{
		UID:            "abcd",
		Namespace:      "alpha",
		LastCheckpoint: spi.CheckpointRef{SQLiteRef: "/var/run/keese/sessions/abcd/session.sqlite"},
	}
	if err := r.Resume(context.Background(), ws); err != nil {
		t.Fatalf("Resume: %v", err)
	}
	calls := exec.callsCopy()
	if len(calls) != 1 {
		t.Fatalf("Resume: got %d exec calls, want 1", len(calls))
	}
	if !strings.Contains(calls[0][2], "test -s /var/run/keese/sessions/abcd/session.sqlite") {
		t.Fatalf("Resume: missing checkpoint validation, argv[2]=%q", calls[0][2])
	}
}

func TestUnsupportedMethodsReturnSentinel(t *testing.T) {
	t.Parallel()
	r := FactoryWithExecutor("keese-goose:1.33.1", &fakeExecutor{})
	ctx := context.Background()
	ws := spi.Workspace{}
	sess := spi.WorkspaceSession{}

	if _, err := r.Run(ctx, "", nil); !errors.Is(err, spi.ErrUnsupported) {
		t.Errorf("Run: got %v, want ErrUnsupported", err)
	}
	if _, err := r.Attach(ctx, sess); !errors.Is(err, spi.ErrUnsupported) {
		t.Errorf("Attach: got %v", err)
	}
	if err := r.CleanupSubAgents(ctx, ws); !errors.Is(err, spi.ErrUnsupported) {
		t.Errorf("CleanupSubAgents: got %v", err)
	}
	if _, err := r.InvokeSubAgent(ctx, ws, spi.SubAgentSpec{}); !errors.Is(err, spi.ErrUnsupported) {
		t.Errorf("InvokeSubAgent: got %v", err)
	}
	if _, err := r.Health(ctx, sess); !errors.Is(err, spi.ErrUnsupported) {
		t.Errorf("Health: got %v", err)
	}
	if _, err := r.StreamEvents(ctx); !errors.Is(err, spi.ErrUnsupported) {
		t.Errorf("StreamEvents: got %v", err)
	}
}

// TestInjectPromptWritesToFifo verifies that InjectPrompt shells into the
// session pod and writes the prompt (shell-escaped) to injectFifoPath.
// It also asserts that embedded newlines are collapsed before writing
// (one write = one supervisor turn).
func TestInjectPromptWritesToFifo(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name           string
		prompt         string
		wantFifoInCmd  bool
		wantSanitised  string
	}{
		{
			name:          "simple prompt",
			prompt:        "you appear stuck — what are you doing?",
			wantFifoInCmd: true,
			wantSanitised: "you appear stuck — what are you doing?",
		},
		{
			name:          "newlines collapsed",
			prompt:        "line1\nline2\r\nline3",
			wantFifoInCmd: true,
			wantSanitised: "line1 line2  line3",
		},
		{
			name:          "single quotes escaped",
			prompt:        "it's stuck",
			wantFifoInCmd: true,
			wantSanitised: "it's stuck",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var capturedCmd []string
			exec := &fakeExecutor{
				respond: func(argv []string) ([]byte, []byte, error) {
					capturedCmd = argv
					return nil, nil, nil
				},
			}
			r := FactoryWithExecutor("keese-goose:1.33.1", exec)
			sess := spi.WorkspaceSession{
				PodName:   "sess-pod-1",
				Namespace: "alpha",
			}
			if err := r.InjectPrompt(context.Background(), sess, tc.prompt); err != nil {
				t.Fatalf("InjectPrompt: %v", err)
			}
			if len(capturedCmd) < 3 {
				t.Fatalf("expected exec argv ≥ 3, got %v", capturedCmd)
			}
			script := capturedCmd[2]
			if tc.wantFifoInCmd && !strings.Contains(script, injectFifoPath) {
				t.Errorf("script does not reference fifo path %q; script=%q", injectFifoPath, script)
			}
			// Verify the sanitised prompt appears in the script (shell-quoted).
			sanitised := strings.ReplaceAll(tc.prompt, "\n", " ")
			sanitised = strings.ReplaceAll(sanitised, "\r", " ")
			escaped := shellescape(sanitised)
			if !strings.Contains(script, escaped) {
				t.Errorf("script does not contain escaped prompt %q; script=%q", escaped, script)
			}
		})
	}
}

// TestInjectPromptNoPodName ensures InjectPrompt returns an error when the
// session has no pod name (prevents a silent no-op).
func TestInjectPromptNoPodName(t *testing.T) {
	t.Parallel()
	exec := &fakeExecutor{}
	r := FactoryWithExecutor("keese-goose:1.33.1", exec)
	err := r.InjectPrompt(context.Background(), spi.WorkspaceSession{}, "hello")
	if err == nil {
		t.Fatal("expected error for empty PodName, got nil")
	}
	if len(exec.callsCopy()) != 0 {
		t.Fatal("expected 0 exec calls for empty PodName")
	}
}

// TestInjectPromptNoExecutor ensures InjectPrompt returns an error when no
// PodExecutor is wired (e.g. pre-startup or factory without executor).
func TestInjectPromptNoExecutor(t *testing.T) {
	t.Parallel()
	r, _ := Factory(nil)
	err := r.InjectPrompt(context.Background(), spi.WorkspaceSession{PodName: "p1", Namespace: "alpha"}, "hello")
	if err == nil {
		t.Fatal("expected error for nil executor, got nil")
	}
}

// TestInjectPromptCapabilityEnabled verifies SupportsInjectPrompt is true
// after TD-P3-04 implementation.
func TestInjectPromptCapabilityEnabled(t *testing.T) {
	t.Parallel()
	r, _ := Factory(map[string]string{"image": "keese-goose:1.33.1"})
	if !r.Capabilities().SupportsInjectPrompt {
		t.Fatal("expected SupportsInjectPrompt=true after TD-P3-04 implementation")
	}
}

func TestRegisterRanInInit(t *testing.T) {
	t.Parallel()
	caps, factory, ok := spi.Lookup(ProviderName)
	if !ok {
		t.Fatalf("provider %q not registered after init()", ProviderName)
	}
	if caps.ProviderName != ProviderName {
		t.Fatalf("registered name: got %q", caps.ProviderName)
	}
	if factory == nil {
		t.Fatal("registered factory is nil")
	}
}
