// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package v1alpha1

import (
	"context"
	"reflect"
	"testing"
)

type stubRuntime struct{ name string }

func (s *stubRuntime) Name() string                 { return s.name }
func (s *stubRuntime) Capabilities() CapabilityMatrix { return CapabilityMatrix{ProviderName: s.name} }
func (s *stubRuntime) Bootstrap(context.Context, Workspace) error { return nil }
func (s *stubRuntime) Run(context.Context, string, map[string]string) (*RunResult, error) {
	return nil, nil
}
func (s *stubRuntime) Attach(context.Context, WorkspaceSession) (*AttachHandle, error) { return nil, nil }
func (s *stubRuntime) Resume(context.Context, Workspace) error                         { return nil }
func (s *stubRuntime) Drain(context.Context, WorkspaceSession) error                   { return nil }
func (s *stubRuntime) CleanupSubAgents(context.Context, Workspace) error               { return nil }
func (s *stubRuntime) InjectPrompt(context.Context, WorkspaceSession, string) error    { return nil }
func (s *stubRuntime) InvokeSubAgent(context.Context, Workspace, SubAgentSpec) (*SubAgentHandle, error) {
	return nil, nil
}
func (s *stubRuntime) Health(context.Context, WorkspaceSession) (*HealthReport, error) { return nil, nil }
func (s *stubRuntime) StreamEvents(context.Context) (<-chan RuntimeEvent, error)       { return nil, nil }

func TestRegisterAndLookup(t *testing.T) {
	resetForTest()

	caps := CapabilityMatrix{ProviderName: "alpha", SPIVersion: "1.0.0", SupportsACP: true}
	factory := func(map[string]string) (AgentRuntime, error) { return &stubRuntime{name: "alpha"}, nil }
	Register("alpha", caps, factory)

	gotCaps, gotFactory, ok := Lookup("alpha")
	if !ok {
		t.Fatal("Lookup(alpha): not found")
	}
	if !reflect.DeepEqual(gotCaps, caps) {
		t.Fatalf("Lookup caps: got %+v, want %+v", gotCaps, caps)
	}
	rt, err := gotFactory(nil)
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	if rt.Name() != "alpha" {
		t.Fatalf("Name: got %q, want alpha", rt.Name())
	}
}

func TestLookupUnknownProvider(t *testing.T) {
	resetForTest()
	if _, _, ok := Lookup("does-not-exist"); ok {
		t.Fatal("Lookup unknown: got ok=true, want false")
	}
}

func TestRegisterDuplicatePanics(t *testing.T) {
	resetForTest()
	Register("dup", CapabilityMatrix{}, func(map[string]string) (AgentRuntime, error) { return nil, nil })

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("Register duplicate: expected panic, got none")
		}
	}()
	Register("dup", CapabilityMatrix{}, func(map[string]string) (AgentRuntime, error) { return nil, nil })
}

func TestRegisterFillsProviderName(t *testing.T) {
	resetForTest()
	caps := CapabilityMatrix{} // no ProviderName set
	Register("beta", caps, func(map[string]string) (AgentRuntime, error) { return nil, nil })
	got, _, ok := Lookup("beta")
	if !ok {
		t.Fatal("Lookup(beta): not found")
	}
	if got.ProviderName != "beta" {
		t.Fatalf("ProviderName: got %q, want beta", got.ProviderName)
	}
}

func TestNamesSorted(t *testing.T) {
	resetForTest()
	Register("zeta", CapabilityMatrix{}, func(map[string]string) (AgentRuntime, error) { return nil, nil })
	Register("alpha", CapabilityMatrix{}, func(map[string]string) (AgentRuntime, error) { return nil, nil })
	Register("mu", CapabilityMatrix{}, func(map[string]string) (AgentRuntime, error) { return nil, nil })
	got := Names()
	want := []string{"alpha", "mu", "zeta"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Names: got %v, want %v", got, want)
	}
}
