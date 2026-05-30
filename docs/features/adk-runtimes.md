<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: feature
category: feature
depends: [../designs/07-agent-runtime-spi.md]
implements_specs: [../specs/keese.ai-v1alpha1-runtime.md]
implements_plans: [../plans/expansion/E0-runtime-spi-expansion.md, ../plans/expansion/E1-adk-python-runtime.md]
source_refs:
  - internal/runtime/providers/adkpython/adkpython.go:1-85
  - internal/runtime/providers/adkpython/register.go:1-12
  - internal/runtime/providers/adkgo/adkgo.go:1-85
  - internal/runtime/providers/adkgo/register.go:1-12
related_skills: [controller-authoring]
status: in-development
implemented_in_phase: expansion-E0
last_verified: 2026-05-29
---

# ADK Python & ADK Go Runtimes

## Summary

Phase E0 introduces skeleton AgentRuntime SPI providers for Google's Agent
Development Kit in both Python (`adkPython`) and Go (`adkGo`) flavors. Each
provider self-registers in the global SPI registry at package init time and
declares a `CapabilityMatrix` (SPI version `1.0.0`, all capability flags
`false`). All lifecycle methods currently return `spi.ErrUnsupported` or
`spi.ErrAttachUnsupported`. The purpose of this phase is to reserve the
registry keys, lock the provider names referenced in `AgentRuntime` CRs, and
establish the file layout that expansion phases E1 (ADK Python) and E3 (ADK
Go) will fill in.

## Behavior

- **Provider registration**: `adkpython.init()` and `adkgo.init()` call
  `spi.Register(ProviderName, capabilities, Factory)` at process startup,
  inserting `"adkPython"` and `"adkGo"` into the registry.
- **Factory construction**: `Factory(config map[string]string)` accepts one
  config key — `"image"` — and returns a `*Runtime`. No validation or
  defaulting occurs at E0.
- **Capabilities**: `Capabilities()` returns the static `CapabilityMatrix`
  with all boolean flags `false` and `MaxSubAgents: 0`. Nothing is negotiated
  at runtime.
- **All SPI methods stub out**: `Bootstrap`, `Run`, `Resume`, `Drain`,
  `CleanupSubAgents`, `InjectPrompt`, `InvokeSubAgent`, `Health`, and
  `StreamEvents` all return `ErrUnsupported`; `Attach` returns
  `ErrAttachUnsupported`.
- **No pod lifecycle**: no Kubernetes Pod or Deployment is created or managed
  by these providers at E0.

## Configuration surface

`AgentRuntime` CRs reference providers via `spec.implementation.adkPython` or
`spec.implementation.adkGo` (key names mirror `ProviderName` constants at
`adkpython.go:14` and `adkgo.go:14`). The only config key the Factory reads
is `"image"` (stored on the `Runtime` struct but unused until E1/E3). See the
full field contract in `docs/specs/keese.ai-v1alpha1-runtime.md`.

## Observability

No events, status conditions, or metrics are emitted by these providers at E0
— all SPI calls return errors before any instrumentation point is reached.
Any `AgentRuntime` CR selecting `adkPython` or `adkGo` is driven to `Degraded`
immediately: the controller's `detectProvider` switch (agentruntime_controller.go:182-194)
falls through to its default error arm because neither provider key is handled. E1/E3
will add real `detectProvider` arms and pod-template logic.

## Known limitations

- **All SPI methods are stubs.** Every method on both providers returns
  `ErrUnsupported` or `ErrAttachUnsupported`. No agent can be started,
  attached to, drained, or health-checked.
- **AgentRuntime controller does not recognize these providers.** The
  controller's `detectProvider` switch (agentruntime_controller.go:182-194)
  handles only `goose`, `claudeCode`, and `aider`. Any `AgentRuntime` CR
  selecting `adkPython` or `adkGo` is driven to `Degraded` immediately upon
  reconcile — the default error arm fires before any SPI call. End-to-end
  operation is not possible. E1/E3 will add the missing switch arms and
  pod-template logic, at which point the provider will transition to `Ready`.
- **No pod template, session-store wiring, or A2A bridge.** These are scoped
  to E1 (ADK Python) and E3 (ADK Go) respectively.
- **Capability matrix is static and zeroed.** All flags remain `false` until
  real capability claims land in E1 (ADK Python) and E3 (ADK Go).

## Change history

- `expansion-E0` (`9373db9`) — skeleton providers created; registry keys
  `"adkPython"` and `"adkGo"` reserved; all methods stub to `ErrUnsupported`.

## References

- Design: `docs/designs/07-agent-runtime-spi.md`
- Spec: `docs/specs/keese.ai-v1alpha1-runtime.md`
- Plan: `docs/plans/expansion/E0-runtime-spi-expansion.md`,
  `docs/plans/expansion/E1-adk-python-runtime.md`
- Source: `internal/runtime/providers/adkpython/`,
  `internal/runtime/providers/adkgo/`
