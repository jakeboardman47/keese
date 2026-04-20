<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: design
category: guardrails
depends: [06-guardrailbinding.md]
related_skills: [guardrail-author]
status: draft
last_verified: 2026-04-20
rollback: see 06-guardrailbinding.md
---

# 06-ii — GuardrailBinding: Full Spec Schema

Companion to [06-guardrailbinding.md](06-guardrailbinding.md). Contains the
canonical YAML shape consumed by the `crd-author` agent and the 05c projector.

## 05c cross-dependency lock

The following field paths are frozen for the 05c `GuardrailBinding → MCPRoute`
projector. Changes require a coordinated iteration with 05c:

- `.spec.tools[].name`
- `.spec.tools[].methods[]`
- `.spec.tools[].argumentsPattern`
- `.spec.tools[].rateLimit.requests`
- `.spec.tools[].rateLimit.window`

## Full spec schema

```yaml
apiVersion: guardrail.operator.keese.ai/v1alpha1
kind: GuardrailBinding
metadata:
  name: acme-default       # cluster singleton is always named "default"
  namespace: keese-acme    # keese-system for the cluster-scoped default
spec:
  scope: cluster | tenant | workspace    # determines merge layer position
  tools:
    allow:
      - name: shell.execute              # [05c-lock] tool identifier
        methods: ["tools/call"]          # [05c-lock] MCP methods permitted
        argumentsPattern: "^(ls|cat)"   # [05c-lock] regex on serialized args
        rateLimit:                       # [05c-lock]
          requests: 100
          window: 1m
      - name: github.search
        methods: ["tools/call"]
    deny:
      - name: shell.execute
        argumentsPattern: ".*rm -rf.*"
  models:
    allow: ["claude-3-5-sonnet", "gpt-4o"]
    deny:  ["gpt-3.5-turbo"]            # deny wins over allow (04a model_gate)
  contentFilters:
    - type: presidio
      configRef:
        name: presidio-default-pii
        namespace: keese-guardrail-configs
    - type: llamaguard
      configRef:
        name: llamaguard-unsafe-content
        namespace: keese-guardrail-configs
  rateLimits:
    perWorkspaceTokens:
      requests: 10000
      window: 1m
  tokenBudgetRef:                        # feeds 10b budget CR; min() across layers
    name: acme-monthly
    namespace: keese-budgets
  kyvernoPolicyRefs:                     # D-01.3 PSS + defence-in-depth
    - name: require-readonly-root-fs
    - name: deny-host-network
  timeWindows:
    allowed:
      - start: "09:00"
        end: "18:00"
        timezone: UTC
  recipeHooks:
    - event: beforeToolCall
      webhookRef:
        name: guardrail-hook
        namespace: keese-acme
status:
  observedGeneration: 1
  phase: Ready                           # Ready | Degraded | Pending
  effectiveParentAllow: ["shell.execute", "github.search"]  # VAP reads this
  effectiveParentDeny: []
  mergedChildCount: 2
  conditions:
    - type: Ready
      status: "True"
    - type: KyvernoPoliciesPresent
      status: "True"
```

## Printer columns (kubebuilder markers)

```go
// +kubebuilder:printcolumn:name="Scope",type=string,JSONPath=`.spec.scope`
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`
// +kubebuilder:printcolumn:name="Children",type=integer,JSONPath=`.status.mergedChildCount`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
```

## ReBAC markers

```go
// +keese:rebac-tuple=model_gate#allows (spec.models.allow[])
// +keese:rebac-tuple=model_gate#denies (spec.models.deny[])
// +keese:rebac-tuple=tool#allowed_in@workspace (spec.tools.allow[].name)
```

## Refs

- [06-guardrailbinding.md](06-guardrailbinding.md) — parent design
- [05c-mcp-policy-enforcement.md](05c-mcp-policy-enforcement.md) — projector consumer
- [../specs/guardrail.operator.keese.ai-v1alpha1.md](../specs/guardrail.operator.keese.ai-v1alpha1.md)
