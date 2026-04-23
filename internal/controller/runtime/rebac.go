// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package runtime

import (
	"context"
	"sync"
)

// RebacWriter is the interface the RuntimeExtension controller uses to manage
// OpenFGA tuples. Callers never reference OpenFGA directly — this boundary
// lets tests inject FakeRebacWriter without a live OpenFGA instance.
type RebacWriter interface {
	// WriteExtensionOwner writes the tuple:
	//   extension:<extensionName>#owner@tenant:<tenantName>
	WriteExtensionOwner(ctx context.Context, extensionName, tenantName string) error

	// DeleteExtensionOwner deletes the owner tuple.
	DeleteExtensionOwner(ctx context.Context, extensionName, tenantName string) error

	// WriteExtensionEnabledIn writes the tuple:
	//   extension:<extensionName>#enabled_in@workspace:<workspaceName>
	WriteExtensionEnabledIn(ctx context.Context, extensionName, workspaceName string) error

	// DeleteExtensionEnabledIn deletes a single enabled_in tuple.
	DeleteExtensionEnabledIn(ctx context.Context, extensionName, workspaceName string) error

	// DeleteAllExtensionTuples removes all tuples (owner + all enabled_in) for extensionName.
	// Used by the finalizer cleanup path.
	DeleteAllExtensionTuples(ctx context.Context, extensionName string) (int, error)

	// CountEnabledIn returns the number of active enabled_in tuples for extensionName.
	CountEnabledIn(ctx context.Context, extensionName string) (int, error)
}

// FakeRebacWriter is an in-memory RebacWriter for tests.
// All operations are safe for concurrent use.
type FakeRebacWriter struct {
	mu sync.Mutex

	// OwnerTuples maps extensionName -> tenantName (at most one owner tuple per extension).
	OwnerTuples map[string]string

	// EnabledInTuples maps extensionName -> set of workspaceNames.
	EnabledInTuples map[string]map[string]struct{}

	// WriteEnabledInErr, if non-nil, is returned by WriteExtensionEnabledIn.
	WriteEnabledInErr error

	// DeleteAllErr, if non-nil, is returned by DeleteAllExtensionTuples.
	DeleteAllErr error
}

// NewFakeRebacWriter returns a zero-value FakeRebacWriter ready for use.
func NewFakeRebacWriter() *FakeRebacWriter {
	return &FakeRebacWriter{
		OwnerTuples:     map[string]string{},
		EnabledInTuples: map[string]map[string]struct{}{},
	}
}

func (f *FakeRebacWriter) WriteExtensionOwner(_ context.Context, extensionName, tenantName string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.OwnerTuples[extensionName] = tenantName
	return nil
}

func (f *FakeRebacWriter) DeleteExtensionOwner(_ context.Context, extensionName, _ string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.OwnerTuples, extensionName)
	return nil
}

func (f *FakeRebacWriter) WriteExtensionEnabledIn(_ context.Context, extensionName, workspaceName string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.WriteEnabledInErr != nil {
		return f.WriteEnabledInErr
	}
	if f.EnabledInTuples[extensionName] == nil {
		f.EnabledInTuples[extensionName] = map[string]struct{}{}
	}
	f.EnabledInTuples[extensionName][workspaceName] = struct{}{}
	return nil
}

func (f *FakeRebacWriter) DeleteExtensionEnabledIn(_ context.Context, extensionName, workspaceName string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if ws, ok := f.EnabledInTuples[extensionName]; ok {
		delete(ws, workspaceName)
	}
	return nil
}

func (f *FakeRebacWriter) DeleteAllExtensionTuples(_ context.Context, extensionName string) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.DeleteAllErr != nil {
		return 0, f.DeleteAllErr
	}
	count := 0
	if ws, ok := f.EnabledInTuples[extensionName]; ok {
		count = len(ws)
		delete(f.EnabledInTuples, extensionName)
	}
	if _, ok := f.OwnerTuples[extensionName]; ok {
		count++
		delete(f.OwnerTuples, extensionName)
	}
	return count, nil
}

func (f *FakeRebacWriter) CountEnabledIn(_ context.Context, extensionName string) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.EnabledInTuples[extensionName]), nil
}
