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
  - 24-tenant-crd.md          # iter-3: add openBaoFallbackAfterSeconds to Tenant spec schema
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

Canonical ExternalSecret YAMLs and iteration log: [11-ii-examples.md](11-ii-examples.md).

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
cloud KMS fallback requires explicit tenant opt-in via `Tenant.spec.secrets`.

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
- `ClusterSecretStore keese-openbao` — cluster-scoped; operator-level paths only.
- `SecretStore keese-openbao-<tenant>` — per tenant namespace; cannot read other tenants.
- `ExternalSecret` per credential; `RefreshInterval: 5m` default, `1m` for high-rotation keys.
- Canonical CRs: [11-ii-examples.md](11-ii-examples.md); files under `config/secrets/`.

## Q2 — Rotation flow (zero pod restart)

Admin writes new OpenBao version → ESO detects within 5 min → K8s Secret updated
(`keese.ai/version=<openbao-version>`) → kubelet projects to gateway pod volume within 1 min
→ broker (17) refreshes Envoy cache (D13 70%/95% TTL). Static keys: ≤ 6 min total lag.
Worst-case formula (05b): `max(remaining_old_TTL, 0.70 × new_TTL)`.

## Q3 — vault-agent sidecar: canonical upstream list

**Allowed only on gateway pods.** Prohibited on agent pods (rules 05.1, 05.2) and
operator/controller pods. Sidecar reads from `vault.keese-system.svc:8200` via SA-token
auth; mounts credentials to `/var/run/keese/upstream-creds/<name>`.

**Hard rule:** any pod labeled `app.kubernetes.io/part-of: agent-runtime` carrying a
`vault-agent` container → VAP admission reject with reason `VaultAgentOnAgentPodForbidden`.
Even for file-mount upstreams, the gateway terminates the client's request and proxies
to the upstream with the file-mounted credential; the agent pod never sees the credential.

| Upstream | Credential format | Why sidecar (not BSP) |
|---|---|---|
| Jira (on-prem) | Basic auth JSON in `/var/run/keese/upstream-creds/jira-<name>/creds.json` | Atlassian Server/DC Kerberos delegation or token-in-file auth |
| PostgreSQL (direct) | DSN string in `/var/run/keese/upstream-creds/postgres-<name>/dsn` | libpq reads `PGSERVICEFILE` or DSN file; not an HTTP target for BSP |
| Redis (password auth) | Password in `/var/run/keese/upstream-creds/redis-<name>/password` | go-redis/redigo read file-mounted password; not an HTTP target for BSP |
| gRPC with client cert | TLS cert pair in `/var/run/keese/upstream-creds/<name>/{cert.pem,key.pem}` | gRPC mutual-TLS requires file paths in code config |
| SFTP / SSH | Private key in `/var/run/keese/upstream-creds/ssh-<name>/id_ed25519` | SSH agent protocol requires file-mount |

All other upstreams (Anthropic, OpenAI, Gemini, Bedrock, Azure OpenAI, GitHub API,
Slack, PagerDuty, etc.) use HTTP BSP — no sidecar needed.

## Q4 — Failover when OpenBao unavailable

### Per-tenant `openBaoFallbackAfterSeconds`

Field: `Tenant.spec.secrets.openBaoFallbackAfterSeconds: int`. VAP bounds [60, 3600];
default 300 (5 min). Tenants with sensitive workloads tighten; tenants tolerant of
OpenBao hiccups loosen. Flag for **24 iter-3**: add this field to the Tenant CRD spec
schema alongside `openBaoFallbackProvider` and `maxStaleSeconds`.

Semantics: after N seconds of OpenBao unreachability, ESO swaps `SecretStore` to
`Tenant.spec.secrets.openBaoFallbackProvider` (`aws|gcp|azure|none`). If `none` (no
cloud KMS in this deployment), continue serving last-known-good K8s Secret until
`maxStaleSeconds` fail-closed threshold.

### Out-of-the-box deployments

- **Self-hosted / on-prem:** `openBaoFallbackProvider: none` only valid choice. OpenBao is sole
  secret source; `maxStaleSeconds` is the fail-closed backstop.
- **EKS / GKE / AKS:** cloud KMS available. Tenants opt in via `openBaoFallbackProvider: aws|gcp|azure`.
- **Hybrid:** OpenBao primary + cloud KMS fallback. Recommended where HA credential availability
  must exceed OpenBao's own SLA.

### Default failover flow

1. OpenBao unreachable → ESO pollers fail → K8s Secrets not updated (last-known-good).
2. Projected Secret in gateway pods unchanged; Envoy cache continues serving.
3. When `keese.ai/version` annotation age > `maxStaleSeconds` (default 86400 s / 24 h):
   ESO emits `SecretStale` event; gateway controller patches BSP to `Ready=False`;
   new requests receive 503 `SecretStaleFailClosed`.
4. Alert `SecretStale` fires at 50% of `maxStaleSeconds`; `SecretStaleFailClosed` at 95%.

Cloud KMS fallback: when `openBaoFallbackProvider` is set and OpenBao unreachable >
`openBaoFallbackAfterSeconds`, ESO swaps `SecretStore`; emits `OpenBaoFallbackActive`;
restores on recovery.

## Q5 — Cloud KMS authentication

| Cloud | Mechanism | K8s SA annotation |
|---|---|---|
| AWS Secrets Manager | IRSA (OIDC STS) | `eks.amazonaws.com/role-arn: arn:aws:iam::<acct>:role/keese-eso-<tenant>` |
| GCP Secret Manager | Workload Identity Federation | `iam.gke.io/gcp-service-account: keese-eso-<tenant>@<proj>.iam.gserviceaccount.com` |
| Azure Key Vault | Workload Identity (Federated) | `azure.workload.identity/client-id: <managed-identity-client-id>` |

Trust policy detail in [04b-ii](04b-ii-oidc-trust.md) — unchanged.
Cloud IAM roles provisioned by `deploy/opentofu/{aws,gcp,azure}/secrets.tf` (21).

## Operational concerns

- **Backup:** 6 h auto-snapshot → object storage; 30-day retention; restore tested monthly in CI; ≤ 15 min RTO.
- **Audit:** OpenBao audit log → OTEL Tier 2 (10a) → ES `keese-secrets-audit-*` (30-day) + Loki (≥ 1 year).
  Format: `(path, operation, sa, decision, error_code)`. No secret values (rule 02).
- **Break-glass:** namespace label `keese.ai/break-glass=true` + annotation `keese.ai/unsafe-vault-bypass=true`
  (rule 05.13); `UnsafeAnnotationAllowed` event + `MEMORY.md` entry with approver from `kubectl auth whoami`.

## Trade-offs

| Option | Decision | Reason |
|---|---|---|
| vault-agent on agent pods | Rejected | Rules 05.1, 05.2 — no upstream creds on agent pods |
| envFrom.secretRef on any pod | Rejected | Rule 05.7 — secrets as projected files only |
| Fail-open indefinitely on stale cache | Rejected | Allows expired credentials; rule 05.6 |
| Cloud KMS fallback always on | Rejected | Increases IAM attack surface; tenant opt-in only |
| ESO ClusterSecretStore for tenant paths | Rejected | Violates least privilege; tenant SecretStore per namespace |
| `openBaoFallbackAfterSeconds` < 60 s | Rejected | ESO poll latency makes sub-60s detection unreliable |

## Failure modes

| Failure | Detection | Mitigation |
|---|---|---|
| OpenBao unreachable | ESO sync error; `VaultUnavailable` event | Last-known-good K8s Secret; fallback opt-in |
| Secret stale > `maxStaleSeconds` | `SecretStaleFailClosed` event | BSP `Ready=False`; 503 new requests |
| ESO sync lag (version drift) | `keese.ai/version` annotation age | Force-sync annotation; `CredentialRotationStale` |
| vault-agent sidecar crash | Empty file mount | Envoy deny (empty cred); PDB ensures 1 healthy pod |
| Cloud KMS fallback auth fail | ESO provider error | Remain on stale K8s Secret; alert fires |
| Break-glass misuse | `UnsafeAnnotationAllowed` event | Audit log + `MEMORY.md` entry; ops review |
| VaultAgentOnAgentPodForbidden | VAP admission reject | Fix pod template; remove vault-agent container |

## Observability

Metrics (OTEL Tier 2 / Prometheus):
- `keese_secret_stale_seconds{tenant,provider}` — age of K8s Secret vs. OpenBao version.
- `keese_eso_sync_failures_total{tenant,store,provider}` — ESO sync error count.
- `keese_openbao_fallback_active{tenant,cloud}` — gauge 1 when fallback is active.

Events: `SecretStale`, `SecretStaleFailClosed`, `OpenBaoFallbackActive`,
`OpenBaoFallbackRecovered`, `UnsafeAnnotationAllowed`, `VaultUnavailable`,
`VaultAgentOnAgentPodForbidden`.

Alerts: `SecretStale` at 50% `maxStaleSeconds` (P2); `SecretStaleFailClosed` at 95% (P1);
`ESOSyncFailure` sustained 15 min (P2).

## Refs

- [04b-ii](04b-ii-oidc-trust.md) — cloud OIDC trust per provider (IRSA / WIF / Azure)
- [05b](05b-credential-injection-patterns.md) — BSP credential types; rotation drain formula
- [10a](10a-otel-topology.md) — audit pipeline; `keese-secrets-audit-*` ES index
- [11-ii-examples.md](11-ii-examples.md) — canonical ExternalSecret YAMLs + iteration log
- [17](17-credential-broker.md) — broker cache state machine; 70%/95% TTL enforcement
- [21](21-opentofu-cloud-deployment.md) — cloud IAM provisioning for ESO roles
- [24](24-tenant-crd.md) — `Tenant.spec.secrets.*`; iter-3 flag for openBaoFallbackAfterSeconds
- [config/secrets/](../../config/secrets/) — ExternalSecret CR templates
