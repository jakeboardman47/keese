// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// WorkflowPhase represents the lifecycle phase of a Workflow.
// +kubebuilder:validation:Enum=Pending;Projecting;Ready;Degraded;Deleting
type WorkflowPhase string

const (
	WorkflowPhasePending    WorkflowPhase = "Pending"
	WorkflowPhaseProjecting WorkflowPhase = "Projecting"
	WorkflowPhaseReady      WorkflowPhase = "Ready"
	WorkflowPhaseDegraded   WorkflowPhase = "Degraded"
	WorkflowPhaseDeleting   WorkflowPhase = "Deleting"
)

// TriggerType discriminates which trigger projection to use.
// +kubebuilder:validation:Enum=Cron;KnativeTrigger;NATSSubscription;HTTPWebhook
type TriggerType string

const (
	TriggerTypeCron             TriggerType = "Cron"
	TriggerTypeKnativeTrigger   TriggerType = "KnativeTrigger"
	TriggerTypeNATSSubscription TriggerType = "NATSSubscription"
	TriggerTypeHTTPWebhook      TriggerType = "HTTPWebhook"
)

// OutputType discriminates which output projection to use.
// +kubebuilder:validation:Enum=KnativeSink;NATSPublish;S3;GitHubPR
type OutputType string

const (
	OutputTypeKnativeSink OutputType = "KnativeSink"
	OutputTypeNATSPublish OutputType = "NATSPublish"
	OutputTypeS3          OutputType = "S3"
	OutputTypeGitHubPR    OutputType = "GitHubPR"
)

// ConcurrencyPolicy controls behaviour when a new WorkflowRun arrives while one is active.
// +kubebuilder:validation:Enum=Allow;Forbid;Replace
type ConcurrencyPolicy string

const (
	ConcurrencyPolicyAllow   ConcurrencyPolicy = "Allow"
	ConcurrencyPolicyForbid  ConcurrencyPolicy = "Forbid"
	ConcurrencyPolicyReplace ConcurrencyPolicy = "Replace"
)

// WorkflowTemplateStep is one step inside a WorkflowTemplate.
// It maps 1:1 onto an Argo WorkflowTemplate step.
type WorkflowTemplateStep struct {
	// Name is a unique identifier for this step within the template.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=63
	Name string `json:"name"`

	// Image is the container image to run for this step.
	// +kubebuilder:validation:MinLength=1
	Image string `json:"image"`

	// Command overrides the container entrypoint.
	// +optional
	Command []string `json:"command,omitempty"`

	// Args supplies arguments to the container.
	// +optional
	Args []string `json:"args,omitempty"`

	// TransportRef names a Transport CR in the same namespace that
	// provides the egress channel for this step.
	// +keese:rebac-tuple=workflow.step.transport
	// +optional
	TransportRef *LocalObjectReference `json:"transportRef,omitempty"`

	// GuardrailBindingRefs names GuardrailBinding CRs applied to this step.
	// +keese:rebac-tuple=workflow.step.guardrail
	// +optional
	GuardrailBindingRefs []LocalObjectReference `json:"guardrailBindingRefs,omitempty"`

	// RetryLimit caps the number of retries for this step.
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=10
	// +kubebuilder:default=3
	// +optional
	RetryLimit int32 `json:"retryLimit,omitempty"`
}

// LocalObjectReference is a reference to an object in the same namespace.
type LocalObjectReference struct {
	// Name of the referenced object.
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
}

// CronTrigger configures a CronJob-based trigger.
type CronTrigger struct {
	// Schedule is a cron expression (e.g. "0 * * * *").
	// +kubebuilder:validation:MinLength=1
	Schedule string `json:"schedule"`

	// Timezone in IANA format (e.g. "UTC", "America/New_York").
	// +optional
	Timezone string `json:"timezone,omitempty"`

	// Suspend disables the CronJob without removing it.
	// +optional
	Suspend bool `json:"suspend,omitempty"`
}

// KnativeTriggerConfig configures a Knative Eventing Trigger.
type KnativeTriggerConfig struct {
	// BrokerRef names the Knative Broker in the same namespace.
	// +kubebuilder:validation:MinLength=1
	BrokerRef string `json:"brokerRef"`

	// Filter is a CloudEvent attribute filter (optional).
	// +optional
	Filter map[string]string `json:"filter,omitempty"`
}

// NATSSubscriptionConfig configures a NATS JetStream consumer trigger.
type NATSSubscriptionConfig struct {
	// StreamName is the JetStream stream to consume from.
	// +kubebuilder:validation:MinLength=1
	StreamName string `json:"streamName"`

	// Subject is the NATS subject pattern.
	// +kubebuilder:validation:MinLength=1
	Subject string `json:"subject"`

	// Durable names the durable consumer.
	// +optional
	Durable string `json:"durable,omitempty"`
}

// HTTPWebhookConfig configures an HTTPRoute-based webhook trigger.
type HTTPWebhookConfig struct {
	// Path is the HTTP path that will activate this workflow (e.g. "/trigger").
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:Pattern=^/
	Path string `json:"path"`

	// SecretRef names a Secret containing the HMAC shared key for webhook verification.
	// +optional
	SecretRef *LocalObjectReference `json:"secretRef,omitempty"`
}

// WorkflowTrigger is a discriminated one-of trigger configuration.
// Exactly one of Cron / KnativeTrigger / NATSSubscription / HTTPWebhook must be set.
// +kubebuilder:validation:XValidation:rule="(has(self.cron) ? 1 : 0) + (has(self.knativeTrigger) ? 1 : 0) + (has(self.natsSubscription) ? 1 : 0) + (has(self.httpWebhook) ? 1 : 0) == 1",message="exactly one trigger variant must be set"
type WorkflowTrigger struct {
	// Type discriminates which variant is populated.
	// +kubebuilder:validation:Required
	Type TriggerType `json:"type"`

	// Cron configures a CronJob-based trigger.
	// +optional
	Cron *CronTrigger `json:"cron,omitempty"`

	// KnativeTrigger configures a Knative Eventing Trigger.
	// +optional
	KnativeTrigger *KnativeTriggerConfig `json:"knativeTrigger,omitempty"`

	// NATSSubscription configures a NATS JetStream trigger.
	// +optional
	NATSSubscription *NATSSubscriptionConfig `json:"natsSubscription,omitempty"`

	// HTTPWebhook configures an HTTPRoute-based webhook trigger.
	// +optional
	HTTPWebhook *HTTPWebhookConfig `json:"httpWebhook,omitempty"`
}

// KnativeSinkOutput names a Knative Addressable sink for workflow output.
type KnativeSinkOutput struct {
	// Ref is a reference to a Knative Addressable (Broker, Channel, Service).
	// +kubebuilder:validation:MinLength=1
	Ref string `json:"ref"`
}

// NATSPublishOutput configures NATS JetStream publish for output.
type NATSPublishOutput struct {
	// Subject is the NATS subject to publish to.
	// +kubebuilder:validation:MinLength=1
	Subject string `json:"subject"`
}

// S3Output configures S3 artifact storage for output.
type S3Output struct {
	// Bucket is the S3 bucket name.
	// +kubebuilder:validation:MinLength=1
	Bucket string `json:"bucket"`

	// KeyPrefix is the object key prefix.
	// +optional
	KeyPrefix string `json:"keyPrefix,omitempty"`

	// CredentialsSecretRef names a Secret with S3 credentials.
	// +optional
	CredentialsSecretRef *LocalObjectReference `json:"credentialsSecretRef,omitempty"`
}

// GitHubPROutput configures a GitHub PR creation for output.
type GitHubPROutput struct {
	// Repo is the GitHub repository in "owner/name" format.
	// +kubebuilder:validation:MinLength=1
	Repo string `json:"repo"`

	// TokenSecretRef names a Secret containing the GitHub token.
	// +kubebuilder:validation:Required
	TokenSecretRef LocalObjectReference `json:"tokenSecretRef"`
}

// WorkflowOutput is a discriminated one-of output configuration.
// +kubebuilder:validation:XValidation:rule="(has(self.knativeSink) ? 1 : 0) + (has(self.natsPublish) ? 1 : 0) + (has(self.s3) ? 1 : 0) + (has(self.githubPR) ? 1 : 0) == 1",message="exactly one output variant must be set"
type WorkflowOutput struct {
	// Name is a unique identifier for this output.
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`

	// Type discriminates which variant is populated.
	// +kubebuilder:validation:Required
	Type OutputType `json:"type"`

	// KnativeSink configures a Knative sink output.
	// +optional
	KnativeSink *KnativeSinkOutput `json:"knativeSink,omitempty"`

	// NATSPublish configures a NATS publish output.
	// +optional
	NATSPublish *NATSPublishOutput `json:"natsPublish,omitempty"`

	// S3 configures S3 artifact output.
	// +optional
	S3 *S3Output `json:"s3,omitempty"`

	// GitHubPR configures a GitHub PR output.
	// +optional
	GitHubPR *GitHubPROutput `json:"githubPR,omitempty"`
}

// RetryBudget configures retry limits for workflow steps.
type RetryBudget struct {
	// Limit is the maximum number of step retries across the whole workflow.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=50
	// +kubebuilder:default=10
	Limit int32 `json:"limit"`

	// BackoffSeconds is the base back-off between retries.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=1000
	// +kubebuilder:default=10
	// +optional
	BackoffSeconds int32 `json:"backoffSeconds,omitempty"`
}

// WorkflowSpec defines the desired state of Workflow.
// +kubebuilder:validation:XValidation:rule="size(self.templates) >= 1",message="spec.templates must have at least one entry"
type WorkflowSpec struct {
	// Entrypoint names the template to run first.
	// +kubebuilder:validation:MinLength=1
	Entrypoint string `json:"entrypoint"`

	// Templates is the ordered list of step templates.
	// Must contain at least one entry.
	// +kubebuilder:validation:MinItems=1
	Templates []WorkflowTemplateStep `json:"templates"`

	// Triggers declares how this Workflow is activated.
	// +optional
	Triggers []WorkflowTrigger `json:"triggers,omitempty"`

	// Outputs declares where results are sent after completion.
	// +optional
	Outputs []WorkflowOutput `json:"outputs,omitempty"`

	// DefaultRetryBudget applies to all steps unless overridden per-step.
	// +optional
	DefaultRetryBudget *RetryBudget `json:"defaultRetryBudget,omitempty"`

	// ArtifactStoreRef names an artifact backend Secret/ConfigMap.
	// Falls back to Tenant.spec.artifactStoreRef when absent.
	// +optional
	ArtifactStoreRef *LocalObjectReference `json:"artifactStoreRef,omitempty"`

	// ConcurrencyPolicy controls what happens when a new WorkflowRun
	// arrives while an existing run is active.
	// +kubebuilder:default=Allow
	// +optional
	ConcurrencyPolicy ConcurrencyPolicy `json:"concurrencyPolicy,omitempty"`
}

// WorkflowStatus defines the observed state of Workflow.
type WorkflowStatus struct {
	// ObservedGeneration is the .metadata.generation last fully reconciled.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Phase summarises the current lifecycle state.
	// +optional
	Phase WorkflowPhase `json:"phase,omitempty"`

	// Conditions holds standard status conditions.
	// +optional
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// WorkflowTemplateRef is the name of the projected Argo WorkflowTemplate.
	// +optional
	WorkflowTemplateRef string `json:"workflowTemplateRef,omitempty"`

	// RunCount is the total number of WorkflowRuns created against this Workflow.
	// +optional
	RunCount int64 `json:"runCount,omitempty"`

	// TupleCount records the number of OpenFGA tuples last written.
	// +optional
	TupleCount int32 `json:"tupleCount,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,shortName=wf
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:printcolumn:name="Ready",type="string",JSONPath=".status.conditions[?(@.type=='Ready')].status"
// +kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase"
// +kubebuilder:printcolumn:name="RunCount",type="integer",JSONPath=".status.runCount"

// Workflow is the Schema for the workflows API.
// It projects an Argo WorkflowTemplate and manages triggers and outputs.
type Workflow struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// +kubebuilder:validation:Required
	Spec   WorkflowSpec   `json:"spec,omitempty"`
	Status WorkflowStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// WorkflowList contains a list of Workflow.
type WorkflowList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Workflow `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Workflow{}, &WorkflowList{})
}
