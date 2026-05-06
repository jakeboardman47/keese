<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: design
category: tenancy
depends: [docs/plans/scaffolding-plan.md, docs/designs/24-tenant-crd.md]
related_skills: [doc-authoring]
status: current
last_verified: 2026-04-20
rollback: |
  Mode A: revert keese operator image via OLM `replaces` chain; no Capsule change needed.
  Mode B: pin Capsule Helm chart to previously validated version via helmfile.lock;
  revert keese operator image via OLM `replaces` chain; run `make bootstrap-infra`.
---

# 01 — Tenancy via Capsule

## Context

A **Tenant** is an organizational/identity concept — it does NOT map 1:1 to a
namespace. A tenant owns one or more namespaces (no prescribed naming); a
**Workspace** is a CR living inside any tenant-owned namespace; multiple Workspaces
may share a namespace. Two modes: **Mode A** (single-namespace, no Capsule) and
**Mode B** (multi-namespace, Capsule present). D26 (2026-04-20) amends D23: keese
now owns a **thin** `Tenant` CRD (`keese.ai/v1alpha1`) holding
keese-specific tenancy config (guardrail defaults, token-budget refs, default
workspace quota) and delegating namespace aggregation to Capsule (Mode B) or
namespace labels (Mode A). Detailed design: `docs/designs/24-tenant-crd.md`.

## Decisions

**D-01.1 — Namespace layout and tenant membership.**
Keese does not prescribe namespace naming. In Mode B each namespace carries label
`capsule.clastix.io/tenant: <tenant>` so Capsule's Tenant CR claims it. Multiple
Workspaces may coexist in one namespace. Tenant membership is expressed through
RBAC: users with `keese-tenant-editor` ClusterRole binding may create Workspaces.
The Workspace controller creates only Workspace-scoped resources (SA, PVC, HTTPRoute,
OpenFGA tuples, NetworkPolicy); namespace creation is a platform-team action.

**D-01.2 — Capsule is opt-in; two deployment modes.**
Flag `--capsule-integration=auto|on|off` (default `auto`) detects Capsule CRDs at
startup. Mode A: namespace-local quota via Workspace mutating webhook; no cross-
namespace aggregation. Tenant membership expressed by labeling namespaces
`keese.ai/tenant=<name>` — immutable after first set (D-01.7). The keese `Tenant` CR
populates `status.namespaces[]` by label selector. Mode B: Workspace controller
labels namespaces to enter the Capsule Tenant tree; Capsule reconciles tenant-level
quota, LimitRange, and RBAC projections. P7 helmfile installs Capsule by default.

**D-01.3 — Kyverno ClusterPolicy for PSS + keese-specific pod admission.**
Every workspace namespace receives `pod-security.kubernetes.io/enforce: restricted`.
Keese ships a `ClusterPolicy` (Kyverno, Enforce mode) in
`config/overlays/base/kyverno-policies/` adding defense-in-depth: deny
`hostNetwork/PID/IPC`, `privileged`, `allowPrivilegeEscalation`; require
`readOnlyRootFilesystem: true` for agent pods; require `runAsNonRoot: true`; require
label `keese.ai/workspace=<name>` for admission in keese-managed namespaces. Goose
writes to workspace PVC (SQLite) or emptyDir — both writable under read-only root.

**D-01.4 — Quota / LimitRange division.**
Mode B: Capsule `Tenant.spec.resourceQuota` / `.limitRanges` carry tenant ceilings.
Both modes: Workspace mutating webhook injects one `ResourceQuota` (from
`Workspace.spec.resources` or tenant default) and one `LimitRange` (`requests ==
limits`). VAP on Workspace creation validates quota ≤ tenant ceiling via CEL.

**D-01.5 — TenantResource propagation and NetworkPolicy / BackendSecurityPolicy.**
Keese does not use `TenantResource` for NetworkPolicy or `BackendSecurityPolicy`.
Workspace controller applies NetworkPolicy via SSA (`fieldOwner:
keese-workspace-controller`). `BackendSecurityPolicy` refs live in a designated
namespace and are projected via `ReferenceGrant`. Capsule `TenantResource` is
reserved for platform use.

**D-01.6 — Capsule version upgrade contract (Mode B only).**
Keese pins Capsule via `helmfile.lock`. Before upgrading: (1) run
`scripts/check-capsule-api.sh` (diffs `capsule.clastix.io/v1beta2/Tenant` schema);
(2) if keese field mappings change, architect-signed commit adding
`docs/plans/migration-capsule-<version>.md` required; (3) CI runs Capsule upgrade
matrix in `e2e.yaml` before merge.

**D-01.7 — Immutable tenant label via VAP (Mode A only).**
The `keese.ai/tenant=<name>` namespace label is immutable after first set and
removal is denied. Enforced by a `ValidatingAdmissionPolicy` (K8s 1.30 GA) on
namespace UPDATE. Two CEL clauses: (1) mutation denied:
`!('keese.ai/tenant' in oldObject.metadata.labels) || oldObject.metadata.labels['keese.ai/tenant'] == object.metadata.labels['keese.ai/tenant']`;
(2) removal denied: `!('keese.ai/tenant' in oldObject.metadata.labels) || ('keese.ai/tenant' in object.metadata.labels)`.
Manifest: `config/overlays/base/vap/namespace-tenant-label.yaml`. In Mode B, Capsule
manages `capsule.clastix.io/tenant` — keese's VAP operates on a different key, no
conflict. Kyverno alternative considered and deferred: VAP is native to K8s 1.30+.

## ClusterRole scaffold

| ClusterRole | Verbs | Resources |
|---|---|---|
| `keese-workspace-viewer` | get, list, watch | workspaces, workspaceshares, agentruntimes, workflows, workflowruns |
| `keese-workspace-editor` | viewer + create, update, patch, delete | workspaces, workspaceshares |
| `keese-runtime-invoker` | create workflowruns; get/list/watch workflows, workspaces, agentruntimes | (as listed) |
| `keese-runtime-admin` | full CRUD | agentruntimes, runtimeextensions |
| `keese-guardrail-author` | full CRUD | guardrailbindings |
| `keese-memory-admin` | full CRUD | memories, sharedmemories |
| `keese-recipe-publisher` | full CRUD | recipes, recipesources |
| `keese-observability-viewer` | get, list, watch | tokenbudgets, events |
| `keese-transport-admin` | full CRUD | transports |

Aggregates: `keese-tenant-admin` (all non-viewer); `keese-tenant-viewer` (all
viewers). In Mode A/B the `keese-tenant-admin` binding can be OwnerRef-scoped to the
keese `Tenant` CR; in Mode B also via Capsule `additionalRoleBindings`.
`docs/designs/24-tenant-crd.md` iter-1 specifies the binding strategy.

## Trade-offs

| Option | Chosen | Rationale |
|---|---|---|
| Own a keese `Tenant` CRD | Yes (D26 amends D23) | K8s-object backing for ReBAC `tenant:X`, finalizers, keese-specific config; does not replicate Capsule aggregation. |
| Mandate Capsule for all deployments | No | Single-namespace installs need no Capsule. |
| Capsule `TenantResource` for NP | No | SSA field ownership ambiguity. |
| vcluster hard isolation at v1 | No | Deferred; validates primitives first. |
| PSS label only | No | Kyverno adds agent-specific invariants PSS cannot enforce. |
| Kyverno for tenant-label immutability | Deferred | VAP (D-01.7) is native K8s 1.30+; no extra operator. |

## Failure modes

| Failure | Detection | Mitigation |
|---|---|---|
| Capsule unavailable (Mode B) | `Phase=Pending`; span `capsule.tenant.ready=false` | Exponential backoff; `WorkspaceStuck` alert after 5m |
| Kyverno enforcement gap | Pod rejected; event `KyvernoPolicyRejected` | Kyverno readiness gate blocks operator start |
| Capsule API breaks (v1beta3) | `check-capsule-api.sh` fails CI | Pin blocks; migration doc required |
| Workspace quota > tenant quota | VAP rejects before persist | CEL error names violated dimension |
| SSA field conflict | Unexpected NP change; conflict counter metric | Platform must not configure `TenantResource` for workspace namespaces |
| Mode detection wrong | Operator logs discovery at INFO | `--capsule-integration=on\|off` override |
| Tenant CR deleted with live Workspaces | Finalizer `finalizers.tenant.keese.ai/workspaces` | Blocks delete until Workspaces are reassigned or deleted |

## Upgrade / rollback

Rollback in frontmatter. Patch Capsule upgrades (no CRD delta): update lock, `helmfile sync`. Minor/major: migration doc + `make smoke`.

## Observability

- OTEL spans: `capsule.tenant.ready` (Mode B), `workspace.quota.applied`, `kyverno.policy.applied`.
- Event reasons (`events.go`): `TenantNotFound`, `QuotaApplied`, `NetworkPolicyApplied`, `WorkspaceStuck`, `KyvernoPolicyRejected`, `CapsuleIntegrationMode`.
- Metrics: `keese_workspace_reconcile_duration_seconds{phase}`, `keese_workspace_capsule_conflict_total`, `keese_workspace_kyverno_reject_total`.
- Audit logs (`(subject, decision, tenant, workspace, action)` spans) land in ES `keese-workspace-audit-*` (primary, 30-day hot) and Loki `{ job="keese-workspace-audit", tenant="..." }` (secondary, ≥ 1-year cold). Both apply redaction per rules 02 and 05.10. OTEL collector fans out to both; see `docs/designs/10a-otel-topology.md`.

## Refs

- [Capsule v1beta2 Tenant API](https://capsule.clastix.io/docs/general/references/tenant-crd)
- [20-api-group-layout.md](20-api-group-layout.md) · [02-workspace-model.md](02-workspace-model.md) · [12-network-isolation.md](12-network-isolation.md) · [05b-credential-injection-patterns.md](05b-credential-injection-patterns.md) · [24-tenant-crd.md](24-tenant-crd.md) · [../plans/rubric.md](../plans/rubric.md)

## Iteration log

### Iteration 1 — 2026-04-20 — score 87.5 (SHIP) — baseline; D-01.1/D-01.3 rewritten; Kyverno added.

### Iteration 2 — 2026-04-20

| # | Category | Weight | Ratio | Score | Notes |
|---|---|---:|---:|---:|---|
| 1 | Scope clarity | 10 | 1.0 | 10 | Modes A/B explicit; tenant/namespace distinction clear. |
| 2 | Architecture fit | 10 | 1.0 | 10 | D3/D23 honored; compose-over-replicate; vcluster deferred. |
| 3 | Security posture | 15 | 1.0 | 15 | PSS + Kyverno; goose write path confirmed; SSA fieldOwner; NP fail-closed. |
| 4 | Automatability | 10 | 0.5 | 5 | `check-capsule-api.sh` TBD; Kyverno path stated, not scaffolded. |
| 5 | Verifiability | 15 | 0.5 | 7.5 | Reject event + metric named; envtest/kuttl awaiting spec phase. |
| 6 | Failure-mode awareness | 10 | 1.0 | 10 | Six failure modes; detection + mitigation each. |
| 7 | Context efficiency | 10 | 1.0 | 10 | ≤ 200 lines; single responsibility. |
| 8 | Docs quality | 5 | 1.0 | 5 | SPDX; frontmatter; no broken links. |
| 9 | Observability | 5 | 1.0 | 5 | OTEL spans, events, metrics. |
| 10 | Operational readiness | 10 | 1.0 | 10 | Two-mode rollback; upgrade contract. |
| | **Total** | 100 | | **92.5** | |

Verdict: SHIP

### Iteration 3 — 2026-04-20

| # | Category | Weight | Ratio | Score | Notes |
|---|---|---:|---:|---:|---|
| 1 | Scope clarity | 10 | 1.0 | 10 | D26 Tenant CRD amendment in Context; Mode A label-selector reconcile in D-01.2. |
| 2 | Architecture fit | 10 | 1.0 | 10 | D26 amendment to D23 documented; Tenant CRD delegates aggregation, does not duplicate Capsule. |
| 3 | Security posture | 15 | 1.0 | 15 | D-01.7: VAP immutability + removal-denial for tenant label; two concrete CEL clauses; Kyverno alternative deferred with rationale. |
| 4 | Automatability | 10 | 0.5 | 5 | VAP manifest path stated; `check-capsule-api.sh` still TBD pre-gate. |
| 5 | Verifiability | 15 | 0.5 | 7.5 | CEL clauses are concrete and testable; envtest/kuttl still awaiting spec phase. |
| 6 | Failure-mode awareness | 10 | 1.0 | 10 | Seventh mode: Tenant CR deletion with live Workspaces; finalizer blocks. |
| 7 | Context efficiency | 10 | 1.0 | 10 | Exactly 200 lines after trim; single responsibility. |
| 8 | Docs quality | 5 | 1.0 | 5 | `depends` updated; 24 cross-ref added; last_verified bumped. |
| 9 | Observability | 5 | 1.0 | 5 | Loki secondary audit destination with retention params and redaction note. |
| 10 | Operational readiness | 10 | 1.0 | 10 | Tenant-deletion finalizer; binding strategy cross-ref to 24 iter-1. |
| | **Total** | 100 | | **97.5** | |

Verdict: SHIP (97.5 ≥ 95 target)

Top gaps: (1) `check-capsule-api.sh` not yet authored — pre-gate acceptable; (2) envtest/kuttl test names await spec phase; (3) VAP + Kyverno manifests deferred to post-gate controller phase.
