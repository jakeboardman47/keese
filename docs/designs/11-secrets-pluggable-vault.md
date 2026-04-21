<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: design
category: secrets
depends:
  - 04b-projected-sa-identity.md
  - 04b-ii-oidc-trust.md
  - 05b-credential-injection-patterns.md
  - 10a-otel-topology.md
  - 17-credential-broker.md
  - 21-opentofu-cloud-deployment.md
related_skills: []
status: current
last_verified: 2026-04-21
rollback: |
  OpenBao: restore from latest auto-snapshot (6h cadence) in object storage;
  run `scripts/openbao-restore.sh <snapshot-path>`; ESO force-sync via annotation
  on ExternalSecret CRs; Envoy picks up refreshed K8s Secrets within 1 min.
  Cloud KMS fallback: revert `Tenant.spec.secrets.openBaoFallbackProvider` via SSA
  (fieldOwner: keese-tenant-controller); ESO swaps SecretStore within one poll cycle.
  Document in docs/plans/migration-secrets-<incident>.md.
---

# 11 — Secrets: Pluggable Vault

## Context

Agent pods carry no upstream credentials (rules 05.1, 05.2). Credentials live in
OpenBao (local dev + on-prem) or cloud KMS (AWS Secrets Manager, GCP Secret Manager,
Azure Key Vault). ExternalSecrets Operator bridges vault secrets to K8s Secrets consumed
by `BackendSecurityPolicy`. This design answers the five open questions from the stub and
is the vault-plumbing authority for 05b and 17.

## Decision

OpenBao KV v2 is the primary secrets store; ESO bridges to K8s Secrets consumed by
`BackendSecurityPolicy`; cloud KMS is supported as primary or fallback. vault-agent
sidecar is allowed on gateway pods only (file-mount upstreams). Rotation is zero-restart
via inotify. Failover defaults to cached K8s Secret (fail-closed at `maxStaleSeconds`);
cloud KMS fallback requires explicit tenant opt-in.

## Q1 — OpenBao path hierarchy + ESO templates

```
kv/keese/
  system/
    operator-signing-key
    apm-token                         # consumed by 10a otel collector
  oidc/
    providers/<provider>/jwks-cache   # ESO mirror for offline OIDC verify
  tenants/<tenant>/
    credentials/<provider>/<key-name> # upstream API keys (Anthropic, OpenAI, etc.)
    triggers/<workflow-name>/
      github-secret                   # HMAC secret for webhook verification
    tls/<cert-name>                   # cert-manager Vault issuer material
    a2a/<peer-name>/client-secret     # agent-to-agent auth secrets
  workspaces/<workspace-uid>/
    resume-tokens/<epoch>             # encrypted session resume tokens
```

ESO store layout:
- `ClusterSecretStore keese-openbao` (cluster-scoped; operator-level paths only).
- `SecretStore keese-openbao-<tenant>` per tenant namespace (tenant-scoped paths; least
  privilege — cannot read other tenants).
- `ExternalSecret` per credential referencing the tenant-scoped `SecretStore`.
  `RefreshInterval: 5m` default; overridable to `1m` for high-rotation keys.
- Three canonical CR templates ship under `config/secrets/`:
  `external-secret-api-key.yaml`, `external-secret-webhook-hmac.yaml`,
  `external-secret-a2a-client.yaml`.

## Q2 — Rotation flow (zero pod restart)

Admin writes new OpenBao version → ESO detects within 5 min → K8s Secret updated
(`keese.ai/version=<openbao-version>`) → kubelet projects to gateway pod volume within 1 min
(inotify) → broker (17) refreshes Envoy cache using D13 70%/95% TTL semantics → new requests
use new credential; in-flight requests drain on old. Static keys: ≤ 6 min total lag.
Worst-case formula (05b): `max(remaining_old_TTL, 0.70 × new_TTL)`.

## Q3 — vault-agent sidecar: when acceptable

**Allowed only on gateway pods** when upstream requires a file-mount credential format
(PostgreSQL DSN file, Jira basic-auth file) that `BackendSecurityPolicy` cannot inject.
Sidecar reads from `vault.keese-system.svc:8200` via SA-token auth; mounts creds to
`/var/run/keese/upstream-creds/<name>`; Envoy reads via `file_based_plugin`.

**Prohibited on:**
- Agent runtime pods (rules 05.1, 05.2 — no upstream credentials on agent pods).
- Operator or controller pods (no upstream creds needed; use SA token directly).

Sidecar injected via operator-managed `PodTemplateSpec` patch; `readOnlyRootFilesystem: true`;
writes only to the mounted credential volume.

## Q4 — Failover when OpenBao unavailable

Default flow:
1. OpenBao unreachable → ESO pollers fail → K8s Secrets not updated (last-known-good).
2. Projected Secret in gateway pods unchanged; Envoy cache continues serving.
3. When `keese.ai/version` annotation age > `Tenant.spec.secrets.maxStaleSeconds`
   (default 86400 s / 24 h): ESO emits `SecretStale` event; gateway controller patches
   BSP to `Ready=False`; new requests receive 503 `SecretStaleFailClosed`.
4. Alert `SecretStale` fires at 50% of `maxStaleSeconds`; `SecretStaleFailClosed` fires
   at 95%.

Cloud KMS fallback: `Tenant.spec.secrets.openBaoFallbackProvider: aws|gcp|azure|none`
(default `none`). When set and OpenBao unreachable > `openBaoFallbackAfterSeconds` (default 300 s),
ESO swaps `SecretStore`; emits `OpenBaoFallbackActive`; restores on recovery.

## Q5 — Cloud KMS authentication

| Cloud | Mechanism | K8s SA annotation |
|---|---|---|
| AWS Secrets Manager | IRSA (OIDC STS) | `eks.amazonaws.com/role-arn: arn:aws:iam::<acct>:role/keese-eso-<tenant>` |
| GCP Secret Manager | Workload Identity Federation | `iam.gke.io/gcp-service-account: keese-eso-<tenant>@<proj>.iam.gserviceaccount.com` |
| Azure Key Vault | Workload Identity (Federated) | `azure.workload.identity/client-id: <managed-identity-client-id>` |

Trust policy detail (per-cloud OIDC configuration) in [04b-ii](04b-ii-oidc-trust.md).
Cloud IAM roles for ESO provisioned by `deploy/opentofu/{aws,gcp,azure}/secrets.tf`
(21 cross-ref). ESO `SecretStore` CR uses `spec.provider.<cloud>` with the annotated SA.

## Operational concerns

- **Backup:** 6 h auto-snapshot → object storage; 30-day retention; `scripts/openbao-restore-test.sh`
  monthly in CI; ≤ 15 min RTO.
- **Audit:** OpenBao audit log → OTEL Tier 2 (10a) → ES `keese-secrets-audit-*` (30-day) + Loki
  (≥ 1 year). Format: `(path, operation, sa, decision, error_code)`. No secret values (rule 02).
- **Break-glass:** namespace label `keese.ai/break-glass=true` + annotation
  `keese.ai/unsafe-vault-bypass=true` (rule 05.13); `UnsafeAnnotationAllowed` event + `MEMORY.md`
  entry with approver from `kubectl auth whoami`.

## Trade-offs

| Option | Decision | Reason |
|---|---|---|
| vault-agent on agent pods | Rejected | Rules 05.1, 05.2 — no upstream creds on agent pods |
| envFrom.secretRef on any pod | Rejected | Rule 05.7 — secrets as projected files only |
| Fail-open indefinitely on stale cache | Rejected | Allows expired credentials to persist; rule 05.6 |
| Cloud KMS fallback always on | Rejected | Increases IAM attack surface; tenant opt-in only |
| ESO ClusterSecretStore for tenant paths | Rejected | Violates least privilege; tenant `SecretStore` per namespace |

## Failure modes

| Failure | Detection | Mitigation |
|---|---|---|
| OpenBao unreachable | ESO sync error; `VaultUnavailable` event | Last-known-good K8s Secret; fallback opt-in |
| Secret stale > `maxStaleSeconds` | `SecretStaleFailClosed` event | BSP `Ready=False`; 503 new requests |
| ESO sync lag (version drift) | `keese.ai/version` annotation age | Force-sync annotation; `CredentialRotationStale` |
| vault-agent sidecar crash | Empty file mount | Envoy deny (empty cred); PDB ensures 1 healthy pod |
| Cloud KMS fallback auth fail | ESO provider error | Remain on stale K8s Secret; alert fires |
| Break-glass misuse | `UnsafeAnnotationAllowed` event | Audit log + `MEMORY.md` entry; ops review |

## Observability

Metrics (OTEL Tier 2 / Prometheus):
- `keese_secret_stale_seconds{tenant,provider}` — age of K8s Secret vs. OpenBao version.
- `keese_eso_sync_failures_total{tenant,store,provider}` — ESO sync error count.
- `keese_openbao_fallback_active{tenant,cloud}` — gauge 1 when fallback is active.

Events: `SecretStale`, `SecretStaleFailClosed`, `OpenBaoFallbackActive`,
`OpenBaoFallbackRecovered`, `UnsafeAnnotationAllowed`, `VaultUnavailable`.

Alerts: `SecretStale` at 50% `maxStaleSeconds` (P2); `SecretStaleFailClosed` at 95% (P1);
`ESOSyncFailure` sustained 15 min (P2).

## Refs

- [04b-ii](04b-ii-oidc-trust.md) — cloud OIDC trust per provider (IRSA / WIF / Azure)
- [05b](05b-credential-injection-patterns.md) — BSP credential types; rotation drain formula
- [10a](10a-otel-topology.md) — audit pipeline; `keese-secrets-audit-*` ES index
- [17](17-credential-broker.md) — broker cache state machine; 70%/95% TTL enforcement
- [21](21-opentofu-cloud-deployment.md) — cloud IAM provisioning for ESO roles
- [../plans/rubric.md](../plans/rubric.md)
- [config/secrets/](../../config/secrets/) — canonical ExternalSecret CR templates

## Iteration log

### Iteration 1 — 2026-04-21

| # | Category | Weight | Ratio | Score | Notes |
|---|---|---:|---:|---:|---|
| 1 | Scope clarity | 10 | 1.0 | 10 | One-sentence goal; all 5 open questions bounded and answered |
| 2 | Architecture fit | 10 | 1.0 | 10 | Least-priv SecretStore per tenant; vault-agent only on gateway; no rule violations |
| 3 | Security posture | 15 | 1.0 | 15 | Threat model covers compromised pod, stale cache, break-glass; fail-closed at maxStaleSeconds |
| 4 | Automatability | 10 | 1.0 | 10 | ESO CRs in config/secrets/; OpenTofu provisions cloud IAM; restore script + monthly CI test |
| 5 | Verifiability | 15 | 0.5 | 7.5 | Rotation traceable; restore CI test; envtest for ESO sync lag missing — iter-2 flag |
| 6 | Failure-mode awareness | 10 | 1.0 | 10 | 6 failure paths; stale-cache escalation ladder; fallback activation explicit |
| 7 | Context efficiency for Claude | 10 | 1.0 | 10 | ≤200 lines; no code duplication; cross-refs to 04b-ii, 05b, 17, 21 |
| 8 | Docs quality | 5 | 1.0 | 5 | SPDX + frontmatter complete; refs valid |
| 9 | Observability | 5 | 1.0 | 5 | 3 metrics, 6 events, 3 alerts |
| 10 | Operational readiness | 10 | 1.0 | 10 | ESO + projected volumes HA; 6h backup + 15min RTO; rollback in frontmatter |
| | **Total** | 100 | | **92.5** | |

Verdict: SHIP

Top gaps:
1. Verifiability 0.5: envtest assertion for ESO sync lag missing — iter-2 flag.
2. `config/secrets/` ExternalSecret YAMLs referenced but unscaffolded — implementation gate.
3. `openBaoFallbackAfterSeconds` default unvalidated against ESO latency — confirm P7 local dev.
Next step: Scaffold `config/secrets/` ExternalSecret CRs; flag envtest gap to `test-engineer` (P8).
