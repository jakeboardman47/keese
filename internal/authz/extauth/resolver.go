// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package extauth

import (
	"fmt"
	"sync/atomic"

	authzv1alpha1 "github.com/keese-ai/keese/api/authz/v1alpha1"
)

// Resolver compiles the cluster + namespaced ToolBinding /
// WorkspaceTool catalogue into an in-memory routing structure.
// Held in atomic.Value so the gRPC server reads lock-free; the
// controller-runtime watcher calls ApplySnapshot to swap.
type Resolver struct {
	snap atomic.Value // *snapshot
}

// NewResolver constructs a Resolver with an empty snapshot. Empty
// snapshot resolves every request to no-match (= deny).
func NewResolver() *Resolver {
	r := &Resolver{}
	r.snap.Store(&snapshot{})
	return r
}

// snapshot is the immutable result of compiling a CRD batch.
type snapshot struct {
	cluster   []*compiledBinding // cluster-scoped (ToolBinding)
	namespace map[string][]*compiledBinding // key = namespace; namespaced WorkspaceTool
}

// compiledBinding pairs the source CR's identity + tool name with
// its compiled match.
type compiledBinding struct {
	bindingName  string         // source CR `metadata.name`
	bindingNS    string         // empty for cluster ToolBinding
	workspaceRef string         // empty = match any in namespace; only set on WorkspaceTool
	toolName     string         // the platform tool name
	subjectFrom  authzv1alpha1.SubjectFromSource
	jwtClaim     string
	workspaceFrom authzv1alpha1.WorkspaceFromSource
	match        *CompiledMatch
}

// ApplySnapshot recompiles every binding into a fresh trie and
// atomically swaps it in. Bindings that fail to compile are dropped
// with their names returned in the second slice; the controller
// translates those into status.conditions[Ready=False].
func (r *Resolver) ApplySnapshot(toolBindings []authzv1alpha1.ToolBinding, workspaceTools []authzv1alpha1.WorkspaceTool) (rejected []string) {
	s := &snapshot{namespace: map[string][]*compiledBinding{}}

	for i := range toolBindings {
		tb := &toolBindings[i]
		cm, err := CompileMatch(tb.Spec.Match, tb.Spec.BodyDiscriminator)
		if err != nil {
			rejected = append(rejected, fmt.Sprintf("ToolBinding/%s: %v", tb.Name, err))
			continue
		}
		s.cluster = append(s.cluster, &compiledBinding{
			bindingName:   tb.Name,
			toolName:      tb.Spec.ToolName,
			subjectFrom:   tb.Spec.SubjectFrom,
			jwtClaim:      tb.Spec.JWTClaimName,
			workspaceFrom: tb.Spec.WorkspaceFrom,
			match:         cm,
		})
	}

	for i := range workspaceTools {
		wt := &workspaceTools[i]
		cm, err := CompileMatch(wt.Spec.Match, wt.Spec.BodyDiscriminator)
		if err != nil {
			rejected = append(rejected, fmt.Sprintf("WorkspaceTool/%s/%s: %v", wt.Namespace, wt.Name, err))
			continue
		}
		var wsRef string
		if wt.Spec.WorkspaceRef != nil {
			wsRef = wt.Spec.WorkspaceRef.Name
		}
		entry := &compiledBinding{
			bindingName:   wt.Name,
			bindingNS:     wt.Namespace,
			workspaceRef:  wsRef,
			toolName:      wt.Spec.ToolName,
			subjectFrom:   wt.Spec.SubjectFrom,
			jwtClaim:      wt.Spec.JWTClaimName,
			workspaceFrom: wt.Spec.WorkspaceFrom,
			match:         cm,
		}
		s.namespace[wt.Namespace] = append(s.namespace[wt.Namespace], entry)
	}

	r.snap.Store(s)
	return rejected
}

// ResolveRequest is what the gRPC server passes to Resolve.
type ResolveRequest struct {
	HTTP      HTTPRequest
	Workspace WorkspaceID // pre-extracted by subject.go
}

// WorkspaceID is the pair (namespace, workspace name) used to scope
// namespaced WorkspaceTool matches.
type WorkspaceID struct {
	Namespace string
	Name      string
	UID       string // optional — used when constructing the FGA `workspace:` user string
}

// ResolveResult is what Resolve returns to the gRPC server.
type ResolveResult struct {
	Matched         bool
	BindingName     string // for audit logging
	BindingNS       string // empty for cluster ToolBindings
	FinalToolName   string // includes optional `.subTool` and (for WorkspaceTool) `<namespace>.` prefix
	SubjectFrom     authzv1alpha1.SubjectFromSource
	JWTClaim        string
	WorkspaceFrom   authzv1alpha1.WorkspaceFromSource
}

// Resolve walks cluster bindings first (first match wins), then
// namespace bindings scoped to the request's workspace's namespace.
// No match → ResolveResult{Matched: false} (caller denies).
func (r *Resolver) Resolve(req *ResolveRequest) *ResolveResult {
	s, _ := r.snap.Load().(*snapshot)
	if s == nil {
		return &ResolveResult{}
	}

	// 1. Cluster ToolBindings.
	for _, cb := range s.cluster {
		if mr := cb.match.Match(&req.HTTP); mr.Matched {
			return &ResolveResult{
				Matched:       true,
				BindingName:   cb.bindingName,
				FinalToolName: composeTool(cb.toolName, mr.SubTool, ""),
				SubjectFrom:   cb.subjectFrom,
				JWTClaim:      cb.jwtClaim,
				WorkspaceFrom: cb.workspaceFrom,
			}
		}
	}

	// 2. Namespace WorkspaceTools — scoped to the workspace's namespace.
	if req.Workspace.Namespace != "" {
		for _, cb := range s.namespace[req.Workspace.Namespace] {
			// Skip when the binding pins a workspaceRef and it doesn't match.
			if cb.workspaceRef != "" && cb.workspaceRef != req.Workspace.Name {
				continue
			}
			if mr := cb.match.Match(&req.HTTP); mr.Matched {
				return &ResolveResult{
					Matched:       true,
					BindingName:   cb.bindingName,
					BindingNS:     cb.bindingNS,
					FinalToolName: composeTool(cb.toolName, mr.SubTool, req.Workspace.Namespace),
					SubjectFrom:   cb.subjectFrom,
					JWTClaim:      cb.jwtClaim,
					WorkspaceFrom: cb.workspaceFrom,
				}
			}
		}
	}

	return &ResolveResult{}
}

// composeTool builds the OpenFGA-form tool name. WorkspaceTool
// resolves to `<namespace>.<toolName>` (per design 22 §"Two
// ToolBinding kinds — cluster + namespaced"). Empty subTool
// shortcircuits to the bare toolName.
func composeTool(toolName, subTool, namespace string) string {
	base := toolName
	if namespace != "" {
		base = namespace + "." + toolName
	}
	if subTool != "" {
		return base + "." + subTool
	}
	return base
}
