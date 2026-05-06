// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package keese

import (
	"context"

	"github.com/keese-ai/keese/internal/rebac"
)

// WorkspaceOpenFGARebacWriter delegates Sync/Delete to the central rebac.Client.
type WorkspaceOpenFGARebacWriter struct {
	Client *rebac.Client
}

func (w *WorkspaceOpenFGARebacWriter) Sync(ctx context.Context, tuples []WorkspaceRebacTuple) error {
	for _, t := range tuples {
		if err := w.Client.Write(ctx, t.Object, t.Relation, t.User); err != nil {
			return err
		}
	}
	return nil
}

func (w *WorkspaceOpenFGARebacWriter) Delete(ctx context.Context, tuples []WorkspaceRebacTuple) error {
	for _, t := range tuples {
		if err := w.Client.Delete(ctx, t.Object, t.Relation, t.User); err != nil {
			return err
		}
	}
	return nil
}

var _ WorkspaceRebacWriter = (*WorkspaceOpenFGARebacWriter)(nil)
