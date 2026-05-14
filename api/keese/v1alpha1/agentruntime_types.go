// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// GooseSpec holds goose-specific configuration.
type GooseSpec struct {
	// Image is the OCI reference for the goose runtime image.
	// In production this must be digest-pinned (rule 05.12).
	// +kubebuilder:validation:MinLength=1
	Image string `json:"image"`

	// ImageTag is informational; admission validates it against SupportedImageVersions.
	// +optional
	ImageTag string `json:"imageTag,omitempty"`

	// MigrationPolicy governs how migration severity gates upgrades.
	// +optional
	MigrationPolicy *GooseMigrationPolicy `json:"migrationPolicy,omitempty"`

	// Sidecars holds optional sidecar configuration.
	// +optional
	Sidecars *GooseSidecars `json:"sidecars,omitempty"`
}

// GooseMigrationPolicy governs upgrade deferral behaviour.
type GooseMigrationPolicy struct {
	// Severity of migration: critical|high|medium|low.
	// +kubebuilder:validation:Enum=critical;high;medium;low
	Severity string `json:"severity"`

	// MaxDeferral is the maximum time this runtime can defer the migration.
	// critical is hard-capped at 1h by the controller.
	// +optional
	MaxDeferral *metav1.Duration `json:"maxDeferral,omitempty"`
}

// GooseSidecars describes optional sidecars the operator injects alongside goose.
type GooseSidecars struct {
	// AcpBridge configures the ACP bridge sidecar. Empty image uses the operator-embedded default digest.
	// +optional
	AcpBridge *AcpBridgeSidecar `json:"acpBridge,omitempty"`
}

// AcpBridgeSidecar configures the ACP bridge sidecar image.
type AcpBridgeSidecar struct {
	// Image is the OCI reference for the ACP bridge sidecar.
	// Empty means use the operator-embedded default digest.
	// +optional
	Image string `json:"image,omitempty"`
}

// ClaudeCodeSpec is a stub; no sub-fields at v1alpha1.
type ClaudeCodeSpec struct{}

// AiderSpec is a stub; no sub-fields at v1alpha1.
type AiderSpec struct{}

// ADKPythonSpec holds configuration for the Google ADK Python runtime provider.
type ADKPythonSpec struct {
	// Image is the OCI reference for the ADK Python runtime image.
	// In production this must be digest-pinned (enforced by VAP adk-runtime-image-digest-pinned).
	// +kubebuilder:validation:MinLength=1
	Image string `json:"image"`

	// PythonVersion is the Python interpreter version to use (e.g. "3.12").
	// +optional
	PythonVersion string `json:"pythonVersion,omitempty"`

	// ADKVersion pins the google-adk package version (e.g. "0.4.0").
	// +optional
	ADKVersion string `json:"adkVersion,omitempty"`

	// SessionStoreRef references a namespace-scoped Secret or ConfigMap that
	// holds the session-store connection parameters for this provider.
	// +optional
	SessionStoreRef *corev1.LocalObjectReference `json:"sessionStoreRef,omitempty"`

	// CompactionInterval controls how often the session store is compacted.
	// +optional
	CompactionInterval *metav1.Duration `json:"compactionInterval,omitempty"`
}

// ADKGoSpec holds configuration for the Google ADK Go runtime provider.
type ADKGoSpec struct {
	// Image is the OCI reference for the ADK Go runtime image.
	// In production this must be digest-pinned (enforced by VAP adk-runtime-image-digest-pinned).
	// +kubebuilder:validation:MinLength=1
	Image string `json:"image"`

	// GoVersion is the Go toolchain version to use (e.g. "1.24").
	// +optional
	GoVersion string `json:"goVersion,omitempty"`

	// ADKVersion pins the google-adk-go module version (e.g. "0.1.0").
	// +optional
	ADKVersion string `json:"adkVersion,omitempty"`

	// SessionStoreRef references a namespace-scoped Secret or ConfigMap that
	// holds the session-store connection parameters for this provider.
	// +optional
	SessionStoreRef *corev1.LocalObjectReference `json:"sessionStoreRef,omitempty"`

	// CompactionInterval controls how often the session store is compacted.
	// +optional
	CompactionInterval *metav1.Duration `json:"compactionInterval,omitempty"`
}

// AgentRuntimeImplementation is the discriminated one-of for the runtime provider.
// CEL XValidation enforces exactly one field is set.
//
// +kubebuilder:validation:XValidation:rule="(has(self.goose) ? 1 : 0) + (has(self.claudeCode) ? 1 : 0) + (has(self.aider) ? 1 : 0) + (has(self.adkPython) ? 1 : 0) + (has(self.adkGo) ? 1 : 0) == 1",message="exactly one of goose, claudeCode, aider, adkPython, or adkGo must be set"
type AgentRuntimeImplementation struct {
	// Goose configures the goose headless runtime provider.
	// +optional
	Goose *GooseSpec `json:"goose,omitempty"`

	// ClaudeCode configures the Claude Code runtime provider (stub at v1alpha1).
	// +optional
	ClaudeCode *ClaudeCodeSpec `json:"claudeCode,omitempty"`

	// Aider configures the aider runtime provider (stub at v1alpha1).
	// +optional
	Aider *AiderSpec `json:"aider,omitempty"`

	// AdkPython configures the Google ADK Python runtime provider.
	// +optional
	AdkPython *ADKPythonSpec `json:"adkPython,omitempty"`

	// AdkGo configures the Google ADK Go runtime provider.
	// +optional
	AdkGo *ADKGoSpec `json:"adkGo,omitempty"`
}

// AgentRuntimeSpec defines the desired state of AgentRuntime.
type AgentRuntimeSpec struct {
	// Implementation selects and configures the runtime provider.
	// Exactly one sub-field must be set (enforced by CEL XValidation).
	Implementation AgentRuntimeImplementation `json:"implementation"`
}

// AgentRuntimePhase is the lifecycle phase of an AgentRuntime.
// +kubebuilder:validation:Enum=Pending;Ready;Degraded;Incompatible
type AgentRuntimePhase string

const (
	// AgentRuntimePhasePending means the runtime has not yet been validated.
	AgentRuntimePhasePending AgentRuntimePhase = "Pending"
	// AgentRuntimePhaseReady means the runtime is registered and healthy.
	AgentRuntimePhaseReady AgentRuntimePhase = "Ready"
	// AgentRuntimePhaseDegraded means the runtime is registered but has errors.
	AgentRuntimePhaseDegraded AgentRuntimePhase = "Degraded"
	// AgentRuntimePhaseIncompatible means the runtime image is outside supported versions.
	AgentRuntimePhaseIncompatible AgentRuntimePhase = "Incompatible"
)

// AgentRuntimeStatus defines the observed state of AgentRuntime.
type AgentRuntimeStatus struct {
	// ObservedGeneration is the .metadata.generation the controller last reconciled.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Phase is the high-level lifecycle phase.
	// +optional
	Phase AgentRuntimePhase `json:"phase,omitempty"`

	// Provider mirrors the detected implementation name (e.g. "goose").
	// +optional
	Provider string `json:"provider,omitempty"`

	// Conditions holds standard meta/v1 condition entries.
	// +optional
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Provider",type=string,JSONPath=`.status.provider`
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// AgentRuntime is the Schema for the agentruntimes API.
// Cluster-scoped; registers a runtime provider implementation.
type AgentRuntime struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   AgentRuntimeSpec   `json:"spec,omitempty"`
	Status AgentRuntimeStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// AgentRuntimeList contains a list of AgentRuntime.
type AgentRuntimeList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []AgentRuntime `json:"items"`
}

func init() {
	SchemeBuilder.Register(&AgentRuntime{}, &AgentRuntimeList{})
}
