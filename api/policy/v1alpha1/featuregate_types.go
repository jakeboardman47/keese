// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// FeatureGateStage carries the lifecycle stage of a feature gate.
// alpha defaults to off; beta defaults to on; ga is unconditional in
// code (the gate is kept as `deprecated` for one minor release for
// backwards compat); deprecated reads emit a Warning event and are
// removed the following minor release. See design 27 §1.
//
// +kubebuilder:validation:Enum=alpha;beta;ga;deprecated
type FeatureGateStage string

const (
	FeatureGateStageAlpha      FeatureGateStage = "alpha"
	FeatureGateStageBeta       FeatureGateStage = "beta"
	FeatureGateStageGA         FeatureGateStage = "ga"
	FeatureGateStageDeprecated FeatureGateStage = "deprecated"
)

// FeatureGateSpec defines the desired state of a FeatureGate.
//
// +kubebuilder:validation:XValidation:rule="self.stage != 'ga' || !has(self.override)",message="override may not be set on a ga-stage gate; the code path is unconditional"
// +kubebuilder:validation:XValidation:rule="self.stage != 'deprecated' || !has(self.override)",message="override may not be set on a deprecated-stage gate; remove the gate or revert promotion"
type FeatureGateSpec struct {
	// Description is a one-line human-readable summary of what the
	// gate controls. Surfaces in `make featuregate-list` and
	// `kubectl describe`.
	//
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=512
	Description string `json:"description"`

	// Stage carries the gate's lifecycle stage; alpha gates default
	// to off, beta to on. See design 27 §1.
	Stage FeatureGateStage `json:"stage"`

	// Override flips the gate's effective value. nil → use the
	// stage's default. true/false → unconditional. Forbidden on
	// stage=ga or stage=deprecated (the in-process eval ignores
	// the gate either way; the CRD enforces this via XValidation).
	//
	// +optional
	Override *bool `json:"override,omitempty"`

	// Owners lists the binaries that consume this gate. Used by the
	// controller to scope drift alerts and by `featuregate-list` to
	// show consumers. Free-form; convention is the binary's image
	// name (e.g. "keese-cosign-webhook", "keese-controller-manager").
	//
	// +optional
	// +kubebuilder:validation:MaxItems=32
	Owners []string `json:"owners,omitempty"`

	// RestartRequired marks gates whose flip cannot take effect
	// mid-process — webhook re-registration, leader election,
	// listener port changes. The controller emits a
	// Event(Reason=RestartRequired) on transition; we do not
	// auto-restart (rule 06 — process lifecycle is operator-controlled).
	//
	// +kubebuilder:default=false
	// +optional
	RestartRequired bool `json:"restartRequired,omitempty"`
}

// FeatureGateStatus reports the projected effective state.
type FeatureGateStatus struct {
	// ObservedGeneration is the last spec generation reconciled.
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Effective is the value actually projected into the
	// keese-features ConfigMap: spec.override ?? defaultFor(stage).
	// +optional
	Effective bool `json:"effective,omitempty"`

	// Consumers is a rolling-window list of binaries that have read
	// the gate at least once recently. Populated from
	// OpenFeature-hook telemetry sent back via the controller's
	// counter. Bounded to 32 entries.
	//
	// +optional
	// +kubebuilder:validation:MaxItems=32
	Consumers []string `json:"consumers,omitempty"`

	// LastTransitionTime is the time the effective value last
	// changed.
	// +optional
	LastTransitionTime *metav1.Time `json:"lastTransitionTime,omitempty"`

	// Conditions holds the standard Kubernetes condition list.
	// Types: Ready, RestartRequired.
	// +optional
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster,shortName=fg
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:printcolumn:name="Stage",type="string",JSONPath=".spec.stage"
// +kubebuilder:printcolumn:name="Effective",type="boolean",JSONPath=".status.effective"
// +kubebuilder:printcolumn:name="Override",type="boolean",JSONPath=".spec.override"
// +kubebuilder:printcolumn:name="Restart",type="boolean",JSONPath=".spec.restartRequired"

// FeatureGate is the Schema for the featuregates API. See design 27.
type FeatureGate struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   FeatureGateSpec   `json:"spec,omitempty"`
	Status FeatureGateStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// FeatureGateList contains a list of FeatureGate.
type FeatureGateList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []FeatureGate `json:"items"`
}

func init() {
	SchemeBuilder.Register(&FeatureGate{}, &FeatureGateList{})
}

// DefaultEffective returns the gate's default effective value
// derived from its stage, ignoring spec.override. Callers compose
// the final value as `override ?? DefaultEffective(stage)`.
func DefaultEffective(stage FeatureGateStage) bool {
	switch stage {
	case FeatureGateStageBeta, FeatureGateStageGA:
		return true
	default:
		// alpha + deprecated default to off; deprecated paths emit
		// a Warning event when read.
		return false
	}
}
