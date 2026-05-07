// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ToolBinding is the cluster-scoped, admin-owned catalogue of
// HTTP-request → OpenFGA `tool:<name>` mappings. The keese-authz
// ext_authz service compiles every accepted ToolBinding into an
// in-memory routing trie and uses it to derive the `tool:` object
// name for an `OpenFGA.Check(user, can_call, tool:<name>)` call.
//
// Cluster scope reflects platform ownership: ToolBindings define the
// canonical names of LLM/MCP endpoints shared across tenants
// (`/anthropic/v1/messages` + `model=opus` → `tool:anthropic.messages.opus-4`).
// For per-tenant ad-hoc tools see `WorkspaceTool` (namespaced).
//
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster,shortName=tb,categories=keese
// +kubebuilder:printcolumn:name=Path,type=string,JSONPath=`.spec.match.paths[0].value`
// +kubebuilder:printcolumn:name=Tool,type=string,JSONPath=`.spec.toolName`
// +kubebuilder:printcolumn:name=Ready,type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`
// +kubebuilder:printcolumn:name=Age,type=date,JSONPath=`.metadata.creationTimestamp`
type ToolBinding struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ToolBindingSpec   `json:"spec,omitempty"`
	Status ToolBindingStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
type ToolBindingList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ToolBinding `json:"items"`
}

// ToolBindingSpec carries the request matcher, optional body
// discriminator, the resolved tool name, and the subject extractor.
type ToolBindingSpec struct {
	// Match describes which incoming HTTP requests bind to this tool.
	// Mirrors Gateway API HTTPRouteMatch shape — paths, methods,
	// headers — so the selector grammar is familiar.
	Match HTTPRouteMatch `json:"match"`

	// ToolName is the OpenFGA `tool:<toolName>` object the
	// ext_authz Check fires against. Must be a stable platform
	// identifier (e.g. `anthropic.messages`); per-tenant ad-hoc
	// tools should use `WorkspaceTool` instead.
	//
	// Final tool name in the Check is `<toolName>` when
	// BodyDiscriminator is unset, OR `<toolName>.<sub>` when
	// BodyDiscriminator yields a non-empty `sub`.
	//
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:Pattern=`^[a-z][a-z0-9.-]*$`
	// +keese:rebac-tuple=tool:<n>#allowed_in@workspace:<w>
	ToolName string `json:"toolName"`

	// BodyDiscriminator optionally derives a sub-tool name from a
	// JSON field in the request body. Use for fine-grained
	// per-API-within-tool authorization (e.g. distinguish
	// `model=opus` from `model=haiku`).
	//
	// +optional
	BodyDiscriminator *BodyDiscriminator `json:"bodyDiscriminator,omitempty"`

	// SubjectFrom selects the source of the OpenFGA `user:<id>`
	// for the Check.
	//
	// +kubebuilder:default=ServiceAccountSubject
	SubjectFrom SubjectFromSource `json:"subjectFrom,omitempty"`

	// JWTClaimName is the projected SA token claim name to read
	// when SubjectFrom is `JWTClaim`. Ignored otherwise.
	//
	// +optional
	JWTClaimName string `json:"jwtClaimName,omitempty"`

	// WorkspaceFrom selects the source of the OpenFGA
	// `workspace:<id>` value used to scope per-workspace
	// `allowed_in` Checks. Default is to parse the SA name
	// `ksa-<wsuid>` (the keese controller's deterministic SA
	// naming convention).
	//
	// +kubebuilder:default=ServiceAccountName
	WorkspaceFrom WorkspaceFromSource `json:"workspaceFrom,omitempty"`
}

// HTTPRouteMatch is the request matcher subset we honor — path,
// method, headers, query params. Mirrors Gateway API but kept
// inline so the schema lands in the CRD without a cross-group
// dependency.
type HTTPRouteMatch struct {
	// Paths matches the request path. Multiple entries are OR'd.
	// At least one entry is required.
	//
	// +kubebuilder:validation:MinItems=1
	Paths []HTTPPathMatch `json:"paths"`

	// Methods matches the HTTP verb. Multiple entries are OR'd.
	// Empty matches every method (treat as "any").
	//
	// +optional
	Methods []HTTPMethod `json:"methods,omitempty"`

	// Headers matches request headers. Each entry is AND'd against
	// the request — every entry must match for the rule to fire.
	//
	// +optional
	Headers []HTTPHeaderMatch `json:"headers,omitempty"`

	// QueryParams matches request query parameters. Each entry is
	// AND'd against the request.
	//
	// +optional
	QueryParams []HTTPQueryParamMatch `json:"queryParams,omitempty"`
}

// HTTPPathMatch is the path-match subset.
type HTTPPathMatch struct {
	// +kubebuilder:default=Exact
	// +kubebuilder:validation:Enum=Exact;PathPrefix;RegularExpression
	Type PathMatchType `json:"type,omitempty"`

	// +kubebuilder:validation:MinLength=1
	Value string `json:"value"`
}

type PathMatchType string

const (
	PathMatchExact             PathMatchType = "Exact"
	PathMatchPathPrefix        PathMatchType = "PathPrefix"
	PathMatchRegularExpression PathMatchType = "RegularExpression"
)

// HTTPMethod is the HTTP verb — restricted to the verbs the gateway
// actually receives from clients.
//
// +kubebuilder:validation:Enum=GET;POST;PUT;PATCH;DELETE;HEAD;OPTIONS
type HTTPMethod string

// HTTPHeaderMatch is the header-match subset.
type HTTPHeaderMatch struct {
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`

	// +kubebuilder:default=Exact
	// +kubebuilder:validation:Enum=Exact;RegularExpression
	Type HeaderMatchType `json:"type,omitempty"`

	// +kubebuilder:validation:MinLength=1
	Value string `json:"value"`
}

type HeaderMatchType string

const (
	HeaderMatchExact             HeaderMatchType = "Exact"
	HeaderMatchRegularExpression HeaderMatchType = "RegularExpression"
)

// HTTPQueryParamMatch is the query-param-match subset.
type HTTPQueryParamMatch struct {
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`

	// +kubebuilder:default=Exact
	// +kubebuilder:validation:Enum=Exact;RegularExpression
	Type HeaderMatchType `json:"type,omitempty"`

	// +kubebuilder:validation:MinLength=1
	Value string `json:"value"`
}

// BodyDiscriminator extracts a sub-tool name from a JSON field in
// the request body. The keese-authz ext_authz service must be wired
// with `with_request_body` so the body is buffered and inspected
// before the upstream call.
type BodyDiscriminator struct {
	// JSONPath is the JSONPath expression evaluated against the
	// request body. The value at JSONPath is then mapped through
	// `Map` to a sub-tool name.
	//
	// Supported subset: `$.field`, `$.parent.child`. Wildcards and
	// filters are not supported (avoid arbitrary-eval risk on the
	// hot path).
	//
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:Pattern=`^\$(\.[a-zA-Z_][a-zA-Z0-9_]*)+$`
	JSONPath string `json:"jsonPath"`

	// Map is the value → sub-tool-name lookup. Values not in Map
	// fall through to `Default`.
	//
	// +kubebuilder:validation:MinProperties=1
	Map map[string]string `json:"map"`

	// Default is the sub-tool name returned when the body value is
	// absent or doesn't match any Map entry. Empty string means
	// "no sub-tool — match the parent ToolName as-is".
	//
	// +optional
	Default string `json:"default,omitempty"`
}

// SubjectFromSource selects the OpenFGA user-id source.
//
// +kubebuilder:validation:Enum=ServiceAccountSubject;JWTClaim
type SubjectFromSource string

const (
	// SubjectFromServiceAccountSubject parses the projected SA
	// token's `sub` claim, expected shape
	// `system:serviceaccount:<ns>:<sa>`. The user-id is the full
	// subject string.
	SubjectFromServiceAccountSubject SubjectFromSource = "ServiceAccountSubject"

	// SubjectFromJWTClaim reads `JWTClaimName` from the projected
	// SA token. The user-id is the claim's string value.
	SubjectFromJWTClaim SubjectFromSource = "JWTClaim"
)

// WorkspaceFromSource selects the OpenFGA workspace-id source.
//
// +kubebuilder:validation:Enum=ServiceAccountName;JWTClaim
type WorkspaceFromSource string

const (
	// WorkspaceFromServiceAccountName parses the SA name
	// `ksa-<workspace-uid>` (keese controller convention).
	WorkspaceFromServiceAccountName WorkspaceFromSource = "ServiceAccountName"

	// WorkspaceFromJWTClaim reads `JWTClaimName` from the SA
	// token. Useful when the operator uses a custom identity
	// shape; the controller-level `audienceTemplates` design 04b
	// already supports per-tenant claim sets.
	WorkspaceFromJWTClaim WorkspaceFromSource = "JWTClaim"
)

// ToolBindingStatus reports compilation state for the binding.
type ToolBindingStatus struct {
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// ObservedGeneration matches metadata.generation at the time
	// of the last successful trie compilation.
	//
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// MatchedRequests is a per-binding hit counter. Useful for
	// detecting orphan bindings.
	//
	// +optional
	MatchedRequests int64 `json:"matchedRequests,omitempty"`
}

func init() {
	SchemeBuilder.Register(&ToolBinding{}, &ToolBindingList{})
}
