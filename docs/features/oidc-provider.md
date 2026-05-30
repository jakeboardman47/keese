<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: feature
category: feature
depends:
  - docs/designs/04b-projected-sa-identity.md
  - docs/designs/04b-ii-oidc-trust.md
implements_specs:
  - docs/specs/authz.keese.ai-v1alpha1.md
implements_plans:
  - docs/plans/demo/tech-debt.md
source_refs:
  - api/authz/v1alpha1/oidcprovider_types.go:1-180
  - internal/controller/authz/oidcprovider_controller.go:1-416
  - internal/controller/authz/jwks.go:1-115
  - internal/controller/authz/template.go:1-105
  - internal/controller/authz/events.go:1-31
  - config/default/bootstrap/oidcprovider-kubernetes-default.yaml
  - config/default/bootstrap/oidcprovider-google.yaml
  - config/default/bootstrap/oidcprovider-github-actions.yaml
  - config/default/bootstrap/oidcprovider-azure-entra.yaml
  - config/default/bootstrap/oidcprovider-okta.yaml
  - config/default/bootstrap/oidcprovider-keycloak.yaml
  - config/default/bootstrap/oidcprovider-gitlab.yaml
related_skills: [controller-authoring]
status: implemented
implemented_in_phase: demo-D3
last_verified: 2026-05-29
---

# OIDCProvider

## Summary

`OIDCProvider` is a cluster-scoped `authz.keese.ai/v1alpha1` CRD that anchors
per-cloud OIDC trust for keese's projected ServiceAccount-token identity model.
Each CR declares an issuer, accepted audience globs, a restricted Go template for
subject normalization, and named audience templates (e.g. `egress`, `supervisor`,
`workflowRun`) that drive projected token mounts in agent pods. The controller
probes the JWKS endpoint on a 5-minute cycle, validates templates against a
six-function Sprig allow-list, and emits Prometheus metrics for each path. Seven
bootstrap `OIDCProvider` CRs for common clouds (Kubernetes in-cluster, Google,
GitHub Actions, Azure Entra, Okta, Keycloak, GitLab) are installed by the
`keese-oidcprovider-bootstrap` Job at cluster install time.

## Behavior

- **Bootstrap CRs**: seven CRs ship in `config/default/bootstrap/`. Those whose
  `spec.issuer` contains a `{token}` or `<token>` placeholder are placed in
  `Degraded` and requeued every hour (`requeuePlaceholderInterval = 1h`) until an
  admin substitutes a real issuer URL. See oidcprovider_controller.go:154-173.
- **Finalizer**: `finalizers.oidcprovider.keese.ai/cache-flush` is added on first
  reconcile. On deletion the controller sends a cache-flush signal to all gateway
  pods (60 s timeout) before removing the finalizer. See
  oidcprovider_controller.go:313-346.
- **Template validation**: `subjectTemplate` and every `audienceTemplates[].template`
  are parsed with the restricted Sprig allow-list (`trimPrefix`, `trimSuffix`,
  `lower`, `upper`, `split`, `replace`). Parse failure sets `Phase=Degraded` and
  `Ready=False`. See template.go:82-105 and oidcprovider_controller.go:203-251.
- **JWKS probe**: every 5 minutes (`requeueJWKSInterval`) the controller hits the
  JWKS endpoint — derived via `/.well-known/openid-configuration` when
  `spec.jwksUri` is absent. The resolved URI is cached in
  `status.resolvedJwksUri` to avoid repeated discovery. See jwks.go:63-95 and
  oidcprovider_controller.go:255-307.
- **Phase**: `Active` when templates parse successfully; `Degraded` on template
  failure or placeholder issuer. JWKS unreachability sets `JWKSReachable=False`
  but does not change `Phase` — template validity and JWKS reachability are
  independent conditions. See oidcprovider_types.go:10-16.
- **SSA**: all writes use `client.Apply` with field owner `keese-oidcprovider-controller`;
  bootstrap fields owned by `keese-oidcprovider-bootstrap` are not clobbered.
  See oidcprovider_controller.go:48-52.
- **ReBAC**: the `tenant.uses_oidc_provider` OpenFGA relation is written by the
  Tenant controller (not this controller); the marker is on the type declaration at
  oidcprovider_types.go:148.

## Configuration surface

Key `OIDCProviderSpec` fields — see oidcprovider_types.go:68-104 and
docs/specs/authz.keese.ai-v1alpha1.md for the full contract:

| Field | Required | Notes |
|---|---|---|
| `spec.issuer` | yes | OIDC issuer URL; JWKS auto-derived unless `jwksUri` is set |
| `spec.jwksUri` | no | Overrides discovery; use for air-gapped / Dex / Pinniped |
| `spec.audiences` | yes (≥1) | Glob patterns matched against `aud` claim (e.g. `keese-egress-*`) |
| `spec.subjectTemplate` | yes | Restricted Sprig template over `{.Claims, .Issuer, .Audience}` |
| `spec.audienceTemplates` | yes (≥1) | Named audience+TTL pairs; must include one named `egress` (CRD `XValidation`-enforced) |
| `spec.audienceTemplates[].expirationSeconds` | yes | Token TTL in [60, 600] s (XValidation-enforced, rule 05.3) |
| `spec.normalization.lowercase` | no | Lowercases rendered subject before OpenFGA check |
| `spec.normalization.trim` | no | Strips whitespace from rendered subject |

Three canonical audience names drive projected token mount paths in agent pods:
`egress` → `/var/run/keese/tokens/egress`, `supervisor` → `/var/run/keese/tokens/supervisor`,
`workflowRun` → `/var/run/keese/tokens/workflowRun`. See oidcprovider_types.go:94-98.

## Observability

Prometheus counters and histograms registered at package init (oidcprovider_controller.go:73-99):

| Metric | Labels | Meaning |
|---|---|---|
| `keese_oidc_template_eval_errors_total` | `provider, template, reason` | Template parse failures |
| `keese_oidc_audience_template_eval_total` | `provider, template, result` | Audience template evaluations |
| `keese_oidc_token_rotation_seconds` | `provider, template` | Observed token rotation durations |
| `keese_gateway_jwks_fetch_failures_total` | `provider` | JWKS endpoint fetch failures |
| `keese_oidc_cache_invalidations_total` | `provider, trigger` | Cache-flush signals sent |

Event reasons from events.go (all via `recorder.Eventf`):

- `TemplateInvalid`, `AudienceTemplateEvalError`, `TemplateValidationSucceeded` — template lifecycle.
- `JWKSUnreachable`, `JWKSReachable` — JWKS probe results.
- `CacheFlushComplete`, `CacheFlushTimeout` — deletion drain.
- `BootstrapPlaceholderIssuer`, `BootstrapCRPreserved` — bootstrap CR state.

Status conditions on `OIDCProvider`: `Ready`, `JWKSReachable`. See
oidcprovider_types.go:106-138. `status.phase` is `Active` or `Degraded`;
`status.resolvedJwksUri` caches the derived JWKS endpoint; `status.lastTemplateValidationTime`
and `status.lastReconcileTime` record reconcile timestamps.

Printer columns: `Age`, `Ready`, `Phase`, `Issuer`, `AudienceTemplates`. See
oidcprovider_types.go:143-147.

## Known limitations

- **`FakeCacheFlusher` wired in production**: `SetupWithManager` nil-guards
  instantiate `&FakeCacheFlusher{}` when no `CacheFlusher` is injected
  (oidcprovider_controller.go:370-371). The production `CacheFlusher` — which
  sends a gRPC cache-flush to gateway pods on OIDCProvider deletion — must still
  be constructed and injected in `cmd/main.go`. Until that wiring lands, deletion
  of an `OIDCProvider` CR does not flush the gateway JWKS cache; stale keys may
  persist until the gateway's own TTL expires.
- **Bootstrap CRs with placeholders are silently `Degraded`**: admins must monitor
  `status.phase` or `kubectl get oidcp` to discover un-substituted bootstrap CRs;
  there is no installation-time validation that all placeholders have been replaced.
- **JWKS probe is reachability-only**: the controller confirms the JWKS endpoint
  returns a 2xx response with a `keys` field (jwks.go:33-60) but does not parse
  or pre-validate individual JWK entries. Key-format errors surface only when the
  Envoy AI Gateway attempts token verification.
- **No conversion webhook at v1alpha1**: promotion to `v1beta1` requires a
  conversion webhook and a migration plan (rule 04.2).

## Change history

- demo-D3 / tech-debt cleanup: initial implementation — controller wired with JWKS
  probe, restricted Sprig template validation, Prometheus metrics, and seven
  bootstrap CRs (docs/plans/demo/tech-debt.md).

## References

- Design: docs/designs/04b-projected-sa-identity.md
- Design: docs/designs/04b-ii-oidc-trust.md
- Spec: docs/specs/authz.keese.ai-v1alpha1.md
- Plan: docs/plans/demo/tech-debt.md
- Source: api/authz/v1alpha1/oidcprovider_types.go
- Source: internal/controller/authz/oidcprovider_controller.go
- Source: internal/controller/authz/jwks.go
- Source: internal/controller/authz/template.go
- Source: internal/controller/authz/events.go
- Source: config/default/bootstrap/ (seven bootstrap CRs)
