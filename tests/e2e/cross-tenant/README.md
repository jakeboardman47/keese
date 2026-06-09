<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: reference
category: testing
status: current
last_verified: 2026-06-09
---

# tests/e2e/cross-tenant/ — CrossTenantAgreement + OIDCProvider e2e (EH6)

Positive complement to the `multi-tenant/` suite, which asserts the
fail-closed NetworkPolicy deny but **explicitly deferred** the
CrossTenantAgreement (CTA) happy-path (needs live OpenFGA). EH6 covers the
two shipped reconcilers
`internal/controller/authz/{crosstenanagreement,oidcprovider}_controller.go`.

## What it asserts

| Step | File | Assertion |
|---|---|---|
| 00 | `00-setup.yaml` / `00-assert.yaml` | Two tenants (`alpha2`,`beta2`) + an `OIDCProvider` CR; provider reconciles to `Active`/`Ready` (live CR complement to `tests/openfga/oidc-provider.yaml`). Prereq-gated (EH4's `check-prereqs.sh` + `check-extauth.sh`). |
| 01 | `01-apply.yaml` / `01-assert.yaml` | Apply CTA `beta2`→`alpha2` (with a computed `expiresAt`); drive **both** tenants' signed approvals; controller reaches `Approved`/`Ready` after writing the trust tuple. |
| 02 | `02-tuple-and-decision.yaml` | Live OpenFGA read confirms `tenant:alpha2#allows_messaging@tenant:beta2` is **present** (proves the real writer ran, not the Noop fallback). Cross-tenant request-path allow/deny probe — **stubbed** (see below). |
| 03 | `03-revoke.yaml` | Revocation via expiry: controller drives `Approved` → `Expired` (`Ready=False`), the shipped tuple-removal path (`transitionToExpired` → `Rebac.Delete`). |
| 04 | `04-assert.yaml` | Live read confirms the trust tuple is now **absent** — the deny-flip at the tuple layer. |
| 05 | `05-delete.yaml` / `05-assert.yaml` | Delete the CTA; the NATS finalizer runs to completion and the object is fully removed (not wedged in `Terminating`). |

### Why expiry, not delete, for the tuple-removal flip

The shipped `crosstenanagreement_controller.go` removes the
`allows_messaging` tuple on the **expiry** path (`transitionToExpired`),
not on plain deletion — `cleanup()` on delete only removes the NATS stream
+ finalizer. So step 03 drives a real revocation by letting `expiresAt`
lapse (`apply-cta.sh` computes `now + EXPIRY_WINDOW_S`, default 150s).
Step 05 then covers the delete/finalizer path separately.

## Shipped-with-stubs

The cross-tenant **request-path** allow/deny through ext_authz (a `beta`
subject reaching the agreed `alpha` resource) is **not yet live**: the
gateway resolver (`internal/authz/extauth/resolver.go`) resolves only the
per-tool `can_call` relation (the EH4 path) and does not yet consult the
cross-tenant `messageable_from`/`allows_messaging` tuples. So
`check-cross-tenant-decision.sh` **skips** with a printed revisit trigger
rather than asserting nothing real. The full CR-reconcile + tuple +
finalizer layer (the controllers under test) **is** covered live.

Revisit triggers:

- `revisit_when_cross_tenant_live` — once `extauth/resolver.go` resolves
  `messageable_from`, re-run with `CROSS_TENANT_DECISION_LIVE=1`;
  `check-cross-tenant-decision.sh` then reuses EH4's request helper
  (`rebac-decision/test-rebac-decision.sh`, **sourced**, not copied) to
  fire the agreed-allow / non-agreed-deny cases.
- `revisit_when_oidc_discovery_live` — the OIDCProvider `JWKSReachable`
  condition (live OIDC discovery to `accounts.google.com`) is **not**
  asserted; only template-driven `Ready` is. Add a `JWKSReachable=True`
  assert once outbound discovery is available in the bootstrap.

## Reused helpers (sourced, not copied)

- `../lib/check-prereqs.sh`, `../lib/check-extauth.sh` (EH4) — prereq gates.
- `../rebac-decision/test-rebac-decision.sh` (EH4) — request/audit/ext_authz
  firing, sourced by `check-cross-tenant-decision.sh` when live.

Additive helpers (this suite):

- `../lib/check-cta-tuple.sh` — live OpenFGA tuple present/absent read.
- `../lib/check-cross-tenant-decision.sh` — request-path probe (stubbed).
- `apply-cta.sh`, `approve-cta.sh` — CTA apply (computed `expiresAt`) +
  per-tenant signed approval driver.

## Prerequisites

- kind `kind-keese` cluster: `make kind-up && make bootstrap-infra`
- Seeded OpenFGA + OpenBao (`dev/bootstrap/openfga/seed.yaml`)
- Operator deployed; `kuttl` on PATH
- `ghcr.io/openfga/cli` pullable (the one-shot tuple-read pod)

## Run

```sh
kubectl kuttl test tests/e2e/cross-tenant --config tests/e2e/kuttl-config.yaml
```

Discoverable by the existing `testDirs: tests/e2e/` glob in
`tests/e2e/kuttl-config.yaml` — no config edit needed.

## Flake risk

Low. Async assertions poll (`kubectl wait` / re-read the live store); no
`sleep`-as-assertion (rule 06). The expiry window (150s) is sized so both
approvals + tuple-present settle before expiry fires; the 03 timeout
(210s) covers the ≤1-minute expiry-requeue cadence.
