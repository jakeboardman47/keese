<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: feature
category: feature
depends:
  - docs/designs/07-agent-runtime-spi.md
  - docs/designs/08a-goose-headless-modes.md
  - docs/designs/18-process-lifecycle.md
implements_specs:
  - docs/specs/agent-runtime-spi.md
  - docs/specs/keese.ai-v1alpha1-runtime.md
implements_plans:
  - docs/plans/demo/D2-runtime-spi-minimum.md
source_refs:
  - internal/runtime/spi/v1alpha1/spi.go:13-87
  - internal/runtime/spi/v1alpha1/registry.go:33-69
  - internal/runtime/providers/goose/goose.go:53-66
  - internal/runtime/providers/goose/goose.go:109-470
  - internal/controller/keese/agentruntime_controller.go:45-204
  - internal/controller/keese/runtime_events.go:9-42
  - cmd/keese-drain/main.go:34-150
  - api/keese/v1alpha1/agentruntime_types.go:114-212
  - api/keese/v1alpha1/runtimeextension_types.go:17-108
related_skills:
  - controller-authoring
status: implemented
implemented_in_phase: demo-D2
last_verified: 2026-05-29
---

# Agent Runtime SPI & Goose Provider

## Summary

The Agent Runtime SPI (`internal/runtime/spi/v1alpha1`) defines a Go interface
(`AgentRuntime`) and a static provider registry that decouple the keese operator
from any specific agent binary. The `AgentRuntime` CRD (cluster-scoped) is the
catalog entry for a runtime provider; `RuntimeExtension` (namespace-scoped) binds
a named tool allow-list to a runtime and writes OpenFGA tuples. The goose provider
is the reference implementation: it runs goose in headless recipe or serve mode,
checkpoints SQLite state on drain, and resumes from checkpoint on pod replacement.
The `keese-drain` binary runs as a preStop lifecycle sidecar to ensure checkpoints
are written before the kubelet sends SIGTERM to the agent container.

## Behavior

- Creating an `AgentRuntime` CR causes the controller to detect the provider name
  from `spec.implementation` (discriminated one-of enforced by CEL XValidation at
  `api/keese/v1alpha1/agentruntime_types.go:117`), check it against the in-process
  registry, and converge the CR to phase `Ready` with condition `Ready=True` and
  reason `RuntimeStarted`.
- An unknown or unregistered provider (including `adkPython`/`adkGo` — see Known
  Limitations) drives the CR to phase `Degraded` with reason `ProviderUnknown`.
- Deleting an `AgentRuntime` is blocked by the finalizer
  `finalizers.agentruntime.keese.ai/drain` until all `RuntimeExtension` objects
  whose `spec.runtimeRef.name` matches are removed.
- Creating a `RuntimeExtension` binds a tool allow-list (`spec.tools`) to a
  runtime and writes `extension:E#enabled_in@workspace:W` OpenFGA tuples per
  admitted Workspace; `status.boundWorkspaces` reflects the live count.
- The goose provider's `Bootstrap` idempotently creates the keese checkpoint
  directory (`/var/run/keese/session/keese-checkpoints/<uid>/`) on the session PVC.
- `Drain` sends SIGTERM to PID 1 in the agent container, polls the goose
  `sessions.db` mtime for stability (within a 90 s budget), then copies the SQLite
  triple into the keese checkpoint dir (`goose.go:142-202`).
- `Resume` copies the checkpoint SQLite back into goose's expected sessions
  directory within a 60 s budget; returns `ErrAgentUnresponsive` on timeout
  (`goose.go:214-241`).
- `InjectPrompt` writes a sanitised single-line turn to a named FIFO at
  `/var/run/keese/session/home/.local/state/goose/inject-fifo` via pod exec, with
  a 5 s write deadline (`goose.go:271-307`).
- `keese-drain` runs as the agent container's preStop hook
  (`lifecycle.preStop.exec: [keese-drain, --pvc-root=..., --timeout=25s]`), writes a
  draining-active sentinel and JSON checkpoint marker atomically, then exits 0 so
  kubelet proceeds with SIGTERM (`cmd/keese-drain/main.go:34-74`).
- Provider registration is static: each provider's `init()` calls `spi.Register`
  with its `CapabilityMatrix` and `Factory`; duplicate names panic at startup
  (`registry.go:33-43`).

## Configuration surface

Key fields are defined in `api/keese/v1alpha1/agentruntime_types.go` and
`api/keese/v1alpha1/runtimeextension_types.go`; do not reproduce here — reference
the types directly.

- `AgentRuntime.spec.implementation` — discriminated one-of selecting `goose`,
  `claudeCode`, `aider`, `adkPython`, or `adkGo` (CEL XValidation: exactly one).
- `GooseSpec.image` — OCI reference for the goose runtime image; must be
  digest-pinned in production (rule 05.12).
- `GooseSpec.migrationPolicy.severity` — `critical|high|medium|low`; `critical`
  is hard-capped at 1 h deferral by the controller.
- `GooseSpec.sidecars.acpBridge.image` — overrides the operator-embedded ACP
  bridge sidecar digest.
- `ADKPythonSpec` / `ADKGoSpec` — `image`, `pythonVersion`/`goVersion`,
  `adkVersion`, optional `sessionStoreRef` and `compactionInterval` (stub; not
  yet wired in controller).
- `RuntimeExtension.spec.runtimeRef.name` — names the `AgentRuntime` to bind.
- `RuntimeExtension.spec.tools[]` — allow-list of tool names; each must exist in
  the effective `GuardrailBinding` policy.
- `keese-drain --pvc-root` / `--timeout` — override defaults (`/var/run/keese/session`,
  `25s`) for non-standard PVC mounts or tighter preStop budgets.

## Observability

Event reasons are defined in
`internal/controller/keese/runtime_events.go`:

| Reason | Kind | Trigger |
|---|---|---|
| `RuntimeStarted` | Normal | `AgentRuntime` reaches `Ready` phase |
| `RuntimeStopped` | Normal | `AgentRuntime` finalizer removed on clean delete |
| `ProviderUnknown` | Warning | `spec.implementation` names unregistered or undetected provider |
| `ImageVersionUnsupported` | Warning | `goose.imageTag` outside `SupportedImageVersions` |
| `SubAgentCleanupTimeout` | Warning | Sub-agent cleanup exceeds drain budget |
| `CredentialExpired` | Warning | SA token or upstream credential expired |
| `ExtensionTupleWritten` | Normal | OpenFGA tuple written for `RuntimeExtension` |
| `ExtensionTupleDeleted` | Normal | OpenFGA tuple deleted for `RuntimeExtension` |
| `ExtensionRuntimeRefInvalid` | Warning | `spec.runtimeRef` names missing `AgentRuntime` |
| `ExtensionOpenFGAUnavailable` | Warning | OpenFGA unreachable during tuple operation |

Status conditions on `AgentRuntime`: `Ready` (True/False). Phases: `Pending`,
`Ready`, `Degraded`, `Incompatible`. `status.observedGeneration` tracks last
reconciled `.metadata.generation`. `keese-drain` emits a structured JSON
`shutdown` event (stdout) with `drain_duration_ms` and `checkpoint_location`
on exit (`cmd/keese-drain/main.go:70-74`).

## Known limitations

- **goose advertises `SupportsSubAgents=false` and `SupportsStreaming=false`.**
  `InvokeSubAgent` returns `ErrSubAgentLimitExceeded`; `CleanupSubAgents` and
  `StreamEvents` return `ErrUnsupported`. These are capability-gated and deferred
  to the TD-P3-05 epic (`goose.go:409-419`).
- **`adkPython` and `adkGo` providers are not handled by `detectProvider`.**
  The switch in `agentruntime_controller.go:182-194` covers only `goose`,
  `claudeCode`, and `aider`. Selecting `adkPython` or `adkGo` falls through to
  the default case (`"spec.implementation: no provider field is set"`) and drives
  the `AgentRuntime` to a permanent `Degraded` phase. This is a known bug; a fix
  requires extending `detectProvider` to cover the two ADK arms.
- `ClaudeCode` and `Aider` are stub implementations with no sub-fields at
  `v1alpha1`; selecting them resolves provider detection but the SPI factory
  returns a no-op runtime.
- `keese-drain` operates without importing the goose provider package; it writes
  the checkpoint marker but delegates the actual SQLite WAL checkpoint to the
  goose process via SIGTERM. If goose exits before flushing the WAL, the
  checkpoint file will be absent.
- No metrics are emitted in this phase; pod labels (`kubectl` filtering) are the
  primary observability mechanism until OTEL counters land in a later phase.

## Change history

Implemented in demo phase D2 (`docs/plans/demo/D2-runtime-spi-minimum.md`);
ADK Python and Go variants added in expansion phase E0 (commit `9373db9`).

## References

- Design: `docs/designs/07-agent-runtime-spi.md`
- Design: `docs/designs/08a-goose-headless-modes.md`
- Design: `docs/designs/18-process-lifecycle.md`
- Spec: `docs/specs/agent-runtime-spi.md`
- Spec: `docs/specs/keese.ai-v1alpha1-runtime.md`
- Plan: `docs/plans/demo/D2-runtime-spi-minimum.md`
- Source: `internal/runtime/spi/v1alpha1/spi.go`
- Source: `internal/runtime/spi/v1alpha1/registry.go`
- Source: `internal/runtime/providers/goose/goose.go`
- Source: `internal/controller/keese/agentruntime_controller.go`
- Source: `internal/controller/keese/runtime_events.go`
- Source: `cmd/keese-drain/main.go`
- Source: `api/keese/v1alpha1/agentruntime_types.go`
- Source: `api/keese/v1alpha1/runtimeextension_types.go`
