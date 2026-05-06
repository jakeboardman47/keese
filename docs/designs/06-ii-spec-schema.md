<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: design
category: guardrails
depends:
  - 06-guardrailbinding.md
  - 05c-mcp-policy-enforcement.md
related_skills: []
status: current
last_verified: 2026-04-20
rollback: Schema changes require CRD conversion webhook at v1beta1 (rule 04.2);
  removing fields is a breaking change — gate behind feature flag.
---

# 06-ii — GuardrailBinding: Spec Schema

Companion to [06-guardrailbinding.md](06-guardrailbinding.md). This file owns
the canonical field-by-field schema, `[05c-lock]` annotations marking fields
05c's projector reads, and the YAML shape for all three binding scopes.

## Canonical spec schema

```yaml
apiVersion: authz.keese.ai/v1alpha1
kind: GuardrailBinding
metadata:
  name: <name>
  namespace: <ns>          # omit for cluster-scoped default
spec:
  # [05c-lock] tools block — projector reads tools.allow, tools.deny, tools[].rateLimit
  tools:
    allow: []              # string list — MCP tool names; empty = allow all
    deny: []               # string list — union with parent deny lists
    # Per-tool rate limits [05c-lock: rate limit block]
    # Fields locked by 06 iter-2; 05c must absorb rename from requestsPerMinute.
    rateLimit:
      requests: 0          # int — request count threshold (0 = no limit)
      window: "1m"         # duration string: "5s", "1m", "10m" etc.
      scope: sa            # enum: tenant | workspace | sa (default: sa)

  # Kyverno ClusterPolicy references — names only, no inline bodies
  kyverno:
    - policyRef: ""        # string — ClusterPolicy .metadata.name

  # OpenFGA tuple ConfigMap reference
  openfga:
    configMapRef:
      name: ""             # ConfigMap in same namespace as binding
      namespace: ""

  # Envoy SecurityPolicy reference
  envoy:
    securityPolicyRef:
      name: ""             # SecurityPolicy name
      namespace: ""        # must be in gateway namespace

  # Recipe hook registrations — serviceRef only; URL form rejected by VAP
  recipeHooks:
    - event: ""            # enum: beforeToolCall | afterToolCall | onError
      serviceRef:
        name: ""           # Service .metadata.name
        namespace: ""      # must be operator-readable namespace
        port: 8443         # int
        path: ""           # string, e.g. "/before-tool-call"

  # Token budget ceilings — merge rule: min() across all bindings
  tokenBudget:
    input: 0               # int tokens; 0 = no limit
    output: 0
    total: 0

  # Inheritance chain — resolved at merge time by controller
  inherit: []              # list of GuardrailBinding refs: {name, namespace}
```

## 05c cross-dependency: schema alignment

05c iter-1 used `.spec.tools[].rateLimit.requestsPerMinute` (int) and
`.spec.tools[].rateLimit.scope` (enum). This design (06 iter-2) locks the
canonical schema as:

```
.spec.tools[].rateLimit:
  requests: int          # renamed from requestsPerMinute
  window: duration       # new — enables non-minute granularity (e.g. "5s")
  scope: enum            # unchanged: tenant | workspace | sa
```

**05c must absorb this rename in its next pass.** The projector implementation
(`internal/guardrail/projector/`) cannot ship until 05c aligns its CEL variable
schema with these field names. This is a doc-level flag; no code is blocked today
because the design gate is still closed (P8).

Fields marked `[05c-lock]` in this schema are the contract surface that
05c's projector keys off. Changes to them require a coordinated update of both
this file and 05c before the design gate opens.

## Scope samples

Six canonical samples (cluster / tenant / workspace × minimal / full-featured)
are split into a companion file to keep this doc under the 200-line cap. See
[06-iii-samples.md](06-iii-samples.md).

## Status schema

```yaml
status:
  phase: ""                    # Ready | Degraded | Pending
  observedGeneration: 0
  effectivePolicy:
    tools:
      allow: []
      deny: []
      rateLimit:
        requests: 0
        window: ""
        scope: ""
    tokenBudget:
      input: 0
      output: 0
      total: 0
  conditions:
    - type: Ready
      status: "True"
      reason: MergeComplete
      message: ""
    - type: ParentReadable
      status: "True"
      reason: ReferenceGrantOK
      message: ""
```
