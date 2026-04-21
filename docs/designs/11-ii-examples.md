<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: design
category: secrets
depends:
  - 11-secrets-pluggable-vault.md
  - 04b-ii-oidc-trust.md
  - 22-workflow-composition-examples.md
related_skills: []
status: current
last_verified: 2026-04-21
rollback: |
  Companion to 11-secrets-pluggable-vault.md. Rollback is identical — see 11 frontmatter.
---

# 11-ii — Secrets: Canonical ExternalSecret YAML Examples + Iteration Log

Split from [11-secrets-pluggable-vault.md](11-secrets-pluggable-vault.md) per 200-line rule.

## Example 1: OpenBao → static API key {#static-api-key}

```yaml
apiVersion: external-secrets.io/v1beta1
kind: SecretStore
metadata:
  name: openbao-keese-tenant-acme
  namespace: keese-acme
spec:
  provider:
    vault:
      server: "http://keese-openbao.keese-system.svc:8200"
      path: "kv"
      version: "v2"
      auth:
        kubernetes:
          mountPath: "kubernetes"
          role: "keese-tenant-acme-reader"
          serviceAccountRef:
            name: keese-tenant-reader
---
apiVersion: external-secrets.io/v1beta1
kind: ExternalSecret
metadata:
  name: anthropic-api-key
  namespace: keese-acme
spec:
  refreshInterval: 5m
  secretStoreRef:
    name: openbao-keese-tenant-acme
    kind: SecretStore
  target:
    name: keese-cred-acme-anthropic
    creationPolicy: Owner
    template:
      metadata:
        annotations:
          keese.ai/version: "{{ .source_version }}"
          keese.ai/rotated-at: "{{ now | date '2006-01-02T15:04:05Z' }}"
  data:
    - secretKey: api-key
      remoteRef:
        key: keese/tenants/acme/credentials/anthropic/primary
        property: value
```

**Rationale.** Canonical pattern for any static upstream API key. `SecretStore` scopes to
the tenant namespace with a per-tenant OpenBao role — no cross-tenant read possible.
`keese-tenant-reader` SA is the only credential carrier; never mounted into an agent pod.
`keese.ai/version` drives `keese_secret_stale_seconds` (11 Q4). Rotation: admin bumps
OpenBao KV version → ESO detects within 5 min → kubelet projects within 1 min; no restart.

## Example 2: AWS Secrets Manager → STS-issued API key {#aws-sm}

```yaml
apiVersion: external-secrets.io/v1beta1
kind: SecretStore
metadata:
  name: aws-sm-keese-tenant-acme
  namespace: keese-acme
spec:
  provider:
    aws:
      service: SecretsManager
      region: us-east-1
      auth:
        jwt:
          serviceAccountRef:
            name: keese-aws-secrets-reader
            # SA annotated: eks.amazonaws.com/role-arn per 04b-ii-oidc-trust.md
---
apiVersion: external-secrets.io/v1beta1
kind: ExternalSecret
metadata:
  name: aws-smithery-key
  namespace: keese-acme
spec:
  refreshInterval: 10m
  secretStoreRef:
    name: aws-sm-keese-tenant-acme
    kind: SecretStore
  target:
    name: keese-cred-acme-aws-smithery
    creationPolicy: Owner
  data:
    - secretKey: api-key
      remoteRef:
        key: keese/acme/aws/smithery-api-key
```

**Rationale.** Used for cloud KMS fallback (`openBaoFallbackProvider: aws`) or as primary
on EKS. `keese-aws-secrets-reader` SA carries `eks.amazonaws.com/role-arn`; IRSA exchanges
the projected SA token for STS credentials scoped to a per-tenant IAM role (see
[04b-ii](04b-ii-oidc-trust.md)). Provisioned by `deploy/opentofu/aws/secrets.tf`. No
static AWS credentials in-cluster.

## Example 3: GitHub webhook HMAC secret {#webhook-hmac}

Used by [22-workflow-composition-examples.md](22-workflow-composition-examples.md) iter-2
for webhook trigger verification.

```yaml
apiVersion: external-secrets.io/v1beta1
kind: ExternalSecret
metadata:
  name: github-webhook-hmac-acme
  namespace: keese-acme
spec:
  refreshInterval: 15m         # slower — webhook secrets rotate less frequently
  secretStoreRef:
    name: openbao-keese-tenant-acme
    kind: SecretStore
  target:
    name: keese-hmac-acme-autonomous-dev
    creationPolicy: Owner
    template:
      data:
        hmac-secret: "{{ .hmac }}"
        algorithm: "sha256"
  data:
    - secretKey: hmac
      remoteRef:
        key: keese/tenants/acme/triggers/autonomous-dev/github-secret
        property: hmac
```

**Rationale.** Webhook HMAC secrets are long-lived shared secrets between GitHub and the
keese trigger controller; 15 min refresh balances ESO poll load against rotation
latency. The `template.data` block inlines the algorithm so the consuming controller
reads a self-contained secret without needing runtime config. The `SecretStore` reuses
the tenant-scoped OpenBao store from Example 1 — no additional store CR required.
`keese-tenant-reader` SA manages access; the secret never appears in agent pod
environment variables or files (rules 05.1, 05.7).

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

Verdict: SHIP (held at draft — gaps 2 and 3 unscaffolded).

### Iteration 2 — 2026-04-21

| # | Category | Weight | Ratio | Score | Notes |
|---|---|---:|---:|---:|---|
| 1 | Scope clarity | 10 | 1.0 | 10 | Companion split clean; all 5 Qs retained; per-tenant fallback field locked |
| 2 | Architecture fit | 10 | 1.0 | 10 | VAP hard rule on vault-agent + agent pods; upstream table exhaustive; 04b-ii unchanged |
| 3 | Security posture | 15 | 1.0 | 15 | VaultAgentOnAgentPodForbidden VAP; fail-closed at [60, 3600] bounds; on-prem `none` path explicit |
| 4 | Automatability | 10 | 1.0 | 10 | Three canonical ESO YAMLs in companion; config/secrets/ templates named; deploy/opentofu cross-ref |
| 5 | Verifiability | 15 | 0.5 | 7.5 | Envtest for ESO sync lag still unscaffolded (P8 flag retained); rotation traceable via annotation age |
| 6 | Failure-mode awareness | 10 | 1.0 | 10 | 7 failure paths; VaultAgentOnAgentPodForbidden added; openBaoFallbackAfterSeconds < 60 s rejected |
| 7 | Context efficiency for Claude | 10 | 1.0 | 10 | Main doc ≤ 200 lines; companion ≤ 200 lines; iteration log offloaded here |
| 8 | Docs quality | 5 | 1.0 | 5 | SPDX + frontmatter on both files; companion in designs/README.md index needed (iter-3 cleanup) |
| 9 | Observability | 5 | 1.0 | 5 | 3 metrics, 7 events (+VaultAgentOnAgentPodForbidden), 3 alerts unchanged |
| 10 | Operational readiness | 10 | 1.0 | 10 | Out-of-the-box deployment subsection covers on-prem / cloud / hybrid; rollback unchanged |
| | **Total** | 100 | | **97.5** | |

Verdict: SHIP

Top gaps:
1. Verifiability 0.5: envtest for ESO sync lag unscaffolded — delegate to `test-engineer` in P8.
2. designs/README.md index does not yet list 11-ii-examples.md — iter-3 cleanup.
3. 24 iter-3 must add `openBaoFallbackAfterSeconds` and `openBaoFallbackProvider` to Tenant CRD spec schema.

Next step: update designs/README.md to index 11-ii-examples.md; flag 24 iter-3 for Tenant spec fields;
delegate envtest gap to `test-engineer` (P8).
