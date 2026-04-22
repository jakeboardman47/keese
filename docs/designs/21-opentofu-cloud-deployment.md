<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: design
category: deployment
depends: [11-secrets-pluggable-vault.md, 14a-olm-channels-upgrades.md, 04b-ii-oidc-trust.md]
related_skills: []
status: current
last_verified: 2026-04-21
rollback: |
  Cluster-only (reversible): delete OLM Subscription + CSV, re-apply prev
  bundle via `operator-sdk run bundle <prev-bundle-image>`. Full destroy
  (irreversible): `tofu destroy` — see §Rollback decision tree.
---

# 21 — OpenTofu Cloud Deployment

**Decision:** `deploy/opentofu/{aws,gcp,azure}/` modules provision cloud infra;
a `keese.tf` root module composes them with `operator-sdk run bundle` to
install keese via OLM. OpenTofu is the sole authoritative source for all cloud
IAM including OIDC trust (cross-cut to design 04b-ii). Local state is forbidden.

## Module catalog

| Cloud | Cluster | IAM / OIDC | Secrets | LB + DNS |
|---|---|---|---|---|
| AWS | EKS (managed node groups, ≥2 AZ) | IAM OIDC provider + per-tenant IRSA role | Secrets Manager | ALB controller + external-DNS |
| GCP | GKE Autopilot | Workload Identity Pool + provider + per-tenant SA binding | Secret Manager | GCLB (NEG) + external-DNS |
| Azure | AKS (`Standard_D4s_v5` × 2 AZ, max 10) | Entra user-assigned managed identity + per-workspace federated credential | Key Vault | App Gateway (AGIC) + external-DNS |

Each module exports four outputs: `cluster_kubeconfig`, `oidc_issuer_url`,
`secret_manager_endpoint`, `lb_dns_name`. Manual cloud-CLI OIDC registration
is forbidden; Conftest policy `deny-manual-oidc` enforces this.

## Module composition (`keese.tf`)

`keese.tf` selects a per-cloud module via `TF_VAR_cloud`, then runs
`scripts/cloud/bootstrap.sh <cloud> <env> <region>` as a `local-exec`
provisioner. The script: (a) Helmfile applies `dev/bootstrap/helmfile.yaml`,
(b) waits for cert-manager webhook ready, (c) runs `operator-sdk run bundle`
with retry (30 s back-off × 3), (d) emits structured log
`{event:"bootstrap_complete", duration_ms:N}`. All module `source` refs must
use `version = "= X.Y.Z"` (Conftest `module-version-pinned.rego` denies ranges).

## State backend

| Cloud | Backend | Locking |
|---|---|---|
| AWS | S3 + KMS SSE-S3 | DynamoDB `LockID` table |
| GCP | GCS + CMEK | GCS object versioning (built-in) |
| Azure | Azure Blob + SSE | Blob storage lease (built-in) |

`backend "local"` is denied by Conftest `deny-local-backend.rego`. State buckets
are cross-region replicated (S3 CRR / GCS multi-region / Azure GRS).

## Conftest policies (`policy/opentofu/`)

`s3-public.rego` — deny public S3 ACL.
`sg-open-ingress.rego` — deny `0.0.0.0/0` ingress on non-LB security groups.
`secrets-encrypted.rego` — deny secrets without encryption key ref.
`module-version-pinned.rego` — deny unpinned or range-versioned module refs.
`deny-local-backend.rego` — deny `backend "local"` blocks.
CI runs `conftest test --policy policy/opentofu/ plan.json` before any apply.

## OIDC trust setup

Cross-cut to `04b-ii-oidc-trust.md`. Each cloud module provisions the OIDC
provider (once per cluster) and per-tenant trust roles via `iam.tf` /
`identity.tf`. The Workspace controller reads resulting ARN/email/clientId
from `Workspace.status.cloudRefs.*`; it never calls cloud APIs directly.

## Failure modes

| # | Failure | Detection | Mitigation |
|---|---|---|---|
| F1 | Cluster create timeout (>30 min) | `tofu apply` non-zero | Re-run; provider retries idempotently |
| F2 | IAM propagation lag (AWS, up to 15 s) | `run bundle` 401 | 30 s back-off retry in bootstrap script |
| F3 | DNS propagation delay (up to 5 min) | NXDOMAIN on health check | Bootstrap polls `dig` with 60 s timeout |
| F4 | State lock contention | `Error acquiring state lock` | CI serializes via GitHub environment + concurrency group |
| F5 | Partial OIDC trust | Workspace pod 401; `keese_gateway_jwks_fetch_failures_total > 0` | Alert P2 (04b-ii §JWKS); operator degrades `BackendSecurityPolicy` |
| F6 | OLM install timeout (>5 min) | `null_resource` non-zero | Re-run; OLM reinstall is idempotent |
| F7 | Secret Manager quota exhaustion | Cloud API 429 | Pre-apply quota check; cloud quota alarm |
| F8 | Partial `tofu destroy` | Dangling state resources | `tofu state rm` + runbook `deploy/opentofu/RUNBOOK.md` |

## Rollback

```
Keese operator regression only?
  YES → cluster-only rollback (reversible):
          kubectl delete subscription keese -n olm
          kubectl delete csv keese.v<version> -n olm
          operator-sdk run bundle <prev-bundle-image> --kubeconfig ...
  NO, cloud infra broken?
    YES → tofu destroy (IRREVERSIBLE — destroys cluster + PVCs + all tenant data)
          Requires: manual approval in GitHub environment "production-destroy"
          Pre-condition: confirm tenant data backed up to external store
```

Multi-region: each region is an independent workspace (`tofu workspace select
<region>`). Rollback in one region does not affect others.

## CI workflow

Cross-cut to `.github/workflows/opentofu.yaml`. Per-PR: OIDC-scoped plan only
(`id-token: write, contents: read`); `conftest test` on plan JSON; plan
artifact attached. On merge: `cloud-apply` environment requires manual approval
from `@keese-ai/infra-approvers`; apply runs; `scripts/cloud/smoke-test.sh`
asserts OLM CSV phase = Succeeded, ≥1 Workspace accepted, BackendSecurityPolicy
Ready. Destroy: separate `opentofu-destroy.yaml` with two-approver gate; never
triggered automatically.

## Observability

OTEL traces per resource via OpenTelemetry Terraform provider → Elastic APM.
Alerts: `keese_tf_apply_duration_seconds > 1800` P2;
`keese_tf_state_lock_wait_seconds > 300` P2. Required Kibana dashboard panels
(authored in P7): `tofu_apply_duration_p95`, `tofu_state_lock_wait_p95`,
`olm_csv_phase` gauge, `bootstrap_success_rate` (7d), `cluster_node_count`
per cloud+region.

## Operational readiness

EKS/GKE/AKS control planes are cloud-managed HA. Worker groups span ≥2 AZs.
OLM runs `replicas: 2`. Upgrade path: re-run `keese.tf` with new
`bundle_image` var; OLM upgrades via `replaces` chain (design 14a); no cluster
recreation. GKE Autopilot self-scales from resource requests (no node config).

## Refs

- [04b-ii-oidc-trust.md](04b-ii-oidc-trust.md) — OIDC trust per cloud
- [11-secrets-pluggable-vault.md](11-secrets-pluggable-vault.md) — secret manager
- [14a-olm-channels-upgrades.md](14a-olm-channels-upgrades.md) — OLM upgrade chain
- [../references/opentofu-cloud-deployment.md](../references/opentofu-cloud-deployment.md)
- [../plans/rubric.md](../plans/rubric.md)

## Iteration log

See [21b-opentofu-iter-log.md](21b-opentofu-iter-log.md) — iter-3 score: 100/100.
