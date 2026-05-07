<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: reference
category: policy
depends: [docs/designs/21-opentofu-cloud-deployment.md]
status: current
last_verified: 2026-05-07
---

# policy/opentofu — Conftest Rego policies

OPA/Conftest policies for the keese OpenTofu modules. Package:
`keese.opentofu.security`. CI runs `conftest test` against the plan JSON
before any `tofu apply` is permitted.

## Rules

| File | What it denies |
|---|---|
| `deny-public-cluster.rego` | EKS `endpoint_public_access=true` with no restricted CIDRs; GKE public master without authorized networks; AKS unrestricted API server IPs |
| `deny-default-iam.rego` | EKS node groups using a default/shared node IAM role instead of a per-cluster explicit role |
| `require-encryption-at-rest.rego` | EKS clusters without `encryption_config` for secrets; GKE clusters without `database_encryption.state=ENCRYPTED`; AKS clusters without `disk_encryption_set_id` |
| `deny-public-bucket.rego` | S3 buckets with public ACLs or incomplete public access blocks; GCS buckets with `allUsers`/`allAuthenticatedUsers` IAM members; Azure Storage Accounts with `allow_blob_public_access=true` |

Each rule file has a sibling `_test.rego` with table-driven allow + deny cases.

## Running tests

```shell
# From repo root
conftest verify policy/opentofu/

# Or via make
make tofu-validate
```

## Adding a new rule

1. Create `policy/opentofu/<rule-name>.rego` in package `keese.opentofu.security`.
2. Create sibling `policy/opentofu/<rule-name>_test.rego` in
   `keese.opentofu.security_test`.
3. Add a row to the table above.
4. Verify `conftest verify policy/opentofu/` passes.
