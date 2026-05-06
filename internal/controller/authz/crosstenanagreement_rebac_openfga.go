// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package authz

import (
	"context"

	"github.com/keese-ai/keese/internal/rebac"
)

// CTAOpenFGARebacWriter delegates Sync/Delete to the central rebac.Client.
type CTAOpenFGARebacWriter struct {
	Client *rebac.Client
}

func (w *CTAOpenFGARebacWriter) Sync(ctx context.Context, tuples []CTARebacTuple) error {
	for _, t := range tuples {
		if err := w.Client.Write(ctx, t.Object, t.Relation, t.User); err != nil {
			return err
		}
	}
	return nil
}

func (w *CTAOpenFGARebacWriter) Delete(ctx context.Context, tuples []CTARebacTuple) error {
	for _, t := range tuples {
		if err := w.Client.Delete(ctx, t.Object, t.Relation, t.User); err != nil {
			return err
		}
	}
	return nil
}

var _ CTARebacWriter = (*CTAOpenFGARebacWriter)(nil)
