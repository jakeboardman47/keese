// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package authz

import "context"

// GuardrailRebacTuple represents a single OpenFGA relationship tuple.
// Format mirrors the OpenFGA tuple model: object#relation@user.
type GuardrailRebacTuple struct {
	// Object is the FGA object string, e.g. "guardrail:my-binding".
	Object string
	// Relation is the FGA relation, e.g. "guardrail.inherits", "guardrail.binds_to_workspace".
	Relation string
	// User is the FGA user string, e.g. "workspace:my-ws".
	User string
}

// GuardrailRebacWriter is satisfied by GuardrailOpenFGARebacWriter (real, production)
// and by a test-only fake (see guardrail_rebac_fake_test.go). The real implementation is wired at startup
// via cmd/main.go when OPENFGA_API_URL is set; otherwise GuardrailNoopRebacWriter
// is used so the operator boots without a live OpenFGA instance.
type GuardrailRebacWriter interface {
	// Sync writes (or no-ops if already present) the given tuples to OpenFGA.
	// It is idempotent — calling Sync with the same tuples twice is safe.
	Sync(ctx context.Context, tuples []GuardrailRebacTuple) error

	// Delete removes the given tuples from OpenFGA.
	// Missing tuples are silently ignored (idempotent).
	Delete(ctx context.Context, tuples []GuardrailRebacTuple) error
}

// GuardrailNoopRebacWriter is a silent no-op GuardrailRebacWriter used when OpenFGA
// is not configured (dev/local run without OPENFGA_API_URL). It does not record calls.
type GuardrailNoopRebacWriter struct{}

func (GuardrailNoopRebacWriter) Sync(_ context.Context, _ []GuardrailRebacTuple) error  { return nil }
func (GuardrailNoopRebacWriter) Delete(_ context.Context, _ []GuardrailRebacTuple) error { return nil }

var _ GuardrailRebacWriter = GuardrailNoopRebacWriter{}
