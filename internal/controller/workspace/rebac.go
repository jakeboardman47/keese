// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package workspace

import "context"

// RebacTuple represents a single OpenFGA relationship tuple.
// Format mirrors the OpenFGA tuple model: object#relation@user.
type RebacTuple struct {
	// Object is the FGA object string, e.g. "workspace:my-ws".
	Object string
	// Relation is the FGA relation, e.g. "owner", "editor", "viewer".
	Relation string
	// User is the FGA user string, e.g. "user:alice" or "service_account:ksa-uid".
	User string
}

// RebacWriter is satisfied by the internal/rebac package once the OpenFGA SDK
// is added to go.mod. Until then, the fake implementation (FakeRebacWriter) is
// used in tests and the real wire-up is deferred.
//
// TODO(spec-followup): replace with internal/rebac.Writer once openfga-sdk is in go.mod.
type RebacWriter interface {
	// Sync writes (or no-ops if already present) the given tuples to OpenFGA.
	// It is idempotent — calling Sync with the same tuples twice is safe.
	Sync(ctx context.Context, tuples []RebacTuple) error

	// Delete removes the given tuples from OpenFGA.
	// Missing tuples are silently ignored (idempotent).
	Delete(ctx context.Context, tuples []RebacTuple) error
}

// FakeRebacWriter is a no-op RebacWriter used in tests and as the default
// when no real OpenFGA client is wired. It records calls for assertion.
type FakeRebacWriter struct {
	Synced  []RebacTuple
	Deleted []RebacTuple
}

func (f *FakeRebacWriter) Sync(_ context.Context, tuples []RebacTuple) error {
	f.Synced = append(f.Synced, tuples...)
	return nil
}

func (f *FakeRebacWriter) Delete(_ context.Context, tuples []RebacTuple) error {
	f.Deleted = append(f.Deleted, tuples...)
	return nil
}

var _ RebacWriter = &FakeRebacWriter{}
