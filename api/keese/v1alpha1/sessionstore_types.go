// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// SessionStoreBackendType discriminates the active SessionStore backend.
// +kubebuilder:validation:Enum=postgres;sqlite
type SessionStoreBackendType string

const (
	// SessionStoreBackendPostgres persists session + event rows in PostgreSQL,
	// with row-level security on tenant_id for DB-layer tenant isolation.
	SessionStoreBackendPostgres SessionStoreBackendType = "postgres"
	// SessionStoreBackendSQLite persists session + event rows in a SQLite file on
	// an existing PVC. Single-replica only (RWO PVC).
	SessionStoreBackendSQLite SessionStoreBackendType = "sqlite"
)

// SessionStorePhase is the lifecycle phase of a SessionStore resource.
// +kubebuilder:validation:Enum=Pending;Migrating;Ready;Degraded;Terminating
type SessionStorePhase string

const (
	SessionStorePhasePending     SessionStorePhase = "Pending"
	SessionStorePhaseMigrating   SessionStorePhase = "Migrating"
	SessionStorePhaseReady       SessionStorePhase = "Ready"
	SessionStorePhaseDegraded    SessionStorePhase = "Degraded"
	SessionStorePhaseTerminating SessionStorePhase = "Terminating"
)

// PostgresSessionBackend configures the PostgreSQL session store.
//
// The connection string is NEVER inlined in this spec. It is referenced by name
// only (rule 05.7): the named Secret is populated from OpenBao via
// ExternalSecrets and is consumed by the session-store adapter sidecar as a
// PROJECTED FILE at /var/run/keese/secrets/dsn — never as an env var.
type PostgresSessionBackend struct {
	// dsnSecretRef names a Secret (in this SessionStore's namespace) whose value
	// holds the PostgreSQL DSN / connection string. Mounted as a projected file at
	// /var/run/keese/secrets/dsn, never as an env var (rule 05.7). The DSN must
	// name a NON-superuser role so RLS cannot be bypassed; admission cannot enforce
	// this (see plan E8 risk table).
	// +kubebuilder:validation:Required
	DSNSecretRef corev1.LocalObjectReference `json:"dsnSecretRef"`

	// maxConnections bounds the adapter's connection pool to this store.
	// +kubebuilder:default=10
	// +kubebuilder:validation:Minimum=1
	// +optional
	MaxConnections *int32 `json:"maxConnections,omitempty"`

	// sslMode is the libpq sslmode for the connection. Defaults to require so the
	// DSN cannot silently fall back to plaintext.
	// +kubebuilder:default=require
	// +kubebuilder:validation:Enum=disable;allow;prefer;require;verify-ca;verify-full
	// +optional
	SSLMode string `json:"sslMode,omitempty"`
}

// SQLiteSessionBackend configures the SQLite-on-PVC session store. Single-replica
// only — the RWO PVC cannot be mounted by more than one writer; the controller
// emits Degraded if a consumer requests > 1 replica.
type SQLiteSessionBackend struct {
	// pvcRef names an EXISTING PersistentVolumeClaim (in this SessionStore's
	// namespace) holding the SQLite database file. The store does not provision the
	// PVC; it must already exist.
	// +kubebuilder:validation:Required
	PVCRef corev1.LocalObjectReference `json:"pvcRef"`

	// dbPath is the SQLite file path relative to the PVC mount root.
	// +kubebuilder:default="sessions.db"
	// +kubebuilder:validation:MinLength=1
	// +optional
	DBPath string `json:"dbPath,omitempty"`
}

// SessionStoreSpec is a discriminated one-of over the supported session-store
// backends. Exactly one backend sub-field must be set; enforced by the CEL
// XValidation rule below (mirrors Memory.spec.provider / Transport.spec.type,
// rule 04.6) — NOT a flat struct.
//
// +kubebuilder:validation:XValidation:rule="[has(self.postgres),has(self.sqlite)].exists_one(x,x)",message="exactly one of postgres|sqlite must be set (SessionStoreOneBackend)"
// +kubebuilder:validation:XValidation:rule="self.type != 'postgres' || has(self.postgres)",message="spec.postgres is required when spec.type=postgres"
// +kubebuilder:validation:XValidation:rule="self.type != 'sqlite' || has(self.sqlite)",message="spec.sqlite is required when spec.type=sqlite"
type SessionStoreSpec struct {
	// workspaceRef is the owning Workspace name in the same namespace. Binding a
	// session store to a workspace is an authorization decision — the reconciler
	// records it as a ReBAC tuple so ext_authz can gate session reads/writes. The
	// OpenFGA `sessionstore` type/relation does not yet exist in the model; the
	// reconciler uses a no-op writer until rebac-modeler lands the relation
	// (see SUMMARY revisit trigger).
	// +keese:rebac-tuple=sessionstore.workspace
	// +kubebuilder:validation:MinLength=1
	WorkspaceRef string `json:"workspaceRef"`

	// type discriminates the active backend sub-field.
	// +kubebuilder:validation:Required
	Type SessionStoreBackendType `json:"type"`

	// postgres configures the PostgreSQL backend (with RLS on tenant_id).
	// +optional
	Postgres *PostgresSessionBackend `json:"postgres,omitempty"`

	// sqlite configures the SQLite-on-PVC backend.
	// +optional
	SQLite *SQLiteSessionBackend `json:"sqlite,omitempty"`
}

// SessionStoreStatus defines the observed state of a SessionStore.
type SessionStoreStatus struct {
	// observedGeneration is the .metadata.generation the controller last reconciled.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// phase is the current lifecycle phase.
	// +optional
	Phase SessionStorePhase `json:"phase,omitempty"`

	// conditions holds standard Kubernetes status conditions.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// migrationVersion is the schema version last applied to the backend. Gates the
	// PG migration: the reconciler skips the (idempotent) migration when this is
	// already current, so migration is not re-run on every reconcile.
	// +optional
	MigrationVersion string `json:"migrationVersion,omitempty"`
}

// Condition type constants for SessionStore.
const (
	// SessionStoreConditionReady is true when the backend is reachable/validated
	// and (for postgres) the schema migration has been applied.
	SessionStoreConditionReady = "Ready"
)

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,shortName=ss,categories=keese
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:printcolumn:name="Ready",type="string",JSONPath=".status.conditions[?(@.type=='Ready')].status"
// +kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase"
// +kubebuilder:printcolumn:name="Backend",type="string",JSONPath=".spec.type"

// SessionStore is the Schema for the sessionstores API. It declares where ADK /
// goose runtimes persist structured session + event logs (postgres or sqlite).
type SessionStore struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   SessionStoreSpec   `json:"spec,omitempty"`
	Status SessionStoreStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// SessionStoreList contains a list of SessionStore.
type SessionStoreList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []SessionStore `json:"items"`
}

func init() {
	SchemeBuilder.Register(&SessionStore{}, &SessionStoreList{})
}
