// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package authz

import (
	"context"

	"github.com/keese-ai/keese/internal/rebac"
)

// GuardrailOpenFGARebacWriter delegates Sync/Delete to the central rebac.Client.
type GuardrailOpenFGARebacWriter struct {
	Client *rebac.Client
}

func (w *GuardrailOpenFGARebacWriter) Sync(ctx context.Context, tuples []GuardrailRebacTuple) error {
	for _, t := range tuples {
		if err := w.Client.Write(ctx, t.Object, t.Relation, t.User); err != nil {
			return err
		}
	}
	return nil
}

func (w *GuardrailOpenFGARebacWriter) Delete(ctx context.Context, tuples []GuardrailRebacTuple) error {
	for _, t := range tuples {
		if err := w.Client.Delete(ctx, t.Object, t.Relation, t.User); err != nil {
			return err
		}
	}
	return nil
}

var _ GuardrailRebacWriter = (*GuardrailOpenFGARebacWriter)(nil)
