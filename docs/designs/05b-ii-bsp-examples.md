<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: design
category: egress
depends:
  - 05b-credential-injection-patterns.md
  - 04b-ii-oidc-trust.md
  - 11-secrets-pluggable-vault.md
  - 17-credential-broker.md
related_skills: []
status: current
last_verified: 2026-04-20
rollback: |
  Split from 05b per 200-line rule. Rollback is identical to 05b — see 05b frontmatter.
---

# 05b-ii — BSP YAML Examples + Iteration Log

Split from [05b-credential-injection-patterns.md](05b-credential-injection-patterns.md) per 200-line rule.
Full YAML examples per credential type and the 05b iteration log.

## Static API key {#static-api-key}

ExternalSecret CR in `keese-credentials` namespace:

```yaml
apiVersion: external-secrets.io/v1beta1
kind: ExternalSecret
metadata:
  name: keese-cred-acme-corp-anthropic
  namespace: keese-credentials
spec:
  refreshInterval: 5m
  secretStoreRef:
    name: keese-openbao
    kind: ClusterSecretStore
  target:
    name: keese-cred-acme-corp-anthropic
    creationPolicy: Owner
  data:
    - secretKey: apiKey
      remoteRef:
        key: kv/keese/tenants/acme-corp/credentials/anthropic/primary
        property: api_key
```

BackendSecurityPolicy referencing the projected Secret:

```yaml
apiVersion: aigateway.envoyproxy.io/v1alpha1
kind: BackendSecurityPolicy
metadata:
  name: bsp-acme-corp-anthropic
  namespace: keese-system
spec:
  targetRef:
    group: aigateway.envoyproxy.io
    kind: AIGatewayRoute
    name: anthropic-route
  apiKey:
    secretRef:
      namespace: keese-credentials
      name: keese-cred-acme-corp-anthropic
      key: apiKey
```

ReferenceGrant (shared gateway / Mode A):

```yaml
apiVersion: gateway.networking.k8s.io/v1beta1
kind: ReferenceGrant
metadata:
  name: keese-credentials-to-system
  namespace: keese-credentials
spec:
  from:
    - group: aigateway.envoyproxy.io
      kind: BackendSecurityPolicy
      namespace: keese-system
  to:
    - group: ""
      kind: Secret
```

## AWS OIDC STS (Bedrock) {#aws-oidc-sts}

```yaml
apiVersion: aigateway.envoyproxy.io/v1alpha1
kind: BackendSecurityPolicy
metadata:
  name: bsp-acme-corp-bedrock
  namespace: keese-system
spec:
  targetRef:
    group: aigateway.envoyproxy.io
    kind: AIGatewayRoute
    name: bedrock-route
  oidc:
    providerUrl: https://kubernetes.default.svc.cluster.local
    roleArn: arn:aws:iam::123456789012:role/keese-acme-corp-bedrock
    tokenExchangeServiceAccounts:
      - namespace: acme-corp-ws-research
        name: keese-ws-research-abc123
```

Trust policy (04b-ii): `aud=keese-egress-acme-corp` AND `sub=system:serviceaccount:acme-corp-ws-research:keese-ws-research-abc123`.

## GCP Workload Identity Federation (Vertex AI) {#gcp-wif}

```yaml
apiVersion: aigateway.envoyproxy.io/v1alpha1
kind: BackendSecurityPolicy
metadata:
  name: bsp-acme-corp-vertex
  namespace: keese-system
spec:
  targetRef:
    group: aigateway.envoyproxy.io
    kind: AIGatewayRoute
    name: vertex-route
  gcpOidc:
    workloadIdentityPool: keese-prod-pool
    provider: keese-k8s-provider
    serviceAccountEmail: keese-acme-corp@my-project.iam.gserviceaccount.com
```

## Azure Entra OIDC (Azure OpenAI) {#azure-entra}

```yaml
apiVersion: aigateway.envoyproxy.io/v1alpha1
kind: BackendSecurityPolicy
metadata:
  name: bsp-acme-corp-azure-openai
  namespace: keese-system
spec:
  targetRef:
    group: aigateway.envoyproxy.io
    kind: AIGatewayRoute
    name: azure-openai-route
  azureOidc:
    tenantId: 72f988bf-86f1-41af-91ab-2d7cd011db47
    clientId: 8f94b6e5-3c41-4b7f-9e80-2a1d5c3f1234
    federatedIdentityCredentialRef:
      namespace: keese-credentials
      name: keese-fic-acme-corp-research
```

## Credential pooling {#credential-pooling}

```yaml
spec:
  pool:
    selection: spillover   # round-robin | least-used | spillover
    members:
      - apiKey:
          secretRef:
            namespace: keese-credentials
            name: keese-cred-acme-corp-anthropic-1
            key: apiKey
      - apiKey:
          secretRef:
            namespace: keese-credentials
            name: keese-cred-acme-corp-anthropic-2
            key: apiKey
```

## Iteration log {#iteration-log}

### Iteration 1 — 2026-04-20

| # | Category | Weight | Ratio | Score | Notes |
|---|---|---:|---:|---:|---|
| 1 | Scope clarity | 10 | 1.0 | 10 | Five open questions answered; scope bounded to gateway injection layer. |
| 2 | Architecture fit | 10 | 1.0 | 10 | D13 three-table, D10, rule 05 honored; no agent-pod secrets. |
| 3 | Security posture | 15 | 1.0 | 15 | Fail-closed on no BSP; dual-constraint OIDC (sub+aud); vault-agent on gateway only; no envFrom/env.valueFrom; 70%/95% drain stated. |
| 4 | Automatability | 10 | 0.5 | 5 | ESO + BSP templates specified; OpenTofu paths cross-referenced; migration Job pattern named. Actual scripts deferred pre-gate. |
| 5 | Verifiability | 15 | 0.5 | 7.5 | Eight failure modes; concrete rotation formula. No envtest fixtures or kuttl tests authored yet (pre-gate acceptable). |
| 6 | Failure-mode awareness | 10 | 1.0 | 10 | Rotation-expired fail-closed; pool spillover; OIDC STS timeout; vault sidecar crash; ReferenceGrant missing; no BSP match → deny. |
| 7 | Context efficiency for Claude | 10 | 1.0 | 10 | At ceiling; YAML split to 05b-ii; single responsibility per file. |
| 8 | Docs quality | 5 | 1.0 | 5 | SPDX; frontmatter complete; rollback concrete; depends full. |
| 9 | Observability | 5 | 1.0 | 5 | Three metric families; six event reasons; OTEL span extension flagged for 10a. |
| 10 | Operational readiness | 10 | 1.0 | 10 | Upgrade/rollback path; migration Job pattern; chart pin via helmfile.lock; drain formula concrete. |
| | **Total** | 100 | | **92.5** | |

Verdict: SHIP (92.5 ≥ 90). `status: current`.

Top gaps:
1. Cat 5 (verifiability): envtest fixtures and kuttl tests not yet authored — pre-gate acceptable per rubric.
2. Cat 4 (automatability): `migrations/bsp-migrate-*.yaml` Job and ESO force-sync annotation script deferred.
3. Cat 5 (verifiability): pool selection behavior under concurrent 429s not unit-tested.

Cross-deps: **17** — pool state machine + rotation window enforcement open for iter-1.
**11** — OpenBao path convention and ESO `RefreshInterval: 5m` must align with 11's rotation model.
**10a** — `keese.rebac.check` span needs `bsp`, `upstream_role`, `exchange_result` for iter-2.
**05a** — NATS KV + `x-keese-tenant` + witness audience settled; no change.

Next step: Flag 17 iter-1 with pool state machine open question and rotation window
contract. Flag 10a iter-2 with span field additions.
