<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: reference
category: deployment
depends: []
related_skills: []
status: draft
last_verified: 2026-04-19
---

# OpenTofu Cloud Deployment

> **Status: draft.** Stub — fill in after design 21 reaches `status: current`.
> Module sources live under `deploy/opentofu/{aws,gcp,azure}/`.

## Contents (to expand)

1. **Module layout** — `deploy/opentofu/{aws,gcp,azure}/` structure; shared modules
   under `deploy/opentofu/modules/`; `variables.tf` / `outputs.tf` conventions.
2. **State backend** — S3+DynamoDB (AWS), GCS (GCP), Azure Storage; encryption at
   rest; lock configuration; workspace isolation per environment.
3. **IAM / Workload Identity** — IRSA (AWS), Workload Identity Federation (GCP),
   Managed Identity (Azure); per-tenant audience binding.
4. **`make tofu-plan` / `tofu-validate`** — CI plan-only flow; OPA `conftest` policy
   checks under `policy/opentofu/`; no `apply` without manual approval gate.
5. **OLM install on top** — `operator-sdk run bundle` after cluster is ready;
   `make smoke-ci` validates end-to-end on provisioned cluster.

## Refs

- [../plans/rubric.md](../plans/rubric.md)
- [Plan file](/Users/marshallmccain/.claude/plans/you-are-an-expert-iridescent-alpaca.md)
- [../designs/21-opentofu-cloud-deployment.md](../designs/21-opentofu-cloud-deployment.md)

TODO(design-gate)
