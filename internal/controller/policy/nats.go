// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package policy

import (
	"context"
	"sync"
)

// NatsSignaler writes and deletes boolean enforcement signals in the NATS KV bucket
// `keese-budget-exceeded`. The real implementation calls the NATS JetStream KV API;
// FakeNatsSignaler is used in tests.
//
// Key scheme (from 10b):
//   - tenant scope:    "tenant/<tenantName>/aggregate"
//   - workspace scope: "workspace/<workspaceUID>/<model>"
//
// Feature status: deferred — paired with the consumer-side enforcer that
// reads the KV before each LLM call. Today the gateway path (Envoy
// ext_proc + keese-authz) doesn't consult this KV; writing without a
// reader is a no-op end-to-end. When the enforcer lands, three pieces
// ship together:
//
//  1. nats-io/nats.go in go.mod (KV producer + consumer)
//  2. A real implementation of this interface (NatsJSSignaler) wired in
//     cmd/main.go behind a NATS_URL env var
//  3. The gateway-side reader (keese-authz ext_proc step) blocking
//     requests when the KV key is "true"
type NatsSignaler interface {
	// SetExceeded writes `true` to the KV key, signaling budget exhaustion.
	// Idempotent: calling it twice for the same key is safe.
	SetExceeded(ctx context.Context, key string) error

	// ClearExceeded deletes the KV key, clearing the exhaustion signal.
	// Idempotent: safe to call when the key does not exist.
	ClearExceeded(ctx context.Context, key string) error
}

// FakeNatsSignaler is a concurrency-safe test double for NatsSignaler.
//
// The reconciler runs in the controller-runtime manager goroutine, while test
// assertions (Eventually) read recorded calls from the Ginkgo goroutine. All
// recorded state is guarded by mu; cross-goroutine readers must use the
// accessor methods (SetCallCount, ClearCallCount, IsExceeded, …) rather than
// touching fields directly. Test setup that runs before the reconciler observes
// a resource may reset state via the Reset* / SetFailNext* helpers, which also
// take the lock so a concurrent reconcile never races the reset.
type FakeNatsSignaler struct {
	mu sync.Mutex

	// exceeded holds the set of keys currently marked as exceeded.
	exceeded map[string]bool

	// setCalls records all SetExceeded calls (key), in order.
	setCalls []string

	// clearCalls records all ClearExceeded calls (key), in order.
	clearCalls []string

	// failNextSet causes the next SetExceeded call to return an error.
	failNextSet bool

	// failNextClear causes the next ClearExceeded call to return an error.
	failNextClear bool
}

type natsSignalError struct{ key string }

func (e natsSignalError) Error() string {
	return "nats: signal operation failed for key " + e.key + " (fake)"
}

// SetExceeded implements NatsSignaler.
func (f *FakeNatsSignaler) SetExceeded(_ context.Context, key string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.setCalls = append(f.setCalls, key)
	if f.failNextSet {
		f.failNextSet = false
		return natsSignalError{key: key}
	}
	if f.exceeded == nil {
		f.exceeded = make(map[string]bool)
	}
	f.exceeded[key] = true
	return nil
}

// ClearExceeded implements NatsSignaler.
func (f *FakeNatsSignaler) ClearExceeded(_ context.Context, key string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.clearCalls = append(f.clearCalls, key)
	if f.failNextClear {
		f.failNextClear = false
		return natsSignalError{key: key}
	}
	if f.exceeded != nil {
		delete(f.exceeded, key)
	}
	return nil
}

// --- Guarded accessors (safe to call from any goroutine) ---

// SetCallCount returns the number of SetExceeded calls recorded so far.
func (f *FakeNatsSignaler) SetCallCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.setCalls)
}

// ClearCallCount returns the number of ClearExceeded calls recorded so far.
func (f *FakeNatsSignaler) ClearCallCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.clearCalls)
}

// SetCallsSnapshot returns a copy of the recorded SetExceeded keys, in order.
func (f *FakeNatsSignaler) SetCallsSnapshot() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.setCalls...)
}

// ClearCallsSnapshot returns a copy of the recorded ClearExceeded keys, in order.
func (f *FakeNatsSignaler) ClearCallsSnapshot() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.clearCalls...)
}

// IsExceeded reports whether the given key is currently marked exceeded.
func (f *FakeNatsSignaler) IsExceeded(key string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.exceeded[key]
}

// --- Guarded mutators for test setup (safe against a concurrent reconcile) ---

// Reset clears all recorded calls and the exceeded set. Use in test setup to
// start a case from a known state without racing an in-flight reconcile.
func (f *FakeNatsSignaler) Reset() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.exceeded = map[string]bool{}
	f.setCalls = nil
	f.clearCalls = nil
}

// SetFailNextSet arms (or disarms) a one-shot error on the next SetExceeded.
func (f *FakeNatsSignaler) SetFailNextSet(v bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failNextSet = v
}

// SetFailNextClear arms (or disarms) a one-shot error on the next ClearExceeded.
func (f *FakeNatsSignaler) SetFailNextClear(v bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failNextClear = v
}

// FailNextSetArmed reports whether the next SetExceeded call is armed to fail.
func (f *FakeNatsSignaler) FailNextSetArmed() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.failNextSet
}

var _ NatsSignaler = &FakeNatsSignaler{}

// budgetExceededKey returns the canonical NATS KV key for the given budget scope.
// tenant scope:    "tenant/<tenantName>/aggregate"
// workspace scope: "workspace/<workspaceUID>/<model>"
func budgetExceededKey(scopeType, scopeID, model string) string {
	return scopeType + "/" + scopeID + "/" + model
}
