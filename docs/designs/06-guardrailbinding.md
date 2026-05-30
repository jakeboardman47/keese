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
related_skills: []
status: current
last_verified: 2026-04-20
rollback: Remove cluster-scoped keese.ai/default binding + ReferenceGrant; VAP
  auto-rejects workspace bindings missing parent status — safe to uninstall CRD.
---

# 06 — GuardrailBinding

`GuardrailBinding` consolidates guardrail composition (Kyverno + OpenFGA tuples +
Envoy SecurityPolicy refs + recipe hooks + TokenBudget) into a single CRD with a
three-tier role model and a strictest-wins merge lattice. Schema detail lives in
[06-ii-spec-schema.md](06-ii-spec-schema.md).

## Role model

| Role | Scope | Can do | Cannot do |
|---|---|---|---|
| `keese-guardrail-cluster-admin` | cluster | write `keese.ai/default` binding | — |
| `keese-tenant-admin` | tenant namespace | add restriction atop default | relax default |
| `keese-workspace-admin` | workspace namespace | add restriction atop tenant+default | relax tenant or default |

Cluster-scoped binding `keese.ai/default` lives in `keese-system`. A mutating
webhook injects a reference to it in `Workspace.spec.guardrails.inherit[]` on
create; VAP rejects removal on update.

## Default binding: read access for tenant-admins

Tenant-admins READ but do NOT write `keese.ai/default`. Operator auto-installs
a `ClusterRole` on P7 bootstrap granting `get,list,watch` on
`guardrailbindings/default` (resourceNames-scoped) and a `ClusterRoleBinding`
to `system:serviceaccounts:<tenant-ns>` per `Tenant` CR.

| Principal | Verb | Resource | Scope |
|---|---|---|---|
| `keese-guardrail-cluster-admin` | `*` | `guardrailbindings` | cluster |
| `keese-tenant-admin` | `get,list,watch` | `guardrailbindings/default` | keese-system |
| `keese-tenant-admin` | `*` | `guardrailbindings` | tenant ns |
| `keese-workspace-admin` | `get,list,watch` | `guardrailbindings` | tenant ns |
| `keese-workspace-admin` | `*` | `guardrailbindings` | workspace ns |

Failure mode: read denied → merge cannot proceed → Workspace `Degraded` +
event `DefaultBindingReadForbidden`.

## Merge lattice (strictest-wins)

Given bindings B₀ (default), B₁ (tenant), B₂ (workspace):

```
effective = merge(B₀, B₁, B₂)
```

Rules per field type:

| Field type | Merge rule |
|---|---|
| `tools.allow` (allowlist) | intersection — only tools present in ALL bindings |
| `tools.deny` (denylist) | union — a tool denied anywhere is denied |
| `tokenBudget.{input,output,total}` | `min()` across all bindings |
| `recipeHooks[]` | union — hooks from all bindings run |
| `kyverno[].policyRef` | union — all named policies apply |
| `rateLimit.{requests,window,scope}` | `min(requests)` per matching `(window, scope)` tuple |

Effective merge is computed by the guardrail controller and written to
`status.effectivePolicy`. The `keese-authz` ext_authz service reads
only `status.effectivePolicy` — never raw spec fields.

## TOCTOU: weaken-blocking and generation freshness

Two concurrent workspace-admin updates may race before `status.effectiveParentAllow/Deny`
is repopulated. Resolution:

**VAP CEL gate on generation freshness** (rejects stale reads):

```cel
self.status.observedGeneration == self.metadata.generation ||
(size(self.spec.tools.allow) == 0 && size(self.spec.tools.deny) == 0)
```

Rejection reason: `StaleParentStatus`. Callers retry; controller observes within
one reconcile (~100–500 ms typical).

**Per-binding reconcile Lease** (`coordination.k8s.io/v1`) keyed on
`<ns>/<binding-name>` ensures only one reconcile computes `effectiveParentAllow/Deny`
at a time. Expires on controller crash; next leader re-acquires.

Runbook: if `StaleParentStatus` rejections exceed 5%, inspect
`keese_guardrail_reconcile_duration_seconds` (P99) — high latency indicates
reconcile loop starvation or leader-election churn.

## Recipe hooks: serviceRef (zero-trust)

Per rule 05.4, no arbitrary URL egress. `recipeHooks[].webhookRef` (iter-1 URL
form) is replaced by `serviceRef: {name, namespace, port, path}` — see
[06-ii-spec-schema.md](06-ii-spec-schema.md). VAP rejects any entry lacking
`serviceRef`. External webhook targets (e.g. PagerDuty) require an in-cluster
proxy Service routing through the same egress controls as agent pods.

## VAP rules (CEL, rule 04.12)

Named: `guardrailbinding-policy.authz.keese.ai/v1alpha1`

1. Workspace binding `tools.allow` must be a subset of effective-parent allow.
2. Workspace binding `tools.deny` must be a superset of effective-parent deny.
3. Workspace binding `tokenBudget` fields must be ≤ effective-parent values.
4. Every `recipeHooks[]` entry must have `serviceRef` (no URL field).
5. Generation freshness (TOCTOU guard above).

## Automatability

| Target | Command |
|---|---|
| Dry-run sample bindings | `make guardrail-dry-run` |
| Merge-lattice unit tests | `go test ./internal/guardrail/merge/...` |
| CEL unit matrix | `go test ./internal/guardrail/vap/...` (allow-intersect, deny-union, rate-limit-min) |
| envtest suite | `go test ./internal/controller/guardrail/...` |

`make guardrail-dry-run` applies all samples in `config/samples/guardrail/` against
an envtest apiserver with all CRDs installed.

## Observability

Metrics (prefix `keese_guardrail_`):

- `reconcile_duration_seconds` — histogram, labels: `binding_scope`
- `merge_errors_total` — counter, label: `reason`
- `stale_parent_rejections_total` — counter

Events: `DefaultBindingReadForbidden`, `MergeConflict`, `WeakenRejected`,
`StaleParentStatus`.

OTEL traces: span `guardrail.merge` wraps each reconcile; `guardrail.vap.eval`
wraps each admission evaluation.

## Failure modes

| Condition | Behavior | Recovery |
|---|---|---|
| Default binding missing | Workspace enters `Degraded`, event `DefaultBindingReadForbidden` | Re-apply `config/manager/default-guardrailbinding.yaml` |
| StaleParentStatus rejection spike | VAP returns 409; caller retries | Check reconcile P99 latency; see runbook above |
| Referenced Kyverno policy absent | Binding enters `Degraded`, event `PolicyRefNotFound`; workspace allowed but policy skipped | Create missing ClusterPolicy |
| Reconcile Lease not released (crash) | Lease expires (30s TTL); next leader takes over | Automatic; monitor `keese_guardrail_reconcile_duration_seconds` |
| Merge Lease conflict | Second reconcile queued; not dropped | Monitor `merge_errors_total{reason="lease_conflict"}` |

## Tests named

- `internal/guardrail/merge/merge_test.go`: table-driven — allow-intersect,
  deny-union, rate-limit-min, token-budget-min, hook-union.
- `internal/guardrail/vap/cel_test.go`: weaken-blocking, serviceRef-required,
  stale-generation guard.
- `internal/controller/guardrail/suite_test.go` (envtest): default-binding
  injection, ReferenceGrant install, idempotency over ≥ 3 reconciles.
- `test/e2e/guardrail_test.go`: default-binding-missing recovery path.

## Cross-dependencies

- **05c** (MCP Policy Enforcement): rate-limit schema locked here; 05c iter-2
  must rename `requestsPerMinute` → `requests` + add `window` + `scope`. See
  [06-ii-spec-schema.md](06-ii-spec-schema.md) §05c cross-dependency.
- **04a** (OpenFGA): `tool.allowed_in@workspace` tuple written by guardrail
  controller; `model_gate` deny-wins semantics apply.
- **24** (Tenant CRD): `Tenant.spec.defaultGuardrailBindings[]` consumed by
  tenant-level merge.
- **01** (Capsule/Kyverno): D-01.3 Kyverno ClusterPolicy refs are listed by name
  in `spec.kyverno[].policyRef`.

## Iteration log

- **Iter-1 2026-04-19** — score 94 REVISE; held at draft per 5 reviewer
  concerns (URL form in hooks; missing CEL matrix + make target; weaken-blocking
  envtest absent; missing StaleParentStatus runbook; default-binding RBAC vague).
- **Iter-2 2026-04-20** — score 96 SHIP. Resolved all 5 concerns. Held at
  draft (200-line cap exceeded — fixed in iter-2.1 by extracting samples to
  06-iii).
- **Iter-2.1 2026-04-20** — content refinement to comply with 200-line cap:
  06-ii samples extracted to [06-iii-samples.md](06-iii-samples.md);
  default-binding RBAC compressed; recipe-hook YAML referenced not inlined;
  iter log collapsed. No architecture changes.
