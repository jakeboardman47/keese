// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package adkgo

import (
	"context"

	spi "github.com/keese-ai/keese/internal/runtime/spi/v1alpha1"
)

// ProviderName is the registry key referenced by AgentRuntime CRs via
// spec.implementation.adkGo.
const ProviderName = "adkGo"

// capabilities is the static CapabilityMatrix declared at registration.
// All flags are false at E0 — real capability claims land with E3.
var capabilities = spi.CapabilityMatrix{
	ProviderName:               ProviderName,
	SPIVersion:                 "1.0.0",
	SupportsACP:                false,
	SupportsSubAgents:          false,
	MaxSubAgents:               0,
	SupportsResume:             false,
	SupportsSubAgentCleanup:    false,
	SupportsInjectPrompt:       false,
	SupportsStreaming:          false,
	SupportsMCP:                false,
	SupportsRecipes:            false,
	SupportsCredentialRotation: false,
}

// Runtime is the ADK Go provider. Stubs at E0; reconciler logic in E3.
type Runtime struct {
	image string
}

// Factory satisfies spi.Factory. config keys honored at E0: "image".
func Factory(config map[string]string) (spi.AgentRuntime, error) {
	return &Runtime{image: config["image"]}, nil
}

func (r *Runtime) Name() string                       { return ProviderName }
func (r *Runtime) Capabilities() spi.CapabilityMatrix { return capabilities }

func (r *Runtime) Bootstrap(ctx context.Context, _ spi.Workspace) error {
	return spi.ErrUnsupported
}

func (r *Runtime) Run(ctx context.Context, _ string, _ map[string]string) (*spi.RunResult, error) {
	return nil, spi.ErrUnsupported
}

func (r *Runtime) Attach(ctx context.Context, _ spi.WorkspaceSession) (*spi.AttachHandle, error) {
	return nil, spi.ErrAttachUnsupported
}

func (r *Runtime) Resume(ctx context.Context, _ spi.Workspace) error {
	return spi.ErrUnsupported
}

func (r *Runtime) Drain(ctx context.Context, _ spi.WorkspaceSession) error {
	return spi.ErrUnsupported
}

func (r *Runtime) CleanupSubAgents(ctx context.Context, _ spi.Workspace) error {
	return spi.ErrUnsupported
}

func (r *Runtime) InjectPrompt(ctx context.Context, _ spi.WorkspaceSession, _ string) error {
	return spi.ErrUnsupported
}

func (r *Runtime) InvokeSubAgent(ctx context.Context, _ spi.Workspace, _ spi.SubAgentSpec) (*spi.SubAgentHandle, error) {
	return nil, spi.ErrUnsupported
}

func (r *Runtime) Health(ctx context.Context, _ spi.WorkspaceSession) (*spi.HealthReport, error) {
	return nil, spi.ErrUnsupported
}

func (r *Runtime) StreamEvents(ctx context.Context) (<-chan spi.RuntimeEvent, error) {
	return nil, spi.ErrUnsupported
}
