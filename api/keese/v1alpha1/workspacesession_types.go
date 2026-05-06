// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// WorkspaceSessionPhase represents the lifecycle phase of a WorkspaceSession.
// +kubebuilder:validation:Enum=Pending;Attaching;Active;Draining;Evicted;Terminating
type WorkspaceSessionPhase string

const (
	WorkspaceSessionPhasePending     WorkspaceSessionPhase = "Pending"
	WorkspaceSessionPhaseAttaching   WorkspaceSessionPhase = "Attaching"
	WorkspaceSessionPhaseActive      WorkspaceSessionPhase = "Active"
	WorkspaceSessionPhaseDraining    WorkspaceSessionPhase = "Draining"
	WorkspaceSessionPhaseEvicted     WorkspaceSessionPhase = "Evicted"
	WorkspaceSessionPhaseTerminating WorkspaceSessionPhase = "Terminating"
)

// SessionMode controls the pod-sharing model for this session.
// +kubebuilder:validation:Enum=shared;per-user;per-attach
type SessionMode string

const (
	SessionModeShared    SessionMode = "shared"
	SessionModePerUser   SessionMode = "per-user"
	SessionModePerAttach SessionMode = "per-attach"
)

// PodRef is a reference to a Pod by name and UID.
type PodRef struct {
	// Name of the Pod.
	Name string `json:"name"`

	// UID of the Pod (used for event correlation).
	// +optional
	UID string `json:"uid,omitempty"`
}

// TokenBudgetRef is a reference to a TokenBudget by name (within the same namespace).
type TokenBudgetRef struct {
	// Name of the TokenBudget.
	Name string `json:"name"`
}

// WorkspaceSessionSpec defines the desired state of WorkspaceSession.
//
// Name pattern: <workspace>-<subject-hash-16>-<session-name>.
//
// Finalizer:
//   - finalizers.workspacesession.operator.keese.ai/cleanup
//     Steps: Draining → AgentRuntime.Drain (90s) → delete Pod → remove OpenFGA tuples → remove finalizer.
//
// SSA fieldOwner: keese-workspacesession-controller
//
// Immutable fields (VAP on UPDATE):
//
//	workspaceRef, attachSubject, sessionName, mode
//
// VAP on CREATE checks (from design 08b-ii):
//
//	AttachNotAllowedOnNonInteractiveWorkspace, DuplicateSession,
//	AttachSessionNameForbidden, AttachGraceOutOfBounds,
//	SessionsPerUserLimitExceeded, ConcurrentAttachLimitExceeded
//
// Range [0,86400] is enforced per-field via +kubebuilder:validation:Minimum/Maximum markers.
// Any spec-level CEL rule on optional int32 fields must use !has(self.field) guards to
// avoid "no such key" errors at CRD install time when the field is absent (omitempty).
type WorkspaceSessionSpec struct {
	// WorkspaceRef is the name of the parent Workspace in the same namespace.
	// The Workspace must have spec.interactive: true (VAP-enforced).
	// Immutable after creation.
	//
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="workspaceRef is immutable (WorkspaceRefImmutable)"
	WorkspaceRef string `json:"workspaceRef"`

	// AttachSubject is the OpenFGA subject form for the attaching identity, e.g. "user:alice@example.com".
	// Immutable after creation. The controller writes session:<uid>#attached_by@<attachSubject>
	// on Active transition and removes it on Terminating.
	//
	// +keese:rebac-tuple=session.attached_by
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="attachSubject is immutable (AttachSubjectImmutable)"
	AttachSubject string `json:"attachSubject"`

	// SessionName is the user-visible session identifier. Defaults to "default".
	// Immutable after creation.
	//
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:default=default
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="sessionName is immutable (SessionNameImmutable)"
	SessionName string `json:"sessionName"`

	// Mode controls the pod-sharing model: shared, per-user, or per-attach.
	// Inherited from Workspace.spec.sessionMode. Immutable after creation.
	//
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="mode is immutable (SessionModeImmutable)"
	Mode SessionMode `json:"mode"`

	// AttachGraceSeconds is the idle-before-eviction timeout. Mutable. Range [0,86400].
	// Inherited from Workspace.spec.attachGrace when not set.
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=86400
	// +optional
	AttachGraceSeconds int32 `json:"attachGraceSeconds,omitempty"`

	// PreserveOnPodFailure keeps the CR alive in a Failed state when the pod crashes,
	// requiring a manual delete. Mutable.
	// +optional
	PreserveOnPodFailure bool `json:"preserveOnPodFailure,omitempty"`
}

// WorkspaceSessionStatus defines the observed state of WorkspaceSession.
type WorkspaceSessionStatus struct {
	// ObservedGeneration is the last generation successfully reconciled.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Phase is the lifecycle phase of the session.
	// +optional
	Phase WorkspaceSessionPhase `json:"phase,omitempty"`

	// LastReconcileTime is the timestamp of the most recent successful reconcile.
	// +optional
	LastReconcileTime metav1.Time `json:"lastReconcileTime,omitempty"`

	// Conditions reports readiness and attach state.
	// +optional
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// PodRef references the backing Pod for this session.
	// +optional
	PodRef *PodRef `json:"podRef,omitempty"`

	// AttachedAt is the UTC timestamp when the first ACP client connected.
	// +optional
	AttachedAt *metav1.Time `json:"attachedAt,omitempty"`

	// LastActivityAt is the UTC timestamp of the most recent ACP frame exchange.
	// +optional
	LastActivityAt *metav1.Time `json:"lastActivityAt,omitempty"`

	// AttachedClientCount is the number of currently connected ACP clients.
	// >1 is valid in shared and per-user mode (multiple terminals, same session).
	// +optional
	AttachedClientCount int32 `json:"attachedClientCount,omitempty"`

	// TokenBudgetRef references the per-session TokenBudget (populated in split sessionMode).
	// +optional
	TokenBudgetRef *TokenBudgetRef `json:"tokenBudgetRef,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,shortName=wsess,categories=keese
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:printcolumn:name="Ready",type="string",JSONPath=".status.conditions[?(@.type=='Ready')].status"
// +kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase"
// +kubebuilder:printcolumn:name="Subject",type="string",JSONPath=".spec.attachSubject"
// +kubebuilder:printcolumn:name="Session",type="string",JSONPath=".spec.sessionName"

// WorkspaceSession is the Schema for the workspacesessions API.
// WorkspaceSession is namespace-scoped (same namespace as parent Workspace).
// Name pattern: <workspace>-<subject-hash-16>-<session-name>.
// Design: docs/designs/08b-goose-acp-stdio-k8s.md + docs/designs/08b-ii-session-crd-spec.md
// Spec: docs/specs/workspace.operator.keese.ai-v1alpha1-ii-session.md
type WorkspaceSession struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   WorkspaceSessionSpec   `json:"spec,omitempty"`
	Status WorkspaceSessionStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// WorkspaceSessionList contains a list of WorkspaceSession.
type WorkspaceSessionList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []WorkspaceSession `json:"items"`
}

func init() {
	SchemeBuilder.Register(&WorkspaceSession{}, &WorkspaceSessionList{})
}
