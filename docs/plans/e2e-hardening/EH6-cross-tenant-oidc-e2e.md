<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: plan
category: phase
depends:
  - README.md
  - EH4-rebac-decision-e2e.md
  - ../../specs/keese.ai-v1alpha1-tenancy-ii-cra.md
  - ../../../internal/controller/authz/crosstenanagreement_controller.go
  - ../../../internal/controller/authz/oidcprovider_controller.go
related_skills: [plan-management]
status: shipped-with-stubs
last_verified: 2026-06-09
revisit_when_cross_tenant_live: true
revisit_when_oidc_discovery_live: true
phase: EH6
model_tier: sonnet
depends_on: [EH4]
agent: test-engineer
outputs:
  - tests/e2e/cross-tenant
  - tests/e2e/lib
---

# EH6 — CrossTenantAgreement + OIDCProvider e2e

**Goal.** The `multi-tenant` suite explicitly **deferred** the CrossTenantAgreement
happy-path (needs live OpenFGA), and OIDCProvider has only an FGA-model unit test
(`tests/openfga/oidc-provider.yaml`) — no live CR reconcile. Cover both shipped
reconcilers.

## Deliverables

A kuttl suite `tests/e2e/cross-tenant/`:

1. **CrossTenantAgreement happy-path:** two tenants (`alpha`, `beta`) + a
   `CrossTenantAgreement` granting `beta` scoped access into `alpha`. Assert the
   controller writes the cross-tenant trust tuple + reaches Ready, then assert a
   `beta` subject **can** reach the agreed `alpha` resource through ext_authz
   (allow), while a non-agreed action is **denied** (fail-closed). This is the
   positive complement to `multi-tenant/`'s NetworkPolicy deny.
2. **CrossTenantAgreement revocation:** delete the CTA; assert the trust tuple is
   removed (finalizer) and the previously-allowed cross-tenant action flips to
   denied.
3. **OIDCProvider reconcile:** apply an `OIDCProvider` CR; assert it reaches Ready
   and the provider is registered (the live-CR complement to the model test).

Prereq-gate via `tests/e2e/lib/check-prereqs.sh` (+ EH4's `check-extauth.sh`).

## Acceptance

- Suite green under `make test-e2e` on a bootstrapped cluster with a seeded store;
  skips cleanly on placeholder prereqs.
- Asserts a CTA allow + a revoke-deny flip + an OIDCProvider Ready.

## Notes for the agent

- Test SHIPPED behavior in `crosstenanagreement_controller.go` +
  `oidcprovider_controller.go` + `internal/rebac/`. If the cross-tenant decision
  path or OIDC discovery isn't live in the bootstrap, mark that step skipped, add
  `revisit_when_cross_tenant_live` / `revisit_when_oidc_discovery_live`, set
  `status: shipped-with-stubs`, and fully cover the CR-reconcile + tuple layer.
- Stay inside `tests/e2e/cross-tenant/` + additive `tests/e2e/lib/` helpers;
  reuse EH4's request/audit helpers (source, don't copy).
