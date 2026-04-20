<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: design
category: secrets
depends: [05b-credential-injection-patterns.md]
related_skills: []
status: draft
last_verified: 2026-04-19
rollback: TODO — document migration path when status flips to current
---

# 11 — Secrets Pluggable Vault

> **Status: draft.** This doc is a gate-placeholder. Architect agent
> fills content across 3 rubric iterations (target ≥ 90/100) before
> any spec or controller code references this design.

## Context

_One paragraph. What problem does this design address? OpenBao (local dev) and
cloud KMS providers (AWS Secrets Manager, GCP Secret Manager, Azure Key Vault)
are bridged via ExternalSecrets Operator to K8s Secrets consumed by
`BackendSecurityPolicy`._

## Open questions (must be answered before `status: current`)

1. What is the exact OpenBao path hierarchy for keese secrets, and which ESO
   `SecretStore` / `ClusterSecretStore` CR templates project them?
2. How does secret rotation work end-to-end — OpenBao version bump → ESO sync
   → K8s Secret update → Envoy hot-reload without pod restart?
3. When is the vault-agent sidecar file-mount pattern acceptable vs. prohibited
   (non-AI upstreams needing file-mount creds per D10)?
4. What is the failover order when OpenBao is unavailable — use cached K8s
   Secret, fail-closed, or fall back to cloud KMS directly?
5. How does each cloud KMS provider authenticate to the keese cluster — IRSA
   (AWS), Workload Identity (GCP), or Managed Identity (Azure)?

## Refs

- [../plans/rubric.md](../plans/rubric.md)
- [Plan file](/Users/marshallmccain/.claude/plans/you-are-an-expert-iridescent-alpaca.md)
- [05b-credential-injection-patterns.md](05b-credential-injection-patterns.md)
- [17-credential-broker.md](17-credential-broker.md)
- [21-opentofu-cloud-deployment.md](21-opentofu-cloud-deployment.md)

TODO(design-gate)
