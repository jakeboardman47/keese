// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package keese

import "context"

// RecipeFakeRebacWriter is a no-op RecipeRebacWriter used in tests.
// It records Sync and Delete calls for assertion.
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
