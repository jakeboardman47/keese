// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

// Unit tests for buildReferenceGrant and referenceGrantName.
// No build tag — runs in the default (unit) tier without envtest.

package keese

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gatewayv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"

	keesev1alpha1 "github.com/keese-ai/keese/api/keese/v1alpha1"
)

func makeTestShare(name, namespace, workspaceName, targetNS string) *keesev1alpha1.WorkspaceShare {
	return &keesev1alpha1.WorkspaceShare{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: keesev1alpha1.WorkspaceShareSpec{
			WorkspaceRef:    keesev1alpha1.LocalObjectReference{Name: workspaceName},
			TargetNamespace: targetNS,
			ReadOnly:        true,
			Grantees:        []string{"alice"},
		},
	}
}

func TestReferenceGrantName(t *testing.T) {
	share := makeTestShare("my-share", "source-ns", "my-workspace", "consumer-ns")
	got := referenceGrantName(share)
	want := "keese-share-my-share"
	if got != want {
		t.Errorf("referenceGrantName = %q, want %q", got, want)
	}
}

func TestBuildReferenceGrant_Namespace(t *testing.T) {
	// ReferenceGrant MUST live in the source (workspace/share) namespace,
	// NOT in the consumer (TargetNamespace), per Gateway API §5.1.
	share := makeTestShare("share-a", "source-ns", "ws-a", "consumer-ns")
	rg := buildReferenceGrant(share)

	if rg.Namespace != "source-ns" {
		t.Errorf("ReferenceGrant.Namespace = %q, want %q (source/workspace namespace)", rg.Namespace, "source-ns")
	}
}

func TestBuildReferenceGrant_FromNamespace(t *testing.T) {
	// From[0].Namespace must be TargetNamespace — the consumer side.
	share := makeTestShare("share-b", "source-ns", "ws-b", "consumer-ns")
	rg := buildReferenceGrant(share)

	if len(rg.Spec.From) == 0 {
		t.Fatal("ReferenceGrant.Spec.From is empty")
	}
	gotNS := string(rg.Spec.From[0].Namespace)
	if gotNS != "consumer-ns" {
		t.Errorf("From[0].Namespace = %q, want %q", gotNS, "consumer-ns")
	}
}

func TestBuildReferenceGrant_ToGroup(t *testing.T) {
	// To[0] must reference the keese.ai group/Workspace kind.
	share := makeTestShare("share-c", "source-ns", "ws-c", "consumer-ns")
	rg := buildReferenceGrant(share)

	if len(rg.Spec.To) == 0 {
		t.Fatal("ReferenceGrant.Spec.To is empty")
	}
	if string(rg.Spec.To[0].Group) != "keese.ai" {
		t.Errorf("To[0].Group = %q, want %q", rg.Spec.To[0].Group, "keese.ai")
	}
	if string(rg.Spec.To[0].Kind) != "Workspace" {
		t.Errorf("To[0].Kind = %q, want %q", rg.Spec.To[0].Kind, "Workspace")
	}
}

func TestBuildReferenceGrant_TypeMeta(t *testing.T) {
	// TypeMeta must be set so SSA client.Apply works correctly.
	share := makeTestShare("share-d", "source-ns", "ws-d", "consumer-ns")
	rg := buildReferenceGrant(share)

	if rg.APIVersion != "gateway.networking.k8s.io/v1beta1" {
		t.Errorf("APIVersion = %q, want %q", rg.APIVersion, "gateway.networking.k8s.io/v1beta1")
	}
	if rg.Kind != "ReferenceGrant" {
		t.Errorf("Kind = %q, want %q", rg.Kind, "ReferenceGrant")
	}
}

func TestBuildReferenceGrant_Labels(t *testing.T) {
	share := makeTestShare("share-e", "source-ns", "ws-e", "consumer-ns")
	rg := buildReferenceGrant(share)

	if rg.Labels["keese.ai/managed-by"] != shareFieldOwner {
		t.Errorf("label keese.ai/managed-by = %q, want %q", rg.Labels["keese.ai/managed-by"], shareFieldOwner)
	}
	if rg.Labels["keese.ai/workspace-share"] != "share-e" {
		t.Errorf("label keese.ai/workspace-share = %q, want %q", rg.Labels["keese.ai/workspace-share"], "share-e")
	}
}

func TestBuildReferenceGrant_DifferentTargetNamespaces(t *testing.T) {
	cases := []struct {
		name      string
		shareNS   string
		targetNS  string
		wantRGNS  string
		wantFrom  gatewayv1beta1.Namespace
	}{
		{"same-ns", "ns-a", "ns-a", "ns-a", "ns-a"},
		{"cross-ns", "ns-b", "ns-c", "ns-b", "ns-c"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			share := makeTestShare("s", tc.shareNS, "ws", tc.targetNS)
			rg := buildReferenceGrant(share)
			if rg.Namespace != tc.wantRGNS {
				t.Errorf("ReferenceGrant.Namespace = %q, want %q", rg.Namespace, tc.wantRGNS)
			}
			if rg.Spec.From[0].Namespace != tc.wantFrom {
				t.Errorf("From[0].Namespace = %q, want %q", rg.Spec.From[0].Namespace, tc.wantFrom)
			}
		})
	}
}
