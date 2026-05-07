<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: reference
category: deployment
depends: [docs/designs/21-opentofu-cloud-deployment.md, docs/designs/04b-ii-oidc-trust.md]
status: current
last_verified: 2026-05-07
---

# OpenTofu — GCP (GKE Autopilot) module

Provisions a GKE Autopilot cluster (regional, private nodes), a Workload
Identity binding for the keese controller, and a GCP Service Account with
scoped Secret Manager access. CMEK etcd encryption is mandatory; Conftest
`require-encryption-at-rest.rego` denies plans without a valid key name.

## Prerequisites

| Tool | Version |
|---|---|
| OpenTofu | ≥ 1.7.0 |
| gcloud CLI | ≥ 469.0 |
| Conftest | ≥ 0.49 |

Required GCP roles for the caller:
`roles/container.admin`, `roles/iam.serviceAccountAdmin`,
`roles/iam.workloadIdentityPoolAdmin`, `roles/secretmanager.admin`,
`roles/cloudkms.admin`.

## State backend

Create the GCS bucket before first `tofu init`. Object versioning provides
built-in locking for the GCS backend. Configure via `backend.tf`:

```hcl
terraform {
  backend "gcs" {
    bucket = "<your-state-bucket>"
    prefix = "keese/<env>"
  }
}
```

`backend "local"` is denied by `policy/opentofu/deny-local-backend.rego`.

## Plan / apply

```shell
export TF_VAR_cluster_name=keese-prod
export TF_VAR_project_id=my-gcp-project
export TF_VAR_region=us-central1
export TF_VAR_database_encryption_key=projects/my-gcp-project/locations/us-central1/keyRings/keese/cryptoKeys/etcd

tofu init
tofu validate

tofu plan -out=plan.tfplan -lock=false
tofu show -json plan.tfplan > plan.json
conftest test plan.json -p ../../../../policy/opentofu/

tofu apply plan.tfplan
```

## Outputs used by keese operator

| Output | Where consumed |
|---|---|
| `kubeconfig_command` | `scripts/cloud/bootstrap.sh` |
| `oidc_issuer_url` | `Workspace.status.cloudRefs.oidcIssuerURL` |
| `workload_identity_pool` | per-tenant KSA Workload Identity annotation |
| `controller_sa_email` | annotates `keese-controller-manager` KSA |

## See also

- [docs/designs/21-opentofu-cloud-deployment.md](../../../../docs/designs/21-opentofu-cloud-deployment.md)
- [docs/designs/04b-ii-oidc-trust.md](../../../../docs/designs/04b-ii-oidc-trust.md)
- [policy/opentofu/](../../../../policy/opentofu/)
