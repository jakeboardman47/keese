// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package keese

import "context"

// WorkspaceFakeRebacWriter is a no-op WorkspaceRebacWriter used in tests.
// It records Sync and Delete calls for assertion.
type WorkspaceFakeRebacWriter struct {
	Synced  []WorkspaceRebacTuple
	Deleted []WorkspaceRebacTuple
}

func (f *WorkspaceFakeRebacWriter) Sync(_ context.Context, tuples []WorkspaceRebacTuple) error {
	f.Synced = append(f.Synced, tuples...)
	return nil
}

func (f *WorkspaceFakeRebacWriter) Delete(_ context.Context, tuples []WorkspaceRebacTuple) error {
	f.Deleted = append(f.Deleted, tuples...)
	return nil
}

var _ WorkspaceRebacWriter = &WorkspaceFakeRebacWriter{}
