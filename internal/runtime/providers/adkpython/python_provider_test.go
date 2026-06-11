// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

//go:build integration

package adkpython

import (
	"reflect"
	"regexp"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"

	keesev1alpha1 "github.com/keese-ai/keese/api/keese/v1alpha1"
)

// secretEnvName matches any env var name or value that looks like a credential.
// Rule 05.2: NONE may appear on an ADK Python pod.
var secretEnvPattern = regexp.MustCompile(`(?i)(API_KEY|SECRET|TOKEN_VALUE|BEARER|PASSWORD|_KEY$|ANTHROPIC_API|OPENAI_API)`)

func testInput() PodInput {
	return PodInput{
		Image:              "ghcr.io/keese/adk-python@sha256:deadbeef",
		WorkspaceName:      "ws-demo",
		TenantName:         "acme",
		ServiceAccountName: "ksa-uid-1234",
		SessionPVCName:     "keese-ws-uid-1234-session",
	}
}

// TestADKPythonProvider_PodRender asserts the security-critical invariants of
// the rendered pod: zero credential-shaped env vars (rule 05.2), the egress SA
// token projected with the correct audience + TTL (rule 05.7), read-only root
// filesystem + drop-ALL + non-root (rule 05.11), and the CA bundle mounted.
func TestADKPythonProvider_PodRender(t *testing.T) {
	spec := BuildPodSpec(testInput())

	if len(spec.Containers) != 1 {
		t.Fatalf("container count: got %d, want 1 (single-container increment)", len(spec.Containers))
	}
	c := spec.Containers[0]
	if c.Name != ContainerName {
		t.Errorf("container name: got %q, want %q", c.Name, ContainerName)
	}

	t.Run("zero credential env vars", func(t *testing.T) {
		for _, e := range c.Env {
			if secretEnvPattern.MatchString(e.Name) {
				t.Errorf("env name %q matches credential pattern (rule 05.2)", e.Name)
			}
			if secretEnvPattern.MatchString(e.Value) {
				t.Errorf("env %q value %q matches credential pattern (rule 05.2)", e.Name, e.Value)
			}
			// No env may carry secret material via secretKeyRef (rule 05.7).
			if e.ValueFrom != nil && e.ValueFrom.SecretKeyRef != nil {
				t.Errorf("env %q uses secretKeyRef — forbidden (rule 05.7)", e.Name)
			}
		}
	})

	t.Run("gateway base-urls present and non-secret", func(t *testing.T) {
		want := map[string]string{
			"ENVOY_AI_GATEWAY_URL":   EnvoyAIGatewayURL,
			"OPENAI_BASE_URL":        EnvoyAIGatewayURL + "/openai/v1",
			"ANTHROPIC_BASE_URL":     EnvoyAIGatewayURL + "/anthropic",
			"GOOGLE_VERTEX_BASE_URL": EnvoyAIGatewayURL + "/vertex",
		}
		got := map[string]string{}
		for _, e := range c.Env {
			got[e.Name] = e.Value
		}
		for k, v := range want {
			if got[k] != v {
				t.Errorf("env %q: got %q, want %q", k, got[k], v)
			}
		}
	})

	t.Run("egress SA token projected", func(t *testing.T) {
		var proj *corev1.ServiceAccountTokenProjection
		for _, v := range spec.Volumes {
			if v.Projected == nil {
				continue
			}
			for _, s := range v.Projected.Sources {
				if s.ServiceAccountToken != nil {
					proj = s.ServiceAccountToken
				}
			}
		}
		if proj == nil {
			t.Fatal("no projected SA token volume found (rule 05.7)")
		}
		if proj.Audience != "keese-egress-acme" {
			t.Errorf("token audience: got %q, want keese-egress-acme", proj.Audience)
		}
		if proj.ExpirationSeconds == nil || *proj.ExpirationSeconds != 600 {
			t.Errorf("token TTL: got %v, want 600", proj.ExpirationSeconds)
		}
		if spec.AutomountServiceAccountToken == nil || *spec.AutomountServiceAccountToken {
			t.Error("automountServiceAccountToken must be false (token via projection only)")
		}
	})

	t.Run("hardened security context", func(t *testing.T) {
		sc := c.SecurityContext
		if sc == nil {
			t.Fatal("container SecurityContext is nil (rule 05.11)")
		}
		if sc.ReadOnlyRootFilesystem == nil || !*sc.ReadOnlyRootFilesystem {
			t.Error("readOnlyRootFilesystem must be true (rule 05.11)")
		}
		if sc.RunAsNonRoot == nil || !*sc.RunAsNonRoot {
			t.Error("runAsNonRoot must be true (rule 05.11)")
		}
		if sc.AllowPrivilegeEscalation == nil || *sc.AllowPrivilegeEscalation {
			t.Error("allowPrivilegeEscalation must be false (rule 05.11)")
		}
		if sc.Capabilities == nil || len(sc.Capabilities.Drop) != 1 || sc.Capabilities.Drop[0] != "ALL" {
			t.Errorf("capabilities.drop must be [ALL], got %+v", sc.Capabilities)
		}
	})

	t.Run("CA bundle mounted read-only", func(t *testing.T) {
		var found bool
		for _, m := range c.VolumeMounts {
			if m.MountPath == caBundleMountPath {
				found = true
				if !m.ReadOnly {
					t.Error("CA bundle mount must be read-only")
				}
			}
		}
		if !found {
			t.Errorf("CA bundle not mounted at %q", caBundleMountPath)
		}
	})

	t.Run("session PVC mounted", func(t *testing.T) {
		var found bool
		for _, m := range c.VolumeMounts {
			if m.MountPath == sessionMountPath {
				found = true
			}
		}
		if !found {
			t.Errorf("session PVC not mounted at %q", sessionMountPath)
		}
	})

	t.Run("command routes through gateway", func(t *testing.T) {
		joined := strings.Join(c.Args, " ")
		if !strings.Contains(joined, "$(ENVOY_AI_GATEWAY_URL)") {
			t.Errorf("args do not reference the gateway: %q", joined)
		}
	})
}

// TestADKPythonProvider_RenderIdempotency asserts the render is a pure function
// of its input: three successive BuildPodSpec calls with identical input produce
// deeply-equal specs. SSA correctness (≤3 reconciles, no churn) depends on this
// byte-stability — a non-deterministic render would re-patch on every reconcile.
func TestADKPythonProvider_RenderIdempotency(t *testing.T) {
	in := testInput()
	first := BuildPodSpec(in)
	for i := 0; i < 3; i++ {
		next := BuildPodSpec(in)
		if !reflect.DeepEqual(first, next) {
			t.Fatalf("render %d differs from first render — not idempotent", i+1)
		}
	}
}

// TestADKPythonProvider_PodInputFromCRs asserts the CR→input marshal reads the
// adkPython image + tenant correctly.
func TestADKPythonProvider_PodInputFromCRs(t *testing.T) {
	ws := &keesev1alpha1.Workspace{}
	ws.Name = "ws-demo"
	ws.Spec.TenantRef.Name = "acme"

	ar := &keesev1alpha1.AgentRuntime{}
	ar.Spec.Implementation.AdkPython = &keesev1alpha1.ADKPythonSpec{
		Image: "ghcr.io/keese/adk-python@sha256:deadbeef",
	}

	in := PodInputFromCRs(ws, ar, "ksa-x", "pvc-x")
	if in.Image != "ghcr.io/keese/adk-python@sha256:deadbeef" {
		t.Errorf("image: got %q", in.Image)
	}
	if in.TenantName != "acme" {
		t.Errorf("tenant: got %q, want acme", in.TenantName)
	}
	if in.WorkspaceName != "ws-demo" {
		t.Errorf("workspace: got %q, want ws-demo", in.WorkspaceName)
	}
	if in.ServiceAccountName != "ksa-x" || in.SessionPVCName != "pvc-x" {
		t.Errorf("sa/pvc: got %q/%q", in.ServiceAccountName, in.SessionPVCName)
	}
}
