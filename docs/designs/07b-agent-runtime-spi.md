<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: design
category: runtime
depends:
  - 07-agent-runtime-spi.md
related_skills: [doc-authoring]
status: current
last_verified: 2026-04-21
rollback: See 07-agent-runtime-spi.md rollback field.
---

# 07b — Agent Runtime SPI: Trade-offs, Failure Modes, Upgrade/Rollback, Observability

See [07-agent-runtime-spi.md](07-agent-runtime-spi.md) for context, interface signatures,
capability matrix, SemVer policy, process ownership, crash handling, and
CredentialRotationExpired flow.

## Trade-offs

**Opaque `Session` vs. serializable session state:** Opaque chosen. Cross-process
serialization couples the SPI to runtime internals. `CheckpointRef` (PVC path) is the
durable cursor; `Session` is in-process only. Runtime providers implement their own
checkpoint format on the workspace PVC.

**`restartPolicy: Never` vs. `OnFailure`:** `Never` chosen. K8s `OnFailure` creates a
new pod without calling `Resume(checkpoint)`; the runtime restarts cold, losing step
progress and violating D24 idempotency. The controller-owned restart + `Resume` call is
the correct pattern for D24/D25.

**Controller-owned lifecycle vs. dedicated runtime controller:** Single workspace
controller chosen. Workspace is the unit of identity, scheduling, and policy. A
dedicated runtime controller would require cross-controller coordination (leader
election + status propagation) for every lifecycle event, adding latency and coupling.

**Optional methods via capability flags vs. interface embedding:** Flat interface with
runtime capability check chosen over Go interface embedding. Embedding creates
compile-time coupling; a missing optional method panics at the call site. Flag-gated
dispatch lets the controller safely skip optional calls for runtimes that don't
declare the capability.

**gRPC health probe vs. HTTP liveness:** gRPC chosen. Every `AgentRuntime` implementation
must expose a gRPC `grpc.health.v1.Health` service at `:8081`; the controller dials
this to detect SPI panics without relying on kubelet liveness (which would restart
the pod without checkpointing). Kubelet liveness probes are ALSO present but tuned for
the full drain window per rule 06.8.

## Failure Modes

| Failure | Detection | Recovery |
|---|---|---|
| `capabilities` JSON invalid | Parse error at startup | `RuntimeCapabilityMismatch` event; `AgentRuntime.phase=Incompatible` |
| `Start` ErrTransient | Controller sees error return | Retry with backoff; max 5 attempts then ErrPermanent |
| `Drain` exceeds budget | `ErrBudget` returned | Partial checkpoint written; pod killed; `Resume` from partial ref |
| 3 SPI panics in 5 min | gRPC health probe failures | `RuntimeCrashed` event; supervisor escalation (23); backoff ≤ 10 min |
| `Resume` not idempotent | Duplicate side effects | D24 test obligation blocks gate; envtest must catch |
| `StreamEvents` channel blocked > 5s | Context cancellation | `StreamEventsBlocked` event; controller falls back to polling `Health` |
| GUPP breach (pending work, no Session) | Controller detects > 2 min | `AgentUnresponsive` event; human page via AlertManager |
| Pod stuck in `Pending` > 5 min | Controller `Watch` + timeout | `PodSchedulingStuck` event; workspace `Degraded` |

## Upgrade and Rollback

**Runtime binary upgrade:** Patch `AgentRuntime.spec.image`. Controller drains existing
session via `Drain`, deletes pod, creates new pod with upgraded image, calls `Start`.
`CapabilityMatrix` re-validated on new pod startup. Rollback: revert `spec.image` →
same drain/restart cycle; no CRD migration at v1alpha1.

**SPI major bump:** Author `docs/plans/migration-runtime-spi-<version>.md` (score ≥ 90).
Deploy old + new interface versions side-by-side. Drain existing sessions under old
interface. Update runtime-controller to call new interface. Flip feature gate. Delete old
version shim. Rollback: revert feature gate; redeploy prior image.

**Goose version pin:** `AgentRuntime.spec.providerVersion` (SemVer). Upgrade path: bump
spec field; controller drains + restarts. Pin validated by `capabilities` stdout on startup
(08a defines the exact mechanism).

## Observability

OTEL spans: `runtime.start`, `runtime.resume`, `runtime.drain`, `runtime.health`,
`runtime.stream_events`. Attributes on all spans: `runtime.provider`,
`runtime.version`, `workspace.uid`, `workspace.namespace`, `tenant.name`.
Cross-ref: 18-process-lifecycle.md for drain span timing requirements.

Metrics:
- `keese_runtime_session_starts_total{provider,result}` — tracks cold-start vs. resume
- `keese_runtime_drain_duration_seconds{provider,result}` — SLO: p99 ≤ 110s
- `keese_runtime_crashes_total{provider}` — alert at rate > 0.1/min
- `keese_runtime_resume_total{provider,result}` — D25 GUPP coverage
- `keese_runtime_gupp_breach_total{workspace,tenant}` — alert immediately

Events (const table: `internal/controller/runtime/agentruntime/events.go`):
`SessionStarted`, `SessionResumed`, `SessionDrained`, `RuntimeCrashed`,
`RuntimeCapabilityMismatch`, `AgentUnresponsive`, `StreamEventsBlocked`,
`CredentialRotationExpired`, `PodSchedulingStuck`.

## Iteration Log

### Iteration 1 — 2026-04-21

| # | Category | Weight | Ratio | Score | Notes |
|---|---|---:|---:|---:|---|
| 1 | Scope clarity | 10 | 1.0 | 10 | 5 open questions answered; method contracts, error taxonomy, idempotency, timeouts explicit. |
| 2 | Architecture fit | 10 | 1.0 | 10 | D8/D9/D24/D25 honored; controller-owns-lifecycle; `restartPolicy: Never`; no panic/Fatal. |
| 3 | Security posture | 15 | 1.0 | 15 | No credential in Session; CredentialRotationExpired routes through controller; 04b/05b cross-refs explicit. |
| 4 | Automatability | 10 | 1.0 | 10 | `check-runtime-spi-apidiff.sh` named; `capabilities` JSON discovery protocol; migration plan gate specified. |
| 5 | Verifiability | 15 | 0.5 | 7.5 | D24 test obligation stated; apidiff script named. SPI Go code + envtest suite not yet authored (post-gate). |
| 6 | Failure-mode awareness | 10 | 1.0 | 10 | 8 failure modes with detection + recovery; partial checkpoint, crash backoff, GUPP breach, scheduling stuck covered. |
| 7 | Context efficiency | 10 | 1.0 | 10 | Split at 200-line boundary (07 + 07b); stubs for 08a/08b/08c/18/23 cross-referenced not inlined. |
| 8 | Docs quality | 5 | 1.0 | 5 | SPDX; frontmatter complete on both files; rollback concrete; all 9 depends cross-linked. |
| 9 | Observability | 5 | 1.0 | 5 | OTEL spans, metrics with SLO, events table with const-file location; GUPP breach alert named. |
| 10 | Operational readiness | 10 | 1.0 | 10 | Upgrade/rollback paths for runtime binary + SPI major bump; crash-backoff ceiling; escalation ladder pointer to 23. |
| | **Total** | 100 | | **92.5** | |

Verdict: SHIP (92.5 ≥ 90 honest threshold). `status: current`.

Top gaps:
1. Cat 5 (verifiability) -7.5: SPI Go interface + envtest suite not yet authored (post-gate).
2. 08a/08b/08c/18/23 remain stubs; full lifecycle validation deferred until they reach `current`.
3. `scripts/check-runtime-spi-apidiff.sh` not implemented; pre-gate acceptable.

Next step: Author 08a (goose headless modes) as first SPI implementer; implement
`internal/runtime/spi/v1alpha1/spi.go` + `spi_types.go` after design gate opens;
write envtest harness covering idempotency + GUPP invariants.
