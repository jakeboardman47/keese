// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package keese

import (
	"context"

	"github.com/keese-ai/keese/internal/rebac"
)

// MemoryOpenFGARebacWriter delegates Write/Delete to the central rebac.Client.
type MemoryOpenFGARebacWriter struct {
	Client *rebac.Client
}

func (w *MemoryOpenFGARebacWriter) Write(ctx context.Context, tuples []MemoryTuple) error {
	for _, t := range tuples {
		if err := w.Client.Write(ctx, t.Object, t.Relation, t.User); err != nil {
			return err
		}
	}
	return nil
}

func (w *MemoryOpenFGARebacWriter) Delete(ctx context.Context, tuples []MemoryTuple) error {
	for _, t := range tuples {
		if err := w.Client.Delete(ctx, t.Object, t.Relation, t.User); err != nil {
			return err
		}
	}
	return nil
}

var _ MemoryRebacWriter = (*MemoryOpenFGARebacWriter)(nil)
