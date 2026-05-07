// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// WorkspaceRef is a reference to a Workspace that receives shared access.
type WorkspaceRef struct {
	// name is the Workspace name.
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`

	// namespace is the namespace containing the Workspace.
	// +kubebuilder:validation:MinLength=1
	Namespace string `json:"namespace"`

	// access controls whether the workspace gets reader or writer access.
	// The controller writes memory.reader or memory.writer ReBAC tuples accordingly.
	// +keese:rebac-tuple=memory.reader
	// +keese:rebac-tuple=memory.writer
	// +kubebuilder:validation:Enum=reader;writer
	// +kubebuilder:default=reader
	// +optional
	Access string `json:"access,omitempty"`
}

// SharedMemorySpec defines the desired state of SharedMemory.
//
// +kubebuilder:validation:XValidation:rule="[has(self.provider.sqlite),has(self.provider.redis),has(self.provider.qdrant),has(self.provider.pgvector),has(self.provider.neo4j),has(self.provider.mem0),has(self.provider.zep)].exists_one(x,x)",message="exactly one of sqlite|redis|qdrant|pgvector|neo4j|mem0|zep must be set"
type SharedMemorySpec struct {
	// tenantRef is the owning Tenant name (cluster-level reference).
	// Only the tenant admin may mutate sharedWith[]; enforced by the
	// SharedMemoryMutationAuthz VAP which calls OpenFGA (≤15ms 1-hop).
	// +kubebuilder:validation:MinLength=1
	// +keese:rebac-tuple=sharedmemory.tenant
	TenantRef string `json:"tenantRef"`

	// provider is the discriminated one-of backend configuration.
	// Same semantics as Memory.spec.provider.
	// +kubebuilder:validation:Required
	Provider MemoryProvider `json:"provider"`

	// embeddingDim is the dimensionality of stored embeddings.
	// Immutable after creation; enforced by the EmbeddingDimImmutable VAP.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65536
	// +optional
	EmbeddingDim int32 `json:"embeddingDim,omitempty"`

	// sharedWith is the list of Workspaces that receive access grants.
	// Each element causes the controller to write memory.reader or memory.writer
	// OpenFGA tuples for the workspace's ServiceAccount.
	// Mutations require tenant admin authz (SharedMemoryMutationAuthz VAP).
	// +optional
	// +listType=map
	// +listMapKey=name
	SharedWith []WorkspaceRef `json:"sharedWith,omitempty"`
}

// SharedMemoryStatus defines the observed state of SharedMemory.
type SharedMemoryStatus struct {
	// observedGeneration is the .metadata.generation the controller last reconciled.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// phase is the current lifecycle phase.
	// +optional
	Phase MemoryPhase `json:"phase,omitempty"`

	// conditions holds standard Kubernetes status conditions.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// rebacTupleCount is the number of OpenFGA tuples currently written for this SharedMemory.
	// +optional
	RebacTupleCount int32 `json:"rebacTupleCount,omitempty"`

	// backendProvisioned is true when the backend resource has been confirmed present.
	// +optional
	BackendProvisioned bool `json:"backendProvisioned,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,shortName=smem
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:printcolumn:name="Ready",type="string",JSONPath=".status.conditions[?(@.type=='Ready')].status"
// +kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase"
// +kubebuilder:printcolumn:name="Provider",type="string",JSONPath=".spec.provider.type"

// SharedMemory is the Schema for the sharedmemories API.
type SharedMemory struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   SharedMemorySpec   `json:"spec,omitempty"`
	Status SharedMemoryStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// SharedMemoryList contains a list of SharedMemory.
type SharedMemoryList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []SharedMemory `json:"items"`
}

func init() {
	SchemeBuilder.Register(&SharedMemory{}, &SharedMemoryList{})
}
