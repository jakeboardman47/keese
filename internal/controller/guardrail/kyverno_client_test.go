// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package guardrail

import (
	"strings"
	"testing"

	kyvernov1 "github.com/kyverno/kyverno/api/kyverno/v1"
)

// TestClusterPolicyName validates the naming helper edge cases.
func TestClusterPolicyName(t *testing.T) {
	tests := []struct {
		name         string
		ns, bn, ref  string
		wantPrefix   string
		wantContains string
		wantMaxLen   int
	}{
		{
			name: "simple",
			ns:   "keese-system", bn: "default-binding", ref: "rate-limit",
			wantPrefix: "keese-", wantContains: "keese-system", wantMaxLen: clusterPolicyNameMaxLen,
		},
		{
			name: "uppercase_and_slash",
			ns:   "my-ns", bn: "My_Binding", ref: "Policy/Foo",
			wantContains: "my-ns", wantMaxLen: clusterPolicyNameMaxLen,
		},
		{
			name: "truncated_long_ref",
			ns:   "ns", bn: "binding", ref: strings.Repeat("x", 300),
			wantPrefix: "keese-", wantMaxLen: clusterPolicyNameMaxLen,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got := clusterPolicyName(tc.ns, tc.bn, tc.ref)
			if len(got) > tc.wantMaxLen {
				t.Errorf("name length %d exceeds DNS subdomain limit %d", len(got), tc.wantMaxLen)
			}
			if tc.wantPrefix != "" && !strings.HasPrefix(got, tc.wantPrefix) {
				t.Errorf("name %q must start with %q", got, tc.wantPrefix)
			}
			if tc.wantContains != "" && !strings.Contains(got, tc.wantContains) {
				t.Errorf("name %q must contain %q", got, tc.wantContains)
			}
		})
	}
}

// TestSanitizeLabel validates the label-value sanitiser.
func TestSanitizeLabel(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"rate-limit", "rate-limit"},
		{"Policy/Foo", "policy-foo"},
		{"MY_POLICY", "my-policy"},
		{"-leading", "leading"},
		{"trailing-", "trailing"},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.input, func(t *testing.T) {
			if got := sanitizeLabel(tc.input); got != tc.want {
				t.Errorf("sanitizeLabel(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

// TestBuildClusterPolicy_Shape validates the constructed ClusterPolicy has the
// correct name, annotations, labels, and empty Spec — without requiring an API server.
func TestBuildClusterPolicy_Shape(t *testing.T) {
	const ns, name, policyRef = "tenant-a", "my-binding", "rate-limit"
	cp := buildClusterPolicy(ns, name, policyRef)

	wantName := clusterPolicyName(ns, name, policyRef)
	if cp.Name != wantName {
		t.Errorf("ClusterPolicy.Name = %q, want %q", cp.Name, wantName)
	}
	if cp.Kind != "ClusterPolicy" {
		t.Errorf("ClusterPolicy.Kind = %q, want %q", cp.Kind, "ClusterPolicy")
	}
	if cp.APIVersion != "kyverno.io/v1" {
		t.Errorf("ClusterPolicy.APIVersion = %q, want %q", cp.APIVersion, "kyverno.io/v1")
	}
	if got := cp.Annotations["keese.ai/owner"]; got != "tenant-a/my-binding" {
		t.Errorf("annotation keese.ai/owner = %q, want %q", got, "tenant-a/my-binding")
	}
	if got := cp.Annotations["keese.ai/policy-ref"]; got != policyRef {
		t.Errorf("annotation keese.ai/policy-ref = %q, want %q", got, policyRef)
	}
	if got := cp.Labels["keese.ai/managed"]; got != "true" {
		t.Errorf("label keese.ai/managed = %q, want %q", got, "true")
	}
	if got := cp.Labels["keese.ai/group"]; got != "guardrail.operator.keese.ai" {
		t.Errorf("label keese.ai/group = %q, want %q", got, "guardrail.operator.keese.ai")
	}
	if len(cp.Spec.Rules) != 0 {
		t.Errorf("Spec.Rules must be empty, got %d rules", len(cp.Spec.Rules))
	}
}

// TestBuildClusterPolicy_PolicyRefLabel validates that a policyRef with special chars
// is correctly sanitised for the keese.ai/policy-ref label.
func TestBuildClusterPolicy_PolicyRefLabel(t *testing.T) {
	cp := buildClusterPolicy("ns", "b", "My/Policy_Name")
	want := "my-policy-name"
	if got := cp.Labels["keese.ai/policy-ref"]; got != want {
		t.Errorf("sanitised label = %q, want %q", got, want)
	}
}

// TestClientKyvernoPolicyProjector_InterfaceCompliance verifies compile-time interface satisfaction.
func TestClientKyvernoPolicyProjector_InterfaceCompliance(t *testing.T) {
	var _ KyvernoPolicyProjector = &ClientKyvernoPolicyProjector{}
	var _ KyvernoPolicyProjector = &FakeKyvernoProjector{}
	_ = kyvernov1.ClusterPolicy{} // ensure the package is used
}
