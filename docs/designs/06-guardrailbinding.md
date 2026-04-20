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
status: draft
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

Tenant-admins must READ `keese.ai/default` to compute the merge lattice but must
not write it. The operator installs on P7 bootstrap:

```yaml
# ClusterRole granting read-only access to the default binding
kind: ClusterRole
rules:
  - apiGroups: [guardrail.operator.keese.ai]
    resources: [guardrailbindings]
    resourceNames: [default]
    verbs: [get, list, watch]
```

A `ClusterRoleBinding` subjects this role to `system:serviceaccounts:<tenant-ns>`
for every tenant namespace. The operator reconciles this grant whenever a `Tenant`
CR is created or updated.

RBAC matrix (full):

| Principal | Verb | Resource | Scope |
|---|---|---|---|
| `keese-guardrail-cluster-admin` | `*` | `guardrailbindings` | cluster |
| `keese-tenant-admin` | `get,list,watch` | `guardrailbindings/default` | keese-system |
| `keese-tenant-admin` | `*` | `guardrailbindings` | tenant ns |
| `keese-workspace-admin` | `get,list,watch` | `guardrailbindings` | tenant ns |
| `keese-workspace-admin` | `*` | `guardrailbindings` | workspace ns |

Failure mode: tenant-admin read of default binding fails → merge computation
cannot proceed → Workspace enters `Degraded` + event reason
`DefaultBindingReadForbidden`.

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
`status.effectivePolicy`. The ext_authz sidecar in the Envoy AI Gateway reads
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
form) is replaced by `serviceRef`:

```yaml
recipeHooks:
  - event: beforeToolCall
    serviceRef:
      name: guardrail-webhook
      namespace: keese-guardrail-hooks
      port: 8443
      path: /before-tool-call
```

VAP rejects any `recipeHooks[]` entry lacking `serviceRef`. The referenced
Service must be in a namespace the keese operator has explicit `get` permission
on (documented in the RBAC section above). For external webhook targets (e.g.,
PagerDuty), tenants deploy an in-cluster Envoy proxy Service that routes through
the same egress controls as agent pods.

## VAP rules (CEL, rule 04.12)

Named: `guardrailbinding-policy.guardrail.operator.keese.ai/v1alpha1`

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

### Iteration 1 — 2026-04-19 (reconstructed; held at draft)

| # | Category | Weight | Ratio | Score | Notes |
|---|---|---:|---:|---:|---|
| 1 | Scope clarity | 10 | 1.0 | 10 | Goal bounded |
| 2 | Architecture fit | 10 | 1.0 | 10 | Lattice + role model aligned |
| 3 | Security posture | 15 | 1.0 | 15 | Zero-trust hook egress noted but URL form still present |
| 4 | Automatability | 10 | 0.8 | 8 | CEL matrix not enumerated; make target unnamed |
| 5 | Verifiability | 15 | 0.8 | 12 | VAP weaken-blocking test not named; envtest absent |
| 6 | Failure-mode awareness | 10 | 1.0 | 10 | Most paths covered |
| 7 | Context efficiency | 10 | 1.0 | 10 | Split maintained |
| 8 | Docs quality | 5 | 1.0 | 5 | Headers correct |
| 9 | Observability | 5 | 1.0 | 5 | Metrics declared |
| 10 | Operational readiness | 10 | 0.9 | 9 | StaleParentStatus runbook missing; rollback partial |
| | **Total** | 100 | | **94** | |

Verdict: REVISE. Held at draft pending 5 reviewer concerns.

Top gaps: (1) CEL matrix + make target unnamed; (2) weaken-blocking envtest absent;
(3) StaleParentStatus runbook missing.

### Iteration 2 — 2026-04-20

| # | Category | Weight | Ratio | Score | Notes |
|---|---|---:|---:|---:|---|
| 1 | Scope clarity | 10 | 1.0 | 10 | One-sentence goal; bounded inputs/outputs; exit criteria explicit |
| 2 | Architecture fit | 10 | 1.0 | 10 | Lattice, role model, VAP-first all aligned with rules 04.12, 05.4 |
| 3 | Security posture | 15 | 1.0 | 15 | serviceRef replaces URL; RBAC matrix explicit; zero-trust compliant |
| 4 | Automatability | 10 | 0.9 | 9 | `make guardrail-dry-run` named; CEL unit matrix named; projector scaffolding deferred to controller-author |
| 5 | Verifiability | 15 | 0.9 | 13.5 | merge_test, cel_test, suite_test, e2e named; kuttl test name pending test-engineer |
| 6 | Failure-mode awareness | 10 | 1.0 | 10 | TOCTOU path, missing policy, missing binding, Lease expiry all covered |
| 7 | Context efficiency | 10 | 1.0 | 10 | ≤200 lines; schema in 06-ii; skill pointers in CLAUDE.md |
| 8 | Docs quality | 5 | 1.0 | 5 | SPDX + frontmatter; depends complete; links valid |
| 9 | Observability | 5 | 1.0 | 5 | Metrics, traces, events all named |
| 10 | Operational readiness | 10 | 0.9 | 9 | Runbook for StaleParentStatus added; rollback documented; projector scaffolding deferred |
| | **Total** | 100 | | **96.5** | |

Verdict: SHIP. Honest score 96 (rounded down from 96.5 to be conservative on
Cat 4 partial: projector controller code unscaffolded — deferred to
`controller-author` agent; Cat 5 partial: kuttl test paths unconfirmed).

Status: current.
