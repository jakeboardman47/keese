// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package v1alpha1

// CapabilityMatrix is the declarative feature matrix every provider
// publishes at registration. The controller checks each flag before
// every optional call. See spec §CapabilityMatrix.
type CapabilityMatrix struct {
	ProviderName string
	SPIVersion   string

	SupportsACP                bool
	SupportsSubAgents          bool
	MaxSubAgents               int // 0 = unlimited
	SupportsResume             bool // D25 GUPP
	SupportsSubAgentCleanup    bool // 07 iter-2
	SupportsInjectPrompt       bool // 23 step-2
	SupportsStreaming          bool
	SupportsMCP                bool
	SupportsRecipes            bool
	SupportsCredentialRotation bool
}

// Workspace is the SPI-side view of a workspace. We mirror only the
// fields a provider needs; the keese controller marshals from the
// real CRD.
type Workspace struct {
	UID            string
	Name           string
	Namespace      string
	LastCheckpoint CheckpointRef
}

// CheckpointRef points to the most recent durable session snapshot.
// SQLiteRef is a path on the workspace PVC; NATSSeq is the last
// JetStream sequence written before checkpoint.
type CheckpointRef struct {
	SQLiteRef string
	NATSSeq   uint64
}

// WorkspaceSession is the SPI-side view of a per-user attach session.
type WorkspaceSession struct {
	UID         string
	Name        string
	Namespace   string
	WorkspaceID string
	PodName     string
}

// SubAgentSpec describes a sub-agent to spawn under a parent.
type SubAgentSpec struct {
	ParentSessionUID string
	RecipeRef        string
	Params           map[string]string
}

// SubAgentHandle returns identifiers for a spawned sub-agent.
type SubAgentHandle struct {
	UID     string
	PodName string
}

// AttachHandle is returned by Attach for a serve-mode session pod.
type AttachHandle struct {
	Endpoint string // ACP endpoint (cluster-local)
	SocketFD int    // optional file descriptor for stdio attach
}

// RunResult is the bounded-recipe Run outcome.
type RunResult struct {
	StepID    string
	ExitCode  int
	Artifacts []string
}

// HealthReport is returned by Health.
type HealthReport struct {
	ActiveSubAgentCount int
	Phase               string // "Idle", "Running", "Draining", "Down"
}

// RuntimeEvent is the typed event flowing on the StreamEvents channel.
type RuntimeEvent struct {
	Type    string
	Payload map[string]string
}
