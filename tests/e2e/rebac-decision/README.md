<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: reference
category: testing
status: current
last_verified: 2026-06-09
---

# tests/e2e/rebac-decision/ — live ReBAC allow/deny (EH4)

Keystone authz e2e. Proves the running `ext_authz` (`keese-authz`) makes a
**CRD-driven** allow-vs-deny decision through the live cluster, against the
real OpenFGA store — not a model unit test. Decision code under test:
`internal/authz/extauth/{check,resolver,subject}.go` + `internal/rebac/`.

## What it asserts

| Case | Assertion |
|---|---|
| `allow-granted` | `my-ws` (grants `anthropic.messages`) fires a request through the gateway → **HTTP 200** (ext_authz allowed). |
| `deny-ungranted` | `deny-ws` (empty `egress.allowedTools`, no tuple) fires the identical request → **HTTP 403** (fail-closed). |
| `deny-audit-clean` | The `keese-authz` deny audit line captures `(tuple/tool, SA/user, workspace, decision=deny)` with **no token / no request body** (rule 05.10). |
| `revoke-flip` | Clearing `my-ws.spec.egress.allowedTools` revokes the grant; re-firing flips **allow→deny within the ext_authz cache TTL**. |

## How the allow/deny split works

`Authorize` resolves a ToolBinding from the request path, extracts the SA
subject from the Bearer JWT, then calls
`fga.Check(service_account:<sa>, can_call, tool:<finalToolName>)`. OpenFGA
grants only when both tuples exist:

- `tenant:<t>#member@service_account:<sa>` — Workspace controller.
- `tool:<n>#allowed_in@workspace:<wsuid>` — written per `egress.allowedTools`
  element.

`deny-ws` carries an empty `allowedTools`, so the second tuple is never
written and the Check fails closed.

## Steps

| File | Kind | Purpose |
|---|---|---|
| `00-apply.yaml` → `deny-workspace.yaml` | TestStep | Provision `deny-ws` + its session under tenant `alpha`. |
| `00-assert.yaml` | TestAssert | Prereq gate (`check-prereqs.sh` + `check-extauth.sh`); wait both workspaces Ready. |
| `01-test.yaml` → `test-rebac-decision.sh` | TestStep | Fire requests; assert allow/deny/audit/revocation. |

## Prerequisites

Reuses `my-ws` from `dev/demo/hello-keese.yaml` (the allow side). Run the
standard bootstrap first:

```sh
make kind-up
make bootstrap-infra
kubectl apply -f dev/demo/hello-keese.yaml
```

Then a seeded OpenFGA store (`scripts/dev/seed-openbao.sh` +
`dev/bootstrap/openfga/seed.yaml`). Both prereq gates skip cleanly when
there is no kubectl context and fail-closed when infra is placeholder.

## Run

```sh
make test-e2e            # includes this suite (kuttl globs tests/e2e/)
# or just this case:
kubectl-kuttl test --config tests/e2e/kuttl-config.yaml --test rebac-decision
```
