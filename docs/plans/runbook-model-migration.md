<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: plan
category: runbook
depends:
  - docs/designs/04a-openfga-authz-model.md
  - docs/designs/04c-token-revocation.md
related_skills: []
status: current
last_verified: 2026-04-20
---

# Runbook — MODEL_MIGRATION (OpenFGA Model Rollout)

Operator runbook for safely swapping an OpenFGA authorization model ID in a
running keese cluster. Follow the 6-step drain-and-rollout defined in 04a.
Author this before any controller code is written (hard requirement 04a iter-4).

## Pre-checks (before entering migration)

```bash
# 1. Record current model ID
kubectl get cm keese-rebac-config -n keese-system \
  -o jsonpath='{.data.OPENFGA_AUTHORIZATION_MODEL_ID}'

# 2. Record current model.fga git SHA
git -C /path/to/keese rev-parse HEAD:dev/bootstrap/openfga/model.fga

# 3. Count in-flight WorkflowRuns (expect 0 before migrating off-peak)
kubectl get workflowruns -A --field-selector status.phase=Running

# 4. Confirm new model ID from seed Job output (logged to keese-seed job pod)
kubectl logs -n keese-system job/keese-openfga-seed --tail=20 | grep "model_id"
```

## Step 1 — Stage new model

The keese-seed Job applies the new `model.fga` and prints the new model ID.
It does NOT update the ConfigMap. Verify the new ID is available in OpenFGA
before entering migration mode.

## Step 2 — Enter MODEL_MIGRATION

```bash
# Sets cluster-wide migration flag; webhook blocks new WorkflowRun creation
kubectl annotate deployment keese-operator -n keese-system \
  keese.ai/model-migration=enter-<new-model-id> --overwrite
```

Watch for the `ModelMigrationEntered` event:

```bash
kubectl get events -n keese-system --field-selector reason=ModelMigrationEntered -w
```

## Step 3 — Observe drain

```bash
# Poll until count reaches 0 (or drain-timeout of 10 min fires)
watch kubectl get workflowruns -A --field-selector status.phase=Running
```

Expected: running count decreases to 0 as WorkflowRuns complete on the old model ID.
New WorkflowRun creation returns `ModelMigrationInProgress` during this window.

## Step 4 — Handle drain timeout (abort path)

If `DrainTimeout` event fires before count reaches 0:

```bash
# Abort: clears flag, restores webhook, does NOT update ConfigMap
kubectl annotate deployment keese-operator -n keese-system \
  keese.ai/model-migration=abort --overwrite

# Confirm ConfigMap NOT updated (still shows old model ID)
kubectl get cm keese-rebac-config -n keese-system \
  -o jsonpath='{.data.OPENFGA_AUTHORIZATION_MODEL_ID}'
```

If abort is unacceptable (critical security patch), use 04c force-revoke on
stuck WorkflowRuns as supervisor SA, then re-enter drain observation.

## Step 5 — Atomic swap (drain complete)

Operator performs this automatically when in-flight count reaches 0:

```bash
# Verify swap completed
kubectl get cm keese-rebac-config -n keese-system \
  -o jsonpath='{.data.OPENFGA_AUTHORIZATION_MODEL_ID}'
# Must equal <new-model-id>
```

## Step 6 — Readiness gate observation

Operator polls all pods for `status.observedModelID`. Do not manually
exit migration until 100% convergence:

```bash
# All values must be identical and equal <new-model-id>
kubectl get pods -n keese-system \
  -l app.kubernetes.io/part-of=keese \
  -o jsonpath='{range .items[*]}{.metadata.name}{"\t"}{.status.observedModelID}{"\n"}{end}' \
  | sort -k2 | uniq -f1
```

If one pod is stuck (event `ReadinessGateStuck`): restart that pod; operator
will re-poll. If stuck for > 5 min, investigate `keese-authz` ext_authz pod health.

## Step 7 — Exit MODEL_MIGRATION

Operator auto-clears the annotation on convergence. Verify:

```bash
kubectl get events -n keese-system --field-selector reason=ModelMigrationComplete
kubectl annotate deployment keese-operator -n keese-system \
  keese.ai/model-migration- 2>/dev/null; echo "annotation cleared (or already gone)"
```

## Rollback

Rollback uses the same sequence with the old model ID as the target:

1. Ensure old model ID still exists in OpenFGA (it is never deleted; seed Job is additive).
2. Enter: `kubectl annotate deployment keese-operator ... keese.ai/model-migration=enter-<old-model-id>`.
3. Drain new-model runs.
4. Atomic swap to old ID.
5. Readiness gate.
6. Exit.

Record the rollback incident in `docs/plans/migration-revocation-<slug>.md`.

## Refs

- [04a-openfga-authz-model.md](../designs/04a-openfga-authz-model.md)
- [04a-ii-testplan.md](../designs/04a-ii-testplan.md) — `TestModelMigration_*` test assertions
- [04c-token-revocation.md](../designs/04c-token-revocation.md) — force-revoke stuck runs
