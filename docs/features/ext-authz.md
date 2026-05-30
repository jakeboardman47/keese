<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: feature
category: feature
depends:
  - docs/designs/22-egress-toolbinding.md
  - docs/designs/04a-openfga-authz-model.md
implements_specs: [docs/specs/egress-authz-protocol.md]
implements_plans: [docs/plans/td-p1-03-extauth-and-group-rename.md]
source_refs:
  - cmd/keese-authz/main.go:1-296
  - internal/authz/extauth/check.go:1-109
  - internal/authz/extauth/resolver.go:1-185
  - internal/authz/extauth/subject.go:1-158
  - internal/authz/extauth/audit.go:1-84
  - internal/authz/extauth/match.go:1-30
  - api/authz/v1alpha1/toolbinding_types.go:1-267
  - api/authz/v1alpha1/workspacetool_types.go:1-103
  - internal/rebac/client.go:152-173
related_skills: [controller-authoring]
status: implemented
implemented_in_phase: td-p1-03
last_verified: 2026-05-29
---

# Egress ext_authz (keese-authz)

## Summary

`keese-authz` is the Envoy ext_authz gRPC service (`envoy.service.auth.v3.Authorization`)
that enforces per-request tool authorization on the Envoy AI Gateway egress path. For
every agent request it compiles cluster-scoped `ToolBinding` and namespaced `WorkspaceTool`
CRs into an in-memory routing snapshot, matches the request against that snapshot, extracts
the caller's identity from the projected ServiceAccount JWT, calls
`OpenFGA.Check(user, can_call, tool:<name>)`, and either injects `x-keese-tool` /
`x-keese-workspace` response headers on allow or returns HTTP 403 on deny. A strict
allowlist audit log records every decision without tokens, bodies, or raw header values.

## Behavior

- **gRPC listener**: `keese-authz` binds gRPC on `:9001`; Envoy's `ext_authz` filter
  connects to it. A separate HTTP `/healthz` endpoint runs on `:8081` for kubelet probes.
- **Snapshot refresh**: every 10 s a `trieRefresher` goroutine lists all `ToolBinding` and
  `WorkspaceTool` CRs and calls `Resolver.ApplySnapshot`
  (`internal/authz/extauth/resolver.go:52`). The snapshot is stored in an `atomic.Value`
  for lock-free reads on the hot path. Bindings that fail to compile are dropped; their
  names are logged as `trie binding rejected`.
- **Match resolution**: cluster-scoped `ToolBinding` entries are tried first (first match
  wins); namespaced `WorkspaceTool` entries are tried second, scoped to the workspace's
  namespace. A `WorkspaceTool` with `spec.workspaceRef` set further restricts matches to
  that specific workspace (`resolver.go:150-156`).
- **Tool name composition**: `ToolBinding` → `tool:<toolName>[.<subTool>]`;
  `WorkspaceTool` → `tool:<namespace>.<toolName>[.<subTool>]` (namespace-scoping prevents
  cross-tenant collision) (`resolver.go:176-184`).
- **Subject extraction**: the projected SA token's `sub` claim
  (`system:serviceaccount:<ns>:ksa-<wsuid>`) is parsed into FGA user-id
  `service_account:<sa>` and workspace `WorkspaceID{Namespace, UID}`. Alternatively,
  `subjectFrom: JWTClaim` reads a named JWT claim (`subject.go:63-98`).
- **FGA Check**: `rebac.Client.Check(ctx, user, "can_call", "tool:<name>")` resolves via
  the `tenant_member from allowed_in` computed relation (`rebac/client.go:163-172`).
- **Allowed response**: injects `x-keese-tool: <toolName>` and
  `x-keese-workspace: <ns>/<name>` headers (`main.go:188-198`).
- **Deny response**: returns gRPC `PERMISSION_DENIED` + HTTP 403 body `permission_denied`.
- **Audit log**: one structured log line per request with fields `request_id`, `path`,
  `method`, `binding`, `binding_ns`, `tool`, `user`, `workspace`, `decision`, `reason`,
  `duration_ms`. Allow logged at `V(1)`; deny at `Info`; FGA errors at `Error`
  (`audit.go:35-56`). No tokens, bodies, or raw Authorization header values are logged.
- **Shutdown**: `signal.NotifyContext(…, SIGTERM, SIGINT)` triggers `GracefulStop` on the
  gRPC server within a 10 s drain budget (`main.go:136,160-163`).

## Configuration surface

| Env var | Required | Purpose |
|---|---|---|
| `OPENFGA_API_URL` | yes | HTTP(S) endpoint for the OpenFGA store |
| `OPENFGA_STORE_ID` | yes | UUID of the OpenFGA store |
| `OPENFGA_AUTHORIZATION_MODEL_ID` | yes | UUID of the authorization model |

`ToolBinding.spec` / `WorkspaceToolSpec` key fields — see
`api/authz/v1alpha1/toolbinding_types.go` and `workspacetool_types.go`:

- `spec.match` (`HTTPRouteMatch`): paths (Exact / PathPrefix / RegularExpression), methods,
  headers, query params — multiple path entries are OR'd; header entries are AND'd.
- `spec.toolName`: stable OpenFGA object name; validated `^[a-z][a-z0-9.-]*$`.
- `spec.bodyDiscriminator` (`BodyDiscriminator`): JSONPath + map → sub-tool name (see
  Known Limitations).
- `spec.subjectFrom` (`ServiceAccountSubject` | `JWTClaim`), `spec.jwtClaimName`.
- `spec.workspaceFrom` (`ServiceAccountName` | `JWTClaim`).
- `spec.workspaceRef` (`WorkspaceTool` only): pins binding to a specific workspace.

## Observability

- **Audit log fields** (structured zap): `decision` (`allow` | `deny`), `reason`
  (`allowed` | `no_binding_matched` | `subject_extraction_failed` | `openfga_denied` |
  `openfga_check_error`), `duration_ms` — emitted for every request (`audit.go:19-31`).
- **Status subresource**: `ToolBindingStatus` / `WorkspaceToolStatus` carry
  `conditions[]` (type `Ready`) and `observedGeneration`. `MatchedRequests` counter field
  is present in the schema but not yet incremented (no controller).
- **No Prometheus metrics**: keese-authz exports no `/metrics` endpoint; the
  `metricsserver.Options{BindAddress: "0"}` line explicitly disables it (`main.go:95`).

## Known limitations

- **No ToolBinding or WorkspaceTool controller**: the CRD types exist in
  `api/authz/v1alpha1/` but no reconciler is implemented. Status conditions are never
  updated; `observedGeneration` stays at zero. The trie reads CRs directly via 10 s
  polling — not informers — so there is up to a 10 s propagation delay after a CR change.
- **No Prometheus metrics**: authorization decisions are observable only through the
  structured audit log. There is no `/metrics` endpoint and no request-rate or latency
  histogram.
- **Body discriminator not wired end-to-end**: `BodyDiscriminator` is modeled in the CRD
  and compiled into the `CompiledMatch` struct but the Envoy `ext_authz` filter is not
  configured with `with_request_body: true`, so the body arrives empty and sub-tool
  discrimination never fires.
- **Credential injection is owned by `BackendSecurityPolicy`, not Lua**: agent pods carry
  a projected SA token in `Authorization: Bearer <jwt>`. The `keese-defense` Lua filter
  (`dev/bootstrap/aigateway/keese-defense-lua-patch.json`) **strips** all inbound
  credential headers (`authorization`, `x-api-key`, `anthropic-api-key`) as a
  defense-in-depth measure (rule 05.2) — it does not rewrite or inject. The upstream
  `x-api-key` header is injected downstream by the `BackendSecurityPolicy`
  `AnthropicAPIKey` type via the AI Gateway's extProc. keese-authz itself performs
  neither the strip nor the injection.
- **JWT signature verification delegated to Envoy**: keese-authz decodes the JWT payload
  without verifying the signature (`subject.go:144`). The `jwt_authn` Envoy filter
  upstream of ext_authz is the signature-verification authority.

## Change history

- `td-p1-03` (2026-05): initial implementation — gRPC ext_authz server, ToolBinding +
  WorkspaceTool CRDs, OpenFGA Check integration, structured audit log, 10 s trie refresh.

## References

- Design: `docs/designs/22-egress-toolbinding.md`, `docs/designs/04a-openfga-authz-model.md`
- Spec: `docs/specs/egress-authz-protocol.md`
- Plan: `docs/plans/td-p1-03-extauth-and-group-rename.md`
- Source: `cmd/keese-authz/main.go`, `internal/authz/extauth/`, `internal/rebac/client.go`,
  `api/authz/v1alpha1/toolbinding_types.go`, `api/authz/v1alpha1/workspacetool_types.go`
