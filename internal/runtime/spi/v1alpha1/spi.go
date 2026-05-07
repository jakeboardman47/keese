// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package v1alpha1

import (
	"context"
	"errors"
)

// AgentRuntime is the SPI every provider implements. See spec §AgentRuntime
// interface for invariants on each method.
type AgentRuntime interface {
	// Identity (static — no I/O).
	Name() string
	Capabilities() CapabilityMatrix

	// Bootstrap provisions PVC dirs + SQLite schema. Idempotent; ≤ 30 s.
	Bootstrap(ctx context.Context, workspace Workspace) error

	// Run executes a bounded recipe; blocks until Succeeded. Idempotent
	// by step ID (D24).
	Run(ctx context.Context, recipe string, params map[string]string) (*RunResult, error)

	// Attach returns an ACP session handle on a serve-mode pod. Recipe-
	// mode sessions return ErrAttachUnsupported.
	Attach(ctx context.Context, session WorkspaceSession) (*AttachHandle, error)

	// Resume restores a session from Workspace.LastCheckpoint. Must
	// complete or return ErrAgentUnresponsive within 60 s (D25 GUPP).
	Resume(ctx context.Context, workspace Workspace) error

	// Drain handles SIGTERM. Budget = 90 s. Steps:
	//   SQLite checkpoint → NATS publish → CleanupSubAgents → close ACP →
	//   flush OTEL keese.process.shutdown → exit 0.
	// Returns ErrBudget if the deadline is exceeded.
	Drain(ctx context.Context, session WorkspaceSession) error

	// CleanupSubAgents drains sub-agents before parent delete (07 iter-2).
	// Behind SupportsSubAgentCleanup capability flag.
	CleanupSubAgents(ctx context.Context, workspace Workspace) error

	// InjectPrompt injects a synthetic user turn (supervision ladder
	// step 2). Behind SupportsInjectPrompt capability flag.
	InjectPrompt(ctx context.Context, session WorkspaceSession, prompt string) error

	// InvokeSubAgent spawns a sub-agent (08c). Returns
	// ErrSubAgentLimitExceeded when MaxSubAgents reached.
	InvokeSubAgent(ctx context.Context, workspace Workspace, spec SubAgentSpec) (*SubAgentHandle, error)

	// Health returns liveness + ActiveSubAgentCount. Used by the
	// kubelet-independent health checker.
	Health(ctx context.Context, session WorkspaceSession) (*HealthReport, error)

	// StreamEvents returns a typed event channel. Blocked > 5 s →
	// StreamEventsBlocked event from the controller side.
	StreamEvents(ctx context.Context) (<-chan RuntimeEvent, error)
}

// Sentinel errors returned across the SPI surface.
var (
	// ErrUnsupported is returned by an SPI method when the underlying
	// provider does not yet implement it. Controllers must check the
	// capability matrix before calling — this is a backstop.
	ErrUnsupported = errors.New("runtime: SPI method unsupported by provider")

	// ErrAttachUnsupported is returned by Attach on recipe-mode pods.
	ErrAttachUnsupported = errors.New("runtime: attach unsupported in recipe mode")

	// ErrAgentUnresponsive is returned by Resume when the session
	// fails to recover within 60 s (D25 GUPP).
	ErrAgentUnresponsive = errors.New("runtime: agent unresponsive")

	// ErrBudget is returned by Drain when the 90s deadline is exceeded.
	ErrBudget = errors.New("runtime: drain budget exceeded")

	// ErrSubAgentLimitExceeded is returned by InvokeSubAgent at the
	// per-provider MaxSubAgents cap.
	ErrSubAgentLimitExceeded = errors.New("runtime: sub-agent limit exceeded")

	// ErrPermanent is returned after retry budget exhaustion (typically
	// from Bootstrap).
	ErrPermanent = errors.New("runtime: permanent failure")

	// ErrTransient indicates a retryable failure.
	ErrTransient = errors.New("runtime: transient failure")
)
