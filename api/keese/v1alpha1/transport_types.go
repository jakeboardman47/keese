// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TransportType enumerates the supported transport protocols.
// +kubebuilder:validation:Enum=nats;a2a;mcp;stdio
type TransportType string

const (
	TransportTypeNATS  TransportType = "nats"
	TransportTypeA2A   TransportType = "a2a"
	TransportTypeMCP   TransportType = "mcp"
	TransportTypeStdio TransportType = "stdio"
)

// TransportPhase represents the lifecycle phase of a Transport.
// +kubebuilder:validation:Enum=Pending;Provisioning;Ready;Degraded;Terminating
type TransportPhase string

const (
	TransportPhasePending      TransportPhase = "Pending"
	TransportPhaseProvisioning TransportPhase = "Provisioning"
	TransportPhaseReady        TransportPhase = "Ready"
	TransportPhaseDegraded     TransportPhase = "Degraded"
	TransportPhaseTerminating  TransportPhase = "Terminating"
)

// AckPolicy enumerates the NATS JetStream ack policies.
// +kubebuilder:validation:Enum=explicit;none;all
type AckPolicy string

const (
	AckPolicyExplicit AckPolicy = "explicit"
	AckPolicyNone     AckPolicy = "none"
	AckPolicyAll      AckPolicy = "all"
)

// A2APeerAuth enumerates the supported A2A peer-authentication modes.
// +kubebuilder:validation:Enum=workspace-sa;mutual-tls
type A2APeerAuth string

const (
	A2APeerAuthWorkspaceSA A2APeerAuth = "workspace-sa"
	A2APeerAuthMutualTLS   A2APeerAuth = "mutual-tls"
)

// A2AScope enumerates the A2A messaging scope values.
// +kubebuilder:validation:Enum=intra-tenant;cross-tenant
type A2AScope string

const (
	A2AScopeIntraTenant A2AScope = "intra-tenant"
	A2AScopeCrossTenant A2AScope = "cross-tenant"
)

// NamespacedObjectRef references a namespaced Kubernetes object.
type NamespacedObjectRef struct {
	// Name of the referenced object.
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`

	// Namespace of the referenced object.
	// +kubebuilder:validation:MinLength=1
	// +optional
	Namespace string `json:"namespace,omitempty"`
}

// ReconnectBackoff configures exponential back-off for stdio reconnects.
type ReconnectBackoff struct {
	// Initial is the initial back-off delay (e.g. "1s").
	// +optional
	Initial string `json:"initial,omitempty"`

	// Multiplier is the factor applied on each retry. Range [1.0, 10.0].
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=10
	// +optional
	Multiplier *float64 `json:"multiplier,omitempty"`

	// Max is the maximum back-off delay (e.g. "30s").
	// +optional
	Max string `json:"max,omitempty"`
}

// StdioConfig configures the stdio bridge transport.
type StdioConfig struct {
	// BridgeImage is the container image for the stdio bridge sidecar. Required.
	// +kubebuilder:validation:MinLength=1
	BridgeImage string `json:"bridgeImage"`

	// InboundQueueDepth is the number of frames buffered inbound. Range [10, 10000].
	// +kubebuilder:default=100
	// +kubebuilder:validation:Minimum=10
	// +kubebuilder:validation:Maximum=10000
	// +optional
	InboundQueueDepth *int32 `json:"inboundQueueDepth,omitempty"`

	// OutboundQueueDepth is the number of frames buffered outbound before drop-oldest.
	// Range [100, 100000].
	// +kubebuilder:default=1000
	// +kubebuilder:validation:Minimum=100
	// +kubebuilder:validation:Maximum=100000
	// +optional
	OutboundQueueDepth *int32 `json:"outboundQueueDepth,omitempty"`

	// ReconnectBufferBytes is the in-memory buffer in bytes for reconnect replay.
	// Range [1048576, 67108864]. Default 4 MiB.
	// +kubebuilder:default=4194304
	// +kubebuilder:validation:Minimum=1048576
	// +kubebuilder:validation:Maximum=67108864
	// +optional
	ReconnectBufferBytes *int64 `json:"reconnectBufferBytes,omitempty"`

	// ReconnectRetries is the maximum number of reconnect attempts. Range [1, 10].
	// +kubebuilder:default=3
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=10
	// +optional
	ReconnectRetries *int32 `json:"reconnectRetries,omitempty"`

	// ReconnectBackoff configures the exponential back-off applied between retries.
	// +optional
	ReconnectBackoff *ReconnectBackoff `json:"reconnectBackoff,omitempty"`
}

// NATSTLSConfig references a cert-manager Certificate for NATS mTLS.
type NATSTLSConfig struct {
	// CertificateRef points to a cert-manager Certificate in the same namespace.
	// +optional
	CertificateRef *NamespacedObjectRef `json:"certificateRef,omitempty"`
}

// NATSStreamConfig holds the JetStream stream configuration used in opt-in ownership mode.
// Only read when annotation keese.ai/auto-create-stream=true is set.
type NATSStreamConfig struct {
	// Subjects lists the NATS subjects covered by this stream.
	// +optional
	Subjects []string `json:"subjects,omitempty"`

	// Retention sets the stream retention policy. Default "limits".
	// +kubebuilder:validation:Enum=limits;interest;workqueue
	// +optional
	Retention string `json:"retention,omitempty"`

	// MaxAge is the maximum age of messages (e.g. "7d").
	// +optional
	MaxAge string `json:"maxAge,omitempty"`

	// Storage sets the JetStream storage backend. Default "file".
	// +kubebuilder:validation:Enum=file;memory
	// +optional
	Storage string `json:"storage,omitempty"`

	// Replicas is the number of stream replicas. Default 3.
	// +kubebuilder:validation:Minimum=1
	// +optional
	Replicas *int32 `json:"replicas,omitempty"`
}

// NATSConfig configures a NATS JetStream transport.
type NATSConfig struct {
	// ClusterRef references the NATS cluster object.
	// +kubebuilder:validation:Required
	ClusterRef NamespacedObjectRef `json:"clusterRef"`

	// StreamName is the JetStream stream name. Max 64 chars.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=64
	StreamName string `json:"streamName"`

	// ConsumerName is the JetStream consumer/durable name. Max 64 chars.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=64
	ConsumerName string `json:"consumerName"`

	// AckPolicy sets the ack policy. Default "explicit".
	// +kubebuilder:default=explicit
	// +optional
	AckPolicy *AckPolicy `json:"ackPolicy,omitempty"`

	// MaxDeliver is the maximum delivery count. Range [1, 100].
	// +kubebuilder:default=3
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=100
	// +optional
	MaxDeliver *int32 `json:"maxDeliver,omitempty"`

	// AckWait is the ack wait duration (e.g. "30s").
	// +optional
	AckWait string `json:"ackWait,omitempty"`

	// TLS configures mTLS for the NATS connection. CertificateRef must resolve.
	// +optional
	TLS *NATSTLSConfig `json:"tls,omitempty"`

	// StreamConfig is the JetStream stream configuration for opt-in ownership mode.
	// Ignored unless annotation keese.ai/auto-create-stream=true is set.
	// +optional
	StreamConfig *NATSStreamConfig `json:"streamConfig,omitempty"`
}

// MCPConfig configures an MCPRoute-backed transport.
type MCPConfig struct {
	// McpRouteRef references the MCPRoute object. Required.
	// Admission validates the MCPRoute exists; emits MCPRouteNotFound if absent.
	// +kubebuilder:validation:Required
	McpRouteRef NamespacedObjectRef `json:"mcpRouteRef"`

	// ProtocolVersion is the MCP protocol version. Default "2024-11-05".
	// +kubebuilder:default="2024-11-05"
	// +optional
	ProtocolVersion string `json:"protocolVersion,omitempty"`

	// ToolTimeout is the per-tool-call timeout duration. Range [1s, 300s]. Default "30s".
	// +optional
	// +keese:rebac-tuple=N/A-quantitative-config
	ToolTimeout string `json:"toolTimeout,omitempty"`
}

// WorkspaceSAConfig configures workspace service-account peer authentication.
type WorkspaceSAConfig struct {
	// Audience is the SA token audience. For WorkflowRun transports this must be
	// keese-wf-<workflow-run-uid> (04b iter-3 workflowRun audience template).
	// +kubebuilder:validation:MinLength=1
	// +keese:rebac-tuple=tenant.allows_messaging
	Audience string `json:"audience"`

	// AuthzTupleCheck enables the OpenFGA cross-tenant check. Defaults to true for
	// scope: cross-tenant. Only meaningful in cross-tenant scope; ignored otherwise.
	// +kubebuilder:default=true
	// +keese:rebac-tuple=workspace.messageable_from
	// +optional
	AuthzTupleCheck *bool `json:"authzTupleCheck,omitempty"`
}

// MutualTLSConfig configures mutual TLS peer authentication.
type MutualTLSConfig struct {
	// CertificateRef points to a cert-manager Certificate in the same namespace.
	// Required for mutual-tls peerAuth mode.
	// +kubebuilder:validation:Required
	CertificateRef NamespacedObjectRef `json:"certificateRef"`

	// ClientCaBundle is an inline PEM CA bundle for client certificate validation.
	// +optional
	ClientCaBundle string `json:"clientCaBundle,omitempty"`

	// AclRef references an ACL resource for mTLS peers.
	// +optional
	AclRef *NamespacedObjectRef `json:"aclRef,omitempty"`
}

// A2AConfig configures an agent-to-agent transport.
type A2AConfig struct {
	// Endpoint is the gRPC target URI. Must start with grpc:// or grpcs://.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:XValidation:rule=`self.startsWith("grpc://") || self.startsWith("grpcs://")`,message="endpoint must start with grpc:// or grpcs://"
	Endpoint string `json:"endpoint"`

	// PeerAuth selects the peer-authentication mode. Default "workspace-sa".
	// +kubebuilder:default="workspace-sa"
	// +optional
	PeerAuth *A2APeerAuth `json:"peerAuth,omitempty"`

	// Scope is the messaging scope. Default "intra-tenant".
	// cross-tenant scope requires an Approved CrossTenantAgreement.
	// +kubebuilder:default="intra-tenant"
	// +keese:rebac-tuple=workspace.messageable_from
	// +optional
	Scope *A2AScope `json:"scope,omitempty"`

	// WorkspaceSA configures workspace service-account authentication.
	// Required when peerAuth=workspace-sa.
	// +optional
	WorkspaceSA *WorkspaceSAConfig `json:"workspaceSA,omitempty"`

	// MutualTLS configures mutual-TLS authentication.
	// Required when peerAuth=mutual-tls.
	// +optional
	MutualTLS *MutualTLSConfig `json:"mutualTLS,omitempty"`
}

// TransportSpec defines the desired state of a Transport.
//
// spec.type is immutable after creation (VAP CEL). Exactly one sub-struct
// matching spec.type must be set; the others must be absent.
//
// +kubebuilder:validation:XValidation:rule="oldSelf.type == self.type",message="spec.type is immutable after creation (TransportTypeImmutable)"
// +kubebuilder:validation:XValidation:rule="self.type == 'nats' ? has(self.nats) && !has(self.a2a) && !has(self.mcp) && !has(self.stdio) : true",message="spec.nats must be set (and only nats) when spec.type=nats (TransportSubfieldMismatch)"
// +kubebuilder:validation:XValidation:rule="self.type == 'a2a' ? has(self.a2a) && !has(self.nats) && !has(self.mcp) && !has(self.stdio) : true",message="spec.a2a must be set (and only a2a) when spec.type=a2a (TransportSubfieldMismatch)"
// +kubebuilder:validation:XValidation:rule="self.type == 'mcp' ? has(self.mcp) && !has(self.nats) && !has(self.a2a) && !has(self.stdio) : true",message="spec.mcp must be set (and only mcp) when spec.type=mcp (TransportSubfieldMismatch)"
// +kubebuilder:validation:XValidation:rule="self.type == 'stdio' ? has(self.stdio) && !has(self.nats) && !has(self.a2a) && !has(self.mcp) : true",message="spec.stdio must be set (and only stdio) when spec.type=stdio (TransportSubfieldMismatch)"
type TransportSpec struct {
	// Type selects the transport protocol. Immutable after creation.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Enum=nats;a2a;mcp;stdio
	Type TransportType `json:"type"`

	// NATS configures NATS JetStream transport. Set only when type=nats.
	// +optional
	NATS *NATSConfig `json:"nats,omitempty"`

	// A2A configures agent-to-agent gRPC transport. Set only when type=a2a.
	// +keese:rebac-tuple=transport.owner
	// +optional
	A2A *A2AConfig `json:"a2a,omitempty"`

	// MCP configures MCPRoute-backed transport. Set only when type=mcp.
	// +optional
	MCP *MCPConfig `json:"mcp,omitempty"`

	// Stdio configures the stdio bridge transport. Set only when type=stdio.
	// +optional
	Stdio *StdioConfig `json:"stdio,omitempty"`
}

// TransportStatus defines the observed state of Transport.
type TransportStatus struct {
	// Phase is the current lifecycle phase of the Transport.
	// +optional
	Phase TransportPhase `json:"phase,omitempty"`

	// ObservedGeneration is the metadata.generation this status was computed from.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Conditions represents the latest state of the Transport.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// ReBAC tuple count written by the last successful sync. Used for debuggability.
	// +optional
	RebacTupleCount int `json:"rebacTupleCount,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:printcolumn:name="Ready",type="string",JSONPath=".status.conditions[?(@.type=='Ready')].status"
// +kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase"
// +kubebuilder:printcolumn:name="Type",type="string",JSONPath=".spec.type"

// Transport is the Schema for the transports API.
type Transport struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   TransportSpec   `json:"spec,omitempty"`
	Status TransportStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// TransportList contains a list of Transport.
type TransportList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Transport `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Transport{}, &TransportList{})
}
