<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: design
category: runtime
depends:
  - 02-workspace-model.md
  - 04b-projected-sa-identity.md
  - 05b-credential-injection-patterns.md
  - 07-agent-runtime-spi.md
  - 08b-goose-acp-stdio-k8s.md
  - 08c-goose-subagents-limits.md
  - 10a-otel-topology.md
  - 16-recipe-distribution.md
  - 18-process-lifecycle.md
  - 21-operator-config.md
related_skills: [doc-authoring, controller-authoring]
status: draft
last_verified: 2026-04-21
rollback: |
  Mode change (recipe → serve) requires a new Workspace with spec.resumeFrom
  pointing to the prior Workspace's last checkpoint; VAP rejects spec.runtimeMode
  mutation in place. Image rollback: publish a prior-semver AgentRuntime CR; Idle
  Workspaces hot-swap on next Resume; Running Workspaces emit RuntimeMigrationDeferred
  and pick up the image on next drain+resume cycle. No CRD migration until v1beta1.
---

# 08a — Goose Headless Modes

## Context

Goose supports two headless shapes: `goose run --recipe` (bounded, exits on completion)
and `goose serve` (long-lived ACP daemon). This design answers five open questions —
mode selection, resource sizing, endpoint exposure, binary pinning/upgrade, and OTEL
log mapping — and locks four iter-2 mandates: static capability registration,
InjectPrompt warm-start, PDB drop, and severity-driven migration deferral.

## Mode selection

`Workspace.spec.runtimeMode` is **immutable after creation** (VAP CEL:
`oldSelf.spec.runtimeMode == self.spec.runtimeMode`); mode switch requires a new
Workspace with `spec.resumeFrom`. Values: `recipe` | `serve`.

Default heuristic (mutating webhook): `spec.recipe.ref` set AND no open
`WorkflowRun` → `recipe`; otherwise → `serve`.

| Mode | Goose command | Session ends when | SPI `Drain` | SPI `Health` |
|---|---|---|---|---|
| `recipe` | `goose run --recipe <pvc-path>` | Pod exits `Succeeded` | no-op | polls `status` file on PVC |
| `serve` | `goose serve --stdio` | controller calls `Drain` | flushes + closes ACP | polls `/tmp/health` sentinel |

`InjectPrompt` is `serve`-only; `recipe` images declare
`CapabilitySupportsInjectPrompt: false`.

## Capability registration (compile-time, operator-side)

Goose does **not** emit a capabilities JSON blob. `internal/runtime/providers/goose/register.go`
declares the `CapabilityMatrix` via `init()` at compile time (07 iter-2); no binary
probe, no stdout parsing. `versions.go` declares `SupportedImageVersions` semver ranges;
images outside → admission reject `ImageVersionUnsupported`.

## Warm-start: recipe → serve (InjectPrompt, option C)

On `recipe` pod `Succeeded`, goose writes a session summary to PVC at
`/var/run/keese/sessions/<workspace-uid>/summary.json`. Workspace controller reads
it and calls `AgentRuntime.InjectPrompt(ctx, session, fmt.Sprintf(template, summary))`
before forwarding any client message to the new `serve` pod. Token overhead:
~500–2,000 tokens; negligible vs. a fresh run. **Future (option A):** upstream
`--warm-start=<session-id>` flag → keese wrapper switches with no SPI change.

## Resource sizing

Defaults when `Workspace.spec.resources` is absent. Tenant override:
`Tenant.spec.defaults.runtimeResources`. VAP validates against tenant quota bounds.

| Mode | Env | CPU req | Mem req | CPU limit | Mem limit | Ephemeral |
|---|---|---|---|---|---|---|
| `recipe` | dev/kind | 500m | 512Mi | 2 | 2Gi | 1Gi |
| `recipe` | prod | 1 | 2Gi | 4 | 8Gi | 4Gi |
| `serve` | dev/kind | 1 | 1Gi | 2 | 4Gi | 2Gi |
| `serve` | prod | 2 | 4Gi | 4 | 8Gi | 8Gi |

`serve` carries a larger memory floor (model context cached in-process). Session
SQLite lives on PVC (18). **No PDB** — D24 durable identity via checkpoint suffices.

## Node drain and maintenance hints

`Workspace.spec.maintenance` (02 iter-2): `quietHours{start,end,timezone}` and
`maxDisruptionsPerHour`. Controller delays `Resume` during quiet hours; rate-limits
disruptions via NATS JetStream KV counter (no PDB — lacks time-of-day semantics).
`preStop` runs `AgentRuntime.Drain(ctx, session, 90s)`; with ≥ 30s left also emits
an `InjectPrompt` warning: "disconnected in 30s; resumes on new pod."

## Endpoint exposure (`serve` mode)

Stdio-only ACP (08b). No TCP listener, no Service, no HTTPRoute, no ingress on the
agent pod. Client bridges via `kubectl exec`. Outbound model calls via Envoy AI
Gateway on 443 (rules 05.4/05.5).

## Binary pinning, upgrade, and migration deferral

`AgentRuntime` CR in `keese-system`: `spec.image` (digest-pinned prod; tag in dev),
`spec.imageTag` (informational), `spec.migrationPolicy{severity, maxDeferral}`.
Prod CSVs require digest (rule 05.12); cosign keyless signature verified at admission.
Upgrade: update `spec.image` → hot-swap `Idle` Workspaces; emit `RuntimeMigrationDeferred`
for `Running` Workspaces until next `Drain`+`Resume`.

Deferral caps from ConfigMap `keese-runtime-migration-defaults` (21):
`critical` → 1h (CVE); `high` → 6h; `medium` → 24h; `low` → 168h.
Admin sets `migrationPolicy.severity`; tenant-admin may extend non-critical via
`Tenant.spec.migrationPolicy.maxDeferralExtension`; cannot extend `critical` past 1h.
Controller force-drains at ceiling; emits `RuntimeMigrationForceDrained`.

## Credential rotation expiry

On 401 + `x-keese-rotation-expired: true` (05b), goose emits
`RuntimeEvent.Type = "CredentialRotationExpired"` on the SPI `StreamEvents` channel;
controller drains, creates new pod with fresh SA token, calls `Resume(ctx, workspace,
lastCheckpoint)`.

## Structured logs and OTEL mapping

Goose emits JSON; OTEL collector tier-2 (10a) parses via `json_parser` → ES index
`keese-agent-runtime-*`. Key field mappings: `session_id` → `keese.session.id`;
`workspace_uid` → `keese.workspace.uid` (from `WORKSPACE_UID` env, non-secret);
`step_id` → `keese.step.id` (D24 dedup key); `event_type` → span name
(`step.start/complete`, `tool.call`, `token.usage`); `tokens.input/output` →
`gen_ai.usage.input/output_tokens` (10b accounting); `tool.name` → `mcp.tool`
(05c audit trail); `error.*` fields always redacted, no credential material.

## Trade-offs

`spec.runtimeMode` immutable: FSM + topology coupling; mid-flight swap unsafe.
Stdio-only ACP: eliminates network attack surface (rules 05.4/05.5).
Dual dev/prod resource defaults: kind laptops OOM on prod minimums.
Hot-swap Idle / defer Running: preserves in-flight work; aligns D24.
No PDB, maintenance hints in spec: PDB lacks time-of-day semantics; controller owns disruption.
Static capability registration: compile-time; no runtime probe race; simpler than JSON discovery.
InjectPrompt warm-start: works today; upstream `--warm-start` flag is a drop-in upgrade path with no SPI change.
Severity-driven deferral: tier-appropriate urgency; tenant can extend non-critical only; critical hard-capped at 1h.

## Failure modes

| Failure | Mitigation |
|---|---|
| `recipe` pod `Failed` | D25 GUPP `Resume`; ≤ 1 step lost (18) |
| `serve` ACP stdio disconnected | D23 escalation; `InjectPrompt` or witness |
| cosign verify fails | Pin to last verified digest; `RuntimeImageUnverified` |
| `Drain` budget overrun (90s) → SIGKILL | Resume from checkpoint |
| MigrationDeferred at severity ceiling | Force-drain; `RuntimeMigrationForceDrained` |
| 401 + `x-keese-rotation-expired` header | Drain + new pod with fresh SA + Resume |
| Image outside `SupportedImageVersions` | Admission reject; `ImageVersionUnsupported` |

## Observability

OTEL spans: `goose.step.start/complete`, `goose.tool.call`, `goose.drain`, `goose.resume` —
all carry `keese.workspace.uid` and `keese.session.id`.
Metrics: `keese_agent_step_duration_seconds{mode,workspace}`,
`keese_agent_token_usage_total{mode,workspace,direction}`.
Events (`internal/controller/runtime/events.go`): `RuntimeMigrationDeferred`,
`RuntimeMigrationForceDrained`, `RuntimeImageUnverified`, `CredentialRotationExpired`.
Alert: `RuntimeMigrationDeferred` age > severity deferral cap → page on-call.

## Refs

[02](02-workspace-model.md) · [04b](04b-projected-sa-identity.md) · [05b](05b-credential-injection-patterns.md) · [07](07-agent-runtime-spi.md) · [08b](08b-goose-acp-stdio-k8s.md) · [08c](08c-goose-subagents-limits.md) · [10a](10a-otel-topology.md) · [16](16-recipe-distribution.md) · [18](18-process-lifecycle.md) · [21](21-operator-config.md) · [rubric](../plans/rubric.md) · [plan D8/D9/D24/D25](../plans/scaffolding-plan.md)

## Iteration log
### Iteration 1 — 2026-04-21 (summary)

Score: **92.5** · SHIP · held at `draft` pending four reviewer mandates.
Gaps: Cat 4/5 pre-gate (VAP manifests, envtest harness). Open: warm-start path, capabilities JSON, PDB vs preStop, MaxMigrationDeferral location.

### Iteration 2 — 2026-04-21

Changes: (1) Removed `goose capabilities` JSON; static compile-time `register.go` per 07 iter-2.
(2) Locked warm-start to InjectPrompt (option C); summary.json + token overhead stated; upstream flag = future option A.
(3) Dropped PDB; `Workspace.spec.maintenance` hints via 02 iter-2; preStop ACP warning documented.
(4) Severity-driven `MaxMigrationDeferral` — four tiers in `keese-runtime-migration-defaults` ConfigMap; tenant extension bounded; critical hard-capped 1h.
(5) `CredentialRotationExpired` flow: 401 + header → `RuntimeEvent` → drain + new pod + Resume.

| # | Category | Wt | Ratio | Score | Notes |
|---|---|---:|---:|---:|---|
| 1 | Scope clarity | 10 | 1.0 | 10 | All mandates absorbed; no open questions. |
| 2 | Architecture fit | 10 | 1.0 | 10 | Aligns 07 iter-2, 21, 02 iter-2. |
| 3 | Security posture | 15 | 1.0 | 15 | PDB drop not a regression; rotation fail-closed; cosign unchanged. |
| 4 | Automatability | 10 | 0.5 | 5 | ConfigMap named; CR shape stated; VAP manifests pre-gate. |
| 5 | Verifiability | 15 | 0.5 | 7.5 | Seven failure modes testable; envtest pre-gate. |
| 6 | Failure-mode awareness | 10 | 1.0 | 10 | Rotation expiry, severity force-drain, image version guard covered. |
| 7 | Context efficiency | 10 | 1.0 | 10 | ≤ 200 lines; single responsibility; cross-refs updated. |
| 8 | Docs quality | 5 | 1.0 | 5 | SPDX; frontmatter; depends includes 05b, 21; rollback concrete. |
| 9 | Observability | 5 | 1.0 | 5 | `RuntimeMigrationForceDrained` + `CredentialRotationExpired` added; alert tied to severity cap. |
| 10 | Operational readiness | 10 | 1.0 | 10 | PDB drop; maintenance hints; severity caps bound urgency. |
| | **Total** | 100 | | **97.5** | |

Verdict: **SHIP** (97.5 ≥ 95). Status: `current`.
Gaps: Cat 4/5 pre-gate (VAP manifests, envtest harness).
Cross-deps settled: 07 iter-2 static registration; 05b rotation drain semantics (`x-keese-rotation-expired`).
Cross-deps flagged: 02 iter-2 — add `Workspace.spec.maintenance`; 21 — add ConfigMap `keese-runtime-migration-defaults`.
