<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: design
category: authz
depends: [01-tenancy-capsule.md, 04a-openfga-authz-model.md]
related_skills: []
status: current
last_verified: 2026-04-20
rollback: |
  Audience template change: revert EGRESS_SA_AUDIENCE_TEMPLATE on the operator
  Deployment; trigger a rolling restart so Workspace controllers re-project tokens
  with the old audience; patch projected volume TTL to 1s on affected pods (forces
  kubelet reissue); document in docs/plans/migration-sa-<slug>.md. Gateway-side:
  redeploy BackendSecurityPolicy CRs referencing the old audience; cache entries
  expire at next TTL tick (≤ 10 min). Cloud IAM trust policy reverts must be
  completed before the old audience's last token expires; the migration doc must
  include the IAM change set and the 15-minute propagation window.
---

# 04b — Projected ServiceAccount Identity

## Context

Agent pods carry no long-lived secrets. Their sole credential is a Kubernetes
projected ServiceAccount (SA) token issued by the cluster OIDC token issuer,
scoped to a per-tenant audience (`keese-egress-<tenant>`), with a short TTL. The
Envoy AI Gateway terminates this token and, via `BackendSecurityPolicy`, exchanges
it for an upstream cloud IAM credential (AWS Bedrock via STS OIDC, GCP Vertex AI
via Workload Identity Federation, Azure OpenAI via Entra Federated Identity) or
injects a static key from OpenBao. Agent identity for audit, OpenFGA authz, and
D24/D25 durable resume is the Workspace UID + namespace + tenant tuple — NOT the
JWT `jti`. Pod churn and token rotation do not create new identities.

## Identity Model

**Durable agent identity (D24):** `(workspace-uid, namespace, tenant)` — stable across
pod churn, token rotation, and SIGKILL restarts. Token rotation is NOT a new identity.

**OpenFGA user subject string** (consumed by 04a — publish this exact value):

```
user:ksa-<workspace-uid>@keese-egress-<tenant>
```

Example: `user:ksa-d3c9f21a-87bb-4e01-a93e-b2f1e9dc01f4@keese-egress-acme-corp`

The `ksa-` prefix disambiguates from human users. The `@keese-egress-<tenant>`
suffix scopes tuples within the OpenFGA store.

**OIDC `sub` claim** (for cloud IAM trust policies):
`system:serviceaccount:<namespace>:keese-ws-<workspace-name>`
(≤ 63 chars; 8-char stable hash suffix if truncated). Cloud trust policies MUST
constrain on both `sub` AND `aud` to prevent cross-tenant privilege escalation.

**D25 resume invariant:** After SIGKILL/restart: same audience, same subject shape, no rekeying.

## Audience Template

Resolved at Workspace reconcile time from operator env var
`EGRESS_SA_AUDIENCE_TEMPLATE=keese-egress-{{.Tenant}}`. `{{.Tenant}}` resolves
to the Capsule Tenant name (Mode B) or `Workspace.spec.tenantRef.name` (Mode A);
stored read-only on `Workspace.status.saAudience`.

Projected volume: `serviceAccountToken.audience=keese-egress-<tenant>`,
`expirationSeconds=600`, mounted read-only at `/var/run/keese/identity/token`.
No other credential files share this path (K8s Secrets mount at
`/var/run/keese/secrets/<name>` per rule 05.7).

## TTL + Refresh

Default TTL: **600 s (10 min)** — aligned with rule 05.3. Refresh at **70% TTL
(420 s)** — consistent with D13 gateway cache. The kubelet handles rotation and
atomically replaces the token file (POSIX rename; no partial-read race). Agent
processes MUST re-read the file before each gateway call, not cache in memory.

| Tenant tier | TTL | Refresh point |
|---|---|---|
| `standard` | 600 s | 420 s |
| `restricted` | 300 s | 210 s |
| `extended` (future, ADR required) | 900 s | 630 s |

`Workspace.spec.tokenTTL` is an optional override; admission VAP enforces the
tier ceiling. Effective TTL stored on `Workspace.status.saTokenTTL`.

**Who refreshes:** Kubelet only. Gateway-pod upstream credential rotation (D13) is
independent and also anchored at 70% TTL. The two refresh clocks are not
synchronized; gateway-side cache miss on expired agent token triggers a fresh OIDC
exchange without dropping the request.

## OIDC Trust Anchoring Per Cloud

Full detail in [04b-ii-oidc-trust.md](04b-ii-oidc-trust.md).

**Issuer:** K8s API server (`/.well-known/openid-configuration`, JWKS at
`/openid/v1/jwks`). Private-cluster operators must expose JWKS via a stable
public endpoint. Operator emits startup warning if `OIDC_ISSUER_URL` is unset
and apiserver URL is a private CIDR.

All three clouds require trust policies constraining BOTH `aud = keese-egress-<tenant>`
AND `sub = system:serviceaccount:<ns>:keese-ws-<name>` to prevent cross-tenant
privilege escalation. OpenTofu modules at `deploy/opentofu/{aws,gcp,azure}/`
provision the OIDC provider and per-tenant IAM role/binding.

**Static API keys (Anthropic, OpenAI):** Not exchanged via OIDC. Agent pods never
see these. Gateway injects from OpenBao via ExternalSecrets → K8s Secret →
`BackendSecurityPolicy.spec.basic`. See 05b.

## Tenant-Move Rotation Contract

Direct `tenantRef` updates on running Workspaces are blocked by admission webhook
(`phase != Terminating`). Tenant moves require Workspace delete + recreate.
On deletion: controller revokes SA; removes OpenFGA tuple
`user:ksa-<uid>@keese-egress-<old-tenant>` within SIGTERM drain window (≤ 60 s).
On creation in new tenant: new SA, new `workspace-uid`, new tuple for new audience.
D24 note: Workspace UID changes on recreate; logical continuity tracked via
`resume.stateRef`, not the token. Old token valid until remaining TTL (≤ 10 min);
event `WorkspaceTenantMoved` includes TTL expiry timestamp.

## Audit Trail

No token bytes, no JWT header/payload beyond named claims (rule 05.10).

Span `keese.workspace.sa_reconcile` (issuance): `keese.identity.subject`,
`.audience`, `.workspace_uid`, `.namespace`, `.ttl_seconds`, `.event=token_projected`.

Span `keese.gateway.authz_check` (use): `keese.authz.subject`, `.audience`,
`.upstream_role`, `.decision` (allow/deny), `.upstream_status`, `.host`.

ES index: `keese-authz-<YYYY.MM.DD>`; 90-day retention; ILM: `dev/bootstrap/elasticsearch/ilm-authz.json`.

## Trade-offs

Per-workspace SA (chosen): tightest trust-policy scoping; SA name encodes workspace;
clean audit trail. Shared tenant SA rejected: one compromised pod impersonates any
workspace in the tenant. TTL = 10 min (rule 05.3 ceiling; kubelet rotation
automatic); 1-hour TTL rejected (blast-radius window). Tenant-move via in-place
update rejected: audience mid-flight risks stale gateway cache; delete+recreate
is safer and aligns with D24 identity semantics.

## Failure Modes

| Failure | Detection | Mitigation |
|---|---|---|
| Kubelet token rotation failure | Expired token → gateway 401 | `IdentityTokenExpired` event; controller re-projects |
| JWKS endpoint unreachable | Gateway 401 | 5-min JWKS cache (fail-open window); then fail-closed; alert `JWKSFetchFailed` |
| Cloud IAM misconfigured | STS/WIF/Entra 403 → gateway 502 | `BackendSecurityPolicy Ready=False`; `CredentialExchangeFailed`; workspace `Degraded` |
| SA name truncation collision | Duplicate SA name | Controller checks existence; `SANameCollision` event; 4-char hash disambiguator |
| SIGKILL mid-token-write | — | Kubelet uses POSIX rename; partial read is impossible |
| Tenant-move TTL race | Old token still valid during window | Accepted per contract; platform may revoke IAM binding immediately |

## Observability

- Metrics: `keese_identity_token_projected_total{tenant,namespace}`,
  `keese_identity_token_ttl_seconds{tenant,tier}`,
  `keese_gateway_authz_decision_total{decision,upstream,tenant}`,
  `keese_gateway_jwks_fetch_failures_total{issuer}`.
- Events: `TokenProjected`, `IdentityTokenExpired`, `JWKSFetchFailed`,
  `CredentialExchangeFailed`, `SANameCollision`, `WorkspaceTenantMoved`.
- Alert: `keese_gateway_jwks_fetch_failures_total > 0 for 5m` → P2.

## Refs

- [../plans/scaffolding-plan.md](../plans/scaffolding-plan.md) — D13, D24, D25
- [04a-openfga-authz-model.md](04a-openfga-authz-model.md) — consumes subject string
- [04b-ii-oidc-trust.md](04b-ii-oidc-trust.md) — cloud trust-policy detail
- [04c-token-revocation.md](04c-token-revocation.md) — consumes TTL + audience
- [05b-credential-injection-patterns.md](05b-credential-injection-patterns.md) — gateway
- [01-tenancy-capsule.md](01-tenancy-capsule.md) — tenant name derivation
- [23-agent-supervision.md](23-agent-supervision.md) — witness SA pattern (open)
- [17-credential-broker.md](17-credential-broker.md) — gateway cache contract
- [../plans/rubric.md](../plans/rubric.md)

## Iteration log

### Iteration 1 — 2026-04-20

| # | Category | Weight | Ratio | Score | Notes |
|---|---|---:|---:|---:|---|
| 1 | Scope clarity | 10 | 1.0 | 10 | Identity model, audience, TTL, trust, tenant-move, audit all bounded. |
| 2 | Architecture fit | 10 | 1.0 | 10 | D13/D24/D25/rule 05 honored; split to 04b-ii per 200-line rule. |
| 3 | Security posture | 15 | 1.0 | 15 | Dual constraint (sub+aud); no env-var secrets; fail-closed TTL; POSIX atomicity. |
| 4 | Automatability | 10 | 0.5 | 5 | Audience template env var + OpenTofu paths stated; modules pre-gate TBD. |
| 5 | Verifiability | 15 | 1.0 | 15 | Six failure modes; SIGKILL resume; D25 test obligation. |
| 6 | Failure-mode awareness | 10 | 1.0 | 10 | Collision, JWKS window, TTL race enumerated. |
| 7 | Context efficiency for Claude | 10 | 1.0 | 10 | Split at ceiling; cloud detail in 04b-ii. |
| 8 | Docs quality | 5 | 1.0 | 5 | SPDX; frontmatter complete; rollback specific. |
| 9 | Observability | 5 | 1.0 | 5 | OTEL spans, metrics, events, ES index, alert named. |
| 10 | Operational readiness | 10 | 1.0 | 10 | Rollback concrete; IAM propagation window; migration doc requirement. |
| | **Total** | 100 | | **95** | |

Verdict: SHIP (95 ≥ 90)

Top gaps: (1) OpenTofu modules not yet authored — pre-gate acceptable. (2) Private-cluster
JWKS proxy optional/deferred — air-gapped operators need this before production.
(3) Witness SA audience pattern unresolved in 23-agent-supervision.md Q4.

Next step: Publish subject string to 04a; publish TTL=600s/refresh=420s to 04c;
confirm 05b reads token from `/var/run/keese/identity/token` per request.
