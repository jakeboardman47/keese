// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// WorkspacePhase is the lifecycle phase of a Workspace.
// +kubebuilder:validation:Enum=Pending;Provisioning;Running;Idle;Evicted;Terminating
type WorkspacePhase string

const (
	WorkspacePhasePending      WorkspacePhase = "Pending"
	WorkspacePhaseProvisioning WorkspacePhase = "Provisioning"
	WorkspacePhaseRunning      WorkspacePhase = "Running"
	WorkspacePhaseIdle         WorkspacePhase = "Idle"
	WorkspacePhaseEvicted      WorkspacePhase = "Evicted"
	WorkspacePhaseTerminating  WorkspacePhase = "Terminating"
)

// WorkspaceSessionMode controls when the agent runtime pod is active for a Workspace.
// Distinct from SessionMode on WorkspaceSession (which controls pod-sharing).
// +kubebuilder:validation:Enum=Always;OnDemand
type WorkspaceSessionMode string

const (
	WorkspaceSessionModeAlways   WorkspaceSessionMode = "Always"
	WorkspaceSessionModeOnDemand WorkspaceSessionMode = "OnDemand"
)

// AttachPolicy controls how new sessions attach to running pods.
// +kubebuilder:validation:Enum=New;Reuse
type AttachPolicy string

const (
	AttachPolicyNew   AttachPolicy = "New"
	AttachPolicyReuse AttachPolicy = "Reuse"
)

// WorkspaceSpec defines the desired state of Workspace.
// +kubebuilder:validation:XValidation:rule="self.interactive == oldSelf.interactive",message="spec.interactive is immutable after creation"
type WorkspaceSpec struct {
	// RuntimeRef references the AgentRuntime that will power this workspace.
	// The runtime controller's Bootstrap method is called to initialize the pod spec.
	// +kubebuilder:validation:Required
	RuntimeRef LocalObjectReference `json:"runtimeRef"`

	// RecipeRef references the Recipe to execute in this workspace.
	// +optional
	RecipeRef *LocalObjectReference `json:"recipeRef,omitempty"`

	// TenantRef identifies the owning Tenant for this workspace.
	// References metav1.ObjectReference until tenancy CRD is generated;
	// TODO(spec-followup): tighten to LocalObjectReference once tenancyv1alpha1.Tenant kind is generated.
	// +kubebuilder:validation:Required
	// +keese:rebac-tuple=workspace.owner
	TenantRef corev1.ObjectReference `json:"tenantRef"`

	// GuardrailBindingRefs lists GuardrailBinding objects to enforce in this workspace.
	// +optional
	// +keese:rebac-tuple=workspace.guardrail_bound
	GuardrailBindingRefs []LocalObjectReference `json:"guardrailBindingRefs,omitempty"`

	// MemoryRefs lists Memory objects accessible within this workspace.
	// +optional
	MemoryRefs []LocalObjectReference `json:"memoryRefs,omitempty"`

	// TransportRefs lists Transport objects (NATS streams, etc.) accessible within this workspace.
	// +optional
	TransportRefs []LocalObjectReference `json:"transportRefs,omitempty"`

	// Interactive indicates whether this workspace supports interactive (human-in-the-loop) sessions.
	// Immutable after creation (enforced by XValidation above).
	// +optional
	// +kubebuilder:default=false
	Interactive bool `json:"interactive,omitempty"`

	// SessionMode controls when the agent runtime pod is active.
	// +optional
	// +kubebuilder:default=OnDemand
	SessionMode WorkspaceSessionMode `json:"sessionMode,omitempty"`

	// AttachPolicy controls how new sessions attach to running pods.
	// +optional
	// +kubebuilder:default=Reuse
	AttachPolicy AttachPolicy `json:"attachPolicy,omitempty"`

	// AttachGrace is the time to wait before considering a session detached.
	// +optional
	// +kubebuilder:default="30s"
	AttachGrace metav1.Duration `json:"attachGrace,omitempty"`

	// ConcurrencyPolicy controls how concurrent WorkflowRuns are handled.
	// +optional
	// +kubebuilder:default=Allow
	ConcurrencyPolicy ConcurrencyPolicy `json:"concurrencyPolicy,omitempty"`

	// Editors lists user identifiers granted editor access to this workspace.
	// +optional
	// +keese:rebac-tuple=workspace.editor
	Editors []string `json:"editors,omitempty"`

	// Viewers lists user identifiers granted viewer access to this workspace.
	// +optional
	// +keese:rebac-tuple=workspace.viewer
	Viewers []string `json:"viewers,omitempty"`

	// SessionStorage is the requested size of the PVC used for SQLite session state.
	// TODO(spec-followup): spec does not define a default size; defaulting to "10Gi" per task brief.
	// +optional
	SessionStorage *resource.Quantity `json:"sessionStorage,omitempty"`

	// Egress carries the per-workspace egress authorization config —
	// today, the allowlist of OpenFGA tool names this workspace's
	// session pods may call. The Workspace controller writes one
	// `tool:<name>#allowed_in@workspace:<wsuid>` ReBAC tuple per
	// element; the keese-authz ext_authz service then resolves
	// `tool:<name>#can_call@<subject>` per request.
	// +optional
	Egress *WorkspaceEgressSpec `json:"egress,omitempty"`

	// A2A enables the agent-to-agent (A2A) HTTP/SSE endpoint on this
	// Workspace and selects its trust scope. When unset, the Workspace
	// exposes no A2A endpoint (the default-deny posture). See E2.
	// +optional
	A2A *WorkspaceA2AConfig `json:"a2a,omitempty"`
}

// WorkspaceA2AScope selects the trust boundary for inbound A2A calls into
// this Workspace.
//
//   - intra-tenant: any peer Workspace owned by the same Tenant may call.
//     The reconciler writes the self tuple `workspace:W#a2a_callable_by@
//     workspace:W` unconditionally when A2A is enabled.
//   - cross-tenant: peers in other tenants may call ONLY when an Approved
//     CrossTenantAgreement (D25/D29) references both tenants. The reconciler
//     writes the `a2a_callable_by` tuple per approved peer; absent a CTA it
//     writes nothing and ext_authz fails closed.
//
// +kubebuilder:validation:Enum=intra-tenant;cross-tenant
type WorkspaceA2AScope string

const (
	// WorkspaceA2AScopeIntraTenant restricts A2A callers to same-tenant peers.
	WorkspaceA2AScopeIntraTenant WorkspaceA2AScope = "intra-tenant"
	// WorkspaceA2AScopeCrossTenant permits cross-tenant callers gated by an
	// Approved CrossTenantAgreement.
	WorkspaceA2AScopeCrossTenant WorkspaceA2AScope = "cross-tenant"
)

// WorkspaceA2AConfig is the per-workspace A2A endpoint + authz config.
type WorkspaceA2AConfig struct {
	// Enabled turns the A2A endpoint on. When true and scope is
	// intra-tenant, the Workspace controller writes the self ReBAC tuple
	// `workspace:W#a2a_callable_by@workspace:W`; the keese-authz ext_authz
	// service then resolves `workspace:W#a2a_callable_by@workspace:<caller>`
	// per inbound A2A request (fail-closed). Cross-tenant scope additionally
	// requires an Approved CrossTenantAgreement before any tuple is written.
	//
	// +optional
	// +kubebuilder:default=false
	// +keese:rebac-tuple=a2a_callable_by
	Enabled bool `json:"enabled,omitempty"`

	// Scope selects the trust boundary for inbound A2A calls.
	// +optional
	// +kubebuilder:default=intra-tenant
	Scope WorkspaceA2AScope `json:"scope,omitempty"`

	// Port is the A2A HTTP/SSE listener port served by the bridge sidecar.
	// Must be in the unprivileged range 1024–65535. Defaults to 8080.
	//
	// +optional
	// +kubebuilder:default=8080
	// +kubebuilder:validation:Minimum=1024
	// +kubebuilder:validation:Maximum=65535
	Port *int32 `json:"port,omitempty"`
}

// WorkspaceEgressSpec is the per-workspace egress authz config.
type WorkspaceEgressSpec struct {
	// AllowedTools is the OpenFGA tool-name allowlist for this
	// workspace. Each entry must match the toolName field of an
	// existing ToolBinding (cluster) or the namespaced name of a
	// WorkspaceTool. The Workspace controller writes one ReBAC
	// tuple per element on Sync and deletes them on cleanup.
	//
	// +optional
	// +keese:rebac-tuple=tool.allowed_in
	AllowedTools []string `json:"allowedTools,omitempty"`
}

// WorkspaceStatus defines the observed state of Workspace.
type WorkspaceStatus struct {
	// ObservedGeneration is the .metadata.generation that the controller last reconciled.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Phase is the current lifecycle phase of the workspace.
	// +optional
	Phase WorkspacePhase `json:"phase,omitempty"`

	// Conditions contains detailed status conditions.
	// +optional
	// +patchMergeKey=type
	// +patchStrategy=merge
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type"`

	// ServiceAccountName is the name of the ServiceAccount created for this workspace.
	// +optional
	ServiceAccountName string `json:"serviceAccountName,omitempty"`

	// NetworkPolicyName is the name of the primary NetworkPolicy (default-deny) for this workspace.
	// +optional
	NetworkPolicyName string `json:"networkPolicyName,omitempty"`

	// PodRef is a reference to the agent runtime Pod (if Running).
	// +optional
	PodRef *corev1.LocalObjectReference `json:"podRef,omitempty"`

	// RebacTupleCount is the number of OpenFGA tuples currently owned by this workspace.
	// Recorded for debuggability.
	// +optional
	RebacTupleCount int32 `json:"rebacTupleCount,omitempty"`
}

// Workspace condition types.
const (
	// WorkspaceConditionReady indicates the workspace is fully provisioned and ready.
	WorkspaceConditionReady = "Ready"
	// WorkspaceConditionProgressing indicates the workspace is being provisioned.
	WorkspaceConditionProgressing = "Progressing"
	// WorkspaceConditionNetworkIsolated indicates network policies are in place.
	WorkspaceConditionNetworkIsolated = "NetworkIsolated"
	// WorkspaceConditionSessionStorageReady indicates the PVC is bound.
	WorkspaceConditionSessionStorageReady = "SessionStorageReady"
)

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,shortName=ws
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=".status.phase"
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=".status.conditions[?(@.type=='Ready')].status"
// +kubebuilder:printcolumn:name="Runtime",type=string,JSONPath=".spec.runtimeRef.name"
// +kubebuilder:printcolumn:name="Interactive",type=boolean,JSONPath=".spec.interactive"
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=".metadata.creationTimestamp"

// Workspace is the Schema for the workspaces API.
type Workspace struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   WorkspaceSpec   `json:"spec,omitempty"`
	Status WorkspaceStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// WorkspaceList contains a list of Workspace.
type WorkspaceList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Workspace `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Workspace{}, &WorkspaceList{})
}
