<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: design
category: api
depends:
  - 05a-envoy-ai-gateway-topology.md
  - 04a-openfga-authz-model.md
  - 20a-api-group-layout.md
related_skills: [crd-authoring, controller-authoring, doc-authoring]
status: current
last_verified: 2026-05-06
rollback: |
  Both kinds are v1alpha1; rollback is delete-CRD + reapply-old-CRD
  on the demo cluster. No conversion webhook at v1alpha1 (rule
  04.13). Keese-authz binary continues to function with no bindings
  (every request is "no-match → permission_denied"); operators can
  flip the gateway's SecurityPolicy.spec.extAuth.failOpen to true to
  short-circuit the authz pipeline during incident recovery.
---

# 22 — Egress ToolBinding: HTTP request → OpenFGA `tool:<name>`

**Decision:** Two new CRDs in `authz.keese.ai/v1alpha1`:

- **`ToolBinding`** (cluster-scoped) — platform-admin catalogue of
  HTTP request → tool name mappings shared across tenants.
- **`WorkspaceTool`** (namespaced) — tenant-admin per-workspace
  bindings for internal APIs the platform catalogue does not know
  about.

Both compile into the same in-memory routing trie inside the
`keese-authz` ext_authz service (TD-P1-03 §C). Cluster matchers are
tried first; namespace matchers fire only for requests whose subject
resolves to a workspace inside that namespace.

## Why two kinds, not one

Platform admins own *naming*: stable, long-lived strings like
`tool:anthropic.messages.opus-4`. These are shared across tenants
and must not be overwritten by per-tenant changes. Cluster scope is
the standard k8s answer.

Tenants own *their* tools: an internal API the platform doesn't
know about, registered ad-hoc into a namespace. Namespace scope
keeps these per-tenant and limits the blast radius of a typo
(can't break another tenant's auth).

The two kinds share most of the spec shape — request matcher, body
discriminator, subject extractor — so callers see one API surface
with two scopes. The keese-authz controller compiles both into a
single trie and tries cluster bindings first.

## Why a CRD, not config

Three reasons a CRD beats a ConfigMap or operator flag:

1. **Reconcilable lifecycle.** Status conditions, observability,
   and admission webhooks all work via standard k8s machinery. A
   ToolBinding can `Status.Ready=False` if its trie compilation
   fails on a malformed JSONPath.
2. **RBAC.** Platform admins delegate `WorkspaceTool` CRUD to
   tenant admins via standard `keese-authz-admin` ClusterRole +
   per-namespace RoleBinding. ConfigMaps don't carry the same
   field-level RBAC.
3. **Diff-friendly review.** Teams already review CRD changes via
   GitOps; tool bindings join that workflow. ConfigMap blob review
   is murkier.

## Spec shape (both kinds, identical except for scope + workspaceRef)

```yaml
apiVersion: authz.keese.ai/v1alpha1
kind: ToolBinding                # or WorkspaceTool
metadata:
  name: anthropic-messages
spec:
  match:                          # Gateway API HTTPRouteMatch shape
    paths:    [{type: Exact, value: /anthropic/v1/messages}]
    methods:  [POST]
    headers:
      - {name: x-ai-eg-model, type: Exact, value: claude-opus-4-7}
    queryParams: []
  toolName: anthropic.messages   # → tool:anthropic.messages
  bodyDiscriminator:             # optional — extracts sub-tool name
    jsonPath: $.model            # restricted: $.f or $.f.g; no wildcards
    map:
      claude-opus-4-7:   opus-4   # → tool:anthropic.messages.opus-4
      claude-sonnet-4-6: sonnet-4
      claude-haiku-4-5:  haiku-4
    default: ""                   # empty → no sub-tool, fall back to toolName
  subjectFrom: ServiceAccountSubject  # ServiceAccountSubject | JWTClaim
  jwtClaimName: ""                    # required when subjectFrom=JWTClaim
  workspaceFrom: ServiceAccountName   # ServiceAccountName | JWTClaim
```

The `WorkspaceTool` spec adds:

```yaml
  workspaceRef:                   # optional; empty = match any in namespace
    name: my-ws
```

and the final tool name resolves with a namespace prefix:
`tool:<namespace>.<toolName>` (e.g., `tool:alpha.internal-search`).

## Selector grammar — Gateway API HTTPRouteMatch subset

The match shape is intentionally a strict subset of Gateway API's
`HTTPRouteMatch`. We support: paths (Exact / PathPrefix /
RegularExpression), methods, headers (Exact / RegularExpression),
query params (Exact / RegularExpression). We do **not** invent new
match types (cookie, fragment, path templates) — the gateway team
already standardized this surface, and reusing it means our CRD is
familiar to anyone who has authored an HTTPRoute.

The body discriminator is the one place we add a non-Gateway-API
surface — Gateway API does not match on body. The discriminator is
deliberately narrow: a single JSONPath subset (`$.field` or
`$.parent.child`), no wildcards or filters, and a static map. This
prevents turning the hot path into a JSON-eval engine.

## Resolution algorithm (keese-authz)

```
fn ResolveTool(req) -> (toolName, allow) {
    // 1. Cluster ToolBindings — first match wins.
    for tb in clusterToolBindings { if matches(req, tb) { return finalName(tb), true } }

    // 2. Namespace WorkspaceTools — first match wins, but ONLY if the
    //    request's resolved workspace lives in the WorkspaceTool's namespace.
    ws := extractWorkspace(req)
    for wt in workspaceToolsIn(ws.namespace) {
        if (wt.spec.workspaceRef == nil || wt.spec.workspaceRef == ws.name) &&
           matches(req, wt) {
            return ws.namespace + "." + finalName(wt), true
        }
    }
    return "", false  // no-match → keese-authz returns DENY with audit
                      // log "(unmatched request)" — fail closed.
}
```

`matches(req, tb)` evaluates path/method/header/query AND-style
within a single rule, OR-style across multiple paths/methods. Body
discriminator reads `req.body` (Envoy's `with_request_body`
buffering provides this).

## Subject + workspace extraction

| `subjectFrom` | extractor |
|---|---|
| `ServiceAccountSubject` (default) | parse the projected SA token's `sub` claim, expected `system:serviceaccount:<ns>:<sa>`. user-id is the full subject string. |
| `JWTClaim` | read `spec.jwtClaimName` from the SA token. user-id is the claim string value. |

| `workspaceFrom` | extractor |
|---|---|
| `ServiceAccountName` (default) | parse SA name `ksa-<wsuid>` (keese controller's deterministic SA naming). |
| `JWTClaim` | read `spec.jwtClaimName` from the SA token (same name as subject claim — caller picks). |

JWTClaim mode supports per-tenant identity templates (D04b
`audienceTemplates`) where workspaces inject custom claims. The
default ServiceAccountName mode covers the demo + every tenant
shipping the standard keese-controller SA naming.

## Status, observability, and admission

`status.conditions[Ready]`:

- `True / RuleCompiled` — trie includes this binding.
- `False / InvalidJSONPath` — bodyDiscriminator JSONPath rejected.
- `False / DuplicateMatch` — two cluster ToolBindings matched the
  same `(path, method, headers, queryParams)` tuple. Last-applied
  wins; the loser's Ready=False makes the conflict visible.
- `False / DuplicateToolName` — two bindings name the same final
  tool string. Allowed for WorkspaceTool (per-namespace) but
  forbidden cluster-side; keese-authz uses last-applied.

Per-binding hit counter: `status.matchedRequests`. Useful for
detecting orphan bindings.

OTEL spans on every Resolve call: `authz.tool_resolved` with
`(toolName, finalToolName, workspace, decision)`. Never the raw
request body, never the SA token — rule 02 + spec §10.

Admission (post-P1, when VAP wiring lands per rule 04.12):

- VAP `ToolBindingNameImmutable` — toolName is immutable post-create
  (the trie keys it; mutation breaks every existing
  `tool:<n>#allowed_in@workspace:<w>` tuple).
- VAP `BodyDiscriminatorJSONPathSyntax` — JSONPath must parse
  against the restricted grammar above.

## Compatibility with `Workspace.spec.egress.allowedTools[]`

A new `Workspace` field `spec.egress.allowedTools[]` (string list)
declares which tool names a workspace's session pods may call. The
Workspace reconciler writes one OpenFGA tuple per element:

```
tool:<name>#allowed_in@workspace:<workspace-uid>
```

The keese-authz `Check` then asks OpenFGA whether the requesting
user has `can_call` on `tool:<name>` — and the model resolves
`can_call` from `tenant_member from allowed_in`, so the check
succeeds only when the workspace's tenant has the SA as a member
AND the workspace has the tool in its `allowed_in` set.

This closes the orphan-tuple gap surfaced during TD-P1-01 (the
`tool:` type was declared in the FGA model but no controller wrote
its tuples).

## Iteration log

| Iter | Date | Focus | Score |
|---|---|---|---:|
| 1 | 2026-05-06 | Correctness & security | 92.5 |
| 2 | 2026-05-06 | Performance & quality | 95.0 |
| 3 | 2026-05-06 | Operational readiness | 95.0 |

## Refs

- [docs/specs/egress-authz-protocol.md](../specs/egress-authz-protocol.md) — Check-tuple shapes
- [docs/designs/05a-envoy-ai-gateway-topology.md](05a-envoy-ai-gateway-topology.md) — gateway extAuth wiring
- [docs/designs/04a-openfga-authz-model.md](04a-openfga-authz-model.md) — `tool` type + `can_call` relation
- [docs/plans/td-p1-03-extauth-and-group-rename.md](../plans/td-p1-03-extauth-and-group-rename.md) — execution plan
