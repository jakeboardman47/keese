// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ModelProviderType enumerates the model endpoints keese can broker through the
// Envoy AI Gateway. The value selects the upstream-specific credential-injection
// and (when enabled) model-discovery behaviour.
// +kubebuilder:validation:Enum=openai;anthropic;gemini;geminiVertex;anthropicVertex;bedrock;azureOpenAI;ollama;sapAICore
type ModelProviderType string

const (
	ModelProviderOpenAI          ModelProviderType = "openai"
	ModelProviderAnthropic       ModelProviderType = "anthropic"
	ModelProviderGemini          ModelProviderType = "gemini"
	ModelProviderGeminiVertex    ModelProviderType = "geminiVertex"
	ModelProviderAnthropicVertex ModelProviderType = "anthropicVertex"
	ModelProviderBedrock         ModelProviderType = "bedrock"
	ModelProviderAzureOpenAI     ModelProviderType = "azureOpenAI"
	ModelProviderOllama          ModelProviderType = "ollama"
	ModelProviderSAPAICore       ModelProviderType = "sapAICore"
)

// ModelProviderPhase is the lifecycle phase of a ModelProvider resource.
// +kubebuilder:validation:Enum=Pending;Validating;Ready;Degraded;Terminating
type ModelProviderPhase string

const (
	ModelProviderPhasePending     ModelProviderPhase = "Pending"
	ModelProviderPhaseValidating  ModelProviderPhase = "Validating"
	ModelProviderPhaseReady       ModelProviderPhase = "Ready"
	ModelProviderPhaseDegraded    ModelProviderPhase = "Degraded"
	ModelProviderPhaseTerminating ModelProviderPhase = "Terminating"
)

// ModelProviderSpec defines the desired state of a ModelProvider: a model
// endpoint plus the source of the upstream credential, decoupled from any
// Recipe. Credentials are NEVER inlined here — only referenced by name
// (rule 05.7); the named Secret is projected as a file at the gateway, never
// mounted as an env var on an agent pod.
//
// +kubebuilder:validation:XValidation:rule="oldSelf.provider == self.provider",message="spec.provider is immutable after creation (ModelProviderTypeImmutable)"
// +kubebuilder:validation:XValidation:rule="self.provider != 'ollama' || has(self.endpoint)",message="spec.endpoint is required when spec.provider=ollama (OllamaEndpointRequired)"
type ModelProviderSpec struct {
	// provider selects the upstream model endpoint family. Immutable after
	// creation — re-pointing a provider at a different upstream would silently
	// change egress credential selection, so callers create a new ModelProvider
	// instead.
	// +kubebuilder:validation:Required
	Provider ModelProviderType `json:"provider"`

	// endpoint is the base URL of the model API. Optional for providers with a
	// well-known default (openai, anthropic, gemini); required for ollama, which
	// is typically in-cluster (e.g. http://ollama.keese-system:11434), and for
	// sapAICore, whose endpoint is tenant-specific.
	// +kubebuilder:validation:Pattern=`^https?://.+`
	// +optional
	Endpoint string `json:"endpoint,omitempty"`

	// credentialSecretRef names a Secret (in this ModelProvider's namespace)
	// holding the upstream credential. The Secret is populated from OpenBao via
	// ExternalSecrets and is consumed by the gateway BackendSecurityPolicy as a
	// PROJECTED FILE, never as an env var (rule 05.7). The credential value is
	// never inlined in this spec. Optional for providers that authenticate via a
	// workload identity / STS exchange at the gateway (e.g. bedrock, geminiVertex)
	// rather than a static secret.
	//
	// Selecting which upstream credential the gateway injects for a workload is
	// an authorization decision; the reconciler records the binding as a ReBAC
	// tuple so ext_authz can gate it.
	// +keese:rebac-tuple=modelprovider.credential
	// +optional
	CredentialSecretRef *corev1.LocalObjectReference `json:"credentialSecretRef,omitempty"`

	// discoveryEnabled turns on periodic polling of the provider's model-list
	// endpoint. Results land in status.availableModels. Gated off by default so
	// keese never hits a provider rate limit unbidden.
	// +kubebuilder:default=false
	// +optional
	DiscoveryEnabled bool `json:"discoveryEnabled,omitempty"`

	// discoveryInterval is the polling period when discoveryEnabled is true.
	// The reconciler backs off exponentially (2x, capped at 30m) on 429.
	// +kubebuilder:default="1h"
	// +kubebuilder:validation:Pattern=`^([0-9]+(s|m|h))+$`
	// +optional
	DiscoveryInterval string `json:"discoveryInterval,omitempty"`

	// model is an optional default model identifier, used when a downstream
	// BackendSecurityPolicy needs a pinned default for this provider.
	// +optional
	Model string `json:"model,omitempty"`
}

// ModelProviderStatus defines the observed state of a ModelProvider.
type ModelProviderStatus struct {
	// observedGeneration is the .metadata.generation the controller last reconciled.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// phase is the current lifecycle phase.
	// +optional
	Phase ModelProviderPhase `json:"phase,omitempty"`

	// conditions holds standard Kubernetes status conditions. The Synced
	// condition carries the discovery poll outcome and timing.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// availableModels is the model-list last fetched from the provider when
	// discoveryEnabled is true. Empty when discovery is off or has not yet run.
	// +optional
	AvailableModels []string `json:"availableModels,omitempty"`

	// lastDiscoveryTime is the timestamp of the last successful model-list poll.
	// +optional
	LastDiscoveryTime *metav1.Time `json:"lastDiscoveryTime,omitempty"`
}

// Condition type constants for ModelProvider.
const (
	// ModelProviderConditionReady is true when the provider config is valid and,
	// if discovery is enabled, the model list has been fetched at least once.
	ModelProviderConditionReady = "Ready"
	// ModelProviderConditionSynced reflects the most recent discovery poll.
	ModelProviderConditionSynced = "Synced"
)

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,shortName=mp,categories=keese
// +kubebuilder:printcolumn:name="Provider",type="string",JSONPath=".spec.provider"
// +kubebuilder:printcolumn:name="Ready",type="string",JSONPath=".status.conditions[?(@.type=='Ready')].status"
// +kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase"
// +kubebuilder:printcolumn:name="Models",type="integer",JSONPath=".status.availableModels.length()"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// ModelProvider is the Schema for the modelproviders API. It declares a model
// endpoint and credential source, decoupled from Recipe.
type ModelProvider struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ModelProviderSpec   `json:"spec,omitempty"`
	Status ModelProviderStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ModelProviderList contains a list of ModelProvider.
type ModelProviderList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ModelProvider `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ModelProvider{}, &ModelProviderList{})
}
