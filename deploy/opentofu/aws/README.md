<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: reference
category: deployment
depends: [docs/designs/21-opentofu-cloud-deployment.md, docs/designs/04b-ii-oidc-trust.md]
status: current
last_verified: 2026-05-07
---

# OpenTofu — AWS (EKS) module

Provisions an EKS cluster (managed node groups across ≥ 2 AZs), an IAM OIDC
provider, and a per-cluster controller ServiceAccount IRSA role. Secrets
encryption is mandatory; the Conftest policy `require-encryption-at-rest.rego`
denies plans without a KMS key ARN.

## Prerequisites

| Tool | Version |
|---|---|
| OpenTofu | ≥ 1.7.0 |
| AWS CLI | ≥ 2.15 |
| Conftest | ≥ 0.49 |

Caller must hold an IAM principal with permissions for: `eks:*`,
`ec2:*` (VPC/subnet/NAT), `iam:Create*`, `iam:Attach*`,
`iam:PassRole`, `secretsmanager:*` in the target region.

## State backend

Create the S3 bucket and DynamoDB table before first `tofu init`.
Configure them in a `backend.tf` (do not commit to git):

```hcl
terraform {
  backend "s3" {
    bucket         = "<your-state-bucket>"
    key            = "keese/<env>/terraform.tfstate"
    region         = "<region>"
    encrypt        = true
    kms_key_id     = "<kms-key-arn>"
    dynamodb_table = "<your-lock-table>"
  }
}
```

`backend "local"` is denied by `policy/opentofu/deny-local-backend.rego`.

## Plan / apply

```shell
# 1. Export required vars (never commit these values)
export TF_VAR_cluster_name=keese-prod
export TF_VAR_region=us-east-1
export TF_VAR_availability_zones='["us-east-1a","us-east-1b"]'
export TF_VAR_secrets_encryption_key_arn=arn:aws:kms:us-east-1:123456789012:key/mrk-...

# 2. Init + validate
tofu init
tofu validate

# 3. Conftest gate (CI runs this automatically)
tofu plan -out=plan.tfplan -lock=false
tofu show -json plan.tfplan > plan.json
conftest test plan.json -p ../../../../policy/opentofu/

# 4. Apply (requires infra-approvers review in CI)
tofu apply plan.tfplan
```

## Outputs used by keese operator

| Output | Where consumed |
|---|---|
| `kubeconfig_command` | `scripts/cloud/bootstrap.sh` — writes kubeconfig |
| `oidc_issuer_url` | `Workspace.status.cloudRefs.oidcIssuerURL` |
| `oidc_provider_arn` | per-tenant IRSA role trust policies |
| `controller_sa_role_arn` | annotates `keese-controller-manager` SA |

## Public endpoint policy

`endpoint_public_access` is enabled only when `eks_public_access_cidrs` is
non-empty. Conftest `deny-public-cluster.rego` blocks plans where public
access is on and the CIDR list is empty or contains `0.0.0.0/0`.

## See also

- [docs/designs/21-opentofu-cloud-deployment.md](../../../../docs/designs/21-opentofu-cloud-deployment.md)
- [docs/designs/04b-ii-oidc-trust.md](../../../../docs/designs/04b-ii-oidc-trust.md)
- [policy/opentofu/](../../../../policy/opentofu/)
