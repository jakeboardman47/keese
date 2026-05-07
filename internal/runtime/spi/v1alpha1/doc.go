// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

// Package v1alpha1 defines the AgentRuntime SPI that every keese
// runtime provider must satisfy. The SPI scopes:
//
//   - Identity: Name + CapabilityMatrix (declarative; no runtime probe).
//   - Lifecycle: Bootstrap, Drain, Resume (D18 process-lifecycle).
//   - Workload: Run, Attach, InjectPrompt, InvokeSubAgent (post-P1).
//   - Observability: Health, StreamEvents (post-P1).
//
// Spec: docs/specs/agent-runtime-spi.md.
//
// TD-P1-02 lands Bootstrap + Drain + Resume across the SPI surface.
// Run / Attach / InjectPrompt / InvokeSubAgent / Health / StreamEvents
// / CleanupSubAgents are accepted as part of the interface but return
// ErrUnsupported until later TD items wire them.
//
// SemVer (rule 04.2 + spec §SemVer):
//
//   - New required method → major bump (`spi/v2alpha1`); requires a
//     migration plan scored ≥ 90.
//   - New optional method or capability flag → minor bump.
//
// Static registration (spec §Static registration): every provider
// imports this package and calls Register in init(). cmd/main.go
// blank-imports each provider, ensuring all CapabilityMatrices land
// before the first AgentRuntime CR is admitted.
package v1alpha1
