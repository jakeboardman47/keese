<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: design
category: runtime
depends:
  - 04a-openfga-authz-model.md
  - 04b-projected-sa-identity.md
  - 05b-credential-injection-patterns.md
  - 08a-goose-headless-modes.md
  - 08b-goose-acp-stdio-k8s.md
  - 08c-goose-subagents-limits.md
  - 18-process-lifecycle.md
  - 20a-api-group-layout.md
  - 23-agent-supervision.md
related_skills: [doc-authoring, controller-authoring]
status: current
last_verified: 2026-04-21
rollback: |
  SPI major bump: author docs/plans/migration-runtime-spi-<version>.md scored ≥ 90;
  deploy old + new interface versions side-by-side; drain existing AgentRuntime CRs;
  update the runtime-controller to call the new interface; flip the feature gate;
  delete old version shim. Rollback: revert feature gate; redeploy prior image; no
  CRD migration required until v1beta1 promotion.
---

# 07 — Agent Runtime SPI

Trade-offs, failure modes, upgrade/rollback, observability, and iteration log:
[07b-agent-runtime-spi.md](07b-agent-runtime-spi.md).

## Context

Keese orchestrates autonomous AI agent workflows on pluggable runtimes. The
`AgentRuntime` SPI is the Go interface contract that every runtime provider
(goose first; claude-code, aider, and custom runtimes later) must satisfy. D24
(durable identity) and D25 (GUPP) are load-bearing: agent identity is the
Workspace, the pod is disposable, and any pending work MUST be resumed by the
controller.

## Interface Signatures

Package: `internal/runtime/spi/v1alpha1`

```go
// AgentRuntime is the pluggable provider contract.
type AgentRuntime interface {
    // Required (every runtime must implement)

    // Start launches a fresh agent process. Returns a Session handle (opaque).
    // ErrTransient (retry), ErrPermanent (terminal), ErrBudget (resources).
    // Idempotent: returns existing healthy Session without starting a duplicate.
    Start(ctx context.Context, ws WorkspaceRef) (Session, error)

    // Resume implements D25 GUPP: restart after pod churn or SIGKILL.
    // MUST be idempotent across three invocations with the same checkpointRef
    // (D24 test obligation). Budget: 120s.
    Resume(ctx context.Context, ws WorkspaceRef, ref CheckpointRef) (Session, error)

    // Drain implements the SIGTERM handler: flush state to PVC/NATS, write a
    // CheckpointRef, close ACP transport. budget ≤ terminationGracePeriodSeconds-5s.
    // ErrBudget: drain incomplete; ref is valid but marked partial=true.
    Drain(ctx context.Context, s Session, budget time.Duration) (CheckpointRef, error)

    // Health returns a non-liveness report (token-usage, step-idle, ACP state).
    // Must return in ≤ 1s; never blocks on the agent process. Called by D23 supervisor.
    Health(ctx context.Context, s Session) HealthReport

    // Capabilities returns the static matrix declared at registration.
    // Called once on plugin startup; cached on AgentRuntime.status.capabilities.
    Capabilities() CapabilityMatrix

    // Optional (gate: matching Capability flag must be true)

    // StreamEvents opens a structured-event channel. Closed when session ends.
    StreamEvents(ctx context.Context, s Session) (<-chan RuntimeEvent, error)

    // InvokeSubAgent spawns a sub-agent within the session's token/tool budget.
    // Max concurrent enforced by caller (08c: 10).
    InvokeSubAgent(ctx context.Context, s Session, spec SubAgentSpec) error

    // AttachMCPServer binds an MCPRoute so the agent can call MCP tools (05a/05c).
    AttachMCPServer(ctx context.Context, s Session, route MCPRouteRef) error
}
```

`Session` is in-process only. `CheckpointRef` (PVC path) is the durable cursor
used by `Resume`. `RuntimeEvent.Type` values include `"StepStarted"`,
`"StepCompleted"`, `"TokenUsed"`, `"CredentialRotationExpired"`,
`"SubAgentSpawned"`, `"SessionDrained"`.

## Capability Matrix and Registration

```go
type CapabilityMatrix struct {
    SupportsStreaming  bool
    SupportsSubAgents bool
    SupportsMCP       bool
    SupportsRecipes   bool   // goose: true; aider: false
    ProviderName      string // e.g. "goose", "claude-code"
    ProviderVersion   string // SemVer of the runtime binary
    SPIVersion        string // SemVer of this SPI package
}
```

**Discovery:** On pod startup, runtime binary is invoked as `<binary> capabilities`
and prints `CapabilityMatrix` JSON to stdout. Runtime controller validates JSON,
checks `SPIVersion` (same major required), caches on `AgentRuntime.status.capabilities`.
Incompatible → `AgentRuntime.phase=Incompatible` + event `RuntimeCapabilityMismatch`.

## SemVer and apidiff

| Change type | Version bump |
|---|---|
| Signature change; removed method | **Major** |
| New optional method added | **Minor** |
| Comment / doc change | **Patch** |

`scripts/check-runtime-spi-apidiff.sh` runs `golang.org/x/exp/apidiff` comparing
HEAD against the prior release tag (`git describe --match 'spi/v*'`). Failure blocks
merge unless `docs/plans/migration-runtime-spi-<version>.md` exists and scores ≥ 90.

## Process Ownership (D24 + D25)

The workspace controller owns runtime-process lifecycle:

1. Controller creates Pod with `restartPolicy: Never`.
2. Calls `Start(ctx, workspaceRef)` via the registered runtime plugin.
3. On Pod `Failed`: reads `Workspace.status.lastCheckpoint`; creates new Pod;
   calls `Resume(ctx, ws, ref)`.
4. GUPP (D25): if `pendingWork != ""` and no active Session, controller MUST call
   `Resume` within one reconcile tick. Breach > 2 min → event `AgentUnresponsive`.

Periodic step-boundary auto-checkpoint: runtime calls `Drain(ctx, s, 0)` after each
recipe step; controller writes ref to `Workspace.status.lastCheckpoint`. Limits
resume restart-point to ≤ 1 step of lost progress on SIGKILL.

## Crash Handling

**SIGTERM:** Controller sends SIGTERM → runtime calls `Drain` → writes `CheckpointRef`
→ exits 0 → controller calls `Resume` if work remains.

**SIGKILL:** Pod `Failed` → controller reads last checkpoint → new Pod → `Resume`.

**SPI panic:** gRPC health probe fails × 3 within 5 min → `RuntimeCrashed` event →
supervisor escalation (23) → backoff capped at 10 min → next `Resume`.

## CredentialRotationExpired Flow (answering 05b open Q)

1. Runtime calls upstream via Envoy AI Gateway.
2. Gateway returns `401 x-keese-rotation-expired: true` (05b §Rotation drain semantics).
3. Runtime emits `RuntimeEvent{Type: "CredentialRotationExpired"}` on `StreamEvents`.
4. Controller observes event → calls `Drain(ctx, s, graceBudget)`.
5. Controller terminates pod; creates new pod (fresh projected SA token, TTL 600s per 04b).
6. Controller calls `Resume(ctx, ws, lastCheckpoint)`.
7. No retry loop at the runtime layer; controller is the retry authority.

## Refs

- [07b-agent-runtime-spi.md](07b-agent-runtime-spi.md) — trade-offs, failure modes, upgrade/rollback, observability, iter log
- [04a-openfga-authz-model.md](04a-openfga-authz-model.md) — `can_revoke`; `supervised_by@witness`
- [04b-projected-sa-identity.md](04b-projected-sa-identity.md) — SA identity; D25 resume invariant
- [05b-credential-injection-patterns.md](05b-credential-injection-patterns.md) — rotation drain; answered open Q
- [08a-goose-headless-modes.md](08a-goose-headless-modes.md) — goose SPI implementation (stub)
- [08b-goose-acp-stdio-k8s.md](08b-goose-acp-stdio-k8s.md) — ACP stdio transport (stub)
- [08c-goose-subagents-limits.md](08c-goose-subagents-limits.md) — 10-concurrent ceiling (stub)
- [18-process-lifecycle.md](18-process-lifecycle.md) — Drain/Checkpoint/Resume lifecycle (stub; feeds this design)
- [20a-api-group-layout.md](20a-api-group-layout.md) — `runtime.operator.keese.ai/v1alpha1`
- [23-agent-supervision.md](23-agent-supervision.md) — crash escalation; witness pattern (stub)
- [../plans/scaffolding-plan.md](../plans/scaffolding-plan.md) — D8, D9, D24, D25
- [../plans/rubric.md](../plans/rubric.md)
