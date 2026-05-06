// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package keese

import (
	"context"

	"github.com/keese-ai/keese/internal/rebac"
)

// TransportOpenFGARebacWriter satisfies the package-local TransportRebacWriter.
type TransportOpenFGARebacWriter struct {
	Client *rebac.Client
}

func (w *TransportOpenFGARebacWriter) Sync(ctx context.Context, tuples []TransportRebacTuple) error {
	for _, t := range tuples {
		if err := w.Client.Write(ctx, t.Object, t.Relation, t.User); err != nil {
			return err
		}
	}
	return nil
}

func (w *TransportOpenFGARebacWriter) Delete(ctx context.Context, tuples []TransportRebacTuple) error {
	for _, t := range tuples {
		if err := w.Client.Delete(ctx, t.Object, t.Relation, t.User); err != nil {
			return err
		}
	}
	return nil
}

var _ TransportRebacWriter = (*TransportOpenFGARebacWriter)(nil)
