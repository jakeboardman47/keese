// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package keese

import (
	"context"

	"github.com/keese-ai/keese/internal/rebac"
)

// RecipeOpenFGARebacWriter delegates Sync/Delete to the central rebac.Client.
type RecipeOpenFGARebacWriter struct {
	Client *rebac.Client
}

func (w *RecipeOpenFGARebacWriter) Sync(ctx context.Context, tuples []RecipeRebacTuple) error {
	for _, t := range tuples {
		if err := w.Client.Write(ctx, t.Object, t.Relation, t.User); err != nil {
			return err
		}
	}
	return nil
}

func (w *RecipeOpenFGARebacWriter) Delete(ctx context.Context, tuples []RecipeRebacTuple) error {
	for _, t := range tuples {
		if err := w.Client.Delete(ctx, t.Object, t.Relation, t.User); err != nil {
			return err
		}
	}
	return nil
}

var _ RecipeRebacWriter = (*RecipeOpenFGARebacWriter)(nil)
