// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package memory

import "context"

// Tuple represents a single OpenFGA relationship tuple.
// Object, Relation, and User follow the OpenFGA tuple format:
//
//	Object:   "<type>:<id>"
//	Relation: "<relation>"
//	User:     "<type>:<id>"
type Tuple struct {
	Object   string
	Relation string
	User     string
}

// RebacWriter abstracts OpenFGA tuple operations so tests can inject a fake.
// The real implementation calls the OpenFGA HTTP/gRPC API.
type RebacWriter interface {
	// Write writes (upserts) the given tuples.
	Write(ctx context.Context, tuples []Tuple) error

	// Delete removes the given tuples. Missing tuples are silently ignored.
	Delete(ctx context.Context, tuples []Tuple) error
}

// MemoryOwnerTuple returns the tuple that grants a workspace ownership of a Memory.
func MemoryOwnerTuple(memoryID, workspaceName string) Tuple {
	return Tuple{
		Object:   "memory:" + memoryID,
		Relation: "owner",
		User:     "workspace:" + workspaceName,
	}
}

// MemoryReaderTuple returns the tuple that grants a workspace read access to a SharedMemory.
func MemoryReaderTuple(memoryID, workspaceName string) Tuple {
	return Tuple{
		Object:   "memory:" + memoryID,
		Relation: "reader",
		User:     "workspace:" + workspaceName,
	}
}

// MemoryWriterTuple returns the tuple that grants a workspace write access to a SharedMemory.
func MemoryWriterTuple(memoryID, workspaceName string) Tuple {
	return Tuple{
		Object:   "memory:" + memoryID,
		Relation: "writer",
		User:     "workspace:" + workspaceName,
	}
}

// FakeRebacWriter is an in-memory RebacWriter for unit and integration tests.
// It is not safe for concurrent use without external synchronisation.
type FakeRebacWriter struct {
	// Written accumulates all tuples passed to Write.
	Written []Tuple

	// Deleted accumulates all tuples passed to Delete.
	Deleted []Tuple

	// WriteErr, if non-nil, is returned by Write.
	WriteErr error

	// DeleteErr, if non-nil, is returned by Delete.
	DeleteErr error
}

// Reset clears accumulated state and error overrides. Call in BeforeEach.
func (f *FakeRebacWriter) Reset() {
	f.Written = nil
	f.Deleted = nil
	f.WriteErr = nil
	f.DeleteErr = nil
}

// Write implements RebacWriter.
func (f *FakeRebacWriter) Write(_ context.Context, tuples []Tuple) error {
	if f.WriteErr != nil {
		return f.WriteErr
	}
	f.Written = append(f.Written, tuples...)
	return nil
}

// Delete implements RebacWriter.
func (f *FakeRebacWriter) Delete(_ context.Context, tuples []Tuple) error {
	if f.DeleteErr != nil {
		return f.DeleteErr
	}
	f.Deleted = append(f.Deleted, tuples...)
	return nil
}
