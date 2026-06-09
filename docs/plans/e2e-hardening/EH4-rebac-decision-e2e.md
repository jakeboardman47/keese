<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: plan
category: phase
depends:
  - README.md
  - ../../specs/egress-authz-protocol.md
  - ../../../internal/authz/extauth/check.go
  - ../../../tests/e2e/lib/check-prereqs.sh
related_skills: [plan-management]
status: planned
last_verified: 2026-06-09
phase: EH4
model_tier: sonnet
depends_on: []
agent: test-engineer
outputs:
  - tests/e2e/rebac-decision
  - tests/e2e/lib
---

# EH4 — Live ReBAC allow/deny e2e (keystone)

**Goal.** Prove the running `ext_authz` makes a **CRD-driven** allow-vs-deny
decision through the live cluster. Today ReBAC is only model-unit-tested
(`tests/openfga/oidc-provider.yaml`) and used as a fail-closed prereq; no suite
asserts a real decision. This is the keystone authz suite that EH5/EH6 build on.

## Deliverables

A kuttl suite `tests/e2e/rebac-decision/` that:

1. **Prereq-gates** on a seeded OpenFGA store + gateway via
   `tests/e2e/lib/check-prereqs.sh` (extend the lib if needed; keep fail-closed).
2. **Allow path:** create the CRDs whose reconcilers write the authorizing tuples
   (e.g. a `Workspace` + the binding that grants a tool/upstream), then fire a
   real request from a workspace pod (projected SA token, mounted CA) through the
   Envoy AI Gateway and assert **HTTP 200** (ext_authz allowed).
3. **Deny path:** identical request from a workspace whose tuple is absent /
   revoked → assert **HTTP 403** from ext_authz (fail-closed), and assert the
   ext_authz audit log captured `(tuple, SA, host, decision)` with **no token
   body** (rule 05.10).
4. **Revocation:** delete the authorizing CR/tuple, re-fire, assert the decision
   flips allow→deny within the cache TTL.

Reuse the request-firing pattern from `tests/e2e/aigw-defense/` and
`scripts/dev/test-aigw-defense.sh`.

## Acceptance

- Suite green under `make test-e2e` against a bootstrapped cluster with a seeded
  store; skips cleanly (prereq gate) when the store/key is a placeholder.
- Asserts at least one real allow (200) and one real deny (403) decision, plus
  the revocation flip.
- No tokens/bodies in any asserted log line.

## Notes for the agent

- Behavior is implemented in `internal/authz/extauth/{check,resolver,subject}.go`
  + `internal/rebac/` — test what ships. If a decision path is a stub, mark the
  step `kuttl`-skipped + add `revisit_when_rebac_decision_live` and set
  `status: shipped-with-stubs`.
- Stay inside `tests/e2e/rebac-decision/` + additive helpers in `tests/e2e/lib/`.
