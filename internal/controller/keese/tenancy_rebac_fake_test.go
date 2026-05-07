// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package keese

import "context"

// TenantFakeRebacWriter is a no-op TenantRebacWriter used in tests.
// It records Sync and Delete calls for assertion.
type TenantFakeRebacWriter struct {
	Synced  []TenantRebacTuple
	Deleted []TenantRebacTuple
}

func (f *TenantFakeRebacWriter) Sync(_ context.Context, tuples []TenantRebacTuple) error {
	f.Synced = append(f.Synced, tuples...)
	return nil
}

func (f *TenantFakeRebacWriter) Delete(_ context.Context, tuples []TenantRebacTuple) error {
	f.Deleted = append(f.Deleted, tuples...)
	return nil
}

var _ TenantRebacWriter = &TenantFakeRebacWriter{}
