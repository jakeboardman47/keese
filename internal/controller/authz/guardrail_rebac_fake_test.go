// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

//go:build integration

package authz

import "context"

// FakeRebacWriter is a no-op GuardrailRebacWriter used in integration tests.
// It records Sync and Delete calls for assertion.
type FakeRebacWriter struct {
	Synced  []GuardrailRebacTuple
	Deleted []GuardrailRebacTuple
	// SyncErr, if non-nil, is returned from every Sync call.
	SyncErr error
	// DeleteErr, if non-nil, is returned from every Delete call.
	DeleteErr error
}

func (f *FakeRebacWriter) Sync(_ context.Context, tuples []GuardrailRebacTuple) error {
	if f.SyncErr != nil {
		return f.SyncErr
	}
	f.Synced = append(f.Synced, tuples...)
	return nil
}

func (f *FakeRebacWriter) Delete(_ context.Context, tuples []GuardrailRebacTuple) error {
	if f.DeleteErr != nil {
		return f.DeleteErr
	}
	f.Deleted = append(f.Deleted, tuples...)
	return nil
}

var _ GuardrailRebacWriter = &FakeRebacWriter{}
