// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package keese

import (
	"testing"

	keesev1alpha1 "github.com/keese-ai/keese/api/keese/v1alpha1"
)

// TestDetectProvider covers the discriminated one-of in
// AgentRuntime.spec.implementation. Regression guard for the bug where
// adkPython/adkGo fell through to the default error and drove the
// AgentRuntime to a permanent Degraded phase even though both providers
// are registered with the SPI registry.
func TestDetectProvider(t *testing.T) {
	tests := []struct {
		name    string
		impl    keesev1alpha1.AgentRuntimeImplementation
		want    string
		wantErr bool
	}{
		{"goose", keesev1alpha1.AgentRuntimeImplementation{Goose: &keesev1alpha1.GooseSpec{}}, "goose", false},
		{"claudeCode", keesev1alpha1.AgentRuntimeImplementation{ClaudeCode: &keesev1alpha1.ClaudeCodeSpec{}}, "claudeCode", false},
		{"aider", keesev1alpha1.AgentRuntimeImplementation{Aider: &keesev1alpha1.AiderSpec{}}, "aider", false},
		{"adkPython", keesev1alpha1.AgentRuntimeImplementation{AdkPython: &keesev1alpha1.ADKPythonSpec{}}, "adkPython", false},
		{"adkGo", keesev1alpha1.AgentRuntimeImplementation{AdkGo: &keesev1alpha1.ADKGoSpec{}}, "adkGo", false},
		{"none", keesev1alpha1.AgentRuntimeImplementation{}, "", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ar := &keesev1alpha1.AgentRuntime{Spec: keesev1alpha1.AgentRuntimeSpec{Implementation: tc.impl}}
			got, err := detectProvider(ar)
			if (err != nil) != tc.wantErr {
				t.Fatalf("detectProvider() err = %v, wantErr = %v", err, tc.wantErr)
			}
			if got != tc.want {
				t.Errorf("detectProvider() = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestDetectProvider_RegisteredProvidersResolve asserts that the providers
// shipped on main (goose, adkPython, adkGo) resolve to names that the SPI
// registry recognizes, so the reconciler reaches Ready rather than Degraded.
func TestDetectProvider_RegisteredProvidersResolve(t *testing.T) {
	for _, impl := range []keesev1alpha1.AgentRuntimeImplementation{
		{Goose: &keesev1alpha1.GooseSpec{}},
		{AdkPython: &keesev1alpha1.ADKPythonSpec{}},
		{AdkGo: &keesev1alpha1.ADKGoSpec{}},
	} {
		ar := &keesev1alpha1.AgentRuntime{Spec: keesev1alpha1.AgentRuntimeSpec{Implementation: impl}}
		name, err := detectProvider(ar)
		if err != nil {
			t.Fatalf("detectProvider() unexpected error: %v", err)
		}
		if !IsRegistered(name) {
			t.Errorf("provider %q resolved but is not registered with the SPI registry", name)
		}
	}
}
