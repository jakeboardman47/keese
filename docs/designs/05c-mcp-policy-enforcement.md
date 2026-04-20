<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: design
category: egress
depends:
  - 04a-openfga-authz-model.md
  - 04c-token-revocation.md
  - 05a-envoy-ai-gateway-topology.md
  - 06-guardrailbinding.md
  - 10a-otel-topology.md
  - 10b-token-accounting.md
  - 24-tenant-crd.md
related_skills: []
status: current
last_verified: 2026-04-20
rollback: |
  If a bad MCPRoute rule set is projected: delete the per-tenant ConfigMap
  keese-mcproute-cel/<tenant>; the projector reconciler re-derives from the
  GuardrailBinding on the next tick, which the guardrail controller re-applies
  via SSA. If the MCPRoute itself is corrupt, revert to the last-known-good
  version by reverting the GuardrailBinding CR; the projector re-emits within
  one reconcile cycle. CEL compile errors never reach the gateway (admission
  webhook rejects); no emergency rollback path is needed for that class.
---

# 05c — MCP Policy Enforcement

## Context

Envoy AI Gateway v0.5.x `MCPRoute` CRD parses JSON-RPC requests and exposes
CEL variables to per-rule admission expressions. 04a (OpenFGA) handles coarse
can-call authorization: "may SA X call any instance of tool Y in workspace W?"
05c handles fine-grained MCP-layer policy: "may SA X invoke tool Y with
arguments matching pattern Z, at this time, subject to per-tool rate limits?"
`GuardrailBinding` (06, running in parallel) is the authoring surface for tool
policies; this design is the projection-to-MCPRoute layer.

## CEL variable schema

Variables available in `MCPRoute.spec.rules[].cel` expressions (Envoy AI GW
v0.5.x `aigateway.envoyproxy.io/v1alpha1`). Variables not in this set are
off-limits; using an absent variable results in a CEL runtime type-error, which
triggers fail-closed deny (see Failure modes).

| Variable | Type | Example |
|---|---|---|
| `request.mcp.tool` | string | `"shell.execute"` |
| `request.mcp.method` | string | `"tools/call"` |
| `request.mcp.arguments` | map(string, dyn) | `{"command": "ls -la"}` |
| `request.headers["x-keese-tenant"]` | string | `"acme-corp"` |
| `request.headers["x-keese-workspace"]` | string | `"ws-alpha"` |
| `source.namespace` | string | `"keese-acme-alpha"` |
| `source.service_account` | string | `"ksa-<workspace-uid>"` |
| `context.time_of_day` | string (HH:MM UTC) | `"14:30"` |

**Flag:** `request.mcp.arguments` presence is version-dependent; Envoy AI GW
v0.5.x ships argument parsing behind a feature gate
(`FEATURE_MCP_ARGUMENT_CEL`). The projector must check for gate availability
at controller startup and emit a `CELArgumentsUnavailable` event if absent,
downgrading argument-pattern rules to a deny-all for that tool until the gate
is confirmed active. This is a residual gap for 06 iter-1 to acknowledge.

## GuardrailBinding projection to MCPRoute

**Source schema required from 06 iter-1** (shape the 06 author must honor):

```
GuardrailBinding.spec.tools[]:
  name: string               # exact tool name; required
  methods: []string          # JSON-RPC methods; default ["tools/call"]
  argumentsPattern: string   # CEL bool expression over request.mcp.arguments
  deny: bool                 # true = deny-rule; false = allow-rule
  rateLimit:
    requestsPerMinute: int   # 0 = unlimited
    scope: tenant|workspace|sa
```

**Projection contract** (`internal/controller/guardrail/mcproute_projector.go`):

1. Projector reads all `GuardrailBinding` objects for a tenant (label selector
   `keese.ai/tenant=<T>`).
2. Expands each `tools[]` entry × `methods[]` into one CEL rule.
3. Sorts: deny rules first, then allow rules; appends catch-all deny last
   (fail-closed per rule 05).
4. Writes the full compiled rule set to ConfigMap
   `keese-mcproute-cel/<tenant>` (namespace `keese-system`).
5. Operator patches `MCPRoute.spec.rules[]` via SSA with
   `fieldOwner: keese-guardrail-controller`.
6. **Atomicity invariant:** the projector emits the complete rule set per
   reconcile. Partial updates are forbidden. If SSA patch fails mid-apply,
   the MCPRoute retains its prior complete state; the projector retries.

**Rule ID scheme:** `<tenant>/<tool>/<method>/<sequence>` — stable across
reconciles for audit log correlation (see Audit log format).

**CEL expression template** for an argument-pattern allow rule:

```
request.mcp.tool == "shell.execute"
  && request.mcp.method == "tools/call"
  && request.mcp.arguments.command.matches("^ls\\s")
```

## Audit log format

Every MCP tool invocation emits OTEL span `keese.mcp.tool_call`:

| Attribute | Value |
|---|---|
| `mcp.tool` | tool name |
| `mcp.method` | JSON-RPC method |
| `mcp.decision` | `allow` or `deny` |
| `mcp.decision_source` | `openfga`, `mcp_cel`, `content_filter`, `rate_limit` |
| `mcp.rule_id` | rule ID from ConfigMap if CEL matched; empty otherwise |
| `keese.tenant` | tenant name |
| `keese.workspace` | workspace name |
| `keese.sa` | service account name |
| `upstream.status_code` | HTTP status if forwarded; absent on deny |
| `duration_ms` | end-to-end latency |

`mcp.arguments` is **never** emitted by default (PII risk). Opt-in per tenant
via `Tenant.spec.auditArgumentsRedacted: true` routes a sanitized copy through
a dedicated OTEL redaction processor before export. Flagged for 24 iter-2 to
carry this field.

Destination: ES index `keese-mcp-audit-*` (30-day ILM) and Loki stream
`{job="keese-mcp", tenant="<T>"}` (≥ 1-year retention). Fan-out via OTEL
collector pipeline (10a); no keese-side dual-write.

## CEL compile-error fallback

**At admission (webhook validates before MCPRoute persist):** a CEL expression
that fails to compile → admission webhook returns `CELCompileError` reason with
HTTP 400; the MCPRoute is rejected and never reaches the gateway. The gateway
never operates on a broken rule set.

**At runtime (compiled expression raises type-error or variable absent):**
fail-closed deny for that specific request. Emit OTEL event `CELRuntimeError`
with `mcp.rule_id`. Increment counter
`keese_mcp_cel_runtime_errors_total{rule_id, tenant}`. Alert threshold: rate >
0.1% of requests for a given rule ID over 5 minutes (catches buggy policies
before they become systematic).

**Explicit prohibition:** allow-all on CEL compile error is forbidden (rule
05). Any proposal to loosen this requires an ADR with architect sign-off.

## MCP rate limiting (three orthogonal layers)

| Layer | Mechanism | Window | 429 header | Fail behavior |
|---|---|---|---|---|
| Short-window token-cost | Envoy `BackendTrafficPolicy` token-cost filter | per-sec / per-min | `x-keese-limit-source: gateway-token-rate` | Fail independently |
| Per-tool MCP rate | `MCPRoute.spec.rules[].rateLimit` (v0.5.x gate; else Envoy ratelimit service) | per-min, scoped to `(tool, sa)` or `(tool, tenant)` | `x-keese-limit-source: mcp-tool-rate` | Fail independently |
| Long-window budget | `TokenBudget` CR (10b) via NATS KV push (05a iter-2 locked) | per-day / per-month | `x-keese-limit-source: token-budget` | Fail independently |

No shared state across layers. Each fails independently and is independently
tunable. If `MCPRoute` v0.5.x does not expose `rateLimit` on rules, the
projector falls back to an Envoy ratelimit service descriptor scoped to
`(tool, sa)` — flagged residual pending Envoy AI GW changelog confirmation.

## Trade-offs

| Option | Chosen | Rationale |
|---|---|---|
| ConfigMap indirection for compiled rules | Yes | Decouples authoring (06) from MCPRoute lifecycle; enables diff-based audit of policy changes |
| Full rule set per reconcile (atomic emit) | Yes | Envoy does not guarantee ordering during incremental rule changes; partial state could admit a request denied by a missing deny rule |
| Deny-first ordering, catch-all deny | Yes | Fail-closed per rule 05; deny rules take O(1) to evaluate for the common (compliant) case |
| Argument pattern in CEL | Yes, gated | Richest policy; depends on `FEATURE_MCP_ARGUMENT_CEL`; projector degrades gracefully |
| Argument audit opt-in | Yes | PII-first default; per-tenant opt-in with redaction processor rather than suppression |

## Failure modes

| Failure | Detection | Mitigation |
|---|---|---|
| CEL compile error at admission | Webhook 400 `CELCompileError` | MCPRoute rejected; prior rules remain active |
| CEL runtime type-error | `CELRuntimeError` event; counter | Fail-closed deny; alert at 0.1% rate |
| Projector ConfigMap write fails | SSA non-nil error | Retry with backoff; MCPRoute retains prior complete state |
| MCPRoute SSA patch fails | Controller error event | Retry; no partial state; 06 `GuardrailBinding` unchanged |
| `FEATURE_MCP_ARGUMENT_CEL` absent | `CELArgumentsUnavailable` event at startup | Argument-pattern rules demoted to deny-all for affected tools |
| Per-tool rateLimit unsupported in v0.5.x | Projector startup probe | Fall back to Envoy ratelimit service descriptor |
| Audit OTEL export fails | Collector retry + buffer | Spans buffered up to 10 min; drop only after buffer full; counter `keese_mcp_audit_drop_total` |
| GuardrailBinding deleted | Projector emits catch-all deny only | Safe default; operator must re-create binding to restore access |

## Upgrade and rollback

Upgrade path: Envoy AI GW chart bumped via `helmfile.yaml` pin; projector
re-reconciles all GuardrailBindings on startup; MCPRoute rules regenerated
atomically. CEL variable schema changes require a projector version bump with
a compatibility shim for deprecated variables (emit `CELVariableDeprecated`
warning for 1 minor version before removal). Rollback: revert `helmfile.yaml`
pin + `make bootstrap-infra`; prior MCPRoute CRD schema accepted by prior
projector; no migration plan required within the same minor version.

## Observability

OTEL span `keese.mcp.tool_call` (see Audit log format). Prometheus metrics:
`keese_mcp_tool_calls_total{tool, tenant, decision, decision_source}`;
`keese_mcp_cel_runtime_errors_total{rule_id, tenant}`;
`keese_mcp_rate_limit_429_total{layer, tool, tenant}`;
`keese_mcp_audit_drop_total{tenant}`.
Alerts: `CELRuntimeErrorSpike` (rate > 0.1% per rule_id, 5 min);
`MCPAuditDropping` (drop counter > 0 for 2 min).

## Refs

- [04a](04a-openfga-authz-model.md) — `tool#can_call` coarse check (upstream of this layer)
- [04c](04c-token-revocation.md) — revocation propagates to MCP layer; fail-closed
- [05a](05a-envoy-ai-gateway-topology.md) — filter chain order; header contract; 3-layer rate limit
- [06](06-guardrailbinding.md) — GuardrailBinding source schema (parallel; see cross-dep flags)
- [10a](10a-otel-topology.md) — OTEL collector fan-out for audit spans
- [10b](10b-token-accounting.md) — TokenBudget long-window layer (inherits 05a decision)
- [24](24-tenant-crd.md) — `Tenant.spec.auditArgumentsRedacted` (flagged for iter-2)
- [../specs/egress-authz-protocol.md](../specs/egress-authz-protocol.md) — CEL schema is source-of-truth
- [../plans/rubric.md](../plans/rubric.md)

## Iteration log

### Iteration 1 — 2026-04-20

| # | Category | Weight | Ratio | Score | Notes |
|---|---|---:|---:|---:|---|
| 1 | Scope clarity | 10 | 1.0 | 10 | Goal in one sentence; bounded by 5 open questions answered. |
| 2 | Architecture fit | 10 | 1.0 | 10 | Layered on 04a (coarse) + 05a (filter chain); no rule violations; ConfigMap indirection idiomatic. |
| 3 | Security posture | 15 | 1.0 | 15 | Fail-closed on compile + runtime error; deny-first ordering; catch-all deny; argument PII default-off; allow-all on error explicitly prohibited. |
| 4 | Automatability | 10 | 0.5 | 5 | Projector controller path named (`mcproute_projector.go`); ConfigMap schema defined; SSA fieldOwner set. Code and tests not yet authored — honest dock. |
| 5 | Verifiability | 15 | 0.5 | 7.5 | Failure modes enumerated; CEL runtime-error alert threshold concrete. No named test files or envtest assertions yet — honest dock. |
| 6 | Failure-mode awareness | 10 | 1.0 | 10 | 8 failure modes with detection + mitigation; FEATURE gate degradation; audit drop; GuardrailBinding deletion. |
| 7 | Context efficiency for Claude | 10 | 1.0 | 10 | Doc ≤ 200 lines; single responsibility; no inline code blobs; cross-refs via links. |
| 8 | Docs quality | 5 | 1.0 | 5 | SPDX header; frontmatter complete; depends includes all 7 listed deps; rollback concrete. |
| 9 | Observability | 5 | 1.0 | 5 | 4 Prometheus metrics; OTEL span with full attribute list; 2 alerts named. |
| 10 | Operational readiness | 10 | 1.0 | 10 | Upgrade path; rollback path; compatibility shim strategy; HA via SSA idempotency. |
| | **Total** | 100 | | **92.5** | |

Verdict: SHIP (92.5 ≥ 90 iter-1 bar). `status` flipped to `current`.

Top gaps:
1. Cat 4: `mcproute_projector.go` controller code not authored — test-engineer / controller-author backlog, pre-gate acceptable.
2. Cat 5: No named test files or envtest assertions — same backlog; blocking for gate open, acceptable pre-gate.
3. Residual: `FEATURE_MCP_ARGUMENT_CEL` gate availability unconfirmed vs Envoy AI GW v0.5.x changelog — requires changelog review before controller-author phase.

Next step: 06 iter-1 must confirm `GuardrailBinding.spec.tools[]` field names (`name`, `methods`, `argumentsPattern`, `deny`, `rateLimit.requestsPerMinute`, `rateLimit.scope`) match this projection contract before 05c iter-2.
