<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: spec
category: contract
depends:
  - ../designs/06-guardrailbinding.md
  - ../designs/06-ii-spec-schema.md
  - ../designs/06-iii-samples.md
  - ../designs/05c-mcp-policy-enforcement.md
related_skills: [guardrail-author, crd-authoring]
status: current
last_verified: 2026-04-21
regression_lock: false
tests:
  unit:
    - internal/guardrail/merge/merge_test.go
    - internal/guardrail/vap/cel_test.go
  envtest:
    - internal/controller/guardrail/suite_test.go
  kuttl:
    - test/e2e/guardrail_test.go
metrics:
  - keese_guardrail_reconcile_duration_seconds
  - keese_guardrail_merge_errors_total
  - keese_guardrail_stale_parent_rejections_total
events:
  - BindingMerged
  - EffectivePolicyComputed
  - DefaultBindingMissing
  - MergeConflict
  - DefaultBindingReadForbidden
  - WeakenRejected
  - StaleParentStatus
  - PolicyRefNotFound
---

# authz.keese.ai v1alpha1 — spec

**Scope:** one kind — `GuardrailBinding`. Collapses Constitution +
GuardrailPolicy + ToolAllowList (D14) into a single composition CRD that
spans Cluster / Tenant / Workspace scopes with strictest-wins merge.
Output: `status.effectivePolicy`, consumed by Recipe + Workspace admission.

## 1. CRD identity

| Field | Value |
|---|---|
| Group | `authz.keese.ai` |
| Version | `v1alpha1` |
| Kind | `GuardrailBinding` |
| Scope | Namespaced (cluster-default lives in `keese-system`) |
| SSA fieldOwner | `keese-guardrailbinding-controller` |
| Finalizer | `finalizers.guardrailbinding.keese.ai/cleanup` |

Printer columns: `Age`, `Ready` (conditions[Ready].status), `Phase`
(status.phase), `Scope` (`keese.ai/binding-scope` label), `ObservedGen`
(status.observedGeneration).

CEL VAP name: `guardrailbinding-policy.authz.keese.ai/v1alpha1`
(rule 04.12 — VAP first).

## 2. Spec schema

Canonical field-by-field schema with `[05c-lock]` annotations lives in
[06-ii-spec-schema.md](../designs/06-ii-spec-schema.md). Summary of top-level
fields:

| Field | Merge rule |
|---|---|
| `spec.tools.allow` | intersection across all bindings |
| `spec.tools.deny` | union across all bindings |
| `spec.tools.rateLimit` | `min(requests)` per `(window, scope)` |
| `spec.tokenBudget.{input,output,total}` | `min()` across all bindings |
| `spec.recipeHooks[]` | union — all hooks run |
| `spec.kyverno[].policyRef` | union — all named ClusterPolicies apply |
| `spec.openfga.configMapRef` | per-binding; controller writes merged tuples |
| `spec.envoy.securityPolicyRef` | per-binding; SSA-applied by controller |
| `spec.inherit[]` | controller resolves chain at merge time |

ReBAC markers (rule 04.14): `+keese:rebac-tuple=tool#allowed_in@workspace`
(allow entries), `+keese:rebac-tuple=tool#denied_in@workspace` (deny entries),
`+keese:rebac-tuple=workspace#has_budget` (tokenBudget.total).

## 3. Status schema

Status schema lives in [06-ii-spec-schema.md §Status schema](../designs/06-ii-spec-schema.md).
Key fields:

- `status.phase`: `Ready | Degraded | Pending`
- `status.observedGeneration`: set on every reconcile; compared by TOCTOU VAP
- `status.effectivePolicy`: merged output consumed by Recipe + Workspace admission
- `status.conditions[]`: `Ready`, `ParentReadable`

## 4. Default-binding auto-injection

Mutating webhook injects `keese.ai/default` (ns `keese-system`) into
`Workspace.spec.guardrails.inherit[]` on Workspace create; VAP (CEL) rejects
removal on update. Operator installs `ClusterRole` granting `get,list,watch`
on `guardrailbindings/default` (resourceNames-scoped) + `ClusterRoleBinding`
to `system:serviceaccounts:<tenant-ns>` per `Tenant` CR on bootstrap.
Failure: read denied → Workspace `Degraded` + `DefaultBindingMissing`.

## 5. Strictest-wins merge lattice

Given bindings B₀ (cluster default), B₁ (tenant), B₂ (workspace):
`effective = merge(B₀, B₁, B₂)` per field-type rules in §2 above.
Full merge table in [06-guardrailbinding.md §Merge lattice](../designs/06-guardrailbinding.md).

Controller writes merged output to `status.effectivePolicy`. Gateway
ext_authz sidecar reads ONLY `status.effectivePolicy` — never raw spec.

## 6. TOCTOU guard (cross-cut with design 16)

VAP CEL: `self.status.observedGeneration == self.metadata.generation ||
(size(self.spec.tools.allow)==0 && size(self.spec.tools.deny)==0)`.
Rejection reason: `StaleParentStatus` (409). Callers retry; controller
converges within one reconcile (~100–500 ms). Per-binding
`coordination.k8s.io/v1` Lease keyed on `<ns>/<name>` prevents concurrent
merge races; TTL 30 s. Recipe + Workspace admission read
`status.effectivePolicy.observedGeneration` and reject stale reads identically.

## 7. RBAC, finalizer, and SSA

RBAC markers on reconciler (rule 04.9): `guardrailbindings` (CRUD),
`guardrailbindings/status` (get;update;patch), `kyverno.io/clusterpolicies`
(get;list;watch), `gateway.envoyproxy.io/securitypolicies`
(get;list;watch;create;update;patch), `coordination.k8s.io/leases`
(get;create;update).

Finalizer `finalizers.guardrailbinding.keese.ai/cleanup` cleans up:
operator-owned Kyverno `ClusterPolicy` copies, OpenFGA tuples (SSA delete on
`openfga.configMapRef` entries), and Envoy `SecurityPolicy` objects bearing
`fieldOwner: keese-guardrailbinding-controller`.

## 8. VAP rules

VAP `guardrailbinding-policy.authz.keese.ai/v1alpha1` enforces:
(1) workspace `tools.allow` ⊆ parent allow; (2) workspace `tools.deny` ⊇
parent deny; (3) workspace `tokenBudget.*` ≤ parent; (4) `recipeHooks[]`
entries require `serviceRef` (no URL); (5) generation freshness (§6).

## 9. Observability

Metrics (prefix `keese_guardrail_`): `reconcile_duration_seconds{binding_scope}`,
`merge_errors_total{reason}`, `stale_parent_rejections_total`. OTEL spans:
`guardrail.merge` (reconcile), `guardrail.vap.eval` (admission). Event reasons
in `internal/controller/guardrail/<kind>/events.go` — see frontmatter `events:`.

## 10. Acceptance tests

| Test | File | Assertion |
|---|---|---|
| Default-binding auto-injection | `suite_test.go` | Webhook injects inherit ref on Workspace create; VAP rejects removal |
| Strictest-wins lattice | `merge_test.go` | allow-intersect, deny-union, rate-limit-min, budget-min, hook-union |
| TOCTOU stale rejection | `cel_test.go` | VAP returns 409 `StaleParentStatus` when observedGen lags generation |
| Merge conflict | `suite_test.go` | `MergeConflict` event; Workspace `Degraded`; reconcile retried |
| Finalizer cleanup | `suite_test.go` | Delete → OpenFGA tuples + SecurityPolicy + ClusterPolicy removed |
| Weaken-blocking | `cel_test.go` | workspace-admin cannot relax tenant/cluster allow/deny/budget |
| ServiceRef required | `cel_test.go` | VAP rejects recipeHooks entry with URL field |
| Idempotency | `suite_test.go` | 3 reconciles with no spec change produce identical status |

## 11. Samples and failure modes

Six canonical samples (cluster / tenant / workspace × minimal / full) in
[06-iii-samples.md](../designs/06-iii-samples.md). Validated by
`make guardrail-dry-run` against envtest API server.

| Condition | Behavior | Recovery |
|---|---|---|
| Default binding missing | Workspace `Degraded`; `DefaultBindingMissing` | Re-apply `config/manager/default-guardrailbinding.yaml` |
| StaleParentStatus spike | 409; caller retries | Check reconcile P99; 06 runbook |
| Kyverno policyRef absent | Binding `Degraded`; `PolicyRefNotFound`; workspace allowed | Create missing ClusterPolicy |
| Reconcile Lease held on crash | Expires (30 s); leader re-acquires | Automatic |
| OpenFGA configMapRef unreadable | `ParentReadable=False`; `DefaultBindingReadForbidden` | Fix RBAC or re-create ConfigMap |

## Iteration log

### Iteration 1 — 2026-04-21 (Correctness & security)

| # | Category | Wt | Ratio | Score | Notes |
|---|---|---:|---:|---:|---|
| 1 | Scope clarity | 10 | 1.0 | 10 | Single kind; bounded I/O; exit criterion explicit. |
| 2 | Architecture fit | 10 | 1.0 | 10 | Aligns 06/06-ii/06-iii/05c; SSA + VAP-first; no rule violations. |
| 3 | Security posture | 15 | 1.0 | 15 | ReBAC markers; serviceRef-only; TOCTOU VAP; fail-closed; RBAC least-priv. |
| 4 | Automatability | 10 | 0.5 | 5 | make + test file names declared; controller code pre-gate — acceptable dock. |
| 5 | Verifiability | 15 | 0.5 | 7.5 | 8 acceptance tests named with file+assertion; not runnable pre-gate — acceptable dock. |
| 6 | Failure-mode awareness | 10 | 1.0 | 10 | 5 modes with detection + recovery; Lease expiry; TOCTOU runbook ref. |
| 7 | Context efficiency | 10 | 1.0 | 10 | Schema/samples deferred to 06-ii/06-iii; no inline blobs. |
| 8 | Docs quality | 5 | 1.0 | 5 | SPDX; frontmatter arrays complete; depends full; no broken links. |
| 9 | Observability | 5 | 1.0 | 5 | 3 metrics; 2 OTEL spans; 8 event reasons in frontmatter. |
| 10 | Operational readiness | 10 | 1.0 | 10 | Finalizer cleanup; Lease HA; rollback via re-apply; idempotency test named. |
| | **Total** | 100 | | **92.5** | |

Verdict: SHIP (92.5 ≥ 90). Cat 4/5 docks are pre-gate acceptable.
Top gaps: (1) Cat 4: controller + make target not authored — controller-author backlog.
(2) Cat 5: test bodies not runnable — test-engineer backlog. (3) None blocking.
Next step: flip `status: current`.
