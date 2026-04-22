<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: spec
category: contract
depends:
  - ../designs/04b-projected-sa-identity.md
  - ../designs/04b-ii-oidc-trust.md
  - ../designs/04a-openfga-authz-model.md
related_skills: [crd-authoring, controller-authoring]
status: draft
last_verified: 2026-04-21
regression_lock: false
tests:
  unit: []
  envtest: []
  kuttl: []
metrics: []
events: []
---

# authz.operator.keese.ai v1alpha1 — spec

> **Status: draft.** This spec is a NEW addition tracking the kind added by
> D28 (OIDCProvider). It is intentionally a stub pending architect dispatch
> — owning designs are all `current`, so the design-gate predicate is
> satisfied for promotion.

## Owning design(s)

All `status: current`:

- [`designs/04b-projected-sa-identity.md`](../designs/04b-projected-sa-identity.md) iter-3 — D28 OIDCProvider integration; `audienceTemplates` (egress, workflowRun, supervisor)
- [`designs/04b-ii-oidc-trust.md`](../designs/04b-ii-oidc-trust.md) — per-cloud trust anchoring; egress is the federated audience
- [`designs/04a-openfga-authz-model.md`](../designs/04a-openfga-authz-model.md) — subject derivation from JWT claims feeds OpenFGA Check

## Kinds covered

Group: `authz.operator.keese.ai/v1alpha1`. Cluster-scoped.

- **OIDCProvider** (D28) — per-issuer JWT-to-OpenFGA-subject transformation config. Spec: `issuer`, `audiences[]`, `subjectTemplate` (Go template; restricted Sprig allow-list), `audienceTemplates[]` (named: `egress`, `workflowRun`, `supervisor`), `jwksUri`, `normalization`. Operator bootstraps default CRs for kubernetes-default + google + github-actions + azure-entra + okta + keycloak + gitlab.

## Acceptance test categories (to fill in)

- Schema: subjectTemplate parse error → `OIDCProvider.status.phase=Degraded` + `TemplateInvalid` event.
- Sprig allow-list enforcement (only trimPrefix/trimSuffix/lower/upper/split/replace).
- audienceTemplates eval: missing required variable → `AudienceTemplateEvalError` event; metric incremented.
- Tenant.spec.oidc.allowedProviders[] gating (cross-cut to tenancy spec).
- JWKS unreachable → 401 from gateway; `OIDCProviderDegraded` after cache TTL.
- Three-token projection mount paths (`/var/run/keese/tokens/{egress,workflowRun,supervisor}`).
- Audit log shape excludes tokens + decoded claims (rule 05.10).

TODO(architect-dispatch): Author iter-1/iter-2/iter-3 to score ≥ 90 honestly.
