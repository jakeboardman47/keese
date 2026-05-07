<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: reference
category: deployment
depends: [docs/designs/21-opentofu-cloud-deployment.md, docs/designs/04b-ii-oidc-trust.md]
status: current
last_verified: 2026-05-07
---

# OpenTofu — Azure (AKS) module

Provisions an AKS cluster across ≥ 2 availability zones, a user-assigned
managed identity for the keese controller, and a federated credential binding
to the keese-controller-manager Kubernetes ServiceAccount (Entra Workload
Identity). CMK disk encryption is mandatory; Conftest
`require-encryption-at-rest.rego` denies plans without `disk_encryption_set_id`.

## Prerequisites

| Tool | Version |
|---|---|
| OpenTofu | ≥ 1.7.0 |
| Azure CLI | ≥ 2.62 |
| Conftest | ≥ 0.49 |

Required Azure RBAC roles:
`Contributor` on the subscription (or scoped RG), plus
`User Access Administrator` to assign Key Vault access policies.

## State backend

Create the Storage Account + container before first `tofu init`. Configure
via `backend.tf`:

```hcl
terraform {
  backend "azurerm" {
    resource_group_name  = "<rg-for-state>"
    storage_account_name = "<your-storage-account>"
    container_name       = "<your-container>"
    key                  = "keese/<env>/terraform.tfstate"
  }
}
```

`backend "local"` is denied by `policy/opentofu/deny-local-backend.rego`.

## Plan / apply

```shell
export TF_VAR_cluster_name=keese-prod
export TF_VAR_location=eastus2
export TF_VAR_disk_encryption_set_id=/subscriptions/.../diskEncryptionSets/keese-des
export TF_VAR_key_vault_id=/subscriptions/.../vaults/keese-kv

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
| `controller_sa_client_id` | `azure.workload.identity/client-id` annotation |
| `controller_sa_annotation` | copy-paste ready annotation value |

## See also

- [docs/designs/21-opentofu-cloud-deployment.md](../../../../docs/designs/21-opentofu-cloud-deployment.md)
- [docs/designs/04b-ii-oidc-trust.md](../../../../docs/designs/04b-ii-oidc-trust.md)
- [policy/opentofu/](../../../../policy/opentofu/)
