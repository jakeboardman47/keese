<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: reference
category: testing
status: shipped-with-stubs
last_verified: 2026-06-09
---

# tests/e2e/token-budget/ — TokenBudget enforcement (EH7)

Proves `policy.keese.ai/TokenBudget` is enforced end-to-end. The reconciler
(`internal/controller/policy/{tokenbudget_controller.go,ratelimit.go,ratelimit_client.go}`)
projects a token-cost budget onto the Envoy AI Gateway as a
`BackendTrafficPolicy` (`keese-tb-<uid>-<model>`) whose `LocalRateLimit`
ceiling tracks the budget's `RemainingTokens`.

## What it asserts

| Step | Case | Assertion | Live by default? |
|---|---|---|---|
| 1 | `projection` | TokenBudget reaches Ready; the projected `BackendTrafficPolicy` exists for `(uid, model)` with `keese.ai/tokenbudget-scope-id`/`-model` annotations and a `LocalRateLimit` rule (x-keese-scope selector + numeric per-second ceiling) reflecting the budget. | yes |
| 2 | `in-budget-200` | A request fired through the gateway under the cap → **HTTP 200** (RemainingTokens > 0 → ceiling > 0). | yes |
| 3 | `over-budget-429` | Driving consumption past `totalTokens` → **HTTP 429** + `status.phase=Exhausted`. | **metering-gated** |
| 4 | `post-reset-200` | After the window boundary the budget recovers: `status.windowStart` advances and a request 200s again. | **metering-gated** |

## Shipped-with-stubs: the metering gate

Over-budget enforcement (steps 3-4) requires the **token-cost metering**
pipeline: the OTEL processor that emits `keese_token_budget_consumed_total`,
which the controller queries (`queryConsumed`) to compute consumed tokens and
drop `RemainingTokens` to 0 (a 0-rps ceiling → 429). The local bootstrap does
**not** wire this pipeline (the controller falls back to
`FakePrometheusQuerier`, reporting zero consumption), so consumption can't be
driven past the cap.

`../lib/check-metering.sh` detects this and `enforce.sh` **skips steps 3-4
cleanly** when the pipeline is absent. Tracking trigger:
`revisit_when_token_metering_live`. When metering lands, the same steps run
unmodified and assert 429 + exhaustion + window-reset recovery.

## Reuse

`enforce.sh` reuses EH4's request-firing helper
(`../rebac-decision/test-rebac-decision.sh`) **by sourcing** its function
definitions (everything above its `── Run ──` marker, via process
substitution) — `fire_request` / `mint_sa_token` / `assert_status` /
`poll_status` / `warm_up_gateway`. No copy, no edit to EH4, and EH4's own
suite never runs.

## Steps

| File | Kind | Purpose |
|---|---|---|
| `00-apply.yaml` → `budget.yaml` | TestStep | Apply the workspace-scoped TokenBudget (small `totalTokens`, 1h window, hard mode) for `my-ws` / `claude-opus-4-7`. |
| `00-assert.yaml` | TestAssert | Prereq gates (`check-prereqs.sh` + `check-extauth.sh`); wait `my-ws` + TokenBudget Ready; assert `status.phase=Ready`. |
| `01-projection.yaml` → `assert-projection.sh` | TestStep | Assert the projected `BackendTrafficPolicy` reflects the budget. |
| `02-enforce.yaml` → `enforce.sh` | TestStep | In-budget 200 (always); over-budget 429 + window reset (metering-gated). |

## Prerequisites

Reuses `my-ws` from `dev/demo/hello-keese.yaml`. Standard bootstrap:

```sh
make kind-up
make bootstrap-infra
kubectl apply -f dev/demo/hello-keese.yaml
```

Both prereq gates skip cleanly with no kubectl context and fail-closed against
placeholder OpenFGA / OpenBao infra.

## Run

```sh
make test-e2e            # includes this suite (kuttl globs tests/e2e/)
# or just this case:
kubectl-kuttl test --config tests/e2e/kuttl-config.yaml --test token-budget
```
