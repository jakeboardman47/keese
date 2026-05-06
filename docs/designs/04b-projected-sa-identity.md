<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: design
category: authz
depends:
  - 04a-openfga-authz-model.md
  - 03-workflow-argo-delegation.md
  - 09-transport-crd.md
related_skills: []
status: current
last_verified: 2026-04-21
rollback: Remove audienceTemplates field; revert agent pods to single egress projection; no tuple migration required.
---

# 04b — Projected ServiceAccount Identity (iter-3)

## Decision

Agent pods carry **three** projected ServiceAccount token projections — one per named
audience template — mounted at separate paths. The OIDCProvider CRD owns the template
definitions. Kubernetes kubelet rotates each token independently at 80% of its TTL
(≤600s per rule 05.3).

## Context

Per-tenant projected SA tokens scope cloud-IAM trust policies for LLM/MCP egress.
The 2026-04-21 a2a reframe added two additional token audiences: a per-WorkflowRun
NATS messaging audience and a per-workspace supervisor audience for human-attach
sessions (design 08b). A single audience cannot satisfy all three because cloud IAM,
NATS, and the ACP supervisor each validate against distinct expected audiences. The
OIDCProvider CRD must therefore support named templates that resolve at token-mint time.

## OIDCProvider CRD sketch

```yaml
# keese.ai/v1alpha1 OIDCProvider
spec:
  issuer: ""              # K8s OIDC issuer URL (JWKS at <issuer>/openid/v1/jwks)
  subjectTemplate: "system:serviceaccount:{{.WorkspaceName}}:agent"
  # +keese:rebac-tuple=workspace.uses_oidc_provider
  audienceTemplates:      # named; operator bootstraps three entries
    - name: egress
      template: "keese-egress-{{.TenantName}}"
      expirationSeconds: 600
    - name: workflowRun
      template: "keese-wf-{{.WorkflowRunUid}}"
      expirationSeconds: 600
    - name: supervisor
      template: "keese-supervisor-{{.WorkspaceUid}}"
      expirationSeconds: 600
  sprigAllowList: [trimPrefix, trimSuffix, lower, upper, split, replace]
```

## Template variables

Variables resolved at token-mint time by the minting controller (workspace or workflow):

| Variable | Source | Optional |
|---|---|---|
| `.TenantName` | Capsule Tenant `.metadata.name` | No |
| `.TenantUid` | Capsule Tenant `.metadata.uid` | No |
| `.WorkspaceName` | Workspace `.metadata.name` | No |
| `.WorkspaceUid` | Workspace `.metadata.uid` | No |
| `.WorkflowRunUid` | WorkflowRun `.metadata.uid` | Yes — `workflowRun` template only |
| `.Subject` | Rendered `subjectTemplate` | No |

Missing a required variable is an `AudienceTemplateEvalError` (see Failure modes).

## Token-mint flow

Agent pod projected volume (set by workspace controller):

```
/var/run/keese/tokens/
  egress        # keese-egress-<tenant>    → Envoy AI Gateway sidecar
  supervisor    # keese-supervisor-<ws-uid> → 08b ACP bridge (human-attach)
```

Workflow controller adds a third projection when Argo Workflow is projected (design 03):

```
  workflowRun   # keese-wf-<run-uid>       → 09 a2a/NATS bridge
```

Each projection is an independent `serviceAccountToken` source in the pod's
`volumes[].projected.sources`. The kubelet rotates each token independently.
NATS topic existence within `keese.tenant.<tenant-uid>.wf.<workflow-run-uid>.*`
is provisioned by the Workflow controller; the `workflowRun` token is the identity
asserted when subscribing. Cross-tenant subscribers must also satisfy
`workspace.messageable_from` ReBAC check at subscribe time (D29 / design 25).

## Cross-cuts

- **Design 03 (iter-3, in-flight):** Workflow controller is responsible for adding
  the `workflowRun` audience projection to the projected-SA spec at Argo Workflow
  projection time. This design does not duplicate that logic.
- **Design 09:** Transport CRD's NATS bridge consumes the `workflowRun` token
  from `/var/run/keese/tokens/workflowRun`. Audience value must match the NATS
  JetStream permission grant provisioned by the Workflow controller.
- **Design 04b-ii (companion):** Cross-cloud OIDC trust-anchor details (JWKS
  endpoint selection, AWS STS AssumeRoleWithWebIdentity, GCP Workload Identity
  Federation, Azure Federated Credential) — see `04b-ii-oidc-trust.md`.

## Failure modes

| Failure | Behavior | Recovery |
|---|---|---|
| Template eval error (missing required variable) | SA-token request fails admission with `AudienceTemplateEvalError`; kubelet retries with backoff; pod stays Pending | Fix OIDCProvider or supply missing variable in WorkflowRun spec |
| `workflowRun` projection missing from agent pod | Workflow controller refuses to project Argo Workflow; emits `MissingWorkflowAudience` event; WorkflowRun.status reflects NotReady | Operator re-reconciles OIDCProvider; ensure audienceTemplates includes `workflowRun` entry |
| Token expired mid-task (SIGKILL scenario) | Bridge reads stale token; NATS / gateway returns 401; agent pod restarts; session state in SQLite PVC resumes | Kubelet rotates at 80% TTL; 600s TTL means ~480s window; acceptable for headless runs |
| OIDCProvider deleted while pods running | Existing tokens valid until expiry; next rotation fails mount; pod CrashLoopBackOff after TTL; workspace controller emits `OIDCProviderMissing` | Restore OIDCProvider or delete and re-create workspace |

## Observability

Metrics (OTEL → ECK):

- `keese_oidc_audience_template_eval_total{template, result}` — counter; `result ∈ {ok, error}`
- `keese_oidc_token_rotation_seconds{template}` — histogram; kubelet-observed rotation latency

Event reasons (finite const table in `events.go`):

- `AudienceTemplateEvalError` — template resolution failed; includes missing variable name
- `MissingWorkflowAudience` — workflow projection refused; `workflowRun` template absent
- `OIDCProviderMissing` — OIDCProvider resource not found during workspace reconcile

OTEL trace span: `oidc.token_mint{template, audience, ttl_seconds}` on each projection.

## Iteration log

### Iteration 1 — 2026-04-19 (correctness + security)

| # | Category | Weight | Ratio | Score | Notes |
|---|---|---:|---:|---:|---|
| 1 | Scope clarity | 10 | 1.0 | 10 | Single egress audience, K8s OIDC issuer decision |
| 2 | Architecture fit | 10 | 1.0 | 10 | Aligns with rule 05.3; projected files only |
| 3 | Security posture | 15 | 1.0 | 15 | TTL ≤600s; no env vars; SA-scoped audience |
| 4 | Automatability | 10 | 0.5 | 5 | Samples not yet authored (pre-gate) |
| 5 | Verifiability | 15 | 0.5 | 7.5 | Envtest pre-gate; no test files yet |
| 6 | Failure-mode awareness | 10 | 0.5 | 5 | Basic expiry only; eval errors absent |
| 7 | Context efficiency | 10 | 1.0 | 10 | Doc ≤200 lines; links not inline content |
| 8 | Docs quality | 5 | 1.0 | 5 | SPDX + frontmatter complete |
| 9 | Observability | 5 | 0.5 | 2.5 | Token rotation metric only; no events |
| 10 | Operational readiness | 10 | 0.5 | 5 | TTL budget; no rollback plan |
| | **Total** | 100 | | **75** | |

Verdict: REVISE. Top gaps: failure modes incomplete, observability events absent, rollback undocumented.

### Iteration 2 — 2026-04-20 (performance + quality)

| # | Category | Weight | Ratio | Score | Notes |
|---|---|---:|---:|---:|---|
| 1 | Scope clarity | 10 | 1.0 | 10 | subjectTemplate + single audience template |
| 2 | Architecture fit | 10 | 1.0 | 10 | Sprig allow-list; rule 05.7 projected files |
| 3 | Security posture | 15 | 1.0 | 15 | Per-tenant audience; JWKS companion (04b-ii stub) |
| 4 | Automatability | 10 | 0.5 | 5 | Samples pre-gate |
| 5 | Verifiability | 15 | 0.5 | 7.5 | Envtest pre-gate |
| 6 | Failure-mode awareness | 10 | 1.0 | 10 | Expiry, deletion, rotation documented |
| 7 | Context efficiency | 10 | 1.0 | 10 | ≤200 lines; companion split for OIDC trust |
| 8 | Docs quality | 5 | 1.0 | 5 | Headers complete |
| 9 | Observability | 5 | 1.0 | 5 | Metric + event reasons added |
| 10 | Operational readiness | 10 | 0.5 | 5 | Rollback noted; HA not explicit |
| | **Total** | 100 | | **82.5** | |

Verdict: REVISE. Top gaps: single audience insufficient for a2a (D29), HA path for multi-projection, samples absent.

### Iteration 3 — 2026-04-21 (operational readiness + a2a reframe)

| # | Category | Weight | Ratio | Score | Notes |
|---|---|---:|---:|---:|---|
| 1 | Scope clarity | 10 | 1.0 | 10 | Three named audience templates; mount paths explicit |
| 2 | Architecture fit | 10 | 1.0 | 10 | D29 intra/cross-tenant model; design 03/09 cross-cuts |
| 3 | Security posture | 15 | 1.0 | 15 | Per-run audience; no key in pod; fail-closed on eval error |
| 4 | Automatability | 10 | 0.5 | 5 | Samples pre-gate (design gate not open) |
| 5 | Verifiability | 15 | 0.5 | 7.5 | Envtest pre-gate; failure-mode assertions defined |
| 6 | Failure-mode awareness | 10 | 1.0 | 10 | Four failure modes; two new eval/projection rows |
| 7 | Context efficiency | 10 | 1.0 | 10 | ≤200 lines; companion 04b-ii for OIDC trust |
| 8 | Docs quality | 5 | 1.0 | 5 | SPDX + frontmatter; depends updated |
| 9 | Observability | 5 | 1.0 | 5 | Two metrics + three event reasons + OTEL span |
| 10 | Operational readiness | 10 | 1.0 | 10 | Rollback documented; TTL budget; pod restart idempotent |
| | **Total** | 100 | | **97.5** | |

Verdict: SHIP (97.5). Residuals: Cat 4 (−5) and Cat 5 (−7.5) are pre-gate structural; not a design gap.

## Refs

- [04a-openfga-authz-model.md](04a-openfga-authz-model.md)
- [04b-ii-oidc-trust.md](04b-ii-oidc-trust.md) — cross-cloud OIDC trust anchoring
- [04c-token-revocation.md](04c-token-revocation.md)
- [03-workflow-argo-delegation.md](03-workflow-argo-delegation.md) — workflowRun projection owner
- [09-transport-crd.md](09-transport-crd.md) — NATS bridge consumes workflowRun token
- [05b-credential-injection-patterns.md](05b-credential-injection-patterns.md)
- [../plans/rubric.md](../plans/rubric.md)
