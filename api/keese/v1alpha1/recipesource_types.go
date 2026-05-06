// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// RecipeSourcePhase is the lifecycle phase of a RecipeSource.
// +kubebuilder:validation:Enum=Pending;Synced;Failed
type RecipeSourcePhase string

const (
	RecipeSourcePhasePending RecipeSourcePhase = "Pending"
	RecipeSourcePhaseSynced  RecipeSourcePhase = "Synced"
	RecipeSourcePhaseFailed  RecipeSourcePhase = "Failed"
)

// RecipeSourceType identifies which source type is active.
// +kubebuilder:validation:Enum=OCI;Git;ConfigMap
type RecipeSourceType string

const (
	RecipeSourceTypeOCI       RecipeSourceType = "OCI"
	RecipeSourceTypeGit       RecipeSourceType = "Git"
	RecipeSourceTypeConfigMap RecipeSourceType = "ConfigMap"
)

// OCISource specifies an OCI artifact source.
type OCISource struct {
	// Registry is the OCI registry hostname, e.g. "ghcr.io".
	// +kubebuilder:validation:MinLength=1
	Registry string `json:"registry"`

	// Repository is the image repository path.
	// +kubebuilder:validation:MinLength=1
	Repository string `json:"repository"`

	// Tag is the image tag (dev only; digest takes precedence).
	// +optional
	Tag string `json:"tag,omitempty"`

	// Digest is the immutable content-addressable digest, e.g. "sha256:abc123...".
	// Required in non-dev namespaces (VAP rule).
	// +optional
	Digest string `json:"digest,omitempty"`

	// SecretRef references a Secret containing OCI pull credentials.
	// The secret is mounted as a projected file (rule 05.7).
	// +optional
	SecretRef *corev1.LocalObjectReference `json:"secretRef,omitempty"`
}

// GitSource specifies a git repository source.
type GitSource struct {
	// URL is the git repository URL.
	// +kubebuilder:validation:MinLength=1
	URL string `json:"url"`

	// Revision is the full 40-character commit SHA.
	// +kubebuilder:validation:Pattern=`^[0-9a-f]{40}$`
	Revision string `json:"revision"`

	// SecretRef references a Secret containing git credentials.
	// +optional
	SecretRef *corev1.LocalObjectReference `json:"secretRef,omitempty"`
}

// ConfigMapSource specifies an inline ConfigMap source (dev only).
// VAP rejects this source type in namespaces that do not have label keese.ai/env=dev.
type ConfigMapSource struct {
	// Name is the ConfigMap name.
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`

	// Namespace is the ConfigMap namespace.
	// +kubebuilder:validation:MinLength=1
	Namespace string `json:"namespace"`
}

// RecipeSourceSpec defines the desired state of RecipeSource.
//
// Exactly one of oci, git, or configMap must be set (discriminated one-of).
//
// +kubebuilder:validation:XValidation:rule="(has(self.oci) ? 1 : 0) + (has(self.git) ? 1 : 0) + (has(self.configMap) ? 1 : 0) == 1",message="exactly one of oci, git, or configMap must be set"
type RecipeSourceSpec struct {
	// OCI specifies an OCI artifact source (preferred in production).
	// +optional
	OCI *OCISource `json:"oci,omitempty"`

	// Git specifies a git repository source with a pinned commit SHA.
	// +optional
	Git *GitSource `json:"git,omitempty"`

	// ConfigMap specifies an inline ConfigMap source (dev namespaces only).
	// +optional
	ConfigMap *ConfigMapSource `json:"configMap,omitempty"`
}

// RecipeSourceStatus defines the observed state of RecipeSource.
type RecipeSourceStatus struct {
	// ObservedGeneration is the .metadata.generation last reconciled.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Phase is the current lifecycle phase.
	// +optional
	Phase RecipeSourcePhase `json:"phase,omitempty"`

	// SourceType records which source type is active.
	// +optional
	SourceType RecipeSourceType `json:"sourceType,omitempty"`

	// ResolvedDigest is the content-addressable digest of the cached artifact.
	// Written after cosign verification succeeds.
	// +optional
	ResolvedDigest string `json:"resolvedDigest,omitempty"`

	// LastVerifiedTime is the timestamp of the most recent successful cosign verification.
	// +optional
	LastVerifiedTime *metav1.Time `json:"lastVerifiedTime,omitempty"`

	// Cached is true when the artifact has been written to the cluster-internal cache.
	// +optional
	Cached bool `json:"cached,omitempty"`

	// Conditions contains detailed status conditions.
	// +optional
	// +listType=map
	// +listMapKey=type
	// +patchStrategy=merge
	// +patchMergeKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type"`
}

const (
	// RecipeSourceConditionReady is true when the source is cached and verified.
	RecipeSourceConditionReady = "Ready"
	// RecipeSourceConditionProgressing is true while the controller is pulling.
	RecipeSourceConditionProgressing = "Progressing"
)

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=".metadata.creationTimestamp"
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=".status.conditions[?(@.type=='Ready')].status"
// +kubebuilder:printcolumn:name="Type",type=string,JSONPath=".status.sourceType"

// RecipeSource is the Schema for the recipesources API.
type RecipeSource struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   RecipeSourceSpec   `json:"spec,omitempty"`
	Status RecipeSourceStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// RecipeSourceList contains a list of RecipeSource.
type RecipeSourceList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []RecipeSource `json:"items"`
}

func init() {
	SchemeBuilder.Register(&RecipeSource{}, &RecipeSourceList{})
}
