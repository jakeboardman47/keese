<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: design
category: egress
depends:
  - 04a-openfga-authz-model.md
  - 04b-projected-sa-identity.md
  - 04b-ii-oidc-trust.md
  - 04c-token-revocation.md
  - 05a-envoy-ai-gateway-topology.md
  - 10a-otel-topology.md
  - 11-secrets-pluggable-vault.md
  - 17-credential-broker.md
  - 24-tenant-crd.md
related_skills: []
status: current
last_verified: 2026-04-20
rollback: |
  If a BSP pattern causes egress regression: revert the Envoy AI Gateway chart pin
  via helmfile.lock (helmfile diff to confirm); patch affected BSPs with the prior
  apiKey.secretRef or oidc stanza using kubectl apply; if K8s Secrets are stale,
  restart the ESO controller pod to force re-sync. Document the incident in
  docs/plans/migration-bsp-<incident>.md before executing rollback in production.
---

# 05b — Credential Injection Patterns

Full YAML examples per credential type: [05b-ii-bsp-examples.md](05b-ii-bsp-examples.md).
Iteration log: [05b-ii-bsp-examples.md#iteration-log](05b-ii-bsp-examples.md#iteration-log).

## Context

Agent pods carry only a projected SA token (04b). Envoy AI Gateway terminates that
token, ext_authz checks OpenFGA (04a), then selects a `BackendSecurityPolicy` (BSP) to
inject the upstream credential. The three-table decomposition (D13) separates routing
from credential source from authz decision. This doc answers the five open questions
from the draft stub. 17 owns the broker state machine; 11 owns vault plumbing.

## Three-table decomposition

```
AIGatewayRoute / MCPRoute
        │  targetRef → Backend (upstream URL)
        ▼
BackendSecurityPolicy  ←── credential type + source ref
        │
        ▼ OpenFGA Check (≤ 25 ms)
credential:C#can_use@service_account:SA  →  allow / deny
```

The credential broker reconciler (17) writes `credential:C#bound_to@workspace:W` when a
BSP is bound. ext_authz (05a step 6) checks `credential:C#can_use@SA` after the
`tool#can_call` check. Both checks fail-closed; failing either → 403.

## Credential type patterns

### Static API key (Anthropic, OpenAI)

OpenBao path: `kv/keese/tenants/<tenant>/credentials/<provider>/<key-name>`.
ExternalSecret in `keese-credentials` namespace; `RefreshInterval: 5m`; targets
K8s Secret `keese-cred-<tenant>-<provider>`. BSP field: `spec.apiKey.secretRef`.
`ReferenceGrant` from `keese-credentials` → gateway namespace required; absent →
`MissingReferenceGrant` event + BSP `Ready=False`.
Full ExternalSecret + BSP YAML: [05b-ii](05b-ii-bsp-examples.md#static-api-key).

### AWS OIDC STS (Bedrock)

Envoy calls `sts:AssumeRoleWithWebIdentity` inline using the agent SA token.
BSP fields: `spec.oidc.providerUrl`, `spec.oidc.roleArn`, `spec.oidc.tokenExchangeServiceAccounts[]`.
Per-tenant IAM role trust policy constrains both `aud=keese-egress-<tenant>` and `sub` (04b-ii).
No K8s Secret required. Cache keyed by `(keese-egress-<tenant>, roleArn)`.
Full YAML: [05b-ii](05b-ii-bsp-examples.md#aws-oidc-sts).

### GCP Workload Identity Federation (Vertex AI)

Envoy exchanges via `sts.googleapis.com/v1/token` then impersonates the GCP SA.
BSP fields: `spec.gcpOidc.workloadIdentityPool`, `.provider`, `.serviceAccountEmail`.
Full YAML: [05b-ii](05b-ii-bsp-examples.md#gcp-wif).

### Azure Entra OIDC (Azure OpenAI)

BSP fields: `spec.azureOidc.tenantId`, `.clientId`, `.federatedIdentityCredentialRef`.
Federated credential per workspace SA provisioned by `deploy/opentofu/azure/identity.tf` (04b-ii).
Full YAML: [05b-ii](05b-ii-bsp-examples.md#azure-entra).

### Credential pooling (multi-key round-robin)

BSP uses `spec.pool[]{selection: round-robin|least-used|spillover; members[]{apiKey.secretRef}}`.
`spillover` promotes on 429. Broker (17) tracks state in NATS KV `keese-pool-state/<bsp-uid>`.
Open for 17 iter-1: state machine; `least-used` persistence across gateway pod restarts.
Full pool YAML: [05b-ii](05b-ii-bsp-examples.md#credential-pooling).

## Rotation drain semantics (D13 70%/95%)

ExternalSecret updates the K8s Secret; BSP reconciler (17) annotates
`status.credentialVersion=<new-hash>`. Gateway pod cache refreshes at 70% TTL.

Drain guarantee: in-flight requests finish with the OLD credential (Envoy connection
pool does not interrupt active streams); NEW requests use the NEW credential on cache
refresh. No request dropped.

**Worst-case formula:**
```
oldest_cred_usable_until = max(remaining_old_TTL, 0.70 × new_TTL)
```

Example: old TTL 120 s remaining; new TTL 3600 s → max(120, 2520) = 2520 s. The broker
(17) MUST NOT revoke the old credential before this window elapses.

**Failure — rotation AND old TTL both expire:** NATS KV version mismatch (04c) causes
cache flush with no valid credential → fail-closed (rule 05.6). Envoy returns 401 +
`x-keese-rotation-expired: true`. BSP reconciler emits `CredentialRotationStale`;
alert `CredentialRotationLate` fires if gap > 30 s p95. Recovery: ESO force-sync via
annotation on the ExternalSecret CR.

## BSP precedence (workspace > tenant > cluster default)

1. **Exact workspace match:** `Workspace.spec.backendPolicyRefs[]` referencing a BSP
   for the upstream host. Tightest scope; written by workspace admin.
2. **Tenant default:** BSPs referenced by `Tenant.spec.credentialPoolRef`, resolved by
   the credential broker to a pool for this tenant.
3. **Cluster default:** operator-managed BSPs in `keese-system`. Requires namespace
   annotation `keese.ai/allow-cluster-credential=true`. Not recommended for production;
   explicit opt-in only.
4. **No match → deny:** ext_authz returns 403 `{"error":"NoBackendCredential",
   "upstream":"<host>"}`. No implicit allow without a BSP.

Resolution uses the `x-keese-tenant` header (05a) and `Workspace.status.saAudience` at
ext_authz decision time. Workspace admin cannot escalate above their own scope.

## Non-AI upstreams (GitHub PAT, Jira, database DSNs)

Where the upstream supports OIDC (GitHub): use BSP `spec.oidc` pointing to
`token.actions.githubusercontent.com`; BSP `spec.oidc.permissionsPolicy` for scope.

Where OIDC is impossible (Jira basic auth, PostgreSQL DSN): a **vault-agent sidecar on
the gateway pod** mounts the credential at `/var/run/keese/upstream-creds/<name>` via
`projected.sources[].secret`. Envoy reads via `file_based_plugin`; no credential
reaches the agent pod (rules 05.1, 05.7). The sidecar is co-deployed with the gateway
pod via operator-managed `PodTemplateSpec` injection — NOT on agent pods.

`envFrom.secretRef` and `env.valueFrom.secretKeyRef` are forbidden on all
keese-managed pods (rule 05.7).

## Trade-offs

Static key in agent pod env: rejected (rules 05.1, 05.2). In-process credential cache
on agent: rejected (rule 05.3; 04b forbids it). Per-workspace BSP only: rejected; tenant
defaults reduce sprawl at scale. Vault-agent on agent pod: rejected; gateway pod is the
credential trust boundary. OIDC STS preferred over stored keys wherever supported.

## Failure modes

| Failure | Mitigation |
|---|---|
| BSP `Ready=False` | `CredentialExchangeFailed` event; workspace `Degraded` |
| `MissingReferenceGrant` | `ReferenceGrantMissing` event; operator creates grant |
| ExternalSecret sync lag | `CredentialRotationStale`; ESO force-sync annotation |
| No BSP match | ext_authz 403 `NoBackendCredential`; no upstream call |
| Pool member 429 (`spillover`) | `PoolMemberExhausted`; promote to next member |
| Vault-agent sidecar crash | PDB ensures 1 healthy pod; empty file mount → deny |
| OIDC STS timeout | Cached cred until 95% TTL; then fail-closed 401 |
| Rotation + old TTL expire | 401 `x-keese-rotation-expired`; `CredentialRotationLate` alert |

## Upgrade / rollback

Pin Envoy AI Gateway chart in `helmfile.lock`. On BSP CRD schema change: apply new CRD
→ migrate BSPs via `migrations/bsp-migrate-<version>.yaml` → patch chart pin → run
`make bootstrap-infra`. Rollback: revert pin → reapply prior BSP stanzas from Git.
Document in `docs/plans/migration-bsp-<incident>.md`.

## Observability

OTEL span `keese.rebac.check` (04a) gains fields `bsp`, `upstream_role`,
`exchange_result` (ok | sts_error | timeout | no_match) — flagged for 10a iter-2.

Metrics: `keese_bsp_exchange_total{tenant,provider,result}`,
`keese_bsp_pool_member_429_total{tenant,bsp,member}`,
`keese_credential_rotation_latency_seconds{tenant,provider}`.

Events: `CredentialExchangeFailed`, `CredentialRotationStale`, `CredentialRotationLate`,
`ReferenceGrantMissing`, `PoolMemberExhausted`, `NoBackendCredential`.

## Refs

- [04a](04a-openfga-authz-model.md) — `credential#can_use` tuple; audit event shape
- [04b](04b-projected-sa-identity.md) · [04b-ii](04b-ii-oidc-trust.md) — SA token; OIDC trust per cloud
- [04c](04c-token-revocation.md) — NATS KV version watch; cache flush; SLO
- [05a](05a-envoy-ai-gateway-topology.md) — ext_authz flow; `x-keese-tenant`; decision steps
- [05b-ii-bsp-examples.md](05b-ii-bsp-examples.md) — full YAML + iteration log
- [10a](10a-otel-topology.md) — OTEL pipeline; flagged: `bsp`, `exchange_result` span fields
- [11](11-secrets-pluggable-vault.md) — OpenBao paths; ESO bridge (stub; cross-ref only)
- [17](17-credential-broker.md) — broker state machine; pool selection; rotation enforcement (stub)
- [24](24-tenant-crd.md) — `spec.credentialPoolRef`; Mode A / Mode B
- [../plans/rubric.md](../plans/rubric.md)
