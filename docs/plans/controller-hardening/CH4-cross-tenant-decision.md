<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: plan
category: phase
depends:
  - README.md
  - ../../specs/egress-authz-protocol.md
  - ../../designs/04a-openfga-authz-model.md
  - ../../../internal/authz/extauth/check.go
  - ../../../internal/authz/extauth/resolver.go
  - ../../../dev/bootstrap/openfga/model.fga
related_skills: [plan-management]
status: planned
last_verified: 2026-06-10
phase: CH4
model_tier: opus
depends_on: []
agent: rebac-modeler
outputs:
  - internal/authz/extauth
  - internal/rebac
---

# CH4 — Cross-tenant messageable_from resolution

**Goal.** Cross-tenant A2A messaging is **not authorized** at the gateway. EH6
found `internal/authz/extauth/resolver.go` / `check.go` only resolve `can_call`
for `tool:` objects (`check.go:84`); a cross-tenant message request is never
checked against the cross-tenant trust tuple, so the decision can't go live.

**Important — no model migration needed.** `dev/bootstrap/openfga/model.fga`
**already** defines `tenant.allows_messaging: [tenant]` (line 61) and
`workspace.messageable_from: [workspace]` (line 93), and the CrossTenantAgreement
controller (`internal/controller/authz/crosstenanagreement_rebac.go`) **already
writes** both tuples on both-side approval / removes them on expiry (verified by
EH6). The gap is purely the **ext_authz decision path** not consulting them.

## Deliverables

1. **Identify the cross-tenant-message request** at ext_authz — read
   `docs/specs/egress-authz-protocol.md` + `resolver.go` to determine how an A2A /
   cross-tenant message request is distinguished from a tool call (path / NATS
   subject / header). Document the discriminator.
2. **Resolve the decision** — for such a request, `fga.Check` the appropriate
   relation (`workspace:<W_to>#messageable_from@workspace:<W_from>`, and/or the
   `tenant:<T_to>#allows_messaging@tenant:<T_from>` directional grant — pick the one
   the tuples are actually written at; confirm against `crosstenanagreement_rebac.go`
   + the model comments). **Fail-closed**: allow only on an explicit grant; deny
   (403) otherwise, like the `can_call` path.
3. **Audit** the decision per rule 05.10 — `(tuple, SA, from, to, decision)`, no
   tokens/bodies.

## Acceptance

- Unit/envtest: an agreed cross-tenant pair (tuple present) → allow; a non-agreed
  or revoked pair → deny; direction matters (a→b granted does not imply b→a).
- The EH6 `cross-tenant/` suite's `CROSS_TENANT_DECISION_LIVE=1` path
  (`revisit_when_cross_tenant_live`) passes end-to-end.
- `CGO_ENABLED=0 go test -race ./internal/authz/... ./internal/rebac/...` green;
  `make lint` clean.

## Notes for the agent (rebac-modeler)

- **Tuple shapes are load-bearing** (rule 05.9, opus-tier review). Match the EXACT
  relation/direction the CTA controller writes — do not invent a new relation or
  change the model unless you prove the existing relations can't express the
  decision (if you must touch `model.fga`, justify it + note migration impact).
- Stay inside `internal/authz/extauth/` + `internal/rebac/`. **Never run bare
  `git stash`/`pop`/`reset`/`checkout <branch>`** (hits the shared checkout).
  macOS gotcha: `CGO_ENABLED=0`. This unblocks EH6's cross-tenant stub.
