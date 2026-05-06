// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package keese

import "context"

// RecipeRebacTuple represents a single OpenFGA relationship tuple.
type RecipeRebacTuple struct {
	// Object is the FGA object string, e.g. "recipe:my-recipe".
	Object string
	// Relation is the FGA relation, e.g. "readable_by", "uses_extension".
	Relation string
	// User is the FGA user string, e.g. "workspace:my-ws".
	User string
}

// RecipeRebacWriter is satisfied by the internal/rebac package.
// Until the OpenFGA SDK is added to go.mod, RecipeFakeRebacWriter is used.
//
// TODO(spec-followup): replace with internal/rebac.Writer once openfga-sdk is in go.mod.
type RecipeRebacWriter interface {
	// Sync writes (or no-ops if already present) the given tuples to OpenFGA.
	Sync(ctx context.Context, tuples []RecipeRebacTuple) error
	// Delete removes the given tuples from OpenFGA. Missing tuples are ignored.
	Delete(ctx context.Context, tuples []RecipeRebacTuple) error
}

// RecipeFakeRebacWriter is a no-op RecipeRebacWriter for tests and default wiring.
type RecipeFakeRebacWriter struct {
	Synced  []RecipeRebacTuple
	Deleted []RecipeRebacTuple
}

func (f *RecipeFakeRebacWriter) Sync(_ context.Context, tuples []RecipeRebacTuple) error {
	f.Synced = append(f.Synced, tuples...)
	return nil
}

func (f *RecipeFakeRebacWriter) Delete(_ context.Context, tuples []RecipeRebacTuple) error {
	f.Deleted = append(f.Deleted, tuples...)
	return nil
}

var _ RecipeRebacWriter = &RecipeFakeRebacWriter{}
