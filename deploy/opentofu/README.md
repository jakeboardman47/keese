<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: reference
category: deployment
depends: [docs/designs/21-opentofu-cloud-deployment.md]
status: current
last_verified: 2026-05-07
---

# deploy/opentofu — module index

Self-contained OpenTofu modules for each supported cloud target. Each module
provisions a managed Kubernetes cluster, cloud-native workload identity, and
the minimal IAM surface required by the keese operator. D4 (cloud deploy) is
deferred; these are authorable artifacts, not applied infra.

## Modules

| Cloud | Path | Cluster | Identity |
|---|---|---|---|
| AWS | [aws/](aws/) | EKS managed node groups (≥ 2 AZ) | IAM OIDC provider + IRSA |
| GCP | [gcp/](gcp/) | GKE Autopilot (regional) | Workload Identity Federation |
| Azure | [azure/](azure/) | AKS (`Standard_D4s_v5`, ≥ 2 AZ) | Entra user-assigned managed identity |

## Owning design

[docs/designs/21-opentofu-cloud-deployment.md](../../docs/designs/21-opentofu-cloud-deployment.md)

## Conftest policies

All plans must pass `conftest test` before apply:

```shell
make tofu-validate
```

Policy files live in [policy/opentofu/](../../policy/opentofu/). See that
directory's README for rule descriptions.

## Prerequisites

| Tool | Minimum | Notes |
|---|---|---|
| OpenTofu | 1.7.0 | `brew install opentofu` or Nix flake (`nix develop`) |
| Conftest | 0.49 | `brew install conftest` or Nix flake |
| Cloud CLI | see each module's README | AWS CLI 2.15 / gcloud 469 / az 2.62 |

OpenTofu and Conftest are included in the project Nix flake (`flake.nix`).
If you are not using the Nix shell, install them from the versions above before
running `make tofu-validate`.

## Running tofu-validate locally

```shell
# From repo root
make tofu-validate
```

This runs `tofu fmt -check` + `tofu init -backend=false` + `tofu validate`
for each module, then `conftest test deploy/opentofu/ -p policy/opentofu/`.

## State backend summary

| Cloud | Backend | Locking |
|---|---|---|
| AWS | S3 + KMS SSE | DynamoDB `LockID` table |
| GCP | GCS + CMEK | GCS object versioning (built-in) |
| Azure | Azure Blob + SSE | Blob lease (built-in) |

`backend "local"` is denied by `policy/opentofu/deny-local-backend.rego`.
Do not commit a `backend.tf` with real bucket names to git.

## CI / apply gates

Per-PR: `tofu plan` (read-only, `GITHUB_TOKEN` OIDC) + `conftest test`.
On merge: `cloud-apply` environment requires manual approval from
`@keese-ai/infra-approvers`. See `.github/workflows/opentofu.yaml`.
