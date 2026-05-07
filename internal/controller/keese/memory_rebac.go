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

// MemoryRebacWriter abstracts OpenFGA tuple operations. The real implementation
// (MemoryOpenFGARebacWriter) is wired at startup via cmd/main.go when OPENFGA_API_URL
// is set. Tests inject a fake writer (see memory_rebac_fake_test.go) directly.
// When OpenFGA is unconfigured, MemoryNoopRebacWriter is used as the fallback.
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

// MemoryNoopRebacWriter is a silent no-op MemoryRebacWriter used when OpenFGA
// is not configured (dev/local run without OPENFGA_API_URL). It does not record calls.
type MemoryNoopRebacWriter struct{}

func (MemoryNoopRebacWriter) Write(_ context.Context, _ []MemoryTuple) error  { return nil }
func (MemoryNoopRebacWriter) Delete(_ context.Context, _ []MemoryTuple) error { return nil }

var _ MemoryRebacWriter = MemoryNoopRebacWriter{}
