<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: spec
category: contract
depends:
  - ../designs/04b-projected-sa-identity.md
  - ../designs/04b-ii-oidc-trust.md
  - ../designs/04a-openfga-authz-model.md
related_skills: [crd-authoring, controller-authoring]
status: current
last_verified: 2026-04-21
regression_lock: false
tests:
  unit:
    - internal/controller/authz/oidcprovider/template_test.go
    - internal/controller/authz/oidcprovider/sprig_allowlist_test.go
    - internal/controller/authz/oidcprovider/subject_render_test.go
  envtest:
    - test/envtest/authz/oidcprovider_suite_test.go
    - test/envtest/authz/oidcprovider_degraded_test.go
    - test/envtest/authz/oidcprovider_deleted_runtime_test.go
  kuttl:
    - test/e2e/authz/oidcprovider_three_token_projection_test.go
    - test/e2e/authz/oidcprovider_jwks_unreachable_test.go
metrics:
  - keese_oidc_template_eval_errors_total{provider,template,reason}
  - keese_oidc_audience_template_eval_total{provider,template,result}
  - keese_oidc_token_rotation_seconds{provider,template}
  - keese_gateway_jwks_fetch_failures_total{provider}
  - keese_oidc_cache_invalidations_total{provider,trigger}
events:
  - TemplateInvalid
  - AudienceTemplateEvalError
  - MissingWorkflowAudience
  - OIDCProviderMissing
  - OIDCProviderDegraded
  - JWKSUnreachable
  - CacheFlushComplete
---

# authz.keese.ai v1alpha1 — spec

Iteration log: [authz.keese.ai-v1alpha1-ii-iter-log.md](authz.keese.ai-v1alpha1-ii-iter-log.md).

## Owning designs (all `status: current`)

- [`04b-projected-sa-identity.md`](../designs/04b-projected-sa-identity.md) iter-3 — three `audienceTemplates`; token-mint flow
- [`04b-ii-oidc-trust.md`](../designs/04b-ii-oidc-trust.md) — per-cloud JWKS anchoring; only `egress` audience reaches cloud IAM
- [`04a-openfga-authz-model.md`](../designs/04a-openfga-authz-model.md) — `subjectTemplate` output is the OpenFGA `service_account:` or `user:` subject

## 1. Kind: OIDCProvider

Group: `authz.keese.ai/v1alpha1`. Cluster-scoped. SSA fieldOwner: `keese-oidcprovider-controller`.

### 1.1 Spec fields

| Field | Type | Required | Description |
|---|---|---|---|
| `issuer` | string (URL) | yes | OIDC issuer URL; JWKS auto-derived as `issuer/.well-known/openid-configuration` unless `jwksUri` set |
| `jwksUri` | string (URL) | no | Explicit JWKS endpoint override (air-gapped / Dex / Pinniped) |
| `audiences[]` | []string (glob) | yes | Glob patterns accepted for `aud` claim validation; e.g. `keese-egress-*` |
| `subjectTemplate` | string | yes | Go template over `{.Claims, .Issuer, .Audience}` with restricted Sprig allow-list |
| `audienceTemplates[]` | []AudienceTemplate | yes | Named templates; at least one entry named `egress` required |
| `normalization` | object | no | `{lowercase: bool, trim: bool}` applied to rendered subject before OpenFGA Check |

`AudienceTemplate` fields: `name` (string), `template` (Go template; same Sprig allow-list),
`expirationSeconds` (int; [60, 600]).

**Sprig allow-list** (`sprigAllowList` fixed — not user-configurable): `trimPrefix`, `trimSuffix`,
`lower`, `upper`, `split`, `replace`. Admission CEL rejects any template referencing a function
outside this set (parse-and-enumerate check on `{{` tokens).

### 1.2 Status fields

| Field | Type | Description |
|---|---|---|
| `phase` | string | `Active` | `Degraded` |
| `observedGeneration` | int64 | Last generation successfully reconciled |
| `lastTemplateValidationTime` | metav1.Time | UTC timestamp of most recent successful template parse |
| `conditions[]` | []metav1.Condition | Standard `Ready`, `JWKSReachable` conditions |

### 1.3 Printer columns

`Age`, `Ready` (from `conditions[type=Ready].status`), `Phase`, `Issuer`, `AudienceTemplates` (count).

### 1.4 VAP CEL invariants

Enforced via `ValidatingAdmissionPolicy` (K8s 1.30 GA; webhook only where CEL insufficient):

1. `subjectTemplate` parses without error — admission rejects on parse failure → `TemplateInvalid` event.
2. Every `audienceTemplates[].template` parses without error.
3. Sprig allow-list: CEL evaluates template tokens; any unrecognized function → deny.
4. `audienceTemplates` contains at least one entry where `name == "egress"`.
5. Every `audienceTemplates[].expirationSeconds` ∈ [60, 600] (rule 05.3 TTL cap).

### 1.5 Bootstrap CRs

Operator install Job creates default `OIDCProvider` CRs at install time (idempotent apply):
`kubernetes-default`, `google`, `github-actions`, `azure-entra`, `okta`, `keycloak`, `gitlab`.
Each ships with provider-appropriate `subjectTemplate` and `audiences[]` defaults.
Bootstrap Job name: `keese-oidcprovider-bootstrap`. Job is owner-ref'd to the operator `Deployment`;
deleted on operator uninstall. Each default CR carries label `keese.ai/bootstrap: "true"`.

### 1.6 Tenant gating

`Tenant.spec.oidc.allowedProviders[]` lists OIDCProvider names accepted for token validation in
that tenant. Tokens from issuers not in the allow-list are rejected 403 `OIDCProviderNotFound`.
ReBAC marker: `// +keese:rebac-tuple=tenant.uses_oidc_provider` on `Tenant.spec.oidc.allowedProviders[]`
(cross-reference tenancy spec).

**Escalation — new ReBAC relation required:** `tenant.uses_oidc_provider` is not present in the
current `04a` OpenFGA model. This relation must be added to `dev/bootstrap/openfga/model.fga`
before the controller phase opens. Flag to `rebac-modeler` agent.

### 1.7 Three-token projection

Agent pods mount three independent `serviceAccountToken` projections (set by workspace controller):

```
/var/run/keese/tokens/
  egress       # keese-egress-<tenant>       → Envoy AI Gateway ext_authz
  supervisor   # keese-supervisor-<ws-uid>  → 08b ACP bridge
  workflowRun  # keese-wf-<run-uid>         → 09 NATS bridge (workflow pods only)
```

Only `egress` is federated to cloud IAM. `workflowRun` and `supervisor` are in-cluster only
(04b-ii). Kubelet rotates each independently at 80% of `expirationSeconds`.

### 1.8 Subject derivation and cache

ext_authz caches rendered subjects keyed by `(issuer, sub, aud)` for ≤5 min. Cache is invalidated
on OIDCProvider update via informer event. Audit log shape (rule 05.10):
`(issuer, rendered_subject, aud, openfga_decision, upstream_status)` — never raw token bytes,
never decoded claim values beyond what is enumerated.

### 1.9 HA and upgrade

OIDCProvider controller runs inside the main operator `Deployment` (`replicas: 2`, leader-election).
PDB: `minAvailable: 1` (`config/default/manager_pdb.yaml`). On operator upgrade, the bootstrap
Job re-applies default CRs via SSA (`fieldOwner=keese-oidcprovider-bootstrap`); user-managed fields
(separate field manager) are preserved. Default CR changes are additive-only across v1alpha1.
Cache-flush gRPC retries 3x with 5 s backoff before timeout.

### 1.10 Finalizer

`finalizers.oidcprovider.keese.ai/cache-flush` — controller sends cache-flush signal to
all gateway pods (via `keese-ext-authz` gRPC admin endpoint) before allowing CR deletion. Emits
`CacheFlushComplete` event. Maximum 60 s drain; after timeout, deletion proceeds and a `JWKSUnreachable`
event is emitted on any gateway that missed the flush.

### 1.11 Failure modes

| Failure | Behavior | Recovery |
|---|---|---|
| Provider missing for issuer | 403 `OIDCProviderNotFound`; `keese_oidc_template_eval_errors_total` incremented | Create matching OIDCProvider CR |
| Provider `phase=Degraded` | 403 `OIDCProviderDegraded` at gateway | Fix JWKS endpoint; reconciler re-validates |
| `subjectTemplate` eval error | Deny 403; `AudienceTemplateEvalError` event; `keese_oidc_template_eval_errors_total{reason=subject}` | Patch OIDCProvider subjectTemplate |
| Audience template eval error | Deny 403; `AudienceTemplateEvalError` event; `keese_oidc_template_eval_errors_total{reason=audience}` | Patch template or supply missing WorkflowRun variable |
| JWKS unreachable at gateway | Gateway 401 after JWKS cache TTL (300 s default); `JWKSUnreachable` event; `phase=Degraded` | Fix JWKS endpoint; reconciler re-validates; `keese_gateway_jwks_fetch_failures_total > 0 for 5m → P2` |
| CR deleted at runtime | Cache serves last-good rendered subject ≤5 min, then deny; `OIDCProviderMissing` event at workspace reconcile | Restore CR or recreate workspace |
| Sprig allow-list violation | Admission VAP denies; `TemplateInvalid` event | Fix template to use only allowed functions |

### 1.12 Observability

Metrics: see frontmatter `metrics[]`. OTEL trace span: `keese.oidc.template_eval{provider, template, result}`.
Loki labels: `{job="keese-oidcprovider-controller", provider="<name>"}`.

## 2. Acceptance tests

| Test | File | Assertion |
|---|---|---|
| `TestSubjectTemplateParseError` | `test/envtest/authz/oidcprovider_suite_test.go` | Invalid template → admission deny + `TemplateInvalid` event; `phase=Degraded` |
| `TestSprigAllowListReject` | `internal/controller/authz/oidcprovider/sprig_allowlist_test.go` | Template with `env` function → VAP deny; no CR created |
| `TestAudienceTemplateEvalError` | `test/envtest/authz/oidcprovider_degraded_test.go` | Missing `.WorkflowRunUid` → `AudienceTemplateEvalError` event + 403 |
| `TestJWKSUnreachable` | `test/e2e/authz/oidcprovider_jwks_unreachable_test.go` | JWKS endpoint returns 503 → gateway 401; `phase=Degraded`; alert threshold fires |
| `TestCRDeletedAtRuntime_CacheExpiry` | `test/envtest/authz/oidcprovider_deleted_runtime_test.go` | CR deleted; requests succeed ≤5 min; then deny 403 |
| `TestThreeTokenProjectionMount` | `test/e2e/authz/oidcprovider_three_token_projection_test.go` | Agent pod mounts all three token paths; each has distinct audience; only `egress` accepted at ext_authz |
| `TestBootstrapCRs_Idempotent` | `test/envtest/authz/oidcprovider_suite_test.go` | Bootstrap Job re-run creates no duplicates; labels `keese.ai/bootstrap: "true"` present |
| `TestCacheInvalidation_OnUpdate` | `test/envtest/authz/oidcprovider_suite_test.go` | OIDCProvider spec patched → informer triggers cache flush; new subject rendered |

## 3. Refs

- [`04b-projected-sa-identity.md`](../designs/04b-projected-sa-identity.md)
- [`04b-ii-oidc-trust.md`](../designs/04b-ii-oidc-trust.md)
- [`04a-openfga-authz-model.md`](../designs/04a-openfga-authz-model.md)
- [`egress-authz-protocol.md`](egress-authz-protocol.md) — sibling spec; consumes rendered subject; do not duplicate
- [`../plans/rubric.md`](../plans/rubric.md)
- [`authz.keese.ai-v1alpha1-ii-iter-log.md`](authz.keese.ai-v1alpha1-ii-iter-log.md)
