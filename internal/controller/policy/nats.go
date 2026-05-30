// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package policy

import "context"

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

// FakeNatsSignaler is a test double for NatsSignaler.
// It records set/clear calls for assertion.
type FakeNatsSignaler struct {
	// Exceeded holds the set of keys currently marked as exceeded.
	Exceeded map[string]bool

	// SetCalls records all SetExceeded calls (key).
	SetCalls []string

	// ClearCalls records all ClearExceeded calls (key).
	ClearCalls []string

	// FailNextSet causes the next SetExceeded call to return an error.
	FailNextSet bool

	// FailNextClear causes the next ClearExceeded call to return an error.
	FailNextClear bool
}

type natsSignalError struct{ key string }

func (e natsSignalError) Error() string {
	return "nats: signal operation failed for key " + e.key + " (fake)"
}

// SetExceeded implements NatsSignaler.
func (f *FakeNatsSignaler) SetExceeded(_ context.Context, key string) error {
	f.SetCalls = append(f.SetCalls, key)
	if f.FailNextSet {
		f.FailNextSet = false
		return natsSignalError{key: key}
	}
	if f.Exceeded == nil {
		f.Exceeded = make(map[string]bool)
	}
	f.Exceeded[key] = true
	return nil
}

// ClearExceeded implements NatsSignaler.
func (f *FakeNatsSignaler) ClearExceeded(_ context.Context, key string) error {
	f.ClearCalls = append(f.ClearCalls, key)
	if f.FailNextClear {
		f.FailNextClear = false
		return natsSignalError{key: key}
	}
	if f.Exceeded != nil {
		delete(f.Exceeded, key)
	}
	return nil
}

var _ NatsSignaler = &FakeNatsSignaler{}

// budgetExceededKey returns the canonical NATS KV key for the given budget scope.
// tenant scope:    "tenant/<tenantName>/aggregate"
// workspace scope: "workspace/<workspaceUID>/<model>"
func budgetExceededKey(scopeType, scopeID, model string) string {
	return scopeType + "/" + scopeID + "/" + model
}
