// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package adkgo

import (
	"context"
	"errors"
	"testing"

	spi "github.com/keese-ai/keese/internal/runtime/spi/v1alpha1"
)

// TestRegistration asserts the package init() registered the adkGo provider
// under the documented name, that Lookup returns the zeroed E0 capability
// matrix, and that the factory builds a Runtime answering to the same name.
func TestRegistration(t *testing.T) {
	caps, factory, ok := spi.Lookup(ProviderName)
	if !ok {
		t.Fatalf("Lookup(%q): provider not registered", ProviderName)
	}
	if factory == nil {
		t.Fatal("Lookup: factory is nil")
	}
	if caps.ProviderName != ProviderName {
		t.Errorf("caps.ProviderName: got %q, want %q", caps.ProviderName, ProviderName)
	}

	rt, err := factory(map[string]string{"image": "ghcr.io/keese/adk-go:test"})
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	if rt.Name() != ProviderName {
		t.Errorf("Runtime.Name(): got %q, want %q", rt.Name(), ProviderName)
	}
}

// TestFactoryHonorsImage asserts the E0 factory reads the "image" config key
// and that a nil/empty config produces an empty image rather than panicking.
func TestFactoryHonorsImage(t *testing.T) {
	tests := []struct {
		name      string
		config    map[string]string
		wantImage string
	}{
		{name: "image set", config: map[string]string{"image": "img:v1"}, wantImage: "img:v1"},
		{name: "image absent", config: map[string]string{}, wantImage: ""},
		{name: "nil config", config: nil, wantImage: ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rt, err := Factory(tc.config)
			if err != nil {
				t.Fatalf("Factory: %v", err)
			}
			got, ok := rt.(*Runtime)
			if !ok {
				t.Fatalf("Factory returned %T, want *Runtime", rt)
			}
			if got.image != tc.wantImage {
				t.Errorf("image: got %q, want %q", got.image, tc.wantImage)
			}
		})
	}
}

// TestCapabilityMatrixZeroed asserts the E0 capability matrix advertises no
// optional features (all flags false / zero) per the adkGo skeleton design.
func TestCapabilityMatrixZeroed(t *testing.T) {
	c := capabilities
	if c.ProviderName != ProviderName {
		t.Errorf("ProviderName: got %q, want %q", c.ProviderName, ProviderName)
	}
	if c.SPIVersion != "1.0.0" {
		t.Errorf("SPIVersion: got %q, want 1.0.0", c.SPIVersion)
	}
	if c.MaxSubAgents != 0 {
		t.Errorf("MaxSubAgents: got %d, want 0", c.MaxSubAgents)
	}
	bools := map[string]bool{
		"SupportsACP":                c.SupportsACP,
		"SupportsSubAgents":          c.SupportsSubAgents,
		"SupportsResume":             c.SupportsResume,
		"SupportsSubAgentCleanup":    c.SupportsSubAgentCleanup,
		"SupportsInjectPrompt":       c.SupportsInjectPrompt,
		"SupportsStreaming":          c.SupportsStreaming,
		"SupportsMCP":                c.SupportsMCP,
		"SupportsRecipes":            c.SupportsRecipes,
		"SupportsCredentialRotation": c.SupportsCredentialRotation,
	}
	for name, v := range bools {
		if v {
			t.Errorf("capability %s: got true, want false (E0 skeleton)", name)
		}
	}
}

// TestCapabilitiesMethodMatchesMatrix asserts Runtime.Capabilities() returns
// the same static matrix registered with the SPI.
func TestCapabilitiesMethodMatchesMatrix(t *testing.T) {
	r := &Runtime{}
	if got := r.Capabilities(); got != capabilities {
		t.Errorf("Capabilities(): got %+v, want %+v", got, capabilities)
	}
}

// TestSPIMethodsReturnUnsupported drives every E0 SPI method and asserts it
// returns the documented sentinel error with no usable result. Run, Attach,
// InvokeSubAgent, Health and StreamEvents return a nil result alongside the
// sentinel; the rest return only the sentinel.
func TestSPIMethodsReturnUnsupported(t *testing.T) {
	ctx := context.Background()
	r := &Runtime{}

	t.Run("Bootstrap", func(t *testing.T) {
		if err := r.Bootstrap(ctx, spi.Workspace{}); !errors.Is(err, spi.ErrUnsupported) {
			t.Errorf("Bootstrap: got %v, want ErrUnsupported", err)
		}
	})
	t.Run("Run", func(t *testing.T) {
		res, err := r.Run(ctx, "recipe", nil)
		if !errors.Is(err, spi.ErrUnsupported) {
			t.Errorf("Run err: got %v, want ErrUnsupported", err)
		}
		if res != nil {
			t.Errorf("Run result: got %+v, want nil", res)
		}
	})
	t.Run("Attach", func(t *testing.T) {
		h, err := r.Attach(ctx, spi.WorkspaceSession{})
		if !errors.Is(err, spi.ErrAttachUnsupported) {
			t.Errorf("Attach err: got %v, want ErrAttachUnsupported", err)
		}
		if h != nil {
			t.Errorf("Attach handle: got %+v, want nil", h)
		}
	})
	t.Run("Resume", func(t *testing.T) {
		if err := r.Resume(ctx, spi.Workspace{}); !errors.Is(err, spi.ErrUnsupported) {
			t.Errorf("Resume: got %v, want ErrUnsupported", err)
		}
	})
	t.Run("Drain", func(t *testing.T) {
		if err := r.Drain(ctx, spi.WorkspaceSession{}); !errors.Is(err, spi.ErrUnsupported) {
			t.Errorf("Drain: got %v, want ErrUnsupported", err)
		}
	})
	t.Run("CleanupSubAgents", func(t *testing.T) {
		if err := r.CleanupSubAgents(ctx, spi.Workspace{}); !errors.Is(err, spi.ErrUnsupported) {
			t.Errorf("CleanupSubAgents: got %v, want ErrUnsupported", err)
		}
	})
	t.Run("InjectPrompt", func(t *testing.T) {
		if err := r.InjectPrompt(ctx, spi.WorkspaceSession{}, "hi"); !errors.Is(err, spi.ErrUnsupported) {
			t.Errorf("InjectPrompt: got %v, want ErrUnsupported", err)
		}
	})
	t.Run("InvokeSubAgent", func(t *testing.T) {
		h, err := r.InvokeSubAgent(ctx, spi.Workspace{}, spi.SubAgentSpec{})
		if !errors.Is(err, spi.ErrUnsupported) {
			t.Errorf("InvokeSubAgent err: got %v, want ErrUnsupported", err)
		}
		if h != nil {
			t.Errorf("InvokeSubAgent handle: got %+v, want nil", h)
		}
	})
	t.Run("Health", func(t *testing.T) {
		hr, err := r.Health(ctx, spi.WorkspaceSession{})
		if !errors.Is(err, spi.ErrUnsupported) {
			t.Errorf("Health err: got %v, want ErrUnsupported", err)
		}
		if hr != nil {
			t.Errorf("Health report: got %+v, want nil", hr)
		}
	})
	t.Run("StreamEvents", func(t *testing.T) {
		ch, err := r.StreamEvents(ctx)
		if !errors.Is(err, spi.ErrUnsupported) {
			t.Errorf("StreamEvents err: got %v, want ErrUnsupported", err)
		}
		if ch != nil {
			t.Errorf("StreamEvents channel: got %v, want nil", ch)
		}
	})
}

// TestRuntimeImplementsSPI is a compile-time + run-time assertion that Runtime
// satisfies the spi.AgentRuntime interface.
func TestRuntimeImplementsSPI(t *testing.T) {
	var _ spi.AgentRuntime = (*Runtime)(nil)
	rt, err := Factory(nil)
	if err != nil {
		t.Fatalf("Factory: %v", err)
	}
	if _, ok := rt.(spi.AgentRuntime); !ok {
		t.Fatal("Factory result does not satisfy spi.AgentRuntime")
	}
}
