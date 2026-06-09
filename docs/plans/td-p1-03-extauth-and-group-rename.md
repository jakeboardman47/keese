<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: plan
category: execution
depends:
  - ../designs/20-api-group-layout.md
  - ../designs/05a-envoy-ai-gateway-topology.md
  - ../specs/egress-authz-protocol.md
  - demo/tech-debt.md
related_skills: [plan-management, controller-authoring, doc-authoring]
status: current
last_verified: 2026-05-06
---

# TD-P1-03 — ext_authz + 3-group API layout

## Goal

Land the OpenFGA `ext_authz` integration (TD-P1-03) on the 3-group API
layout — **3 groups mirroring Kubernetes' `core` / `rbac` / `policy`
shape**:

| Group | Kinds | Mirrors |
|---|---|---|
| `keese.ai/v1alpha1` | Tenant, Workspace, WorkspaceSession, WorkspaceShare, Memory, SharedMemory, Recipe, RecipeSource, AgentRuntime, RuntimeExtension, Transport, Workflow, WorkflowRun | k8s `core/v1` |
| `authz.keese.ai/v1alpha1` | OIDCProvider, CrossTenantAgreement, GuardrailBinding, **ToolBinding** (new, cluster), **WorkspaceTool** (new, namespaced) | k8s `rbac.authorization.k8s.io/v1` |
| `policy.keese.ai/v1alpha1` | TokenBudget | k8s `policy/v1` |

The `.operator.` segment in every group name is dropped; no upstream
uses it (`cert-manager.io`, `gateway.networking.k8s.io`, …).

## Sequencing rationale

Doing the new ext_authz CRDs (`ToolBinding`, `WorkspaceTool`) first
in the old layout would land them in a soon-to-be-renamed group —
churn we'd pay twice. Phase A (rename) lands first, then Phase B
adds the new kinds in their final home, then Phase C/D wires the
keese-authz service.

## Phases

### Phase A — Group consolidation (10 → 3)

Touches 267 files (~80 are generated and regenerable via
`make manifests`). Execute in this order — every step preserves a
working build:

1. **A1**: Author this plan + flip
   [docs/designs/20-api-group-layout.md](../designs/20-api-group-layout.md)
   wholesale to describe the new layout. Rule update:
   [.claude/rules/04-kubernetes.md](../../.claude/rules/04-kubernetes.md)
   §1 "Group domain" gets the new 3-group set + drops `.operator.`.
2. **A2**: Move Go packages under `api/`. Each group merges into a
   single `api/<newgroup>/v1alpha1/` directory:
   - `api/keese/v1alpha1/` ← merge of `workspace`, `workflow`,
     `runtime`, `memory`, `recipe`, `transport`, plus `tenancy`'s
     Tenant.
   - `api/authz/v1alpha1/` ← extend with `tenancy.CrossTenantAgreement`
     and `guardrail.GuardrailBinding`.
   - `api/policy/v1alpha1/` ← rename of today's `observability`.
3. **A3**: Update `groupversion_info.go` in each new group with the
   new GroupVersion (`keese.ai`, `authz.keese.ai`, `policy.keese.ai`).
4. **A4**: Mass import-rewrite across `internal/controller/`,
   `cmd/main.go`, all `*_test.go`. Pattern:
   `<oldgroup>v1alpha1` → the new group's package alias. Where two
   alias names collide, prefix imports (e.g.
   `keesev1alpha1.Workspace`, `authzv1alpha1.GuardrailBinding`).
5. **A5**: Move controllers under `internal/controller/` to mirror
   the new group structure:
   - `internal/controller/keese/` ← workspace, workflow, runtime,
     memory, recipe, transport, tenancy.Tenant.
   - `internal/controller/authz/` ← oidcprovider,
     crosstenanagreement, guardrailbinding, toolbinding (new),
     workspacetool (new).
   - `internal/controller/policy/` ← tokenbudget.
6. **A6**: `make manifests` to regenerate
   `config/crd/bases/`, `config/rbac/`, `bundle/manifests/`.
   Bulk-delete the stale generated files first; the regenerator
   produces clean output.
7. **A7**: Update kustomize collateral:
   `config/default/kustomization.yaml`,
   `config/manifests/kustomization.yaml`, `config/samples/`,
   `dev/demo/`. Each sample's `apiVersion:` line moves to its new
   group.
8. **A8**: Update OLM bundle: `bundle.Dockerfile` references; CSV
   `customresourcedefinitions.owned[]` block; `ANNOTATIONS.yaml`
   if it carries the group set.
9. **A9**: Update docs:
   - `docs/specs/`: rename per-group spec files; update
     frontmatter `depends`. The 12 spec files collapse to 3.
   - `docs/designs/`: text references to the old per-subgroup domain
     bulk-replaced with the 3-group names.
   - `docs/plans/README.md`, `docs/plans/demo/*.md` task tables.
   - `CLAUDE.md` task table rows that name groups.
   - `MEMORY.md` per-group entries get a one-line note pointing to
     this plan.
   - `README.md` tech stack table.
10. **A10**: Tests. Three classes:
    - Unit tests under `internal/controller/<group>/*_test.go`:
      mostly fixed by import rewrite.
    - Envtest suites: `suite_test.go` files load CRDs from
      `config/crd/bases/`; the path stays the same so suites work
      after `make manifests`.
    - kuttl `tests/e2e/aigw-defense/`: each test step's `apiVersion:`
      moves.
11. **A11**: Rename `PROJECT` `resources[].api.crdVersion` group
    fields. operator-sdk regenerates many things from this; check
    it agrees with what `make manifests` produced.
12. **A12**: In-cluster migration: the rename CANNOT use a
    conversion webhook (rule 04.13: "no conversion webhooks at
    v1alpha1"). Migration is delete-old-CRDs + apply-new-CRDs +
    reapply CRs. Demo-cluster only; no production blast radius.
    Document the steps in `docs/references/migration-rename-2026-05.md`
    so future contributors can replay.

**Acceptance**: `go build ./...` clean, `go test -short ./...`
green, `make manifests` clean, `kubectl apply -f dev/demo/hello-keese.yaml`
returns Tenant+Workspace+WorkspaceSession Ready=True against a
fresh cluster (or against the existing cluster after the migration
steps in A12).

### Phase B — New CRDs in their final home

13. **B1**: Author
    `api/authz/v1alpha1/toolbinding_types.go` (cluster-scoped) and
    `api/authz/v1alpha1/workspacetool_types.go` (namespaced).
    Selector shape mirrors Gateway API `HTTPRouteMatch`
    (`{path, method, headers, queryParams}`) plus an optional
    `bodyDiscriminator{jsonPath, map}` for sub-API names. Both
    accept `subjectFrom: serviceAccountSubject | jwtClaim:<name>`.
14. **B2**: Author the design and spec:
    - `docs/designs/22-egress-toolbinding.md` — design
      (3 iterations to ≥ 90).
    - `docs/specs/authz.keese.ai-v1alpha1.md` — combined spec for
      every authz kind (OIDCProvider, CrossTenantAgreement,
      GuardrailBinding, ToolBinding, WorkspaceTool).
15. **B3**: Controllers under `internal/controller/authz/`:
    - `toolbinding_controller.go` — watches both ToolBinding and
      WorkspaceTool, compiles the in-memory routing trie, exposes
      a `ResolveTool(req) → (toolName, allowed bool)` snapshot via
      a thread-safe atomic.Value. The keese-authz service reads
      from this snapshot.
16. **B4**: Workspace controller extension: `Workspace.spec.egress.allowedTools[]`
    (string list). Reconciler writes
    `tool:<name>#allowed_in@workspace:<wsuid>` per element on Sync,
    deletes on cleanup. Closes the orphan-tuple gap from the
    TD-P1-01 inventory.
17. **B5**: `make manifests`, regen samples, update demo
    `hello-keese.yaml`.

**Acceptance**: ToolBinding + WorkspaceTool CRs install on a fresh
cluster; the toolbinding controller logs the trie compilation; new
sample CRs in `config/samples/` pass `kubectl apply --dry-run=server`.

### Phase C — keese-authz ext_authz service

18. **C1**: New binary `cmd/keese-authz/main.go`. gRPC server
    implementing Envoy's `envoy.service.auth.v3.Authorization`
    (`Check`). Listens on `:9001`. Reads tool-resolver snapshot
    via `internal/controller/authz/resolver.go` shared package.
19. **C2**: `internal/authz/extauth/`:
    - `match.go` — Gateway-API style HTTPRouteMatch evaluator
      against `CheckRequest.attributes.request.http`.
    - `subject.go` — extract user (SA subject parse OR JWT claim
      lookup) + workspace (SA name `ksa-<wsuid>` parse OR header
      injection by upstream filter).
    - `check.go` — call `internal/rebac.Client.Read(ctx, "tool:<n>",
      "can_call", "user:<subject>")` (or `Check` once we add it).
      Currently the rebac.Client only exposes Write/Delete/Read;
      add a `Check(ctx, user, relation, object) (bool, error)`
      thin wrapper around the SDK's `Check` API.
    - `audit.go` — structured log of `(tool, workspace, decision,
      upstream_status)` per spec §10. Never tokens; never bodies.
20. **C3**: Image: `Dockerfile.keese-authz` (separate from the
    operator) producing `keese-authz:demo`. Distroless static.
21. **C4**: Bootstrap manifest
    `dev/bootstrap/aigateway/keese-authz.yaml`:
    - Deployment (1 replica; 100m / 128Mi requests)
    - Service `keese-authz.keese-system.svc:9001`
    - Envoy Gateway `SecurityPolicy` with `spec.targetRefs`
      pointing at the AIGatewayRoute (or the Gateway) and
      `spec.extAuth.grpc{backendRefs: [keese-authz]}`.
22. **C5**: Update `dev/demo/hello-keese.yaml` —
    `spec.egress.allowedTools: [anthropic.messages]` on the
    Workspace; new `ToolBinding/anthropic-messages` cluster CR in
    bootstrap so the path → tool name mapping is in place at
    cluster install time.

**Acceptance**: with the demo workspace's `allowed_in` tuple
present, `goose run --recipe …` returns 200 from the gateway.
Removing the tuple manually (`fga tuple delete …`) makes the same
call return 403 from the gateway with a `permission_denied` body
and a single audit log line on the keese-authz Pod (no token, no
body). The 4-case `make test-e2e-aigw-defense` test still passes.

### Phase D — Verify + capture

23. **D1**: End-to-end smoke captured to `.plan-logs/`.
24. **D2**: Update `docs/plans/demo/tech-debt.md`: close TD-P1-03;
    open one or two follow-on TDs the rename + ext_authz exposed.

## Continuation: remaining P1 backlog

After this lands, work continues in the order the user already
chose ("in order"):

- **TD-P1-04** — Cosign-verify ValidatingWebhook on InstallPlans.
  Requires the OLM 14a F7 design as starting point; ships as a
  webhook in `cmd/keese-cosign-webhook/` (consistent with Phase
  C's pattern of breaking helper services out of the main operator
  binary).
- **TD-P1-05** — CI-built signed bundle replacing the local-build
  fallback. Wires `cosign sign --keyless` into the OLM bundle
  release pipeline.
- **TD-P1-06** — Workspace controller predicate ADR
  (`keese.ai/managed: "true"`). One-paragraph ADR + apply-or-revert
  decision.
- **TD-P1-07** — Kuttl Tenant→Workspace→WorkspaceSession progression
  case + `kuttl` in `flake.nix`.
- **TD-P1-09** — sqlite single-pod-per-Memory invariant. Doc-only
  in [docs/specs/keese.keese.ai-v1alpha1.md](../specs/) (or split-out
  memory subspec) describing the constraint; admission VAP to
  enforce it follows in TD-P2-08.
- **TD-P1-10** — `dev/bootstrap/install-crds.sh` automation. Wire
  into the helmfile `hooks: prepare` so chart bumps don't strand
  CRDs. Closes the EG v1.7 / v1.6 / v1.4 paper-cut.
- **TD-P1-11** — WorkspaceSession reconciler watches: add
  `Owns(&corev1.Pod{})` + `Owns(&corev1.PersistentVolumeClaim{})`
  and broaden the predicate to fire on `keese.ai/poke*`
  annotations. Recovers the "delete pod + reapply session" pain
  documented during TD-P1-02 verification.

Plus the TD-P1-01 / TD-P1-02 follow-ons (workflow type in
model.fga, openfga seed image, operator-side Drain wiring,
remaining SPI methods, pod-name plumbing).

## Risk register

| Risk | Mitigation |
|---|---|
| 267-file rename leaves a half-renamed tree on session boundary | Each step in Phase A keeps `go build ./...` clean before commit; resumable |
| CRD migration breaks the running demo cluster | Phase A12 runs against the demo cluster only; production gate is closed (rule 14) |
| Generated bundle assets drift from CSV | `make bundle-validate` runs in Phase A6 |
| New API groups conflict with the prior rule 04.1 domain requirement | Rule 04.1 is rewritten in step A1; ADR is this plan |
| ext_authz service introduces a new SPOF in front of every LLM call | One replica is fine for demo; production requires HA + circuit breaker (TD-P2 follow-on) |
| Body-discriminator JSON parsing requires Envoy `with_request_body` config — increases latency | Spec budget is p99 ≤ 50 ms; measure during Phase D verify, escalate to TD-P2 if breached |

## Estimate

- Phase A: 1–2 sessions of mechanical rename work
- Phase B: 1 session (CRD authoring + design + spec)
- Phase C: 1 session (service + manifest + tests)
- Phase D: ½ session (verify + capture)

Total: roughly 4 sessions to fully close TD-P1-03 with the rename
folded in.
