<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: design
category: packaging
depends: [20-api-group-layout.md, 17-credential-broker.md]
related_skills: [validate-bundle, olm-bundle-authoring]
status: current
last_verified: 2026-04-21
---

# 14a — OLM Channels and Upgrade Strategy

**Decision:** Three channels (`stable`, `candidate`, `fast`) with a
`replaces:`-chain upgrade graph, `skipRange:` for hotfix bypasses, and
cosign keyless OIDC bundle signing enforced at install and upgrade.
Script contracts, test assertions, and the iteration log live in
[14a-ii-iterations.md](14a-ii-iterations.md).

## Context

keese ships via OLM. Without a deliberate channel and upgrade-graph
design, operators collide on CRD ownership, skip breaking schema changes,
and lose the ability to roll back to a known-good bundle. This doc
governs the upgrade path from `keese.v0.0.1` forward. OLM dependency
declarations live in [14b-olm-dependencies.md](14b-olm-dependencies.md).

## 1. Channel Map

| Channel | Audience | Cadence | release-please tag |
|---|---|---|---|
| `fast` | CI / dev clusters | Every semver cut | `v*` |
| `candidate` | Pre-prod / staging | RC bake (≥ 72 h soak) | `v*-rc.*` |
| `stable` | Production | After ≥ 1 wk candidate soak + scorecard PASS | promoted from candidate |

**Promotion criteria — `candidate` → `stable`:** `operator-sdk scorecard`
passes all suites; zero open critical/high CVEs; ≥ 1 wk staging soak
with no reconcile-error spike; release engineer updates `annotations.yaml`
channel field via PR.

`installPlanApproval: Manual` is required on `stable` — ops must review
CRD diffs before approval.

## 2. Upgrade Graph

### replaces chain (standard path)

Every CSV declares exactly one predecessor:

```yaml
spec:
  replaces: keese.vX.Y.Z
```

`scripts/set-csv-replaces.sh <new-ver> <prev-ver>` patches this field and
re-runs `make bundle`. Called by `.github/workflows/release.yaml` after
release-please opens the release PR. Idempotent.

**Rule:** every minor and patch release adds one `replaces:` entry. Never
skip a predecessor in the standard chain.

### skipRange (hotfix / emergency bypass)

```yaml
metadata:
  annotations:
    olm.skipRange: '>=0.3.0 <0.4.0'
```

Use when a range of CSVs is known-broken and OLM must upgrade past them
in one step. `skipRange` is additive to `replaces:` — do not remove
`replaces:`. Requires a Locked Decision entry in `docs/plans/README.md`
with second-reviewer sign-off.

## 3. CRD Compatibility Per Upgrade

All 13 keese CRDs are `v1alpha1` (confirmed: `bundle/manifests/keese.clusterserviceversion.yaml`, `keese.v0.0.1`).

### v1alpha1 minor upgrades

Backward-compatible only: new optional fields, new enum values, looser
validation. Field removal, type changes, or tightened validation are
**forbidden** without a conversion webhook (rule 04.2 / 04.13).

CSV `owned` entry carries a single version; OLM updates the CRD in-place.
The reconciler must converge existing instances without a migration job.

### v1alpha1 → v1beta1 promotion

1. Add conversion webhook (`strategy: Webhook`) before the promoting CSV.
2. CSV `owned` declares both versions; `v1beta1` carries `storage: true`.
3. Requires `docs/plans/migration-<kind>.md` scored ≥ 90 (rule 04.2).
4. Promoting CSV carries `replaces:` pointing to last `v1alpha1`-only CSV.

**Conversion webhook HA:** 2 replicas, `PodDisruptionBudget minAvailable: 1`,
`terminationGracePeriodSeconds: 30`, `failurePolicy: Fail`.

## 4. Bundle Signing and Upgrade Verification

Anchored from rule 05.12:

- cosign keyless OIDC; no long-lived key material.
- OIDC issuer: `https://token.actions.githubusercontent.com`
- Identity regexp: `https://github.com/keese-ai/keese/.github/workflows/.*`
- `cosign attest --predicate sbom.json` on every bundle image.

`scripts/bundle-sign-verify.sh <bundle-image-digest>` runs `cosign verify`
with the issuer + regexp above; exits non-zero on failure. Required status
check before `make catalog-push`. A pre-install ValidatingWebhook rejects
`InstallPlan` approval if the bundle image digest is unsigned.

## 5. Rollback Runbook

OLM does not support automatic rollback.

1. Pin `Subscription.spec.startingCSV` to last known-good CSV name.
2. Set `installPlanApproval: Manual`.
3. Delete the bad CSV: `kubectl delete csv keese.vBAD -n operators`.
4. OLM recreates an `InstallPlan` for the pinned version; approve it.
5. Verify `status.conditions` on a sample Workspace shows `Ready: True`.
6. If the bad CSV introduced a breaking CRD schema change, restore from
   the good bundle artifact: `kubectl apply -f bundle/manifests/<crd>.yaml`.
7. File post-mortem; add skipped range to next release CSV's `skipRange`.

**Finalizer drain:** before step 3, confirm no `Terminating` workspaces.
Patch finalizers only after confirming external resources are deleted.

## 6. Failure Modes

| # | Failure | Detection | Mitigation |
|---|---|---|---|
| F1 | InstallPlan not approved | OLM InstallPlan status | Manual approval gate; alert on stale > 5 min |
| F2 | CRD ownership conflict | OLM install-time check | Unique group domains (rule 04.1) + 14b deps |
| F3 | Bundle image missing / digest mismatch | ImagePullBackOff | Digest-pinned images; cosign verify gate in CI |
| F4 | replaces chain gap | OLM "no upgrade path" | set-csv-replaces.sh enforced in CI PR check |
| F5 | Conversion webhook unavailable | CRD conversion error | HA webhook (2 replicas, PDB); rollback runbook |
| F6 | skipRange misconfigured | Subscribers miss compatible release | Locked Decision entry + second reviewer required |
| F7 | Bundle image unsigned / tampered | Pre-install webhook rejects plan | bundle-sign-verify.sh blocks release; fail-closed |

## 7. Observability

- Alert on `InstallPlan` in `Failed` phase > 5 min.
- `keese_olm_upgrade_total{channel, from_version, to_version, result}` counter
  emitted by a post-install Job.
- `keese_bundle_signature_verify_duration_seconds` histogram from the
  pre-install webhook.
- Structured log: `{"event":"bundle_upgrade","from":"v0.2.1","to":"v0.3.0","channel":"stable","result":"success"}`.

## Next Steps

1. Implement `scripts/set-csv-replaces.sh` and `scripts/bundle-sign-verify.sh` (P8).
2. Create `test/e2e/olm-upgrade/` kuttl suite — see [14a-ii-iterations.md](14a-ii-iterations.md) for full assertion spec (P8).
3. Wire both scripts into `.github/workflows/release.yaml` (P8).
4. At first v1beta1 promotion, create `docs/plans/migration-<kind>.md`.

_Iteration log and rubric scores: [14a-ii-iterations.md](14a-ii-iterations.md)_
