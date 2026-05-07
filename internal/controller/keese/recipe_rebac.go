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

// RecipeRebacWriter is satisfied by RecipeOpenFGARebacWriter (real, production)
// and by a test-only fake (see recipe_rebac_fake_test.go). The real implementation is wired at startup
// via cmd/main.go when OPENFGA_API_URL is set; otherwise RecipeNoopRebacWriter
// is used so the operator boots without a live OpenFGA instance.
type RecipeRebacWriter interface {
	// Sync writes (or no-ops if already present) the given tuples to OpenFGA.
	Sync(ctx context.Context, tuples []RecipeRebacTuple) error
	// Delete removes the given tuples from OpenFGA. Missing tuples are ignored.
	Delete(ctx context.Context, tuples []RecipeRebacTuple) error
}

// RecipeNoopRebacWriter is a silent no-op RecipeRebacWriter used when OpenFGA
// is not configured (dev/local run without OPENFGA_API_URL). It does not record calls.
type RecipeNoopRebacWriter struct{}

func (RecipeNoopRebacWriter) Sync(_ context.Context, _ []RecipeRebacTuple) error  { return nil }
func (RecipeNoopRebacWriter) Delete(_ context.Context, _ []RecipeRebacTuple) error { return nil }

var _ RecipeRebacWriter = RecipeNoopRebacWriter{}
