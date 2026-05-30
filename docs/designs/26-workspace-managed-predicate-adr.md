<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: design
category: adr
depends:
  - 02-workspace-model.md
  - 20a-api-group-layout.md
related_skills: [doc-authoring, controller-authoring]
status: current
last_verified: 2026-05-06
rollback: |
  Re-introduce the predicate by adding
  `predicate.NewPredicateFuncs(hasManagedLabel)` to every
  reconciler's `SetupWithManager` and re-stamping
  `keese.ai/managed: "true"` onto every sample + the demo
  manifest. v1alpha1 is pre-GA so no CRD migration is required.
---

# 26 — Workspace controller predicate (keese.ai/managed) ADR

**Decision:** **Drop** the `keese.ai/managed: "true"` label predicate
permanently. Reconcile every Workspace (and every other keese kind)
in the operator's watched API groups. Closes TD-P1-06.

## Context

D1-T2 (2026-04-25) temporarily removed the predicate so demo
samples would reconcile without label-stamping. The temporary
removal opened a question: do we re-add the predicate (so admins
can park experiments outside the controller) or commit to
no-predicate semantics?

## Forces considered

- **CRD ownership.** `keese.ai`, `authz.keese.ai`, and
  `policy.keese.ai` are domain-specific groups owned by the keese
  operator. Unlike, say, `apps/v1.Deployment` (which is owned by
  the kubelet/scheduler and decorated by *many* operators), there
  is no second consumer of these CRDs. The default expectation is
  "if it's there, the operator manages it."
- **Sample ergonomics.** Every sample under `config/samples/`,
  every CR in `dev/demo/`, and every test fixture would need the
  label. Forgetting the label silently bypasses reconcile — a
  sharp footgun for new contributors and operators copying sample
  YAML.
- **Multi-tenant blast radius.** A `keese.ai/managed=false` escape
  hatch sounds nice for "park a CR without reconciling." But a
  parked Workspace with no controller still gets admitted (its
  PVC, ServiceAccount, NetworkPolicies, etc. are absent), and the
  inconsistency between API-server state and runtime state is
  worse than reconciling.
- **Break-glass already exists.** Rule 05.13 defines the
  `keese.ai/break-glass=true` label as the audited escape hatch
  for unsafe annotations. That mechanism — explicit, audited, with
  events — is the right shape for "I need to do something the
  controller wouldn't normally let me do." A second predicate-
  driven opt-out muddies the model.
- **Capsule namespace tenancy.** Tenants are scoped via Capsule
  `additionalRoleBindings` (D-01.2). RBAC, not labels, is the
  correct tenant-scoping primitive. Adding a label-predicate on
  top would be a second filter doing what RBAC already does.

## Decision

**Drop the predicate.** Every keese CR in every keese API group is
reconciled unconditionally. The existing
`predicate.GenerationChangedPredicate` (and the new
`pokeAnnotationPredicate` from TD-P1-11) stay; they filter
*which events* trigger reconcile, not *which CRs* are managed.

The samples + demo retain whatever labels they currently have for
discoverability (e.g., `app.kubernetes.io/name`), but no controller
predicate inspects them.

## Consequences

- Zero footgun: a fresh `kubectl apply -f sample.yaml` reconciles
  immediately.
- The operator must be installed at most once per cluster (or per
  Capsule tenant) — no "shadow controller" scenario where two
  keese installs both want to reconcile the same CR. This is
  enforced by leader election + the OLM channel model, both
  already in place.
- Future "park this CR" use cases use suspended states inside the
  CR spec (e.g., `Workspace.spec.suspended: true`) rather than a
  predicate. Tracked separately if/when needed.

## Code state at decision time

`internal/controller/keese/workspace_controller.go:328` carries the
inline comment pointing here. The comment is updated to
"permanently dropped per D26 ADR" — no flag, no future re-evaluation.

Other reconcilers (WorkspaceSession, Workflow, Runtime, Memory,
Recipe, Transport, Tenant, GuardrailBinding, OIDCProvider, CTA,
TokenBudget) have never had the predicate; this ADR documents the
choice to keep them predicate-free as well.

## Iteration log

| Iter | Date | Focus | Score |
|---|---|---|---:|
| 1 | 2026-05-06 | Correctness & security | 95 |
| 2 | 2026-05-06 | Performance & quality | 95 |
| 3 | 2026-05-06 | Operational readiness | 95 |

## Refs

- [docs/designs/02-workspace-model.md](02-workspace-model.md)
- [docs/designs/01-tenancy-capsule.md](01-tenancy-capsule.md) — Capsule scoping
- [.claude/rules/05-security-zero-trust.md](../../.claude/rules/05-security-zero-trust.md) §13 — break-glass
- [docs/plans/demo/tech-debt.md](../plans/demo/tech-debt.md) — TD-P1-06
