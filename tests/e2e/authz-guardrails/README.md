<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: reference
category: testing
status: shipped-with-stubs
last_verified: 2026-06-09
revisit_when_guardrail_extproc_live: true
---

# tests/e2e/authz-guardrails/ — GuardrailBinding + WorkspaceTool e2e (EH5)

Covers the `authz.keese.ai` tooling/guardrail surface end-to-end against a
live cluster: the **shipped** GuardrailBinding reconciler
(`internal/controller/authz/guardrailbinding_controller.go`), the
default-inherit invariant (`docs/designs/06-guardrailbinding.md`), and the
WorkspaceTool → OpenFGA tuple → live ext_authz allow/deny decision.

## Reconciler reality (read before extending)

The EH5 plan named three reconcilers
(`guardrailbinding`, `toolbinding`, `workspacetool`). Only **one ships**:
`GuardrailBindingReconciler`. `ToolBinding` and `WorkspaceTool` are **not**
reconciled into tuples by a controller — they are request-time catalogue
objects consumed by `keese-authz` ext_authz
(`internal/authz/extauth/resolver.go`) to map an HTTP request to a
`tool:<name>` object. The **authorizing** tuple
(`tool:<name>#allowed_in@workspace:<ws>`) is written by the **Workspace**
controller from `spec.egress.allowedTools`
(`internal/controller/keese/workspace_controller.go`). This suite therefore
drives the tool allow/deny through `WorkspaceTool` resolution + a Workspace
`allowedTools` grant — the real shipped path — not a non-existent
WorkspaceTool reconciler.

## What it asserts

| Step | Case | Assertion |
|---|---|---|
| `01` | `status-*` | `eh5-tenant` + `eh5-workspace` reach `Phase=Ready` / `Ready=True`; `status.observedGeneration == metadata.generation`; `effectivePolicy` stamped (rule 04.4). |
| `01` | `default-inherit` | Workspace binding `ParentReadable=True`; merged `effectivePolicy.tools.deny` is the **union** of inherited tenant deny (`net.raw`) + workspace deny (`browser.navigate`) — the parent restriction is not dropped (design 06). |
| `01` | `events-*` | Controller emits finite-table reasons `BindingMerged` + `EffectivePolicyComputed` (rule 04.11). |
| `02` | `grant-allow` | Granting `alpha.eh5-search` on `my-ws.egress.allowedTools` writes the `allowed_in` tuple; `GET /eh5-search` → **HTTP 200**. |
| `02` | `revoke-deny` | Removing the grant deletes the tuple; re-firing flips **allow→deny within the ext_authz cache TTL → HTTP 403** (fail-closed). |
| `02` | `guardrail-block` | **SKIPPED** unless `GUARDRAIL_EXTPROC=1` — see stub below. |

## Stub: gateway-side guardrail ext_proc

The gateway-side guardrail enforcement (Presidio / LlamaGuard `ext_proc`)
is **not live** in the bootstrap. The `guardrail-block` case is skipped and
this suite is `status: shipped-with-stubs` with trigger
**`revisit_when_guardrail_extproc_live`**. Set `GUARDRAIL_EXTPROC=1` once
the ext_proc filter is deployed to enable it. The live CRD-reconcile +
tuple layers (steps 01 + 02 cases 1–2) are covered in full.

## Steps

| File | Kind | Purpose |
|---|---|---|
| `guardrails.yaml` | fixtures | Tenant + workspace GuardrailBindings (scope chain, explicit inherit). |
| `workspacetool.yaml` | fixture | Namespaced WorkspaceTool `eh5-search` → `tool:alpha.eh5-search`. |
| `00-apply.yaml` | TestStep | Apply the fixtures. |
| `00-assert.yaml` | TestAssert | Prereq gates (`check-prereqs` + `check-extauth`); wait bindings + `my-ws` Ready. |
| `01-guardrail-status.yaml` → `test-authz-guardrails.sh` | TestStep | Status / observedGeneration / default-inherit / event reasons. |
| `02-tool-allow-deny.yaml` → `test-tool-allow-deny.sh` | TestStep | WorkspaceTool tuple → allow(200)/revoke-deny(403); guardrail ext_proc (skipped). |

## Reuse (no copies)

The request-firing primitives are sourced from
[`../lib/fire-request.sh`](../lib/fire-request.sh) — the EH4 pattern
extracted into `lib/` (additive) so EH5 reuses it without copying or
editing EH4's `tests/e2e/rebac-decision/test-rebac-decision.sh`. The
prereq gates (`../lib/check-prereqs.sh`, `../lib/check-extauth.sh`) are the
same gates EH4 uses.

## Prerequisites

Reuses tenant `alpha` + `my-ws` from `dev/demo/hello-keese.yaml` and the
operator's bootstrap cluster-default GuardrailBinding
(`config/default/bootstrap/guardrailbinding-cluster-default.yaml`). Run the
standard bootstrap first:

```sh
make kind-up
make bootstrap-infra
kubectl apply -f dev/demo/hello-keese.yaml
```

## Run

```sh
make test-e2e            # includes this suite (kuttl globs tests/e2e/)
# or just this case:
kubectl-kuttl test --config tests/e2e/kuttl-config.yaml --test authz-guardrails
```
