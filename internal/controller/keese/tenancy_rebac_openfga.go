// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package keese

import (
	"context"

	"github.com/keese-ai/keese/internal/rebac"
)

// TenantOpenFGARebacWriter delegates Sync/Delete to the central rebac.Client.
type TenantOpenFGARebacWriter struct {
	Client *rebac.Client
}

func (w *TenantOpenFGARebacWriter) Sync(ctx context.Context, tuples []TenantRebacTuple) error {
	for _, t := range tuples {
		if err := w.Client.Write(ctx, t.Object, t.Relation, t.User); err != nil {
			return err
		}
	}
	return nil
}

func (w *TenantOpenFGARebacWriter) Delete(ctx context.Context, tuples []TenantRebacTuple) error {
	for _, t := range tuples {
		if err := w.Client.Delete(ctx, t.Object, t.Relation, t.User); err != nil {
			return err
		}
	}
	return nil
}

var _ TenantRebacWriter = (*TenantOpenFGARebacWriter)(nil)
