// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package v1alpha1

import (
	"fmt"
	"sort"
	"sync"
)

// Factory builds an AgentRuntime instance for a given AgentRuntime CR.
// It receives the per-provider config (an opaque map populated by the
// controller from the CRD's `spec.implementation.<provider>` field).
type Factory func(config map[string]string) (AgentRuntime, error)

type entry struct {
	caps    CapabilityMatrix
	factory Factory
}

var (
	registryMu sync.RWMutex
	registry   = map[string]entry{}
)

// Register installs a provider's CapabilityMatrix and Factory. Callers
// invoke this in init(); duplicate names panic so the operator fails
// fast at startup rather than silently shadowing a provider.
//
// Spec §Static registration: cmd/main.go blank-imports each provider
// to drive these init() calls.
func Register(name string, caps CapabilityMatrix, factory Factory) {
	registryMu.Lock()
	defer registryMu.Unlock()
	if _, exists := registry[name]; exists {
		panic(fmt.Sprintf("runtime: provider %q already registered", name))
	}
	if caps.ProviderName == "" {
		caps.ProviderName = name
	}
	registry[name] = entry{caps: caps, factory: factory}
}

// Lookup returns the CapabilityMatrix and Factory for the named
// provider, or (zero, nil, false) when not registered. Admission
// rejects unknown providerRefs by checking this.
func Lookup(name string) (CapabilityMatrix, Factory, bool) {
	registryMu.RLock()
	defer registryMu.RUnlock()
	e, ok := registry[name]
	if !ok {
		return CapabilityMatrix{}, nil, false
	}
	return e.caps, e.factory, true
}

// Names returns every registered provider name, sorted. Stable ordering
// is needed for deterministic admission error messages and tests.
func Names() []string {
	registryMu.RLock()
	defer registryMu.RUnlock()
	names := make([]string, 0, len(registry))
	for n := range registry {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// resetForTest wipes the registry. Test-only; never call from
// production code.
func resetForTest() {
	registryMu.Lock()
	defer registryMu.Unlock()
	registry = map[string]entry{}
}
