<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: design
category: deployment
depends: [11-secrets-pluggable-vault.md, 14a-olm-channels-upgrades.md]
related_skills: []
status: draft
last_verified: 2026-04-19
rollback: TODO — document migration path when status flips to current
---

# 21 — OpenTofu Cloud Deployment

> **Status: draft.** This doc is a gate-placeholder. Architect agent
> fills content across 3 rubric iterations (target ≥ 90/100) before
> any spec or controller code references this design.

## Context

_One paragraph. What problem does this design address? Per-cloud OpenTofu modules
under `deploy/opentofu/{aws,gcp,azure}/` provision EKS/GKE/AKS clusters, secret
managers, IAM/WI bindings, and DNS. The keese OLM bundle installs on top via
`operator-sdk run bundle`._

## Open questions (must be answered before `status: current`)

1. What is the state backend for each cloud (S3+DynamoDB / GCS / Azure Storage)
   and how is state encryption and locking configured in the module?
2. How does the IAM/WI binding module create the per-tenant IRSA role
   (AWS) / Workload Identity binding (GCP) / Managed Identity (Azure)?
3. What is the `tofu apply` approval gate in CI — GitHub environment protection
   rule, manual dispatch, or OPA policy check on the plan output?
4. How does the OpenTofu module handle multi-region deployment — separate
   workspaces per region, or a single module with region as variable?
5. What is the rollback procedure when a `tofu apply` fails mid-way — partial
   resource creation, automatic destroy, or manual recovery runbook?

## Refs

- [../plans/rubric.md](../plans/rubric.md)
- [Plan file](/Users/marshallmccain/.claude/plans/you-are-an-expert-iridescent-alpaca.md)
- [11-secrets-pluggable-vault.md](11-secrets-pluggable-vault.md)
- [../references/opentofu-cloud-deployment.md](../references/opentofu-cloud-deployment.md)

TODO(design-gate)
