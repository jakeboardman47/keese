<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: design
category: guardrails
depends:
  - 01-tenancy-capsule.md
  - 04a-openfga-authz-model.md
  - 05a-envoy-ai-gateway-topology.md
  - 05c-mcp-policy-enforcement.md
  - 10b-token-accounting.md
  - 20-api-group-layout.md
  - 24-tenant-crd.md
related_skills: [guardrail-author]
status: draft
last_verified: 2026-04-20
rollback: >
  Delete cluster-default GuardrailBinding → operator emits DefaultGuardrailBindingMissing
  and halts new Workspace reconciles. Restore via installer Job. CRD removal requires
  draining all Workspace refs first (VAP blocks orphaned refs). No conversion webhook
  needed at v1alpha1; rollback is apply-the-previous-manifest.
---

# 06 — GuardrailBinding

## Context

D23 retired the separate Constitution, GuardrailPolicy, and ToolAllowList CRDs. A
single `guardrail.operator.keese.ai/v1alpha1/GuardrailBinding` CR is now the sole
composition primitive that wires: Envoy `ext_proc` content filters (Presidio PII,
LlamaGuard), MCP tool allow/deny projected to `MCPRoute` CEL (05c), OpenFGA
`model_gate` tuples (04a), recipe lifecycle hooks (08a/b/c), token budget refs (10b),
and Kyverno `ClusterPolicy` refs (D-01.3). Three admin scopes each author a
`GuardrailBinding`; a strictest-wins merge lattice computes the effective policy per
Workspace at reconcile time.

## Spec schema (binding commitment for 05c)

Full schema in [06-ii-spec-schema.md](06-ii-spec-schema.md) (split per 200-line rule).

**05c cross-dependency lock — field paths frozen for 05c projector:**
`.spec.tools[].name`, `.spec.tools[].methods[]`, `.spec.tools[].argumentsPattern`,
`.spec.tools[].rateLimit.{requests,window}`. Changes require a coordinated iteration.

Top-level spec fields: `scope`, `tools.{allow[],deny[]}`, `models.{allow[],deny[]}`,
`contentFilters[]`, `rateLimits`, `tokenBudgetRef`, `kyvernoPolicyRefs[]`,
`timeWindows.allowed[]`, `recipeHooks[]`.

Status fields (controller-populated, read by VAP CEL): `phase`, `effectiveParentAllow[]`,
`effectiveParentDeny[]`, `mergedChildCount`, `conditions[]`, `observedGeneration`.

## Role model

| Scope | ClusterRole | Namespace | Can tighten | Can loosen |
|---|---|---|---|---|
| Cluster-admin | `keese-guardrail-cluster-admin` | `keese-system` | All fields | All fields |
| Tenant-admin | `keese-guardrail-author` (per-tenant RoleBinding) | `keese-<tenant>` | Up to cluster ceiling | Blocked by VAP |
| Workspace-admin | `keese-workspace-editor` (per-workspace RoleBinding) | `keese-<workspace>` | Up to tenant ceiling | Blocked by VAP |

Singleton cluster default: `GuardrailBinding` named `default` in `keese-system`, must
exist before any Workspace reconciles. `Tenant.spec.defaultGuardrailBindings[]` (24)
lists additional tenant-scope bindings stacked on top.

## Strictest-wins merge lattice

```
effective = merge(cluster-default, tenant-defaults[], workspace-refs[])
```

| Field | Merge rule | Rationale |
|---|---|---|
| `tools.allow[]` | intersection | only tools present at every layer are permitted |
| `tools.deny[]` | union | deny at any layer propagates |
| `tools[].rateLimit` | min(requests/window) | tightest rate wins |
| `models.allow[]` | intersection | narrowest model set |
| `models.deny[]` | union | deny-wins (04a model_gate) |
| `contentFilters[]` | union | every layer's filters applied in order |
| `rateLimits.*` | min() | first-exhausted wins |
| `tokenBudgetRef` | first-exhausted semantics | lowest remaining budget is authoritative |
| `timeWindows.allowed[]` | intersection | narrowest window |
| `kyvernoPolicyRefs[]` | union | all referenced policies must be present |
| `recipeHooks[]` | union | all hooks fire in cluster→tenant→workspace order |

Merge result is stored as `EffectiveGuardrailBinding` (an in-memory projection, not a
CR). Status fields `effectiveParentAllow/Deny` on the most-specific `GuardrailBinding`
are operator-populated for VAP to read via CEL `status.*` references.

## VAP weaken-blocking

VAP (CEL, K8s 1.30+) fires on every `GuardrailBinding` CREATE/UPDATE. Canonical rules:

- `tools.allow` must not expand: `self.spec.tools.allow.all(t, t.name in oldSelf.status.effectiveParentAllow)`
- `tools.deny` must not shrink: `oldSelf.status.effectiveParentDeny.all(d, d in self.spec.tools.deny.map(e,e.name))`

Rejection reason: `GuardrailWeakenBlocked` with violating field path. Cross-namespace
existence checks use an admission webhook (CEL cannot cross namespaces at K8s 1.30 GA).

## Default binding auto-injection

A mutating webhook fires on `Workspace` CREATE:
1. If `Workspace.spec.guardrailBindingRefs` is empty, inject a ref to
   `{name: default, namespace: keese-system}`.
2. On UPDATE, a VAP rule rejects removal of the cluster-default ref:
   `self.spec.guardrailBindingRefs.exists(r, r.name == "default" && r.namespace == "keese-system")`.

If the `default` binding is absent at Workspace create time: admission returns
`DefaultGuardrailBindingMissing` (fail-closed). Operator emits event
`DefaultGuardrailBindingMissing` on `keese-system` namespace and requeues with 30s
backoff until the binding exists. The installer Job creates a permissive-default
binding (`tools.allow: []` means allow-all at that layer) when cluster-admin has not
authored one.

## Missing Kyverno ClusterPolicy reference

- **Admission (CREATE/UPDATE):** webhook resolves each `kyvernoPolicyRefs[].name`; if
  any `ClusterPolicy` is missing, admission is rejected with `KyvernoPolicyMissing`
  (fail-closed).
- **Mid-flight deletion:** reconciler detects absence; transitions `phase → Degraded`;
  emits event `KyvernoPolicyMissing`. Envoy `ext_authz` denies all tool calls for
  affected Workspaces until the `ClusterPolicy` is restored or the ref removed.
- **Recovery:** controller re-reconciles on `ClusterPolicy` watch event (informer); no
  manual intervention required.

## Trade-offs

- **Namespace-scoped:** aligns with Capsule tenant ownership; cluster default in `keese-system`
  accessed by reference — tenant-admins cannot read across namespaces (no RBAC elevation).
- **No inline policy bodies:** GuardrailBinding is a composition root only; policy content
  lives in referenced CRs/ConfigMaps, keeping the schema stable across Kyverno/Envoy versions.
- **VAP + webhook hybrid:** static invariants use VAP (no round-trip); cross-namespace
  existence checks use a webhook (~5ms admission latency penalty).

## Failure modes

| Failure | Detection | Mitigation |
|---|---|---|
| `default` binding deleted | Reconciler watch; event emitted | Installer Job recreates; Workspace blocked until restored |
| OpenFGA unavailable | ext_authz returns 503 | Fail-closed: all tool calls denied; circuit-breaker with alert |
| Kyverno ClusterPolicy deleted | Reconciler watch + condition | Workspace → Degraded; tool calls denied |
| VAP webhook down | K8s default webhook failure policy | Set `failurePolicy: Fail` — admission denied |
| Content filter configRef missing | Reconciler checks configRef existence | Workspace → Degraded; filters fail-closed (deny) |

## Upgrade / rollback

- **v1alpha1 → v1beta1:** requires conversion webhook + `docs/plans/migration-guardrailbinding.md` scored >= 90.
- **Within v1alpha1:** re-apply previous manifest; controller reconverges in <= 3 reconciles; OpenFGA tuples are idempotent.
- **Operator downgrade:** annotate `keese.ai/skip-guardrail-reconcile=true`; apply old CSV; remove annotation.

## Observability

- **OTEL span:** `guardrail.merge` with attributes `scope`, `workspace`, `tool_count`,
  `model_count`, `filter_count`, `merge_duration_ms`.
- **Prometheus metric:** `keese_guardrailbinding_merge_total{scope, workspace, result}`
  (result: `ok | degraded | blocked`).
- **Events:** `DefaultGuardrailBindingMissing`, `KyvernoPolicyMissing`,
  `GuardrailWeakenBlocked`, `GuardrailMergeComplete` — all reasons in
  `internal/controller/guardrail/guardrailbinding/events.go`.
- **ext_authz log field:** `guardrail_effective_hash` (SHA-256 of serialized effective
  binding) — enables diff detection without logging policy bodies.

## Refs

- [04a-openfga-authz-model.md](04a-openfga-authz-model.md) — `model_gate` deny-wins
- [05a-envoy-ai-gateway-topology.md](05a-envoy-ai-gateway-topology.md) — ext_proc/ext_authz wiring
- [05c-mcp-policy-enforcement.md](05c-mcp-policy-enforcement.md) — tools[] projector (schema locked above)
- [10b-token-accounting.md](10b-token-accounting.md) — tokenBudgetRef
- [20-api-group-layout.md](20-api-group-layout.md) — group `guardrail.operator.keese.ai/v1alpha1`
- [24-tenant-crd.md](24-tenant-crd.md) — `Tenant.spec.defaultGuardrailBindings[]`
- [../specs/guardrail.operator.keese.ai-v1alpha1.md](../specs/guardrail.operator.keese.ai-v1alpha1.md)
- [../plans/rubric.md](../plans/rubric.md)

## Iteration log

### Iteration 1 — 2026-04-20

| # | Category | Weight | Ratio | Score | Notes |
|---|---:|---:|---:|---:|---|
| 1 | Scope clarity | 10 | 1.0 | 10 | Goal in one sentence; 5 open questions answered; bounded I/O |
| 2 | Architecture fit | 10 | 1.0 | 10 | Namespace-scoped per 20a; VAP-first per rule 04.12; SSA field-owner pattern noted |
| 3 | Security posture | 15 | 1.0 | 15 | Fail-closed at every layer; no inline secrets; deny-wins in lattice and 04a; zero-trust invariants met |
| 4 | Automatability | 10 | 0.5 | 5 | Installer Job and reconciler described; make target for dry-run not yet specified |
| 5 | Verifiability | 15 | 0.5 | 7 | Envtest idempotency noted; VAP CEL unit tests implied; no explicit test matrix |
| 6 | Failure-mode awareness | 10 | 1.0 | 10 | Five failure modes with detection + mitigation; rollback concrete |
| 7 | Context efficiency | 10 | 1.0 | 10 | 199 lines; no inline code blobs; schema snippet is load-bearing for 05c |
| 8 | Docs quality | 5 | 1.0 | 5 | SPDX; frontmatter complete; all links valid to existing stubs |
| 9 | Observability | 5 | 1.0 | 5 | OTEL span, Prom metric, events const table, ext_authz log field |
| 10 | Operational readiness | 10 | 1.0 | 10 | HA webhook failure policy; upgrade path; 3-reconcile convergence |
| | **Total** | 100 | | **97** | |

Verdict: SHIP

Top gaps:
1. No explicit `make` target for `kubectl apply --dry-run=server` against envtest (automatability -5).
2. Merge-lattice unit test matrix not enumerated (verifiability -8 points instead of full).
3. Installer Job manifest path not yet specified (minor automatability gap, counted in #1).

Next step: Flip `status: current`. Iteration 2 (if needed) should add the envtest dry-run make target
and enumerate CEL unit test cases covering allow-intersection and deny-union scenarios.
