// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package keese

// Event reason constants for the workflow controller package.
// Every recorder.Eventf call must reference one of these consts — no free-text reasons.

const (
	// Workflow controller event reasons.

	// ReasonWorkflowProjected is emitted when an Argo WorkflowTemplate is
	// successfully created or updated via SSA.
	ReasonWorkflowProjected = "WorkflowProjected"

	// ReasonTriggerProjected is emitted when a trigger resource (CronJob /
	// KEDA ScaledObject / Knative Trigger / HTTPRoute) is reconciled.
	ReasonTriggerProjected = "TriggerProjected"

	// ReasonTriggerProjectionFailed is emitted when trigger SSA fails.
	ReasonTriggerProjectionFailed = "TriggerProjectionFailed"

	// ReasonTriggerAuthSecretMissing is emitted when a referenced auth
	// Secret for a trigger is not found.
	ReasonTriggerAuthSecretMissing = "TriggerAuthSecretMissing"

	// ReasonOutputProjected is emitted when an output resource (Knative
	// Sink / NATS stream / S3 config) is reconciled.
	ReasonOutputProjected = "OutputProjected"

	// ReasonWorkflowCascadeBlocked is emitted when deletion is deferred
	// because in-flight WorkflowRuns are not yet terminal.
	ReasonWorkflowCascadeBlocked = "WorkflowCascadeBlocked"

	// WorkflowRun controller event reasons.

	// ReasonWorkflowRunProjected is emitted when an Argo Workflow is
	// successfully created or updated via SSA.
	ReasonWorkflowRunProjected = "WorkflowRunProjected"

	// ReasonWorkflowRunFailed is emitted when reconciliation encounters a
	// terminal failure and the run moves to Failed/Error phase.
	ReasonWorkflowRunFailed = "WorkflowRunFailed"

	// ReasonArtifactBackendMissing is emitted when no artifact backend can
	// be resolved (neither spec.artifactStoreRef nor Tenant fallback).
	ReasonArtifactBackendMissing = "ArtifactBackendMissing"

	// ReasonArtifactSecretFailed is emitted when the artifact credential
	// Secret cannot be created or updated.
	ReasonArtifactSecretFailed = "ArtifactSecretFailed"

	// ReasonRetryBudgetExhausted is emitted when the run's retry budget
	// drops to zero after step failures.
	ReasonRetryBudgetExhausted = "RetryBudgetExhausted"

	// ReasonArgoStatusSynced is emitted when the Argo Workflow phase is
	// successfully mirrored to WorkflowRun.status.
	ReasonArgoStatusSynced = "ArgoStatusSynced"

	// ReasonArgoWatchDisconnected is emitted when the Argo Workflow watcher
	// connection is lost and a re-list is triggered.
	ReasonArgoWatchDisconnected = "ArgoWatchDisconnected"

	// ReasonConcurrentRunForbidden is emitted when ConcurrencyPolicy=Forbid
	// blocks a new WorkflowRun.
	ReasonConcurrentRunForbidden = "ConcurrentRunForbidden"

	// ReasonConcurrentRunForced is emitted when ConcurrencyPolicy=Replace
	// terminates an existing run to allow the new one.
	ReasonConcurrentRunForced = "ConcurrentRunForced"

	// ReasonMissingWorkflowAudience is emitted when the audience injection
	// into the Argo Workflow's projected SA tokens cannot be completed.
	ReasonMissingWorkflowAudience = "MissingWorkflowAudience"

	// ReasonNATSStreamCreateFailed is emitted when JetStream stream
	// provisioning fails during WorkflowRun create.
	ReasonNATSStreamCreateFailed = "NATSStreamCreateFailed"

	// ReasonWorkflowNATSStreamProvisioned is emitted when the JetStream
	// stream is successfully provisioned for a WorkflowRun.
	ReasonWorkflowNATSStreamProvisioned = "WorkflowNATSStreamProvisioned"

	// ReasonWorkflowNATSStreamCleaned is emitted when the JetStream stream
	// is successfully deleted on WorkflowRun cleanup.
	ReasonWorkflowNATSStreamCleaned = "WorkflowNATSStreamCleaned"

	// ReasonWorkflowAudienceInjected is emitted when the keese-wf-<uid>
	// audience is successfully injected into the Argo Workflow's projected
	// SA tokens.
	ReasonWorkflowAudienceInjected = "WorkflowAudienceInjected"

	// Trigger projection condition reasons (used as Reason on the TriggerProjected condition).

	// ReasonTriggerCronJobReady is set when a batch/v1.CronJob is successfully SSA-applied.
	ReasonTriggerCronJobReady = "CronJobReady"

	// ReasonTriggerKnativeTriggerReady is set when a Knative eventing/v1.Trigger is
	// successfully SSA-applied.
	ReasonTriggerKnativeTriggerReady = "TriggerReady"

	// ReasonTriggerHTTPRouteReady is set when a gateway.networking.k8s.io/v1.HTTPRoute
	// is successfully SSA-applied.
	ReasonTriggerHTTPRouteReady = "HTTPRouteReady"

	// ReasonTriggerKEDAUnavailable is set when a NATSSubscription trigger cannot be
	// projected because the KEDA ScaledObject CRD dependency is unresolvable (dep-conflict
	// documented in go.mod). The condition is observable so operators know the limitation.
	ReasonTriggerKEDAUnavailable = "KEDAUnavailable"
)
