<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: spec
category: contract
depends:
  - ../designs/07-agent-runtime-spi.md
  - ../designs/07b-agent-runtime-spi.md
  - ../designs/08a-goose-headless-modes.md
  - ../designs/08b-goose-acp-stdio-k8s.md
  - ../designs/08c-goose-subagents-limits.md
  - ../designs/18-process-lifecycle.md
  - ../designs/23-agent-supervision.md
related_skills: [doc-authoring, controller-authoring]
status: current
last_verified: 2026-04-21
regression_lock: false
tests:
  unit: [TestCapabilityMatrix_GooseFlags, TestRegister_DuplicateProviderPanics,
         TestBootstrap_Idempotent, TestRun_ResumeIdempotencyByStepID,
         TestDrain_BudgetEnforced, TestCleanupSubAgents_LeakDetection,
         TestCapabilityGate_InjectPromptUnsupported]
  envtest: [TestBootstrap_EnvtestIdempotent3Reconciles, TestDrain_SIGTERMExitCode0,
            TestResume_GUPPTimeout60s, TestCleanupSubAgents_OrphanPodsDeleted]
  kuttl: []
metrics: [keese_runtime_session_starts_total, keese_runtime_drain_duration_seconds,
          keese_runtime_crashes_total, keese_runtime_resume_total,
          keese_runtime_gupp_breach_total]
events: [RuntimeStarted, RuntimeStopped, SubAgentCleanupTimeout, ProviderUnknown,
         ImageVersionUnsupported, CredentialExpired, AgentUnresponsive,
         SessionStarted, SessionResumed, SessionDrained, RuntimeCrashed,
         RuntimeCapabilityMismatch, StreamEventsBlocked]
---

# agent-runtime-spi — spec

**Goal:** Precise Go interface and lifecycle invariants every `AgentRuntime` provider
in `github.com/keese-ai/keese/internal/runtime/spi/v1alpha1` must honor.
Goose is the reference provider (`internal/runtime/providers/goose/`, post-gate).

Iteration log: [`agent-runtime-spi-iter.md`](agent-runtime-spi-iter.md).

## SemVer and apidiff gate

New **required** method → major package bump (`spi/v2alpha1`);
new optional method or cap flag → minor bump.
`scripts/check-runtime-spi-apidiff.sh` gates PRs in `lint.yaml`.
Major bump requires `docs/plans/migration-runtime-spi-<version>.md` (score ≥ 90).

## Static registration

```go
// internal/runtime/providers/goose/register.go
func init() {
    spi.Register("goose", spi.CapabilityMatrix{
        ProviderName: "goose", SPIVersion: "1.0.0",
        SupportsACP: true, SupportsSubAgents: true, MaxSubAgents: 10,
        SupportsResume: true, SupportsSubAgentCleanup: true,
        SupportsInjectPrompt: true, SupportsStreaming: true,
        SupportsMCP: true, SupportsRecipes: true,
        SupportsCredentialRotation: true,
    }, newGooseRuntime)
}
```

`cmd/operator/main.go` blank-imports each provider. Admission rejects unknown
`providerRef` (`UnknownProvider`) and out-of-range images (`ImageVersionUnsupported`).
No runtime binary probe; no stdout-parsed JSON.

## CapabilityMatrix

```go
type CapabilityMatrix struct {
    ProviderName, SPIVersion    string
    SupportsACP                 bool
    SupportsSubAgents           bool
    MaxSubAgents                int  // 0 = unlimited
    SupportsResume              bool // D25 GUPP
    SupportsSubAgentCleanup     bool // 07 iter-2
    SupportsInjectPrompt        bool // 23 step-2
    SupportsStreaming, SupportsMCP, SupportsRecipes bool
    SupportsCredentialRotation  bool
}
```

Controller checks flags before every optional call; absent → skip + emit event.

## AgentRuntime interface

```go
type AgentRuntime interface {
    // Identity (static)
    Name() string
    Capabilities() CapabilityMatrix

    // Bootstrap: provisions PVC dirs + SQLite schema. Idempotent; ≤ 30 s.
    Bootstrap(ctx context.Context, workspace Workspace) error
    // Run: bounded recipe; blocks until Succeeded. Idempotent by StepID (D24).
    Run(ctx context.Context, recipe string, params map[string]string) (*RunResult, error)
    // Attach: ACP session on serve-mode pod. recipe-mode → ErrAttachUnsupported.
    Attach(ctx context.Context, session WorkspaceSession) (*AttachHandle, error)
    // Resume: D25 GUPP. MUST complete or return AgentUnresponsive within 60 s.
    Resume(ctx context.Context, workspace Workspace) error
    // Drain: SIGTERM handler. Budget = 90 s (120 s grace − 20 s OTEL − 10 s buf).
    // Steps: SQLite checkpoint → NATS publish → CleanupSubAgents → close ACP →
    // flush OTEL keese.process.shutdown → exit 0. ErrBudget if deadline exceeded.
    Drain(ctx context.Context, session WorkspaceSession) error
    // CleanupSubAgents: drain sub-agents before parent delete (07 iter-2).
    // SupportsSubAgentCleanup guard. ErrTransient → batch pod-delete by label.
    CleanupSubAgents(ctx context.Context, workspace Workspace) error
    // InjectPrompt: synthetic user turn; supervision ladder step 2 (23).
    // SupportsInjectPrompt guard.
    InjectPrompt(ctx context.Context, session WorkspaceSession, prompt string) error
    // InvokeSubAgent: spawn sub-agent (08c). ErrSubAgentLimitExceeded at cap.
    InvokeSubAgent(ctx context.Context, workspace Workspace, spec SubAgentSpec) (*SubAgentHandle, error)
    // Health: liveness + ActiveSubAgentCount; gRPC :8081 for kubelet-independent check.
    Health(ctx context.Context, session WorkspaceSession) (*HealthReport, error)
    // StreamEvents: typed event channel. Blocked > 5 s → StreamEventsBlocked.
    StreamEvents(ctx context.Context) (<-chan RuntimeEvent, error)
}
```

## Key types

```go
type Workspace    struct { UID, Name, Namespace string; LastCheckpoint CheckpointRef }
type CheckpointRef struct { SQLiteRef string; NATSSeq uint64 }
type RunResult    struct { StepID string; ExitCode int; Artifacts []string }
type HealthReport struct { ActiveSubAgentCount int; Phase string }
type RuntimeEvent struct { Type string; Payload map[string]string }
```

## D25 GUPP and Drain invariants

`Resume` reopens `Workspace.LastCheckpoint.SQLiteRef`; deduplicates by
`last_committed_step_id` (18 D24). `AgentUnresponsive` → 23 ladder step 4.

`preStop` writes `/tmp/draining` (readiness NotReady, rule 06.9). PVC checkpoint:
`/var/run/keese/sessions/<workspace-uid>/session.sqlite` (atomic rename).

## Acceptance tests

Unit (idempotency, budget, capability gating):
`TestBootstrap_Idempotent`, `TestRun_ResumeIdempotencyByStepID`,
`TestDrain_BudgetEnforced`, `TestCleanupSubAgents_LeakDetection`,
`TestCapabilityGate_InjectPromptUnsupported`.

Envtest (lifecycle integration):
`TestBootstrap_EnvtestIdempotent3Reconciles` — status converges, no drift.
`TestDrain_SIGTERMExitCode0` — exit 0; checkpoint on PVC.
`TestResume_GUPPTimeout60s` — AgentUnresponsive after 60 s; event emitted.
`TestCleanupSubAgents_OrphanPodsDeleted` — sub-agent pods deleted on parent drain.

## Failure modes

| Failure | Mitigation |
|---|---|
| `Bootstrap` error | Retry backoff; max 5 → ErrPermanent |
| `Resume` → AgentUnresponsive | 23 ladder step 4 (pod restart) |
| `Drain` → ErrBudget | Resume from partial ref; `in_flight_remaining > 0` |
| `CleanupSubAgents` ErrTransient | Batch pod-delete `keese.ai/parent-workspace:<uid>` |
| Unknown `providerRef` | Admission: UnknownProvider; phase Incompatible |
| Image out of version range | Admission: ImageVersionUnsupported |
| `StreamEvents` blocked > 5 s | StreamEventsBlocked event; poll Health |
| GUPP breach | Resume → AgentUnresponsive → escalate |

## Observability

OTEL: one span per interface method; attributes `runtime.provider`, `runtime.version`,
`workspace.uid`, `workspace.namespace`, `tenant.name`. Events const table:
`internal/controller/runtime/agentruntime/events.go`. Metrics + events: frontmatter
arrays; SLO targets in 07b.

## Rollback

Revert `AgentRuntime.spec.image` → drain/restart cycle.
Major SPI bump: side-by-side deploy → drain → gate flip → shim delete.
No CRD migration at v1alpha1.

## Iteration log

Full rubric tables in [`agent-runtime-spi-iter.md`](agent-runtime-spi-iter.md).

| Iter | Date | Focus | Score | Status |
|---|---|---|---:|---|
| 1 | 2026-04-21 | Correctness & security | 92.5 | SHIP |
| 2 | 2026-04-21 | Performance & quality | 95 | SHIP |
| 3 | 2026-04-21 | Operational readiness | 97.5 | SHIP — `current` |

Pre-gate acceptable gaps: Cat 4 (`check-runtime-spi-apidiff.sh` CI wiring, P8);
Cat 5 (envtest harness authors post-gate with `controller-author` agent).
