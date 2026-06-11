// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

//go:build integration

package keese

import (
	"reflect"
	"regexp"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	keesev1alpha1 "github.com/keese-ai/keese/api/keese/v1alpha1"
	"github.com/keese-ai/keese/internal/runtime/providers/adkpython"
)

// secretEnvPattern flags any credential-shaped env name/value (rule 05.2).
var secretEnvPattern = regexp.MustCompile(`(?i)(API_KEY|SECRET|BEARER|PASSWORD)`)

func adkDiscriminatorWorkspace() *keesev1alpha1.Workspace {
	ws := makeWorkspace("default", "ws-adk")
	ws.UID = "uid-adk-0001"
	ws.Spec.Interactive = true
	ws.Spec.TenantRef.Name = "acme"
	return ws
}

func adkSession() *keesev1alpha1.WorkspaceSession {
	return &keesev1alpha1.WorkspaceSession{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "sess-adk",
			Namespace: "default",
			UID:       "sess-uid-0001",
		},
		Spec: keesev1alpha1.WorkspaceSessionSpec{
			Mode:         keesev1alpha1.SessionModePerUser,
			WorkspaceRef: "ws-adk",
		},
	}
}

func adkRuntime() *keesev1alpha1.AgentRuntime {
	ar := &keesev1alpha1.AgentRuntime{}
	ar.Name = "adk-runtime"
	ar.Spec.Implementation.AdkPython = &keesev1alpha1.ADKPythonSpec{
		Image: "ghcr.io/keese/adk-python@sha256:cafe",
	}
	return ar
}

func gooseRuntime() *keesev1alpha1.AgentRuntime {
	ar := &keesev1alpha1.AgentRuntime{}
	ar.Name = "goose-runtime"
	ar.Spec.Implementation.Goose = &keesev1alpha1.GooseSpec{
		Image: "ghcr.io/keese/goose:dev",
	}
	return ar
}

// TestBuildSessionPod_ADKDiscriminator asserts the T5 discriminator routes an
// adkPython AgentRuntime to the ADK Python pod template, with every rule-05
// invariant satisfied on the rendered pod.
//
// E1b T3: the ADK pod now has TWO containers — the adk-python runtime and the
// a2a-bridge sidecar. The ADK container is selected by name (order-independent).
func TestBuildSessionPod_ADKDiscriminator(t *testing.T) {
	ws := adkDiscriminatorWorkspace()
	pod := buildSessionPodObject(adkSession(), ws, adkRuntime(), "ws-adk-pod")

	if len(pod.Spec.Containers) != 2 {
		t.Fatalf("container count: got %d, want 2 (adk-python + a2a-bridge sidecar)", len(pod.Spec.Containers))
	}
	var c corev1.Container
	var sawADK, sawBridge bool
	for _, ctr := range pod.Spec.Containers {
		switch ctr.Name {
		case adkpython.ContainerName:
			c, sawADK = ctr, true
		case adkpython.BridgeContainerName:
			sawBridge = true
		}
	}
	if !sawADK {
		t.Fatalf("ADK container %q not found, got %+v", adkpython.ContainerName,
			containerNames(pod.Spec.Containers))
	}
	if !sawBridge {
		t.Errorf("a2a-bridge sidecar %q not found (E1b T3), got %+v",
			adkpython.BridgeContainerName, containerNames(pod.Spec.Containers))
	}
	if c.Image != "ghcr.io/keese/adk-python@sha256:cafe" {
		t.Errorf("image: got %q, want the adkPython image", c.Image)
	}
	// ADK path must NOT carry the goose init container.
	if len(pod.Spec.InitContainers) != 0 {
		t.Errorf("ADK pod must have no init containers, got %d", len(pod.Spec.InitContainers))
	}

	t.Run("zero credential env — every container incl. sidecar (rule 05.2)", func(t *testing.T) {
		for _, ctr := range pod.Spec.Containers {
			for _, e := range ctr.Env {
				if secretEnvPattern.MatchString(e.Name) || secretEnvPattern.MatchString(e.Value) {
					t.Errorf("container %q env %q=%q matches credential pattern", ctr.Name, e.Name, e.Value)
				}
				if e.ValueFrom != nil && e.ValueFrom.SecretKeyRef != nil {
					t.Errorf("container %q env %q uses secretKeyRef — forbidden (rule 05.7)", ctr.Name, e.Name)
				}
			}
		}
	})

	t.Run("egress token + RO root + CA mount", func(t *testing.T) {
		var sawToken, sawCA bool
		for _, v := range pod.Spec.Volumes {
			if v.Projected != nil {
				for _, s := range v.Projected.Sources {
					if s.ServiceAccountToken != nil &&
						s.ServiceAccountToken.Audience == "keese-egress-acme" {
						sawToken = true
					}
				}
			}
		}
		for _, m := range c.VolumeMounts {
			if m.MountPath == "/var/run/keese/ca" && m.ReadOnly {
				sawCA = true
			}
		}
		if !sawToken {
			t.Error("egress SA token (audience keese-egress-acme) not projected")
		}
		if !sawCA {
			t.Error("CA bundle not mounted read-only at /var/run/keese/ca")
		}
		sc := c.SecurityContext
		if sc == nil || sc.ReadOnlyRootFilesystem == nil || !*sc.ReadOnlyRootFilesystem {
			t.Error("ADK container must set readOnlyRootFilesystem=true (rule 05.11)")
		}
	})

	t.Run("shared ObjectMeta (labels + owner ref) intact", func(t *testing.T) {
		if pod.Labels["keese.ai/tenant"] != "acme" {
			t.Errorf("tenant label: got %q", pod.Labels["keese.ai/tenant"])
		}
		if len(pod.OwnerReferences) != 1 || pod.OwnerReferences[0].Kind != "WorkspaceSession" {
			t.Errorf("owner ref not preserved: %+v", pod.OwnerReferences)
		}
	})
}

// TestBuildSessionPod_GooseUnaffected asserts the goose path is byte-identical
// before/after the discriminator refactor: the agent container keeps the goose
// image + name and the goose init container is present.
func TestBuildSessionPod_GooseUnaffected(t *testing.T) {
	ws := adkDiscriminatorWorkspace()
	pod := buildSessionPodObject(adkSession(), ws, gooseRuntime(), "ws-goose-pod")

	if len(pod.Spec.Containers) != 1 || pod.Spec.Containers[0].Name != "agent" {
		t.Fatalf("goose path must render a single 'agent' container, got %+v",
			containerNames(pod.Spec.Containers))
	}
	if pod.Spec.Containers[0].Image != "ghcr.io/keese/goose:dev" {
		t.Errorf("goose image: got %q", pod.Spec.Containers[0].Image)
	}
	// Goose path retains its keese-resume init container.
	if len(pod.Spec.InitContainers) != 1 || pod.Spec.InitContainers[0].Name != "keese-resume" {
		t.Errorf("goose path must keep keese-resume init container, got %+v",
			containerNames(pod.Spec.InitContainers))
	}
}

// TestBuildSessionPod_ADKIdempotency asserts the controller-side ADK render is
// deterministic across 3 successive builds — required for ≤3-reconcile SSA
// convergence with no churn (rule 06.6).
func TestBuildSessionPod_ADKIdempotency(t *testing.T) {
	ws := adkDiscriminatorWorkspace()
	first := buildSessionPodObject(adkSession(), ws, adkRuntime(), "ws-adk-pod")
	for i := 0; i < 3; i++ {
		next := buildSessionPodObject(adkSession(), ws, adkRuntime(), "ws-adk-pod")
		if !reflect.DeepEqual(first.Spec, next.Spec) {
			t.Fatalf("ADK pod render %d differs from first — not idempotent", i+1)
		}
	}
}

func containerNames(cs []corev1.Container) []string {
	out := make([]string, 0, len(cs))
	for _, c := range cs {
		out = append(out, c.Name)
	}
	return out
}
