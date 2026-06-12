// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

// NOTE: BuildPodSpec / BuildNetworkPolicy are pure functions of their inputs —
// they need no API server. Per rule 06-testing they belong in the default unit
// tier (no build tag) so they run under `go test -short` and keep the adkgo
// coverage floor (test/coverage-targets.yaml: 100.0) honest. The E1 adkPython
// sibling tagged its equivalent tests `integration`, which dropped its own
// `-short` coverage below floor; E3 deliberately does not repeat that. These
// still run under `go test -tags=integration` (untagged tests always compile).

package adkgo

import (
	"reflect"
	"regexp"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"

	keesev1alpha1 "github.com/keese-ai/keese/api/keese/v1alpha1"
)

// secretEnvPattern matches any env var name or value that looks like a
// credential. Rule 05.2: NONE may appear on an ADK Go pod.
var secretEnvPattern = regexp.MustCompile(`(?i)(API_KEY|SECRET|TOKEN_VALUE|BEARER|PASSWORD|_KEY$|ANTHROPIC_API|OPENAI_API)`)

func testInput() PodInput {
	return PodInput{
		Image:              "ghcr.io/keese/adk-go@sha256:deadbeef",
		WorkspaceName:      "ws-demo",
		TenantName:         "acme",
		ServiceAccountName: "ksa-uid-1234",
		SessionPVCName:     "keese-ws-uid-1234-session",
	}
}

// TestADKGoProvider_PodRender asserts the security-critical invariants of the
// rendered pod: zero credential-shaped env vars (rule 05.2), the egress SA token
// projected with the correct audience + TTL (rule 05.7), read-only root
// filesystem + drop-ALL + non-root (rule 05.11), and the CA bundle mounted.
// Mirrors TestADKPythonProvider_PodRender — the ADK Go pod must be byte-for-byte
// the same security posture as the Python sibling, only the command differs.
func TestADKGoProvider_PodRender(t *testing.T) {
	spec := BuildPodSpec(testInput())

	// The pod has TWO containers — the ADK Go runtime plus the a2a-bridge
	// sidecar (reused from E1). Both must be hardened with zero credential env.
	if len(spec.Containers) != 2 {
		t.Fatalf("container count: got %d, want 2 (adk-go + a2a-bridge sidecar)", len(spec.Containers))
	}
	c := containerByName(t, spec, ContainerName)

	t.Run("zero credential env — every container (rule 05.2)", func(t *testing.T) {
		for _, ctr := range spec.Containers {
			for _, e := range ctr.Env {
				if secretEnvPattern.MatchString(e.Name) {
					t.Errorf("container %q env name %q matches credential pattern (rule 05.2)", ctr.Name, e.Name)
				}
				if secretEnvPattern.MatchString(e.Value) {
					t.Errorf("container %q env %q value %q matches credential pattern (rule 05.2)", ctr.Name, e.Name, e.Value)
				}
				// No env may carry secret material via secretKeyRef (rule 05.7).
				if e.ValueFrom != nil && e.ValueFrom.SecretKeyRef != nil {
					t.Errorf("container %q env %q uses secretKeyRef — forbidden (rule 05.7)", ctr.Name, e.Name)
				}
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

	t.Run("hardened security context — every container (rule 05.11)", func(t *testing.T) {
		for _, ctr := range spec.Containers {
			sc := ctr.SecurityContext
			if sc == nil {
				t.Fatalf("container %q SecurityContext is nil (rule 05.11)", ctr.Name)
			}
			if sc.ReadOnlyRootFilesystem == nil || !*sc.ReadOnlyRootFilesystem {
				t.Errorf("container %q: readOnlyRootFilesystem must be true (rule 05.11)", ctr.Name)
			}
			if sc.RunAsNonRoot == nil || !*sc.RunAsNonRoot {
				t.Errorf("container %q: runAsNonRoot must be true (rule 05.11)", ctr.Name)
			}
			if sc.AllowPrivilegeEscalation == nil || *sc.AllowPrivilegeEscalation {
				t.Errorf("container %q: allowPrivilegeEscalation must be false (rule 05.11)", ctr.Name)
			}
			if sc.Capabilities == nil || len(sc.Capabilities.Drop) != 1 || sc.Capabilities.Drop[0] != "ALL" {
				t.Errorf("container %q: capabilities.drop must be [ALL], got %+v", ctr.Name, sc.Capabilities)
			}
		}
	})

	t.Run("pod-level runAsNonRoot + distroless-static nonroot uid (rule 05.11)", func(t *testing.T) {
		psc := spec.SecurityContext
		if psc == nil {
			t.Fatal("pod SecurityContext is nil (rule 05.11)")
		}
		if psc.RunAsNonRoot == nil || !*psc.RunAsNonRoot {
			t.Error("pod runAsNonRoot must be true")
		}
		if psc.RunAsUser == nil || *psc.RunAsUser != 65532 {
			t.Errorf("pod runAsUser must be 65532 (distroless/static nonroot), got %v", psc.RunAsUser)
		}
	})

	t.Run("adk-go single static binary command (no interpreter)", func(t *testing.T) {
		if len(c.Command) != 1 || c.Command[0] != "/app/adk-go" {
			t.Errorf("command must be [/app/adk-go], got %+v", c.Command)
		}
	})

	t.Run("a2a-bridge sidecar present, hardened, no keys (reused from E1)", func(t *testing.T) {
		b := containerByName(t, spec, BridgeContainerName)

		// The bridge fronts peer ingress on A2ABridgePort.
		var sawPort bool
		for _, p := range b.Ports {
			if p.ContainerPort == A2ABridgePort {
				sawPort = true
			}
		}
		if !sawPort {
			t.Errorf("bridge must expose port %d, got %+v", A2ABridgePort, b.Ports)
		}

		// Rule 05.2: the bridge carries NO env at all (no keys, no secrets).
		if len(b.Env) != 0 {
			t.Errorf("bridge must have zero env vars (rule 05.2), got %+v", b.Env)
		}

		// The bridge mounts the projected MCP ConfigMap read-only and nothing
		// else (no PVC, no SA token, no CA bundle).
		if len(b.VolumeMounts) != 1 || b.VolumeMounts[0].MountPath != mcpConfigMountPath || !b.VolumeMounts[0].ReadOnly {
			t.Errorf("bridge must mount only %q read-only, got %+v", mcpConfigMountPath, b.VolumeMounts)
		}

		// The ADK container's localhost A2A port must NOT be the peer-facing one.
		if A2APort == A2ABridgePort {
			t.Error("ADK localhost port and bridge peer port must differ")
		}
	})

	t.Run("mcp-config volume optional (E6 populates)", func(t *testing.T) {
		var found bool
		for _, v := range spec.Volumes {
			if v.Name != "mcp-config" {
				continue
			}
			found = true
			if v.ConfigMap == nil || v.ConfigMap.Optional == nil || !*v.ConfigMap.Optional {
				t.Errorf("mcp-config volume must be an optional ConfigMap, got %+v", v.VolumeSource)
			}
		}
		if !found {
			t.Error("mcp-config volume not present")
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

// TestADKGoProvider_RenderIdempotency asserts the render is a pure function of
// its input: three successive BuildPodSpec calls with identical input produce
// deeply-equal specs. SSA correctness (≤3 reconciles, no churn) depends on this
// byte-stability — a non-deterministic render would re-patch on every reconcile.
func TestADKGoProvider_RenderIdempotency(t *testing.T) {
	in := testInput()
	first := BuildPodSpec(in)
	for i := 0; i < 3; i++ {
		next := BuildPodSpec(in)
		if !reflect.DeepEqual(first, next) {
			t.Fatalf("render %d differs from first render — not idempotent", i+1)
		}
	}
}

// TestADKGoProvider_PodInputFromCRs asserts the CR→input marshal reads the adkGo
// image + tenant correctly.
func TestADKGoProvider_PodInputFromCRs(t *testing.T) {
	ws := &keesev1alpha1.Workspace{}
	ws.Name = "ws-demo"
	ws.Spec.TenantRef.Name = "acme"

	ar := &keesev1alpha1.AgentRuntime{}
	ar.Spec.Implementation.AdkGo = &keesev1alpha1.ADKGoSpec{
		Image: "ghcr.io/keese/adk-go@sha256:deadbeef",
	}

	in := PodInputFromCRs(ws, ar, "ksa-x", "pvc-x")
	if in.Image != "ghcr.io/keese/adk-go@sha256:deadbeef" {
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

// TestADKGoProvider_BridgeImageFallback asserts the a2a-bridge sidecar uses the
// dev fallback image when BridgeImage is empty, and the injected (digest-pinned,
// prod) image when set (rule 05.12).
func TestADKGoProvider_BridgeImageFallback(t *testing.T) {
	t.Run("empty falls back to dev tag", func(t *testing.T) {
		spec := BuildPodSpec(testInput())
		b := containerByName(t, spec, BridgeContainerName)
		if b.Image != defaultBridgeImage {
			t.Errorf("bridge image: got %q, want fallback %q", b.Image, defaultBridgeImage)
		}
	})

	t.Run("injected digest-pinned image used", func(t *testing.T) {
		in := testInput()
		in.BridgeImage = "ghcr.io/keese-ai/a2a-bridge@sha256:feedface"
		spec := BuildPodSpec(in)
		b := containerByName(t, spec, BridgeContainerName)
		if b.Image != "ghcr.io/keese-ai/a2a-bridge@sha256:feedface" {
			t.Errorf("bridge image: got %q, want injected digest", b.Image)
		}
	})
}

// containerByName returns the container with the given name, failing the test if
// absent.
func containerByName(t *testing.T, spec corev1.PodSpec, name string) corev1.Container {
	t.Helper()
	for _, c := range spec.Containers {
		if c.Name == name {
			return c
		}
	}
	t.Fatalf("container %q not found in pod spec", name)
	return corev1.Container{}
}
