// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package keese

import "context"

// MemoryTuple represents a single OpenFGA relationship tuple.
// Object, Relation, and User follow the OpenFGA tuple format:
//
//	Object:   "<type>:<id>"
//	Relation: "<relation>"
//	User:     "<type>:<id>"
type MemoryTuple struct {
	Object   string
	Relation string
	User     string
}

// MemoryRebacWriter abstracts OpenFGA tuple operations so tests can inject a fake.
// The real implementation calls the OpenFGA HTTP/gRPC API.
type MemoryRebacWriter interface {
	// Write writes (upserts) the given tuples.
	Write(ctx context.Context, tuples []MemoryTuple) error

	// Delete removes the given tuples. Missing tuples are silently ignored.
	Delete(ctx context.Context, tuples []MemoryTuple) error
}

// MemoryOwnerTuple returns the tuple that grants a workspace ownership of a Memory.
func MemoryOwnerTuple(memoryID, workspaceName string) MemoryTuple {
	return MemoryTuple{
		Object:   "memory:" + memoryID,
		Relation: "owner",
		User:     "workspace:" + workspaceName,
	}
}

// MemoryReaderTuple returns the tuple that grants a workspace read access to a SharedMemory.
func MemoryReaderTuple(memoryID, workspaceName string) MemoryTuple {
	return MemoryTuple{
		Object:   "memory:" + memoryID,
		Relation: "reader",
		User:     "workspace:" + workspaceName,
	}
}

// MemoryWriterTuple returns the tuple that grants a workspace write access to a SharedMemory.
func MemoryWriterTuple(memoryID, workspaceName string) MemoryTuple {
	return MemoryTuple{
		Object:   "memory:" + memoryID,
		Relation: "writer",
		User:     "workspace:" + workspaceName,
	}
}

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
