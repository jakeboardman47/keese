// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package keese

import (
	"context"
	"sync"
)

// RuntimeFakeRebacWriter is an in-memory RuntimeRebacWriter for tests.
// All operations are safe for concurrent use.
type RuntimeFakeRebacWriter struct {
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

// NewRuntimeFakeRebacWriter returns a zero-value RuntimeFakeRebacWriter ready for use.
func NewRuntimeFakeRebacWriter() *RuntimeFakeRebacWriter {
	return &RuntimeFakeRebacWriter{
		OwnerTuples:     map[string]string{},
		EnabledInTuples: map[string]map[string]struct{}{},
	}
}

func (f *RuntimeFakeRebacWriter) WriteExtensionOwner(_ context.Context, extensionName, tenantName string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.OwnerTuples[extensionName] = tenantName
	return nil
}

func (f *RuntimeFakeRebacWriter) DeleteExtensionOwner(_ context.Context, extensionName, _ string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.OwnerTuples, extensionName)
	return nil
}

func (f *RuntimeFakeRebacWriter) WriteExtensionEnabledIn(_ context.Context, extensionName, workspaceName string) error {
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

func (f *RuntimeFakeRebacWriter) DeleteExtensionEnabledIn(_ context.Context, extensionName, workspaceName string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if ws, ok := f.EnabledInTuples[extensionName]; ok {
		delete(ws, workspaceName)
	}
	return nil
}

func (f *RuntimeFakeRebacWriter) DeleteAllExtensionTuples(_ context.Context, extensionName string) (int, error) {
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

func (f *RuntimeFakeRebacWriter) CountEnabledIn(_ context.Context, extensionName string) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.EnabledInTuples[extensionName]), nil
}
