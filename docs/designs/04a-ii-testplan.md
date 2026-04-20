<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: design
category: authz
depends:
  - 04a-openfga-authz-model.md
  - 04c-token-revocation.md
related_skills: [test-engineer]
status: current
last_verified: 2026-04-20
rollback: |
  Test plan documents are additive. Remove a row if the feature is removed.
  No production impact.
---

# 04a-ii — OpenFGA Auth Model: Test Plan and CI Automation

Companion to `04a-openfga-authz-model.md`. Contains the named test backlog
and CI automation matrix detail that belong to the test-engineer's delivery
scope but are architecture-relevant enough to index.

## CI automation matrix

| Automation | Script | Runs in | Fail condition |
|---|---|---|---|
| Model DSL syntax | `scripts/check-openfga-model.sh` (wraps `fga model validate`) | pre-commit + `lint.yaml` | exits 2 if `model.fga` fails DSL validation |
| Tuple assertions | `scripts/check-openfga-assertions.sh` (wraps `fga model test`) | pre-commit + `test.yaml` | exits 2 if any YAML assertion returns unexpected result |
| ReBAC marker presence | `scripts/check-rebac-markers.sh` (existing P3 hook) | pre-commit | exits non-zero if `+keese:rebac-tuple` absent on authz-affecting CRD field |
| MODEL_MIGRATION e2e | `make test-model-migration` drives `test/e2e/model_migration_drain_test.go` | `test.yaml` e2e matrix | test failure if drain does not reach in-flight=0 within timeout or abort does not clear flag |

**Pre-commit anchors.** `scripts/check-openfga-model.sh` and
`scripts/check-openfga-assertions.sh` are not yet implemented (test-engineer backlog,
pre-gate). When implemented, they register under the `local` hooks section of
`.pre-commit-config.yaml` alongside the existing `rebac-markers` and
`signal-handling` hooks.

**MODEL_MIGRATION controller.** Entry point to be implemented post-gate:
`internal/controller/rebac/modelmigration_controller.go`. The e2e test file
`test/e2e/model_migration_drain_test.go` defines the acceptance criteria below.

## Named test assertions

| Test name | Location | Assertion |
|---|---|---|
| `TestTupleShape_TenantAdmin` | `tests/openfga/tenant_admin.yaml` | `Check(tenant:T#admin@user:U)` returns allowed for direct tuple; denied for non-admin user |
| `TestComputedRelation_TenantMember` | `tests/openfga/tenant_member.yaml` | `Check(workspace:W#tenant_member@SA)` walks owner→member; allowed when SA is tenant member; denied for SA in a different tenant |
| `TestComputedRelation_CanCall` | `tests/openfga/can_call.yaml` | `Check(tool:X#can_call@SA)` walks 4–5 hops; allowed when tool is `allowed_in` workspace AND SA is tenant member; denied when either condition false |
| `TestCanRevoke_ServiceAccount` | `tests/openfga/can_revoke_sa.yaml` | `Check(workspace:W#can_revoke@service_account:keese-supervisor)` allowed after operator install Job writes the tuple |
| `TestCanRevoke_Witness` | `tests/openfga/can_revoke_witness.yaml` | `Check(workspace:W#can_revoke@witness:WIT)` allowed only after supervision controller writes the tuple; denied for unassigned witnesses |
| `TestCanRevoke_UserDenied` | `tests/openfga/can_revoke_user_denied.yaml` | `Check(workspace:W#can_revoke@user:U)` always denied (no tuple shape accepts `user:*`) |
| `TestForceRevoke_AdmissionAllow` | `test/envtest/admission/forcerevoke_allow_test.go` | PATCH `Workspace.spec.forceRevoke` as `service_account:keese-supervisor` → admission allows; Event `ForceRevokeAttempt` with `decision=allowed` |
| `TestForceRevoke_AdmissionDeny` | `test/envtest/admission/forcerevoke_deny_test.go` | PATCH same as `user:bob` → admission denies with reason `ForbiddenToRevoke`; Event `ForceRevokeAttempt` with `decision=denied` |
| `TestModelMigration_Drain` | `test/e2e/model_migration_drain_test.go` | Seed a WorkflowRun; enter MODEL_MIGRATION; new WorkflowRun creation blocked with `ModelMigrationInProgress`; original run completes on old model ID; readiness gate blocks exit until all pods report `observedModelID=new` |
| `TestModelMigration_DrainTimeoutAbort` | `test/e2e/model_migration_timeout_test.go` | Seed long-running WorkflowRun; enter MODEL_MIGRATION with 1-min timeout; assert `DrainTimeout` event; assert migration aborts cleanly; ConfigMap NOT updated; flag cleared |
| `TestModelMigration_PartialRolloutBlocked` | `test/e2e/model_migration_partial_test.go` | Block one ext_authz pod from reporting; assert operator does NOT exit MODEL_MIGRATION until timeout; event `ReadinessGateStuck` emitted |
| `TestAuditEvent_NoTokenBytes` | `test/envtest/audit/no_token_test.go` | Run a Check; assert the emitted `keese-openfga-audit-*` ES document and Loki log line contain NO token bytes and NO request bodies (rule 05.10) |
| `TestFailClosed_OpenFGADown` | `test/e2e/extauthz_openfga_down_test.go` | Kill OpenFGA pod; assert ext_authz returns 503; assert Envoy denies; assert `AuthzCheckFailed` event rate exceeds 1% threshold and alert fires |

## Refs

- [04a-openfga-authz-model.md](04a-openfga-authz-model.md)
- [04c-token-revocation.md](04c-token-revocation.md)
- [../plans/runbook-model-migration.md](../plans/runbook-model-migration.md)
- [../../.pre-commit-config.yaml](../../.pre-commit-config.yaml)
- [../../.claude/rules/05-security-zero-trust.md](../../.claude/rules/05-security-zero-trust.md)
