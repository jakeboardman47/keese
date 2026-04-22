<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: plan
category: audit
depends:
  - README.md
  - rubric.md
  - ../designs/README.md
  - ../specs/README.md
related_skills: [plan-management]
status: current
last_verified: 2026-04-22
---

# Gate-Open Audit — 2026-04-22

Pre-flight review before flipping `gate_status: closed → open` in
[../plans/README.md](README.md). All 62 designs and 27 specs are
`status: current` (62/62 design slots = 61 current + 1 superseded).
This doc surfaces what an architect-signer should look at before
signing the gate-open commit.

## Score honesty audit

Across this multi-month authoring effort, **score inflation** has been
the dominant audit finding. The pattern: an architect agent claims
100/100 by treating "test SPECS named in the design doc" as full Cat 5
(Verifiability) credit, when the established discipline is that pre-gate,
Cat 5 docks 0.5 (−7.5) until test FILES are committed. Same pattern on
Cat 4 (Automatability) with named-but-uncommitted scripts.

### Designs — claim vs. honest rescore

Recovered or honestly-scored from prior cascades; flagged here for
the gate-open review. None of these are below the rubric SHIP threshold
(≥85), but several are below the conventional ≥90 target.

| Doc | Claim | Honest | Audit reason |
|---|---:|---:|---|
| [12-network-isolation](../designs/12-network-isolation.md) | 100 | ~95 | `check-np-labels.sh` named in CI/Tilt; not committed |
| [14a-olm-channels-upgrades](../designs/14a-olm-channels-upgrades.md) | 100 | ~92.5 | kuttl tests SPEC'd; no test files committed |
| [14b-olm-dependencies](../designs/14b-olm-dependencies.md) | 100 | ~95 | `keese_dep_health_gauge` defined; no startup-probe code |
| [16-recipe-distribution](../designs/16-recipe-distribution.md) | 100 | ~92.5 | 4 envtest cases NAMED; no files committed |
| [19-ide-and-debugging](../designs/19-ide-and-debugging.md) | 100 | ~92.5 | Smoke scripts referenced; not committed |
| [21-opentofu-cloud-deployment](../designs/21-opentofu-cloud-deployment.md) | 100 | ~95 | 3 smoke assertions named; no test file |

### Specs — claim vs. honest rescore

| Spec | Claim | Honest | Audit reason |
|---|---:|---:|---|
| [runtime](../specs/runtime.operator.keese.ai-v1alpha1.md) | 100 | ~95 | Test names locked; bodies pre-gate |
| [recipe](../specs/recipe.operator.keese.ai-v1alpha1.md) | 100 | ~92.5 | 8 envtest cases NAMED; bodies pre-gate |

### Lowest honest scorers (still ≥ 90)

| Doc | Honest | Reason |
|---|---:|---|
| [13-cli-tunnel-wireguard](../designs/13-cli-tunnel-wireguard.md) | 92.5 | Cat 4/5 explicit pre-gate dock; cleanest honest baseline |
| [guardrail spec](../specs/guardrail.operator.keese.ai-v1alpha1.md) | 92.5 | Cat 4 −5 + Cat 5 −7.5 explicit |
| [egress-authz-protocol](../specs/egress-authz-protocol.md) | 92.5 | Cat 5 −7.5 explicit |
| [authz spec](../specs/authz.operator.keese.ai-v1alpha1.md) | 92.5 | Cat 4/5 acknowledged |
| [tenancy spec](../specs/tenancy.operator.keese.ai-v1alpha1.md) | 92.5 | iter-4 recovery from honest 87.5 |

## Outstanding controller-phase backlog (not blocking gate-open)

These are fixable post-gate; flagged here so the controller-author
agent has a punch list:

1. **`scripts/check-openfga-model.sh`** + **`scripts/check-openfga-assertions.sh`** (04a iter-4 residual; 04a-iii).
2. **`status.observedModelID`** on controller / ext_authz pods — required for MODEL_MIGRATION readiness gate (04a iter-4).
3. **`test/e2e/model_migration_drain_test.go`** — 04a-ii test plan named, body deferred.
4. **`scripts/check-memory-provisioning.sh`** — 15 memory; only Cat 4 holdout.
5. **`config/overlays/base/vap/workspace-*.yaml`** + **workspacesession-create.yaml** — workspace spec Cat 4 holdout.
6. **`scripts/check-np-labels.sh`** — 12 network-isolation Cat 4 holdout.
7. **kuttl Calico CI matrix** — 12 network-isolation Cat 5 holdout.
8. **OLM upgrade kuttl suite** — 14a-ii steps 1-8 spec'd, bodies deferred.
9. **Smoke scripts for IDE / OpenTofu / recipe / etc.** — many designs name them.

## Cross-cuts now closed

- D29 CrossTenantAgreement: design 25 + spec tenancy + 04a iter-5/iter-6 + model.fga + tests fixture all coherent.
- D28 OIDCProvider: design 04b iter-3 + authz spec + 04a iter-6 + model.fga (NEW relation `tenant.uses_oidc_provider`) + tests/openfga/oidc-provider.yaml all coherent.
- D27 WorkspaceSession: design 08b-ii + workspace spec ii-session companion all coherent.
- D26 Tenant: design 24/24b + tenancy spec ii-tenant all coherent.
- D25 GUPP / D24 durable identity: design 23 + agent-runtime-spi spec all coherent.

## Recommendation

The gate-open predicate is satisfied per the rubric (all docs ≥ 85 SHIP
honestly; 6 inflated 100s and 2 inflated spec 100s rescore to 92.5–95
honestly, all ≥ 90). The controller-phase backlog above is the proper
place for the Cat 4/5 work; not a blocker for opening the gate.

**Architect signer should review:** the 8 inflated 100s (6 design + 2
spec) for any architectural concerns the inflation might be masking.
Spot-checks during this audit found no such concerns — the inflations
were uniformly about test-fixture-commit timing, not design correctness.

## Refs

- [README.md](README.md) — gate status pointer (currently `closed`)
- [rubric.md](rubric.md) — scoring framework
- [scaffolding-summary.md](scaffolding-summary.md) — initial scaffolding state
- [scaffolding-plan.md](scaffolding-plan.md) — D1–D29 decision table
- [../designs/README.md](../designs/README.md) — design index (62, all current/superseded)
- [../specs/README.md](../specs/README.md) — spec index (27, all current)
- [../../MEMORY.md](../../MEMORY.md) — running cascade decision log
