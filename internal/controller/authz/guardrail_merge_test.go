// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package authz

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	authzv1alpha1 "github.com/keese-ai/keese/api/authz/v1alpha1"
)

// makeBinding is a test helper that constructs a minimal GuardrailBinding.
func makeBinding(name string, allow, deny []string, budget *authzv1alpha1.TokenBudget, rl *authzv1alpha1.RateLimit) *authzv1alpha1.GuardrailBinding {
	b := &authzv1alpha1.GuardrailBinding{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
	}
	tools := &authzv1alpha1.ToolsSpec{
		Allow:     allow,
		Deny:      deny,
		RateLimit: rl,
	}
	b.Spec.Tools = tools
	b.Spec.TokenBudget = budget
	return b
}

func ptr[T any](v T) *T { return &v }

func TestMergeBindings(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		bindings    []*authzv1alpha1.GuardrailBinding
		wantAllow   []string
		wantDeny    []string
		wantInput   int64
		wantTotal   int64
		wantReqRate int64
		wantErr     bool
	}{
		{
			name:      "empty chain returns empty policy",
			bindings:  nil,
			wantAllow: nil,
			wantDeny:  nil,
		},
		{
			name: "single binding passthrough",
			bindings: []*authzv1alpha1.GuardrailBinding{
				makeBinding("b0", []string{"tool_a", "tool_b"}, []string{"tool_bad"}, &authzv1alpha1.TokenBudget{Input: 1000, Total: 2000}, nil),
			},
			wantAllow: []string{"tool_a", "tool_b"},
			wantDeny:  []string{"tool_bad"},
			wantInput: 1000,
			wantTotal: 2000,
		},
		{
			name: "allow intersection — narrower binding",
			bindings: []*authzv1alpha1.GuardrailBinding{
				makeBinding("cluster", []string{"tool_a", "tool_b", "tool_c"}, nil, nil, nil),
				makeBinding("tenant", []string{"tool_a", "tool_b"}, nil, nil, nil),
			},
			wantAllow: []string{"tool_a", "tool_b"},
		},
		{
			name: "deny union across bindings",
			bindings: []*authzv1alpha1.GuardrailBinding{
				makeBinding("cluster", nil, []string{"tool_bad_1"}, nil, nil),
				makeBinding("workspace", nil, []string{"tool_bad_2"}, nil, nil),
			},
			wantDeny: []string{"tool_bad_1", "tool_bad_2"},
		},
		{
			name: "token budget min — all three fields",
			bindings: []*authzv1alpha1.GuardrailBinding{
				makeBinding("cluster", nil, nil, &authzv1alpha1.TokenBudget{Input: 5000, Output: 5000, Total: 10000}, nil),
				makeBinding("tenant", nil, nil, &authzv1alpha1.TokenBudget{Input: 3000, Output: 6000, Total: 8000}, nil),
			},
			wantInput: 3000,
			wantTotal: 8000,
		},
		{
			name: "token budget zero treated as no limit",
			bindings: []*authzv1alpha1.GuardrailBinding{
				makeBinding("cluster", nil, nil, &authzv1alpha1.TokenBudget{Input: 0, Total: 5000}, nil),
				makeBinding("workspace", nil, nil, &authzv1alpha1.TokenBudget{Input: 1000, Total: 0}, nil),
			},
			wantInput: 1000, // 0 = no limit, 1000 wins
			wantTotal: 5000, // 0 = no limit, 5000 wins
		},
		{
			name: "rate limit min requests",
			bindings: []*authzv1alpha1.GuardrailBinding{
				makeBinding("cluster", nil, nil, nil, &authzv1alpha1.RateLimit{Requests: 100, Window: "1m", Scope: "tenant"}),
				makeBinding("workspace", nil, nil, nil, &authzv1alpha1.RateLimit{Requests: 30, Window: "1m", Scope: "workspace"}),
			},
			wantReqRate: 30,
		},
		{
			name: "allow widening rejected",
			bindings: []*authzv1alpha1.GuardrailBinding{
				makeBinding("cluster", []string{"tool_a"}, nil, nil, nil),
				makeBinding("workspace", []string{"tool_a", "tool_b"}, nil, nil, nil), // tries to add tool_b
			},
			wantErr: true,
		},
		{
			name: "three-tier merge: cluster -> tenant -> workspace",
			bindings: []*authzv1alpha1.GuardrailBinding{
				makeBinding("cluster", []string{"tool_a", "tool_b", "tool_c"}, []string{"bad_1"}, &authzv1alpha1.TokenBudget{Input: 10000, Total: 20000}, nil),
				makeBinding("tenant", []string{"tool_a", "tool_b"}, []string{"bad_2"}, &authzv1alpha1.TokenBudget{Input: 7000, Total: 15000}, nil),
				makeBinding("workspace", []string{"tool_a"}, []string{"bad_3"}, &authzv1alpha1.TokenBudget{Input: 5000, Total: 10000}, nil),
			},
			wantAllow: []string{"tool_a"},
			wantDeny:  []string{"bad_1", "bad_2", "bad_3"},
			wantInput: 5000,
			wantTotal: 10000,
		},
	}

	for _, tc := range tests {
		tc := tc // capture range variable
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ep, err := MergeBindings(tc.bindings)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error but got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if ep == nil {
				t.Fatalf("expected non-nil EffectivePolicy")
			}

			// Check allow list.
			if len(tc.wantAllow) > 0 {
				if !stringSliceEqual(ep.Tools.Allow, tc.wantAllow) {
					t.Errorf("allow: got %v, want %v", ep.Tools.Allow, tc.wantAllow)
				}
			}

			// Check deny list.
			if len(tc.wantDeny) > 0 {
				if !stringSliceEqual(ep.Tools.Deny, tc.wantDeny) {
					t.Errorf("deny: got %v, want %v", ep.Tools.Deny, tc.wantDeny)
				}
			}

			// Check budget.
			if tc.wantInput != 0 && ep.TokenBudget.Input != tc.wantInput {
				t.Errorf("tokenBudget.input: got %d, want %d", ep.TokenBudget.Input, tc.wantInput)
			}
			if tc.wantTotal != 0 && ep.TokenBudget.Total != tc.wantTotal {
				t.Errorf("tokenBudget.total: got %d, want %d", ep.TokenBudget.Total, tc.wantTotal)
			}

			// Check rate limit.
			if tc.wantReqRate != 0 {
				if ep.Tools.RateLimit == nil {
					t.Fatalf("expected rateLimit but got nil")
				}
				if ep.Tools.RateLimit.Requests != tc.wantReqRate {
					t.Errorf("rateLimit.requests: got %d, want %d", ep.Tools.RateLimit.Requests, tc.wantReqRate)
				}
			}
		})
	}
}

// stringSliceEqual compares two sorted string slices for equality.
func stringSliceEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
