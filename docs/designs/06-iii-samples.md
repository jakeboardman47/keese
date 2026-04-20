<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: design
category: guardrails
depends:
  - 06-guardrailbinding.md
  - 06-ii-spec-schema.md
related_skills: [guardrail-author]
status: draft
last_verified: 2026-04-20
rollback: see 06-guardrailbinding.md
---

# 06-iii — GuardrailBinding: Scope Samples

Companion to [06-guardrailbinding.md](06-guardrailbinding.md) and
[06-ii-spec-schema.md](06-ii-spec-schema.md). Six canonical samples
(cluster / tenant / workspace × minimal / full) used by
`config/samples/guardrail/` and `make guardrail-dry-run`.

## Cluster scope — minimal (default binding)

```yaml
# config/manager/default-guardrailbinding.yaml
apiVersion: guardrail.operator.keese.ai/v1alpha1
kind: GuardrailBinding
metadata:
  name: default
  namespace: keese-system
  labels:
    keese.ai/binding-scope: cluster
spec:
  tools:
    deny: []
  tokenBudget:
    total: 1000000
```

## Cluster scope — full-featured

```yaml
apiVersion: guardrail.operator.keese.ai/v1alpha1
kind: GuardrailBinding
metadata:
  name: default-strict
  namespace: keese-system
  labels:
    keese.ai/binding-scope: cluster
spec:
  tools:
    allow: [file_read, web_search, code_exec]
    deny: [shell_exec, kubectl_exec]
    rateLimit:
      requests: 100
      window: "1m"
      scope: sa
  kyverno:
    - policyRef: keese-default-tool-policy
  tokenBudget:
    input: 500000
    output: 100000
    total: 600000
  recipeHooks:
    - event: beforeToolCall
      serviceRef:
        name: audit-webhook
        namespace: keese-guardrail-hooks
        port: 8443
        path: /audit
```

## Tenant scope — minimal

```yaml
apiVersion: guardrail.operator.keese.ai/v1alpha1
kind: GuardrailBinding
metadata:
  name: acme-tenant-policy
  namespace: tenant-acme
  labels:
    keese.ai/binding-scope: tenant
spec:
  inherit:
    - name: default
      namespace: keese-system
  tools:
    deny: [web_search]     # adds to parent deny — tightens only
  tokenBudget:
    total: 200000          # tighter than cluster default
```

## Tenant scope — full-featured

```yaml
apiVersion: guardrail.operator.keese.ai/v1alpha1
kind: GuardrailBinding
metadata:
  name: acme-tenant-strict
  namespace: tenant-acme
  labels:
    keese.ai/binding-scope: tenant
spec:
  inherit:
    - name: default
      namespace: keese-system
  tools:
    allow: [file_read]     # subset of cluster allow — valid
    deny: [web_search, shell_exec]
    rateLimit:
      requests: 30
      window: "1m"
      scope: workspace
  kyverno:
    - policyRef: acme-data-residency
    - policyRef: acme-pii-filter
  tokenBudget:
    input: 100000
    output: 20000
    total: 120000
  recipeHooks:
    - event: onError
      serviceRef:
        name: pagerduty-proxy
        namespace: tenant-acme-infra
        port: 8443
        path: /alert
```

## Workspace scope — minimal

```yaml
apiVersion: guardrail.operator.keese.ai/v1alpha1
kind: GuardrailBinding
metadata:
  name: ws-dev-policy
  namespace: ws-acme-dev
  labels:
    keese.ai/binding-scope: workspace
spec:
  inherit:
    - name: acme-tenant-policy
      namespace: tenant-acme
  tokenBudget:
    total: 50000           # tighter than tenant — valid
```

## Workspace scope — full-featured

```yaml
apiVersion: guardrail.operator.keese.ai/v1alpha1
kind: GuardrailBinding
metadata:
  name: ws-dev-strict
  namespace: ws-acme-dev
  labels:
    keese.ai/binding-scope: workspace
spec:
  inherit:
    - name: acme-tenant-strict
      namespace: tenant-acme
  tools:
    deny: [web_search, file_write]  # adds file_write — valid tightening
    rateLimit:
      requests: 10
      window: "1m"
      scope: sa
  envoy:
    securityPolicyRef:
      name: ws-dev-egress-policy
      namespace: keese-gateway
  tokenBudget:
    input: 20000
    output: 5000
    total: 25000
```
