// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package authz

import "context"

// CTAFakeRebacWriter is a no-op CTARebacWriter used in tests.
// It records Sync and Delete calls for assertion.
type CTAFakeRebacWriter struct {
	Synced  []CTARebacTuple
	Deleted []CTARebacTuple
}

func (f *CTAFakeRebacWriter) Sync(_ context.Context, tuples []CTARebacTuple) error {
	f.Synced = append(f.Synced, tuples...)
	return nil
}

func (f *CTAFakeRebacWriter) Delete(_ context.Context, tuples []CTARebacTuple) error {
	f.Deleted = append(f.Deleted, tuples...)
	return nil
}

var _ CTARebacWriter = &CTAFakeRebacWriter{}
