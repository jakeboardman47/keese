// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ProviderType enumerates supported memory backends.
// +kubebuilder:validation:Enum=sqlite;redis;qdrant;pgvector;neo4j;mem0;zep
type ProviderType string

const (
	ProviderSQLite   ProviderType = "sqlite"
	ProviderRedis    ProviderType = "redis"
	ProviderQdrant   ProviderType = "qdrant"
	ProviderPGVector ProviderType = "pgvector"
	ProviderNeo4j    ProviderType = "neo4j"
	ProviderMem0     ProviderType = "mem0"
	ProviderZep      ProviderType = "zep"
)

// MemoryPhase is the lifecycle phase of a Memory resource.
// +kubebuilder:validation:Enum=Pending;Provisioning;Ready;Degraded;Terminating
type MemoryPhase string

const (
	MemoryPhasePending      MemoryPhase = "Pending"
	MemoryPhaseProvisioning MemoryPhase = "Provisioning"
	MemoryPhaseReady        MemoryPhase = "Ready"
	MemoryPhaseDegraded     MemoryPhase = "Degraded"
	MemoryPhaseTerminating  MemoryPhase = "Terminating"
)

// SQLiteConfig holds configuration for the sqlite backend.
type SQLiteConfig struct {
	// storageSize is the PVC storage request for the SQLite database file.
	// +kubebuilder:default="1Gi"
	// +optional
	StorageSize string `json:"storageSize,omitempty"`

	// storageClassName selects the StorageClass for the PVC.
	// +optional
	StorageClassName string `json:"storageClassName,omitempty"`

	// reclaimPolicy controls whether the PVC is deleted or retained on Memory deletion.
	// +kubebuilder:default=Retain
	// +kubebuilder:validation:Enum=Retain;Delete
	// +optional
	ReclaimPolicy string `json:"reclaimPolicy,omitempty"`
}

// RedisConfig holds configuration for the redis backend.
type RedisConfig struct {
	// address is the Redis endpoint (host:port).
	// +kubebuilder:validation:MinLength=1
	Address string `json:"address"`

	// replicas is the number of Redis replicas. Must be ≥2 outside dev namespaces (HA VAP).
	// +kubebuilder:default=1
	// +kubebuilder:validation:Minimum=1
	// +optional
	Replicas int32 `json:"replicas,omitempty"`

	// credentialSecretRef names the K8s Secret containing redis credentials, mounted
	// as a projected file per rule 05.7.
	// +optional
	CredentialSecretRef string `json:"credentialSecretRef,omitempty"`
}

// QdrantConfig holds configuration for the qdrant vector backend.
type QdrantConfig struct {
	// collectionName is the Qdrant collection to use.
	// +kubebuilder:validation:MinLength=1
	CollectionName string `json:"collectionName"`

	// endpoint is the Qdrant gRPC endpoint.
	// +kubebuilder:validation:MinLength=1
	Endpoint string `json:"endpoint"`

	// replicas is the replication factor for the collection. Must be ≥2 outside dev (HA VAP).
	// +kubebuilder:default=1
	// +kubebuilder:validation:Minimum=1
	// +optional
	Replicas int32 `json:"replicas,omitempty"`

	// credentialSecretRef names the K8s Secret containing the Qdrant API key, mounted
	// as a projected file per rule 05.7.
	// +optional
	CredentialSecretRef string `json:"credentialSecretRef,omitempty"`
}

// PGVectorConfig holds configuration for the pgvector backend.
type PGVectorConfig struct {
	// dsn is a reference to a K8s Secret key containing the PostgreSQL DSN.
	// The secret is mounted as a projected file — never as an env var (rule 05.7).
	// +kubebuilder:validation:MinLength=1
	DSNSecretRef string `json:"dsnSecretRef"`

	// tableName is the pgvector table to use.
	// +kubebuilder:default=keese_memory
	// +optional
	TableName string `json:"tableName,omitempty"`
}

// Neo4jConfig holds configuration for the neo4j graph backend.
type Neo4jConfig struct {
	// uri is the Bolt URI for the Neo4j instance.
	// +kubebuilder:validation:MinLength=1
	URI string `json:"uri"`

	// credentialSecretRef names the K8s Secret containing neo4j credentials, mounted
	// as a projected file per rule 05.7.
	// +optional
	CredentialSecretRef string `json:"credentialSecretRef,omitempty"`
}

// Mem0Config holds configuration for the Mem0 hosted provider.
// Credentials are brokered via OpenBao → ExternalSecrets → projected file (rule 05.7).
type Mem0Config struct {
	// apiEndpoint overrides the default Mem0 SaaS endpoint.
	// +optional
	APIEndpoint string `json:"apiEndpoint,omitempty"`

	// credentialSecretRef names the K8s Secret (populated by ExternalSecrets) that
	// is mounted as a projected file at /var/run/keese/secrets/<name>.
	// +kubebuilder:validation:MinLength=1
	CredentialSecretRef string `json:"credentialSecretRef"`
}

// ZepConfig holds configuration for the Zep hosted provider.
// Credentials are brokered via OpenBao → ExternalSecrets → projected file (rule 05.7).
type ZepConfig struct {
	// apiEndpoint overrides the default Zep SaaS endpoint.
	// +optional
	APIEndpoint string `json:"apiEndpoint,omitempty"`

	// credentialSecretRef names the K8s Secret (populated by ExternalSecrets) that
	// is mounted as a projected file at /var/run/keese/secrets/<name>.
	// +kubebuilder:validation:MinLength=1
	CredentialSecretRef string `json:"credentialSecretRef"`
}

// MemoryProvider is a discriminated one-of over all supported backends.
// Exactly one provider sub-field must be set; enforced by CEL XValidation below.
//
// +kubebuilder:validation:XValidation:rule="[has(self.sqlite),has(self.redis),has(self.qdrant),has(self.pgvector),has(self.neo4j),has(self.mem0),has(self.zep)].exists_one(x,x)",message="exactly one of sqlite|redis|qdrant|pgvector|neo4j|mem0|zep must be set"
type MemoryProvider struct {
	// type discriminates the active provider field.
	// +kubebuilder:validation:Enum=sqlite;redis;qdrant;pgvector;neo4j;mem0;zep
	Type ProviderType `json:"type"`

	// sqlite configures the SQLite-on-PVC backend.
	// +optional
	SQLite *SQLiteConfig `json:"sqlite,omitempty"`

	// redis configures the Redis backend.
	// +optional
	Redis *RedisConfig `json:"redis,omitempty"`

	// qdrant configures the Qdrant vector backend.
	// +optional
	Qdrant *QdrantConfig `json:"qdrant,omitempty"`

	// pgvector configures the PostgreSQL pgvector backend.
	// +optional
	PGVector *PGVectorConfig `json:"pgvector,omitempty"`

	// neo4j configures the Neo4j graph backend.
	// +optional
	Neo4j *Neo4jConfig `json:"neo4j,omitempty"`

	// mem0 configures the Mem0 hosted provider.
	// +optional
	Mem0 *Mem0Config `json:"mem0,omitempty"`

	// zep configures the Zep hosted provider.
	// +optional
	Zep *ZepConfig `json:"zep,omitempty"`
}

// MemorySpec defines the desired state of Memory.
type MemorySpec struct {
	// workspaceRef is the owning Workspace name in the same namespace.
	// The controller writes a memory.owner ReBAC tuple for this workspace.
	// +keese:rebac-tuple=memory.owner
	// +kubebuilder:validation:MinLength=1
	WorkspaceRef string `json:"workspaceRef"`

	// provider is the discriminated one-of backend configuration.
	// +kubebuilder:validation:Required
	Provider MemoryProvider `json:"provider"`

	// embeddingDim is the dimensionality of stored embeddings.
	// Immutable after creation; enforced by the EmbeddingDimImmutable VAP.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65536
	// +optional
	EmbeddingDim int32 `json:"embeddingDim,omitempty"`
}

// MemoryStatus defines the observed state of Memory.
type MemoryStatus struct {
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

	// rebacTupleCount is the number of OpenFGA tuples written for this Memory.
	// Exposed for debuggability — not for reconcile decisions.
	// +optional
	RebacTupleCount int32 `json:"rebacTupleCount,omitempty"`

	// backendProvisioned is true when the backend resource has been confirmed present.
	// +optional
	BackendProvisioned bool `json:"backendProvisioned,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,shortName=mem
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:printcolumn:name="Ready",type="string",JSONPath=".status.conditions[?(@.type=='Ready')].status"
// +kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase"
// +kubebuilder:printcolumn:name="Provider",type="string",JSONPath=".spec.provider.type"

// Memory is the Schema for the memories API.
type Memory struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   MemorySpec   `json:"spec,omitempty"`
	Status MemoryStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// MemoryList contains a list of Memory.
type MemoryList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Memory `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Memory{}, &MemoryList{})
}
