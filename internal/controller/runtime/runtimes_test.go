// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

// Package runtime — pure-logic unit tests for the runtime registry.
// These do NOT require envtest (no integration build tag).
package runtime

import (
	"testing"
)

func TestIsRegistered_builtins(t *testing.T) {
	cases := []struct {
		name string
		want bool
	}{
		{"goose", true},
		{"claudeCode", true},
		{"aider", true},
		{"unknown-provider", false},
		{"", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := IsRegistered(tc.name)
			if got != tc.want {
				t.Errorf("IsRegistered(%q) = %v, want %v", tc.name, got, tc.want)
			}
		})
	}
}

func TestRegisterImpl_idempotent(t *testing.T) {
	// Re-registering the same name must not panic or double-count.
	RegisterImpl("goose")
	RegisterImpl("goose")
	if !IsRegistered("goose") {
		t.Fatal("goose should still be registered")
	}
}

func TestRegisterImpl_new(t *testing.T) {
	const name = "test-only-provider"
	RegisterImpl(name)
	if !IsRegistered(name) {
		t.Fatalf("expected %q to be registered", name)
	}
}

func TestRegisteredImpls_contains_builtins(t *testing.T) {
	impls := RegisteredImpls()
	want := map[string]bool{"goose": false, "claudeCode": false, "aider": false}
	for _, name := range impls {
		if _, ok := want[name]; ok {
			want[name] = true
		}
	}
	for k, found := range want {
		if !found {
			t.Errorf("RegisteredImpls() missing builtin %q", k)
		}
	}
}
