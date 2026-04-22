// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package recipe

import "context"

// RebacTuple represents a single OpenFGA relationship tuple.
type RebacTuple struct {
	// Object is the FGA object string, e.g. "recipe:my-recipe".
	Object string
	// Relation is the FGA relation, e.g. "readable_by", "uses_extension".
	Relation string
	// User is the FGA user string, e.g. "workspace:my-ws".
	User string
}

// RebacWriter is satisfied by the internal/rebac package.
// Until the OpenFGA SDK is added to go.mod, FakeRebacWriter is used.
//
// TODO(spec-followup): replace with internal/rebac.Writer once openfga-sdk is in go.mod.
type RebacWriter interface {
	// Sync writes (or no-ops if already present) the given tuples to OpenFGA.
	Sync(ctx context.Context, tuples []RebacTuple) error
	// Delete removes the given tuples from OpenFGA. Missing tuples are ignored.
	Delete(ctx context.Context, tuples []RebacTuple) error
}

// FakeRebacWriter is a no-op RebacWriter for tests and default wiring.
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
