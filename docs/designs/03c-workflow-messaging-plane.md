<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: design
category: workflow
depends:
  - 03-workflow-argo-delegation.md
  - 04a-openfga-authz-model.md     # iter-5: tenant.allows_messaging + workspace.messageable_from
  - 04b-projected-sa-identity.md   # iter-3: workflowRun audience template
  - 09-transport-crd.md            # iter-3: spec.a2a.scope consumes topic-naming contract
  - 25-cross-tenant-agreement.md   # D29: Approved CRA required for cross-tenant participants
related_skills: [controller-authoring]
status: current
last_verified: 2026-04-21
rollback: |
  NATS stream teardown is idempotent (delete-if-exists). If an iter-3 deployment is
  rolled back, in-flight streams remain until TTL expiry or manual `nats stream delete`.
  Per-run streams are owner-ref'd to Argo Workflows — GC removes them when the Workflow
  is deleted regardless of operator version. CrossTenantAgreement admission check is
  additive: removing it on rollback relaxes admission (fail-open risk); log via
  MEMORY.md if bypassed under break-glass.
---

# 03c — Workflow Messaging Plane

Companion to [03](03-workflow-argo-delegation.md). Documents the four messaging-plane
duties added to the Workflow controller in iter-3 (2026-04-21).

## 1. NATS topic provisioning

At WorkflowRun creation the controller calls JetStream `AddStream`:

| Config field | Value |
|---|---|
| `name` | `keese-tenant-<tenant-uid>-wf-<run-uid>` |
| `subjects` | `["keese.tenant.<t>.wf.<r>.>"]` |
| `retention` | `workqueue` |
| `maxAge` | `WorkflowRun.spec.timeout` |
| `storage` | `file` |
| `replicas` | `3` |

Sub-topics (e.g., `keese.tenant.<t>.wf.<r>.steps.alpha`, `…events`) are created by the
recipe, not the controller. Topic existence within the prefix IS intra-tenant authz — the
NATS server validates JWT audience; no per-message ReBAC check needed. Tenant-scoped
audit log: grep `keese.tenant.<t>.*`.

The stream is owner-ref'd to the Argo Workflow → GC on Workflow deletion. Cleanup also
via `ttlStrategy.secondsAfterCompletion` (default 7 d). RBAC marker on the controller:
`// +keese:rebac-tuple=workspace:messageable_from` (covers stream provisioning as an
authz side-effect of the WorkflowRun existing in the workspace).

Failure: `NATSStreamCreateFailed` event; WorkflowRun stays Pending; controller retries
with backoff.

## 2. Per-WorkflowRun SA audience injection

At Argo Workflow projection time the controller SSA-patches each step pod's projected
ServiceAccount token to add the `workflowRun` audience (`keese-wf-<run-uid>`), in
addition to the existing `egress` and `supervisor` audiences from the Workspace token.

SSA patch target: `volumes[name=keese-sa-token].projected.sources[].serviceAccountToken`
per Argo template. TTL ≤ 600 s (rule 05.3); kubelet rotates at 80%.

Dependency: requires the `workflowRun` named template from `OIDCProvider.spec.audienceTemplates`
(04b iter-3). If the template is absent, the controller raises `MissingWorkflowAudience`
and leaves WorkflowRun Pending.

RBAC marker: `// +keese:rebac-tuple=workspace:messageable_from` (audience scopes the
step pod's messaging capability to its own run stream).

## 3. CrossTenantAgreement admission check

Cross-tenant peers are derived **implicitly** from `Workflow.spec.templates[]`:
the controller scans every `transportRef` in the templates and resolves each
referenced `transport.operator.keese.ai/v1alpha1/Transport`. Any Transport with
`spec.a2a.scope: cross-tenant` (09 iter-3) carries an `endpoint` whose target
workspace is in a different tenant — that resolved workspace is a cross-tenant
peer. **No new top-level WorkflowRun spec field is needed**; the Transport CR is
the declarative surface for cross-tenant intent. (Decision Q2(b), 2026-04-21.)

Implementation: VAP first (CEL, K8s 1.30 GA) cannot do the cross-resource fan-out
walk alone — admission webhook fetches each `transportRef`, dereferences the
peer Workspace + Tenant, then runs the CRA lookup below per derived peer
(rule 04.12; webhook scope justified by cross-resource dereference).

Lookup (per derived cross-tenant peer `p`):

```
CrossTenantAgreement where:
  spec.from.tenantRef  == thisWorkflowRun.tenant
  spec.to.tenantRef    == p.tenant
  spec.from.workspaceSelector matches thisWorkspace
  spec.to.workspaceSelector   matches p.workspace
  status.phase         == Approved
  status.expiresAt     > now
```

No matching Approved CRA → admission rejects with `CrossTenantAgreementMissing`;
message surfaces the missing `(from, to)` pair AND the offending `transportRef`
that introduced the cross-tenant scope (so the workspace author knows which
Transport to revise or which CRA to negotiate).

Runtime enforcement is at the messaging transport (09) via the ReBAC tuple
`workspace:<p>#messageable_from@workspace:<this>` (04a iter-5). The admission check
is a fast-fail UX layer; bypass is impossible at the transport.

## 4. Stream teardown on WorkflowRun completion

When the Argo Workflow reaches a terminal phase (Succeeded / Failed / Error), the
controller deletes the JetStream stream after `ttlStrategy.secondsAfterCompletion`
(default 7 d) to allow post-mortem replay. On Workflow deletion (owner-ref GC), stream
deletion is immediate.

Failure: NATS unavailable at teardown → `NATSStreamDeleteFailed` event; controller
retries with exponential backoff; stream retained until next reconcile succeeds.

## Iteration log

### Iteration 2 — 2026-04-21 — full scoring table

| # | Category | Wt | Ratio | Score | Notes |
|---|---|---:|---:|---:|---|
| 1 | Scope clarity | 10 | 1.0 | 10 | Same-namespace model, concurrencyPolicy semantics, interactive mutual exclusion — all bounded with explicit enforcement points. |
| 2 | Architecture fit | 10 | 1.0 | 10 | Dropped ephemeral namespaces; operator RBAC narrowed; Capsule tenant-tree consistent; NP from 12 applies naturally. |
| 3 | Security posture | 15 | 1.0 | 15 | Narrower operator RBAC (no namespace create/delete); owner-ref Secret GC; NP in Workspace namespace covers all Argo pods; VAP immutability on interactive; no wildcard policy. |
| 4 | Automatability | 10 | 0.5 | 5 | concurrencyPolicy admission path named; VAP for interactive named; Replace drain-window stated. Make targets pre-gate. |
| 5 | Verifiability | 15 | 0.5 | 7.5 | 9 failure modes (2 new); Replace race and interactive reject testable. Envtest names pre-gate P8. |
| 6 | Failure-mode awareness | 10 | 1.0 | 10 | Replace drain timeout, Forbid race, interactive reject added; all 9 modes have detection + mitigation. |
| 7 | Context efficiency | 10 | 1.0 | 10 | Trigger projections split to 03b; iter-2 table moved to 03c keeps 03 ≤ 200 lines. |
| 8 | Docs quality | 5 | 1.0 | 5 | SPDX; frontmatter updated; depends list includes 12; 02 iter-2 flagged; 22 iter-2 flagged; rollback updated. |
| 9 | Observability | 5 | 1.0 | 5 | 4 OTEL spans (+ replace_drain); 12 event reasons; 5 metrics. |
| 10 | Operational readiness | 10 | 1.0 | 10 | TTL strategy explicit; Argo chart pin flagged for 14b; RBAC reduction documented; NP coverage via workspace namespace clear. |
| | **Total** | 100 | | **95** | |

### Iteration 3 — 2026-04-21

| # | Category | Wt | Ratio | Score | Notes |
|---|---|---:|---:|---:|---|
| 1 | Scope clarity | 10 | 1.0 | 10 | Four messaging duties bounded: provision, inject, admit, teardown. Cross-tenant peers derived from transportRefs (no new WorkflowRun field required). |
| 2 | Architecture fit | 10 | 1.0 | 10 | Consumes 04a iter-5 ReBAC relations correctly; VAP-first admission per 04.12; SSA with fieldOwner; owner-ref GC. |
| 3 | Security posture | 15 | 1.0 | 15 | No wildcard NATS policy; JWT audience scoped to run-uid; ReBAC tuples declared; CTA admission fast-fail + transport enforcement layered; break-glass rollback risk noted. |
| 4 | Automatability | 10 | 0.5 | 5 | Admission and stream provisioning paths named; RBAC markers stated. Make targets and envtest names remain pre-gate (unchanged). |
| 5 | Verifiability | 15 | 0.5 | 7.5 | +4 failure modes testable; CTA admit path testable in envtest against fake CRA. Full envtest spec pre-gate P8. |
| 6 | Failure-mode awareness | 10 | 1.0 | 10 | All 4 new modes have detection + mitigation + retry contract; stream retained on NATS unavailability is explicit. |
| 7 | Context efficiency | 10 | 1.0 | 10 | Companion 03c holds messaging detail + iter logs; 03 stays ≤ 200 lines; single-responsibility split maintained. |
| 8 | Docs quality | 5 | 1.0 | 5 | SPDX + frontmatter on 03c; 03 depends updated; README.md index updated; rollback entry on 03c. |
| 9 | Observability | 5 | 1.0 | 5 | +2 OTEL spans; +4 events; +3 metrics declared with label cardinality noted. |
| 10 | Operational readiness | 10 | 1.0 | 10 | Stream maxAge bound to WorkflowRun timeout; 7 d post-mortem replay window; owner-ref GC; retry-with-backoff on NATS unavailability. |
| | **Total** | 100 | | **96** | |

Verdict: **SHIP** (96 ≥ 90). Status: `current`.

Top gaps: (1) Cat 4/5: make targets + envtest names pre-gate — unchanged, acceptable pre-design-gate. (2) Admission webhook must fan-out across `transportRef` resolution — CEL VAP alone insufficient (cross-resource lookup); webhook scope justified per rule 04.12. (3) 04b iter-3 + 09 iter-3 landed alongside this iter-3; `MissingWorkflowAudience` and `CrossTenantAgreementMissing` paths now wired end-to-end.

Cross-deps settled (iter-3): 04a iter-5 LANDED; 04b iter-3 LANDED; 09 iter-3 LANDED; NATS stream config fully specified; CTA admission logic documented (transportRef-derived); stream teardown semantics explicit. **Q2(b) decision recorded:** cross-tenant peers derived implicitly from `transportRef`s with `scope: cross-tenant` — no new WorkflowRun spec field.
Cross-deps flagged: D29/25 (CTA CRD spec — design 25 still at draft).
