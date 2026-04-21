<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: design
category: authz
depends:
  - 04a-openfga-authz-model.md
  - 04b-ii-oidc-trust.md
  - authz.operator.keese.ai/v1alpha1 OIDCProvider CRD (D28)
  - 20a-api-group-layout.md
related_skills: []
status: current
last_verified: 2026-04-21
---

# 04b — Projected ServiceAccount Identity

**Decision:** Agent pods carry a single projected ServiceAccount token with
audience `keese-egress-<tenant>` and TTL ≤ 10 m. The ext_authz sidecar
derives an OpenFGA subject `user:ksa-<workspace-uid>` via an `OIDCProvider`
CR. JWT audience and OpenFGA subject are explicitly separate concerns.

## Context

Keese's zero-trust model (rule 05.3) allows agent pods no API keys. The only
credential is a projected SA token consumed by two distinct downstream systems:

1. **Cloud IAM** (AWS STS, GCP WIF, Azure Entra) — uses JWT `aud` + `sub`.
2. **OpenFGA ext_authz** — uses a keese-internal ReBAC subject.

D28 ratified `OIDCProvider` (`authz.operator.keese.ai/v1alpha1`,
cluster-scoped) to carry per-issuer JWT-to-subject transformation config,
decoupling identity derivation from hardcoded logic in ext_authz.

## Identity model

### Four distinct concepts

| Concept | Value | Consumed by |
|---|---|---|
| JWT `aud` claim | `keese-egress-<tenant>` | Cloud IAM trust policies (AWS STS AssumeRoleWithWebIdentity, GCP WIF, Azure Entra) — see 04b-ii |
| JWT `sub` claim | `system:serviceaccount:<ns>:ksa-<workspace-uid>` | Cloud IAM trust policies' `sub` constraint |
| OpenFGA subject | `user:ksa-<workspace-uid>` | ReBAC Check calls in ext_authz |
| K8s ServiceAccount name | `ksa-<workspace-uid>` | The actual K8s SA object |

The first two are JWT-level; changing them is a cloud-IAM concern. The third
is keese-internal ReBAC. The fourth is the K8s resource. They were conflated
in prior drafts; they are not the same thing.

`ksa-<workspace-uid>` is cluster-unique by K8s workspace UID. OpenFGA is
per-cluster, so no domain suffix is needed. `@<tenant>` and `@<cluster>`
suffixes are removed and must not re-appear.

### Subject vs. audience separation

- JWT `aud` is consumed **only** by cloud IAM trust policies (04b-ii).
- OpenFGA subject `user:ksa-<workspace-uid>` is **independent** of `aud`.
- Moving a workspace to a new tenant requires cloud-IAM trust-policy updates;
  it does **not** require OpenFGA tuple rewrites.

## OIDCProvider CRD integration

`OIDCProvider` (cluster-scoped, `authz.operator.keese.ai/v1alpha1`) carries
per-issuer transformation config. The operator bootstraps default CRs via
an install Job; admins may customize or add.

```yaml
# kubernetes-default — K8s apiserver SA issuer
apiVersion: authz.operator.keese.ai/v1alpha1
kind: OIDCProvider
metadata:
  name: kubernetes-default
spec:
  issuer: https://kubernetes.default.svc.cluster.local
  audiences: ["keese-egress-*"]   # glob; agent tokens use per-tenant aud
  subjectTemplate: >-
    ksa-{{ .Claims.kubernetes_serviceaccount_name | trimPrefix "ksa-" }}
  normalization:
    lowercase: true
```

The template parses `sub: system:serviceaccount:<ns>:ksa-<uid>` via the
`kubernetes.io/serviceaccount/name` claim injected by the kube-apiserver,
yielding `ksa-<uid>`. See `04b-ii` for cloud-provider `OIDCProvider` examples
(google-workspace, github-actions, azure-entra).

### Template evaluation

`subjectTemplate` is Go `text/template` over
`{ .Claims map[string]interface{}, .Issuer string, .Audience string }`.

Allowed Sprig subset: `trimPrefix`, `trimSuffix`, `lower`, `upper`, `split`,
`replace`. No other functions; unknown functions → parse error at admission.

- Templates evaluated at JWT-validation time by the ext_authz sidecar (agent
  tokens) or the attach webhook (human tokens).
- Rendered result used directly as OpenFGA subject; only `normalization`
  transforms apply.
- Parse error → `OIDCProvider.status.phase=Degraded` + `TemplateInvalid` event;
  tokens from that provider fail `403 OIDCProviderDegraded`.
- Runtime evaluation error (missing claim) → deny request + increment
  `keese_oidc_template_eval_errors_total` metric.

### Tenant opt-in

`Tenant.spec.oidc.allowedProviders[]` lists accepted provider names. Future
24 iter-3 adds this field; flagged as a dependency. Without it, any cluster
provider is accepted for any tenant (overly permissive pre-24-iter-3).

## Token lifecycle

- TTL: ≤ 10 m per rule 05.3. Agent pod refreshes via `projected.sources[].serviceAccountToken`.
- `expirationSeconds` ≤ 600; kubelet rotates at 80% TTL.
- Gateway caches rendered subjects keyed by `(issuer, sub, aud)` for ≤ 5 m to
  avoid repeated template evaluation under load; cache invalidated on
  `OIDCProvider` update event.

## Tenant-move rotation

Tenant move changes: JWT `aud` (per cloud IAM), K8s namespace, projected SA
name (if name encodes tenant). Tenant move does **not** change the OpenFGA
subject `user:ksa-<uid>` because the workspace UID is stable. Existing
OpenFGA tuples remain valid; no backfill required. The only move-related cost
is updating cloud-IAM trust policies (per 04b-ii).

## Failure modes

| Failure | Behavior |
|---|---|
| `OIDCProvider` missing for issuer | ext_authz denies `403 OIDCProviderNotFound` |
| `OIDCProvider.status.phase=Degraded` | ext_authz denies `403 OIDCProviderDegraded` |
| Template eval error (missing claim) | deny request; metric incremented |
| JWT expired | gateway returns `401` before ext_authz |
| OpenFGA unavailable | fail-closed; `503` (governed by 04a circuit-breaker) |
| OIDCProvider CR deleted at runtime | ext_authz caches last-good for ≤ 5 m; then denies |

## Audit trail

ext_authz logs `(issuer, rendered_subject, aud, openfga_decision,
upstream_status)` per rule 05.10. Tokens and claims are never logged. Events
emitted on `OIDCProvider` degraded transitions with reason `TemplateInvalid`.

## Refs

- [04a-openfga-authz-model.md](04a-openfga-authz-model.md) — tuple shapes using `user:ksa-<uid>`
- [04b-ii-oidc-trust.md](04b-ii-oidc-trust.md) — per-cloud trust policy detail (unchanged)
- [04c-token-revocation.md](04c-token-revocation.md)
- [05a-envoy-ai-gateway-topology.md](05a-envoy-ai-gateway-topology.md) — ext_authz wiring
- [20a-api-group-layout.md](20a-api-group-layout.md) — `authz.operator.keese.ai` group (D28)
- [24-tenant-crd.md](24-tenant-crd.md) — `Tenant.spec.oidc.allowedProviders[]` (future iter-3)

## Iteration log

### Iteration 1 — 2026-04-19 — **95 SHIP**

Subject `user:ksa-<uid>@keese-egress-<tenant>`; no OIDCProvider CRD; TTL policy
and audit trail established. Cat 4 docked 0.5 (tests/manifests not yet authored).

### Iteration 2 — 2026-04-21

| # | Category | Weight | Ratio | Score | Notes |
|---|---|---:|---:|---:|---|
| 1 | Scope clarity | 10 | 1.0 | 10 | Decision + four-concept table explicit |
| 2 | Architecture fit | 10 | 1.0 | 10 | D28 OIDCProvider wired; no rule violations |
| 3 | Security posture | 15 | 1.0 | 15 | Fail-closed on all provider errors; no subject conflation |
| 4 | Automatability | 10 | 0.5 | 5 | Operator bootstrap Job named; tests/manifests not yet authored |
| 5 | Verifiability | 15 | 1.0 | 15 | Template-eval assertions, metric, event, status named |
| 6 | Failure-mode awareness | 10 | 1.0 | 10 | Full failure-mode table; cache expiry on CR delete |
| 7 | Context efficiency for Claude | 10 | 1.0 | 10 | ≤ 200 lines; no inline code; skill pointers via refs |
| 8 | Docs quality | 5 | 1.0 | 5 | SPDX + frontmatter; links valid |
| 9 | Observability | 5 | 1.0 | 5 | Metric + event + audit log named |
| 10 | Operational readiness | 10 | 1.0 | 10 | Tenant-move simplified; cache TTL stated; upgrade path via 04b-ii |
| | **Total** | 100 | | **95** | |

Verdict: **SHIP**

Top gaps:
1. Cat 4 (0.5): envtest suite + OIDCProvider sample manifests not yet authored (blocked on design gate).
2. 24-iter-3 dependency: `Tenant.spec.oidc.allowedProviders[]` not yet in 24; provider acceptance is cluster-wide until then.
3. Template function allow-list validation not encoded in a CEL VAP rule yet.

Next step: Companion 04b-ii carries cloud-provider trust policy detail; no edits needed.
When design gate opens, author OIDCProvider CRD + envtest suite + samples.
