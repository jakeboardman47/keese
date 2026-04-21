<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: design
category: runtime
depends:
  - 02-workspace-model.md
  - 04b-projected-sa-identity.md
  - 07-agent-runtime-spi.md
  - 08b-goose-acp-stdio-k8s.md
  - 08c-goose-subagents-limits.md
  - 10a-otel-topology.md
  - 16-recipe-distribution.md
  - 18-process-lifecycle.md
related_skills: [doc-authoring, controller-authoring]
status: current
last_verified: 2026-04-21
rollback: |
  Mode change (recipe → serve or vice versa) requires a new Workspace with
  spec.resumeFrom pointing to the prior Workspace's last checkpoint; VAP rejects
  spec.runtimeMode mutation in place (same immutability rule as spec.topology).
  Image rollback: publish a prior-semver AgentRuntime CR; Workspaces in Idle
  hot-swap on next Resume; Running Workspaces emit RuntimeMigrationDeferred and
  pick up the image on next drain+resume cycle. No CRD migration until v1beta1.
---

# 08a — Goose Headless Modes

## Context

Goose supports two headless execution shapes: `goose run --recipe <path>` (bounded,
exits on completion) and `goose serve` (long-lived ACP daemon). Choosing the wrong
shape wastes resources or breaks interactivity. This design answers the five open
questions from the stub: mode selection criteria, resource sizing, endpoint exposure,
binary pinning and upgrade, and structured log fields with OTEL mapping. It is the
concrete implementation contract for the goose provider in the `AgentRuntime` SPI
(design 07).

## Mode selection

`Workspace.spec.runtimeMode` controls which goose entrypoint the controller launches.
Valid values: `recipe` | `serve`. The field is **immutable after creation** (VAP CEL:
`oldSelf.spec.runtimeMode == self.spec.runtimeMode`); a mode switch requires a new
Workspace with `spec.resumeFrom`.

**Operator default heuristic** (mutating webhook; applied if field absent):
`spec.recipe.ref` set AND no open `WorkflowRun` → `recipe`; otherwise → `serve`.

| Mode | Goose command | Session ends when | SPI `Drain` | SPI `Health` |
|---|---|---|---|---|
| `recipe` | `goose run --recipe <pvc-path>` | Pod exits `Succeeded` | no-op (task complete) | polls `status` file on PVC |
| `serve` | `goose serve --stdio` | controller calls `Drain` | flushes + closes ACP transport | polls `/tmp/health` sentinel on PVC |

Controller watches Pod `Succeeded` in `recipe` mode; on `Failed` calls `Resume`
(D25). In `serve` mode `Start` is called once; subsequent messages arrive via ACP
(08b). `InjectPrompt` is `serve`-only; `recipe` images set `CapabilitySupportsInjectPrompt: false`.

## Resource sizing

Defaults applied by the workspace controller when `Workspace.spec.resources` is
absent. Tenant override: `Tenant.spec.defaults.runtimeResources`. VAP validates
workspace `resources` against `Tenant.spec.resourceQuota` bounds.

| Mode | Env | CPU req | Mem req | CPU limit | Mem limit | Ephemeral |
|---|---|---|---|---|---|---|
| `recipe` | dev/kind | 500m | 512Mi | 2 | 2Gi | 1Gi |
| `recipe` | prod | 1 | 2Gi | 4 | 8Gi | 4Gi |
| `serve` | dev/kind | 1 | 1Gi | 2 | 4Gi | 2Gi |
| `serve` | prod | 2 | 4Gi | 4 | 8Gi | 8Gi |

`serve` mode carries a larger memory floor because goose caches the active model
context window in-process. Ephemeral storage covers the recipe OCI layer extract
and tool-call scratch; the session SQLite lives on the PVC (design 18), not here.

Dev/kind defaults are intentionally soft (tilt loop runs on a laptop). `serve` pods
get a `minAvailable: 1` PDB; `recipe` pods do not (short-lived).

## Endpoint exposure (`serve` mode)

Goose in `serve` mode speaks ACP over **stdio only** (design 08b). There is no
TCP listener, no ClusterIP Service, and no HTTPRoute on the agent pod.

- Client (IDE, `kubectl-keese attach`) bridges stdio via `kubectl exec` to the pod.
- All outbound model calls travel via Envoy AI Gateway on 443 (rule 05.4).
- The SPI `InjectPrompt` call is routed through the stdio ACP transport already open
  for the active client session — not a new port.
- No ingress is created for the agent pod at any topology (rule 05.5).

## Binary pinning and upgrade

**`AgentRuntime` CR** in `keese-system` namespace (`runtime.operator.keese.ai/v1alpha1`):

```
spec:
  image: ghcr.io/keese-ai/goose-runtime@sha256:<digest>   # prod (pinned by digest)
  imageTag: "1.2.3"                                        # informational only
```

In dev overlays the tag form `ghcr.io/keese-ai/goose-runtime:1.2.3` is allowed;
prod CSVs and OLM bundles must use digest form (rule 05.12). An admission webhook
verifies the cosign keyless signature on CR create with OIDC identity regexp
`https://github.com/keese-ai/keese/.github/workflows/.*`.

**Capability discovery:** on pod startup the controller invokes
`<binary> capabilities` (JSON to stdout) and caches the result on
`AgentRuntime.status.capabilities`. SPIVersion major mismatch →
`AgentRuntime.status.phase = Incompatible` + event `RuntimeCapabilityMismatch`.

**Upgrade flow:** update `spec.image` digest on `AgentRuntime` CR → controller
hot-swaps `Idle` Workspaces on next `Resume`; emits `RuntimeMigrationDeferred`
for `Running` Workspaces, which pick up the new image after their next
`Drain`+`Resume`. Mid-step migration is forbidden (gated on
`status.activeSessionRef == ""`). `MaxMigrationDeferral = 24h` per workspace
before controller force-drains.

## Structured logs and OTEL mapping

Goose emits JSON log lines; OTEL collector tier-2 (10a) parses via `json_parser`
and routes to ES index `keese-agent-runtime-*`.

| Goose field | OTEL attribute | Notes |
|---|---|---|
| `session_id` | `keese.session.id` | Opaque cursor |
| `workspace_uid` | `keese.workspace.uid` | From `WORKSPACE_UID` env (not a secret) |
| `step_id` | `keese.step.id` | D24 dedup key |
| `event_type` | span name | `step.start`, `step.complete`, `tool.call`, `token.usage` |
| `tokens.input` | `gen_ai.usage.input_tokens` | 10b accounting processor |
| `tokens.output` | `gen_ai.usage.output_tokens` | Same |
| `tool.name` | `mcp.tool` | 05c audit trail |
| `error.type` / `error.message` | same | Body always redacted; no credential material |

## Trade-offs

| Decision | Rationale |
|---|---|
| `spec.runtimeMode` immutable (VAP) | FSM + topology coupling; mid-flight swap is unsafe |
| stdio ACP (no TCP Service) | Eliminates network attack surface; rule 05.4/05.5 |
| Dual defaults dev/prod | Kind laptops OOM on prod defaults; prod floor is a hard minimum |
| Hot-swap Idle, defer Running | Preserves in-flight work; aligns D24 durable identity |
| `goose capabilities` JSON discovery | Runtime-declared matrix enables future providers without controller changes |

## Failure modes

| Failure | Detection | Mitigation |
|---|---|---|
| `recipe` pod `Failed` | Controller watches Pod phase | D25 GUPP `Resume`; ≤ 1 step lost (18) |
| `serve` ACP stdio disconnected | `Health` poll (sentinel absent) | D23 escalation; `InjectPrompt` or witness |
| `capabilities` JSON invalid | Parse error on startup | `phase=Incompatible`; `RuntimeCapabilityMismatch` event |
| cosign verify fails | Webhook rejects CR | Pin to last verified digest; `RuntimeImageUnverified` alert |
| `Drain` budget overrun (90s) | SIGKILL → pod `Failed` | Resume from checkpoint; `in_flight_remaining>0` in shutdown span |
| MigrationDeferred accumulates | `serve` pods never drain | `MaxMigrationDeferral=24h`; controller force-drains at ceiling |

## Upgrade / rollback

Rollback path in frontmatter. Re-publishing a prior digest reverts the upgrade;
Idle Workspaces hot-swap on next Resume. Running Workspaces defer until drain.
No CRD migration until SPI major-bumps (design 07 SemVer table).

## Observability

- **OTEL spans:** `goose.step.start/complete`, `goose.tool.call`, `goose.drain`,
  `goose.resume` — all carry `keese.workspace.uid` and `keese.session.id`.
- **Metrics:** `keese_agent_step_duration_seconds{mode,workspace}`,
  `keese_agent_token_usage_total{mode,workspace,direction}`.
- **Events** (`internal/controller/runtime/events.go`): `RuntimeCapabilityMismatch`,
  `RuntimeMigrationDeferred`, `RuntimeImageUnverified`.
- **Alert:** `RuntimeMigrationDeferred` age > 24h → page on-call.

## Refs

[02](02-workspace-model.md) · [04b](04b-projected-sa-identity.md) · [07](07-agent-runtime-spi.md) · [08b](08b-goose-acp-stdio-k8s.md) · [08c](08c-goose-subagents-limits.md) · [10a](10a-otel-topology.md) · [16](16-recipe-distribution.md) · [18](18-process-lifecycle.md) · [rubric](../plans/rubric.md) · [plan D8/D9/D24/D25](../plans/scaffolding-plan.md)

## Iteration log

### Iteration 1 — 2026-04-21

| # | Category | Wt | Ratio | Score | Notes |
|---|---|---:|---:|---:|---|
| 1 | Scope clarity | 10 | 1.0 | 10 | Five open questions answered; two modes bounded; exit criteria explicit. |
| 2 | Architecture fit | 10 | 1.0 | 10 | D8/D9 honored; D24 dedup via step_id; D25 GUPP covered; VAP-first immutability; SSA fieldOwner from 07. |
| 3 | Security posture | 15 | 1.0 | 15 | No TCP on agent pod; cosign verify at admission; stdio-only ACP; no secrets in logs; `WORKSPACE_UID` is non-secret. |
| 4 | Automatability | 10 | 0.5 | 5 | Defaults table complete; VAP CEL stated; `AgentRuntime` CR shape shown. Scripts not yet authored (pre-gate). |
| 5 | Verifiability | 15 | 0.5 | 7.5 | Failure modes table present; envtest harness for capability mismatch + mode-immutability not yet authored (pre-gate P8). |
| 6 | Failure-mode awareness | 10 | 1.0 | 10 | Six failure modes; SIGKILL → resume; migration deferral ceiling; cosign rejection path. |
| 7 | Context efficiency | 10 | 1.0 | 10 | ≤ 200 lines; single responsibility; cross-refs via relative paths; no inline code. |
| 8 | Docs quality | 5 | 1.0 | 5 | SPDX + frontmatter complete; `depends` list correct; rollback concrete. |
| 9 | Observability | 5 | 1.0 | 5 | OTEL spans, metrics, events, alert named; log-field mapping table complete. |
| 10 | Operational readiness | 10 | 1.0 | 10 | Hot-swap Idle / defer Running upgrade; 24h deferral ceiling; PDB for serve pods; rollback path. |
| | **Total** | 100 | | **92.5** | |

Verdict: **SHIP** (92.5 ≥ 90). Status: `current`.

Top gaps:
1. Cat 4 (0.5): VAP manifests + `AgentRuntime` CR manifests not yet generated (pre-gate; P8 controller phase).
2. Cat 5 (0.5): envtest for capability mismatch, mode-immutability CEL, and cosign admission not yet authored (post gate-open).

Cross-deps settled: 07 SPI fully mapped; 02 topology immutability pattern reused; 18 drain budget (90s) and checkpoint location consumed; 10a json_parser routing named.
Cross-deps flagged: 08b (ACP stdio transport — stub, parallel); 08c (10-concurrent ceiling — stub, parallel); 16 (recipe OCI pull — stub).
Open (≤4): (1) Does `goose serve --stdio` accept a `--recipe` warm-start flag, or must warm-start use `InjectPrompt`? (2) Exact `goose capabilities` JSON schema version needs upstream alignment. (3) Is PDB `minAvailable:1` sufficient during node drain, or is a dedicated `preStop` hook needed? (4) Should `MaxMigrationDeferral` be an `AgentRuntime.spec` field vs. cluster ConfigMap default?
