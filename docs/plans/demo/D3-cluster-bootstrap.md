<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: plan
category: phase
depends:
  - README.md
  - ../../../dev/bootstrap/helmfile.yaml
  - ../../designs/05a-envoy-ai-gateway-topology.md
  - ../../designs/05b-credential-injection-patterns.md
  - ../../designs/05b-ii-bsp-examples.md
related_skills: [plan-management, infra-bootstrap]
status: planned
last_verified: 2026-04-25
---

# D3 — Cluster bootstrap + Anthropic LLM wiring

**Refinement pass:** correctness & security.
**Effort:** 3–4 h. **Owner agent:** `infra-bootstrap`.
**Parallel with:** D2.

## Goal

Bring up the cluster-side dependencies a Workspace pod needs (Envoy AI
Gateway, NATS, OpenFGA, OpenBao, ExternalSecrets, cert-manager) and ship
the live YAMLs for "Anthropic, static API key, via gateway." Today these
exist only in design docs.

## Inputs (already in repo)

- [dev/bootstrap/helmfile.yaml](../../../dev/bootstrap/helmfile.yaml) lists
  every chart we need with pinned (but `# unverified-2026`) versions.
- [dev/bootstrap/openbao/seed.sh](../../../dev/bootstrap/openbao/) seeds
  OpenBao at install time.
- Reference YAMLs in [docs/designs/05b-ii-bsp-examples.md](../../designs/05b-ii-bsp-examples.md):
  ExternalSecret + BackendSecurityPolicy + AIGatewayRoute + ReferenceGrant
  for the static-API-key path.

## Tasks

### T1 — Verify chart versions, commit `helmfile.lock`

Every chart in `helmfile.yaml` is tagged `# unverified-2026`. Run:

```sh
helmfile -f dev/bootstrap/helmfile.yaml deps
helmfile -f dev/bootstrap/helmfile.yaml build > /tmp/helmfile-rendered.yaml
```

For each chart, fix any version that doesn't resolve. Highest-risk
candidates per the audit: `envoy-ai-gateway 0.2.0`, `openbao 0.4.0`,
`eck-operator 2.13.0`. Use `helm search repo <chart> --versions` to
pick the nearest valid release.

Commit `helmfile.lock` (gitignored today; explicitly include).

Acceptance: `helmfile build` succeeds with zero `chart not found` errors.

### T2 — Author live LLM-credential manifests

Three new files under [config/samples/transport/](../../../config/samples/) (or `dev/bootstrap/aigateway/` if you prefer them deployed by helmfile rather than as samples — pick one and stick with it for the demo):

**`anthropic-secret.yaml`** — for the demo, an `ExternalSecret` that pulls
from OpenBao path `secret/data/llm/anthropic` (key `apiKey`). For the
absolute-fastest demo path, you may instead apply a literal `Secret`
named `anthropic-api-key` directly — flagged in tech debt.

**`anthropic-bsp.yaml`** — `BackendSecurityPolicy` (Envoy AI Gateway CRD)
referencing the Secret, header `x-api-key`, target Backend `anthropic`.

**`anthropic-aigatewayroute.yaml`** — `AIGatewayRoute` listening at
`/v1/messages`, model match `claude-*`, ParentRef the gateway, BackendRef
the Anthropic Backend.

Plus the `Backend` CR pointing at `https://api.anthropic.com:443`, and a
`ReferenceGrant` if the Secret is in a different namespace from the BSP.

Reference: copy from [05b-ii-bsp-examples.md §1](../../designs/05b-ii-bsp-examples.md). Resolve the `<TODO:>` placeholders.

Acceptance: `kubectl apply -f config/samples/transport/` returns 0;
`kubectl get aigatewayroute -n keese-system` shows the route Ready.

### T3 — Seed OpenBao with the Anthropic key

Edit [dev/bootstrap/openbao/seed.sh](../../../dev/bootstrap/openbao/) to
write the dev key from `$ANTHROPIC_API_KEY` (env var read from
`.env.local`, gitignored, never committed):

```sh
vault kv put secret/llm/anthropic apiKey="${ANTHROPIC_API_KEY:?required}"
```

Add the variable to [.env.local.example](../../../.env.local.example) with
an empty value and a comment.

Acceptance: post-bootstrap, `kubectl exec` into openbao pod and `vault kv
get secret/llm/anthropic` returns the key (developer's local key for the
demo cluster only — never commit).

### T4 — NetworkPolicy sanity

Workspace egress NetPol allows `keese-system/envoy-ai-gateway:443` and
`keese-system/nats:4222`. Confirm the actual Service names match what
the helmfile chart releases use. The `envoy-ai-gateway` chart by default
exposes `Service: envoy-<gatewayclass>-<gateway>-<hash>`, **not** a
fixed name — you may need a static Service alias.

Action: add `dev/bootstrap/aigateway/service-alias.yaml` —
`Service: envoy-ai-gateway` of type `ExternalName` (or `ClusterIP`
with selector matching the chart's pods) so the Workspace egress NetPol
resolves.

Acceptance: from a debug pod in tenant namespace `alpha`, `curl -v
https://envoy-ai-gateway.keese-system.svc:443/healthz` returns 200.

### T5 — cert-manager-issued CA for the gateway

For the demo, the Anthropic-bound HTTPS connection terminates at Envoy AI
Gateway, which then re-establishes TLS to `api.anthropic.com`. The agent
pod needs the gateway's serving cert CA. Use cert-manager
`ClusterIssuer: selfsigned-root` → `Issuer: keese-aigw-ca` → `Certificate:
keese-aigw-server` mounted on the gateway. Publish the CA bundle as
ConfigMap `keese-system/aigateway-ca` (key `ca.crt`).

Reference: [docs/designs/05a-envoy-ai-gateway-topology.md §TLS](../../designs/05a-envoy-ai-gateway-topology.md).

Acceptance: agent pod with the volume mount from D2-T1 trusts the
gateway's serving cert.

### T6 — One-command bootstrap target

`Makefile` already has `bootstrap-infra`. Confirm it runs the helmfile
sync **and** applies the new manifests from T2 and T4. Add a final
`kubectl wait --for=condition=Ready` line per CRD so the target blocks
until everything settles.

Acceptance: `make bootstrap-infra` is idempotent; running twice produces
no diff.

## Out of scope (→ tech-debt §infra)

- ANY non-Anthropic LLM provider (no OIDC-STS for Bedrock, no WIF for
  Vertex, no Entra for Azure OpenAI).
- TokenBudget enforcement at the gateway.
- ext_authz wiring through OpenFGA (Permit-All for demo; flagged P1).
- Full pre-install ValidatingWebhook for cosign on bundle install plans.
- Capsule Tenant CRs created by helmfile (tenants are demo CRs only).
- OTEL collector destination wiring beyond defaults.

## Verification

- `make bootstrap-infra` exit 0 against a fresh cluster.
- `kubectl get pods -A | grep -v Running | grep -v Completed` returns
  zero rows after a 5-min wait.
- `kubectl describe aigatewayroute -n keese-system anthropic` shows
  Accepted=True, ResolvedRefs=True.
- A standalone `curl -k https://envoy-ai-gateway.keese-system.svc/v1/messages
  -d @prompt.json -H 'x-keese-token: <SA-JWT>'` from a debug pod returns a
  valid Anthropic response. (For demo, x-keese-token can be a literal
  bearer if ext_authz is permit-all — flagged tech debt.)

## Failure modes

| Failure | Symptom | Recovery |
|---|---|---|
| Chart version doesn't exist | `helmfile build` fails | Pick the closest minor; lock the version; flag in tech debt |
| ExternalSecrets sync fails (OpenBao not unsealed) | ES status `SecretSyncedError` | Re-run `dev/bootstrap/openbao/seed.sh`; the seed init-container needs to finish before ES kicks |
| AIGatewayRoute Accepted=False | gateway controller logs `parentRef not found` | Confirm the Gateway CR was created by the chart; manually create one if not |
| Anthropic returns 401 | wrong API key path in BSP | Re-check `secretRef.key` in BSP matches OpenBao key name |
| Pod can't dial gateway | NetworkPolicy mismatch | `kubectl exec` from agent pod, `nc -zv envoy-ai-gateway.keese-system 443` |

## Iteration log

### Iteration 1 — 2026-04-25

| # | Category | Weight | Ratio | Score | Notes |
|---|---|---:|---:|---:|---|
| 1 | Scope clarity | 10 | 1.0 | 10 | Six tasks; one provider; one credential type |
| 2 | Architecture fit | 10 | 1.0 | 10 | Matches 05a/05b; honors rule 05.4 (egress through gateway) |
| 3 | Security posture | 15 | 1.0 | 15 | No keys in git; OpenBao-backed; CA bundle distributed via ConfigMap |
| 4 | Automatability | 10 | 1.0 | 10 | `make bootstrap-infra` is the entry point |
| 5 | Verifiability | 15 | 0.5 | 7.5 | Manual curl checks defined; no automated e2e test added (deferred to D5 smoke) |
| 6 | Failure-mode awareness | 10 | 1.0 | 10 | 5-row failure table covers chart, ES, route, auth, network |
| 7 | Context efficiency | 10 | 1.0 | 10 | <200 lines; reference docs linked, not pasted |
| 8 | Docs quality | 5 | 1.0 | 5 | SPDX + frontmatter + relative links |
| 9 | Observability | 5 | 0.5 | 2.5 | Logs only; no Grafana wiring this phase |
| 10 | Operational readiness | 10 | 0.5 | 5 | ext_authz permit-all and literal Secret fallback both flagged tech debt |
| | **Total** | 100 | | **85** | |

Verdict: SHIP

Top gaps:
1. ext_authz against OpenFGA is permit-all for demo — security posture relies on the network boundary alone. Tech-debt P1.
2. No automated e2e for the LLM round-trip — D5 covers manually.
3. helmfile.lock content depends on what versions exist 2026-04-25 — values may shift Saturday.

Next step: T1 first (gates everything else); T2 + T3 + T5 in parallel; T4 + T6 last.
