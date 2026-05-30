// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package extauth_test

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	authzv1alpha1 "github.com/keese-ai/keese/api/authz/v1alpha1"
	"github.com/keese-ai/keese/internal/authz/extauth"
)

func clusterTB(name, path, tool string) authzv1alpha1.ToolBinding {
	return authzv1alpha1.ToolBinding{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: authzv1alpha1.ToolBindingSpec{
			Match: authzv1alpha1.HTTPRouteMatch{
				Paths: []authzv1alpha1.HTTPPathMatch{{Type: authzv1alpha1.PathMatchExact, Value: path}},
			},
			ToolName: tool,
		},
	}
}

func nsWT(ns, name, path, tool, wsRef string) authzv1alpha1.WorkspaceTool {
	wt := authzv1alpha1.WorkspaceTool{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
		Spec: authzv1alpha1.WorkspaceToolSpec{
			Match: authzv1alpha1.HTTPRouteMatch{
				Paths: []authzv1alpha1.HTTPPathMatch{{Type: authzv1alpha1.PathMatchExact, Value: path}},
			},
			ToolName: tool,
		},
	}
	if wsRef != "" {
		wt.Spec.WorkspaceRef = &authzv1alpha1.NamespaceLocalRef{Name: wsRef}
	}
	return wt
}

func TestResolver_ClusterFirstWins(t *testing.T) {
	t.Parallel()
	r := extauth.NewResolver()
	r.ApplySnapshot(
		[]authzv1alpha1.ToolBinding{clusterTB("anthropic", "/v1/messages", "anthropic.messages")},
		[]authzv1alpha1.WorkspaceTool{nsWT("alpha", "shadow", "/v1/messages", "shadow", "")},
	)
	res := r.Resolve(&extauth.ResolveRequest{
		HTTP:      extauth.HTTPRequest{Path: "/v1/messages"},
		Workspace: extauth.WorkspaceID{Namespace: "alpha"},
	})
	if !res.Matched || res.FinalToolName != "anthropic.messages" {
		t.Fatalf("got %+v want cluster ToolBinding match", res)
	}
}

func TestResolver_NamespaceMatch_NamespacePrefix(t *testing.T) {
	t.Parallel()
	r := extauth.NewResolver()
	r.ApplySnapshot(nil,
		[]authzv1alpha1.WorkspaceTool{nsWT("alpha", "search", "/search", "internal-search", "")},
	)
	res := r.Resolve(&extauth.ResolveRequest{
		HTTP:      extauth.HTTPRequest{Path: "/search"},
		Workspace: extauth.WorkspaceID{Namespace: "alpha", Name: "my-ws"},
	})
	if !res.Matched {
		t.Fatalf("expected match")
	}
	if res.FinalToolName != "alpha.internal-search" {
		t.Fatalf("toolName: got %q want alpha.internal-search", res.FinalToolName)
	}
}

func TestResolver_NamespaceScopeRespected(t *testing.T) {
	t.Parallel()
	// WorkspaceTool in `alpha` is invisible to a request from `beta`.
	r := extauth.NewResolver()
	r.ApplySnapshot(nil,
		[]authzv1alpha1.WorkspaceTool{nsWT("alpha", "search", "/search", "internal-search", "")},
	)
	res := r.Resolve(&extauth.ResolveRequest{
		HTTP:      extauth.HTTPRequest{Path: "/search"},
		Workspace: extauth.WorkspaceID{Namespace: "beta"},
	})
	if res.Matched {
		t.Fatalf("expected no-match across namespaces; got %+v", res)
	}
}

func TestResolver_WorkspaceRefRestricted(t *testing.T) {
	t.Parallel()
	r := extauth.NewResolver()
	// WorkspaceTool pinned to `my-ws`. Other workspaces in the same
	// namespace should NOT match.
	r.ApplySnapshot(nil,
		[]authzv1alpha1.WorkspaceTool{nsWT("alpha", "private", "/private", "private-tool", "my-ws")},
	)
	res := r.Resolve(&extauth.ResolveRequest{
		HTTP:      extauth.HTTPRequest{Path: "/private"},
		Workspace: extauth.WorkspaceID{Namespace: "alpha", Name: "other-ws"},
	})
	if res.Matched {
		t.Fatalf("expected workspaceRef restriction; got %+v", res)
	}
	res = r.Resolve(&extauth.ResolveRequest{
		HTTP:      extauth.HTTPRequest{Path: "/private"},
		Workspace: extauth.WorkspaceID{Namespace: "alpha", Name: "my-ws"},
	})
	if !res.Matched || res.FinalToolName != "alpha.private-tool" {
		t.Fatalf("expected match for pinned workspace; got %+v", res)
	}
}

func TestResolver_NoMatch(t *testing.T) {
	t.Parallel()
	r := extauth.NewResolver()
	r.ApplySnapshot(
		[]authzv1alpha1.ToolBinding{clusterTB("a", "/a", "a")},
		nil,
	)
	res := r.Resolve(&extauth.ResolveRequest{
		HTTP: extauth.HTTPRequest{Path: "/zzz"},
	})
	if res.Matched {
		t.Fatalf("expected no match; got %+v", res)
	}
}

func TestResolver_RejectedReportsBadJSONPath(t *testing.T) {
	t.Parallel()
	r := extauth.NewResolver()
	bad := authzv1alpha1.ToolBinding{
		ObjectMeta: metav1.ObjectMeta{Name: "broken"},
		Spec: authzv1alpha1.ToolBindingSpec{
			Match: authzv1alpha1.HTTPRouteMatch{
				Paths: []authzv1alpha1.HTTPPathMatch{{Type: authzv1alpha1.PathMatchExact, Value: "/x"}},
			},
			ToolName: "broken",
			BodyDiscriminator: &authzv1alpha1.BodyDiscriminator{
				JSONPath: "$..wildcard",
				Map:      map[string]string{"a": "b"},
			},
		},
	}
	rejected := r.ApplySnapshot([]authzv1alpha1.ToolBinding{bad}, nil)
	if len(rejected) != 1 {
		t.Fatalf("expected 1 rejected; got %v", rejected)
	}
}
