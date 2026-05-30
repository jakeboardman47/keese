// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package featuregate

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

func newGates(t *testing.T, defaults map[Gate]bool, initial map[string]bool) (*Gates, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "gates.json")
	if initial != nil {
		b, _ := json.Marshal(initial)
		if err := os.WriteFile(path, b, 0o600); err != nil {
			t.Fatalf("seed gates.json: %v", err)
		}
	}
	g, err := New(context.Background(), Options{
		Path:       path,
		Defaults:   defaults,
		Binary:     "test-binary",
		Registerer: prometheus.NewRegistry(),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = g.Close() })
	return g, path
}

func TestEnabled_DefaultUsedWhenFileAbsent(t *testing.T) {
	g, _ := newGates(t,
		map[Gate]bool{CosignInstallPlanVerify: true},
		nil)
	if !g.Enabled(context.Background(), CosignInstallPlanVerify) {
		t.Fatalf("expected default true")
	}
}

func TestEnabled_FileOverridesDefault(t *testing.T) {
	g, _ := newGates(t,
		map[Gate]bool{CosignInstallPlanVerify: true},
		map[string]bool{string(CosignInstallPlanVerify): false})
	if g.Enabled(context.Background(), CosignInstallPlanVerify) {
		t.Fatalf("expected file false to override default true")
	}
}

func TestEnabled_UnknownGateReturnsFalse(t *testing.T) {
	g, _ := newGates(t, nil, nil)
	if g.Enabled(context.Background(), Gate("not-a-gate")) {
		t.Fatalf("unknown gate must return false")
	}
}

func TestEnabled_HotReloadOnFileWrite(t *testing.T) {
	g, path := newGates(t,
		map[Gate]bool{CosignInstallPlanVerify: false},
		map[string]bool{string(CosignInstallPlanVerify): false})
	if g.Enabled(context.Background(), CosignInstallPlanVerify) {
		t.Fatalf("initial false expected")
	}
	// Atomic-ish rewrite via temp + rename so fsnotify sees a coherent file.
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp,
		[]byte(`{"cosign-installplan-verify":true}`), 0o600); err != nil {
		t.Fatalf("write tmp: %v", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		t.Fatalf("rename: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if g.Enabled(context.Background(), CosignInstallPlanVerify) {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("hot reload did not propagate within 2s; snapshot=%v",
		g.Snapshot())
}

func TestEnabled_MalformedFileFallsBackToDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gates.json")
	if err := os.WriteFile(path, []byte("not-json"), 0o600); err != nil {
		t.Fatalf("write garbage: %v", err)
	}
	g, err := New(context.Background(), Options{
		Path: path,
		Defaults: map[Gate]bool{
			CosignInstallPlanVerify: true,
		},
		Binary:     "test-binary",
		Registerer: prometheus.NewRegistry(),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = g.Close() })
	if !g.Enabled(context.Background(), CosignInstallPlanVerify) {
		t.Fatalf("expected default true after malformed-JSON fallback")
	}
}

func TestSnapshot_IsolatedFromInternal(t *testing.T) {
	g, _ := newGates(t,
		map[Gate]bool{CosignInstallPlanVerify: true},
		nil)
	snap := g.Snapshot()
	snap[CosignInstallPlanVerify] = false
	if !g.Enabled(context.Background(), CosignInstallPlanVerify) {
		t.Fatalf("snapshot mutation must not bleed into internal state")
	}
}
