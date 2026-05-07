// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package keese

import "context"

// MemoryFakeRebacWriter is an in-memory MemoryRebacWriter for unit and integration tests.
// It is not safe for concurrent use without external synchronisation.
type MemoryFakeRebacWriter struct {
	// Written accumulates all tuples passed to Write.
	Written []MemoryTuple

	// Deleted accumulates all tuples passed to Delete.
	Deleted []MemoryTuple

	// WriteErr, if non-nil, is returned by Write.
	WriteErr error

	// DeleteErr, if non-nil, is returned by Delete.
	DeleteErr error
}

// Reset clears accumulated state and error overrides. Call in BeforeEach.
func (f *MemoryFakeRebacWriter) Reset() {
	f.Written = nil
	f.Deleted = nil
	f.WriteErr = nil
	f.DeleteErr = nil
}

// Write implements MemoryRebacWriter.
func (f *MemoryFakeRebacWriter) Write(_ context.Context, tuples []MemoryTuple) error {
	if f.WriteErr != nil {
		return f.WriteErr
	}
	f.Written = append(f.Written, tuples...)
	return nil
}

// Delete implements MemoryRebacWriter.
func (f *MemoryFakeRebacWriter) Delete(_ context.Context, tuples []MemoryTuple) error {
	if f.DeleteErr != nil {
		return f.DeleteErr
	}
	f.Deleted = append(f.Deleted, tuples...)
	return nil
}
