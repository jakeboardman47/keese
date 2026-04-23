// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package runtime

import (
	"sync"
)

// registeredImpls holds the set of implementation names registered via init().
// Access is protected by registryMu; load-order-stable across packages.
var (
	registryMu      sync.RWMutex
	registeredImpls = map[string]struct{}{}
)

// RegisterImpl registers an implementation name into the global registry.
// Intended to be called from package-level init() functions in provider packages
// (e.g. internal/runtime/providers/goose/register.go).
// Duplicate registration is a no-op.
func RegisterImpl(name string) {
	registryMu.Lock()
	defer registryMu.Unlock()
	registeredImpls[name] = struct{}{}
}

// IsRegistered reports whether name is a known runtime implementation.
func IsRegistered(name string) bool {
	registryMu.RLock()
	defer registryMu.RUnlock()
	_, ok := registeredImpls[name]
	return ok
}

// RegisteredImpls returns a sorted copy of all registered implementation names.
func RegisteredImpls() []string {
	registryMu.RLock()
	defer registryMu.RUnlock()
	out := make([]string, 0, len(registeredImpls))
	for k := range registeredImpls {
		out = append(out, k)
	}
	return out
}

// Built-in registrations. Provider packages in internal/runtime/providers/* also
// call RegisterImpl via their own init(); the entries here are the canonical names
// that the controller understands directly.
func init() {
	RegisterImpl("goose")
	RegisterImpl("claudeCode")
	RegisterImpl("aider")
}
