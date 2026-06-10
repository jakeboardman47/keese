<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: reference
category: testing
status: shipped-with-stubs
last_verified: 2026-06-09
---

# tests/e2e/token-budget/ — TokenBudget enforcement (EH7 + CH5d)

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
| 3 | `over-budget-429` | Driving consumption past `totalTokens` → **HTTP 429** + the response carries `x-keese-limit-source: token-budget` + `status.phase=Exhausted`. | **metering-gated** |
| 4 | `post-reset-200` | After the window boundary the budget recovers: `status.windowStart` advances and a request 200s again. | **metering-gated** |

The CH5d addition over EH7 is the **`x-keese-limit-source: token-budget`**
assertion (step 3). That header attributes the 429 to the *long-window*
`TokenBudget` loop (NATS-KV exceeded boolean → ext_authz `local_reply`,
ADR 30 / 05a / 10b), distinct from the gateway's own *short-window*
`gateway-token-rate` cap. `enforce.sh` polls response **headers** for it via a
local helper that mirrors EH4's curl but dumps `-D -` headers with the body
discarded (rule 05.10) — EH4's sourced `fire_request` returns only the status
code, so it cannot see the header.

## Shipped-with-stubs: the metering gate

Over-budget enforcement (steps 3-4) drives the **full** token-cost metering
loop end-to-end:

```
gateway usage → OTEL collector OTLP→/ingest shaping → keese-token-meter (:dev)
  → keese_token_budget_consumed_total → reconciler increase()[window] crossover
  → NATS-KV keese-budget-exceeded → ext_authz local_reply → 429 + x-keese-limit-source
```

The reconciler hop (CH5c) is **complete**, and this e2e is wired, but the live
series still depends on **CH5b's two remaining stubs**:

- `revisit_when_meter_image_live` — the `ghcr.io/keese-ai/keese-token-meter:dev`
  image must be built + kind-loaded (`make token-meter-load`); until then the
  `keese-token-meter` Deployment in `monitoring` has no runnable image.
- `revisit_when_collector_ingest_shaping` — the Tier-1 OTEL collector must shape
  its OTLP token-cost datapoint into the meter's `/ingest` JSON record; until
  the live collector image does so, the meter relabels nothing.

`../lib/check-metering-fully-live.sh` collapses both into the umbrella
precondition **`revisit_when_metering_fully_live`** and `enforce.sh` **skips
steps 3-4 cleanly** (exit 2) when any precondition is unmet — never a fake pass
(rule 06). When CH5b's stubs land, the same steps run unmodified and assert
429 + `x-keese-limit-source: token-budget` + exhaustion + window-reset recovery.
(`../lib/check-metering.sh` remains the narrower "is the consumed series
present?" probe.)

## Reuse

`enforce.sh` reuses EH4's request-firing helper
(`../rebac-decision/test-rebac-decision.sh`) **by sourcing** its function
definitions (everything above its `── Run ──` marker, via process
substitution) — `fire_request` / `mint_sa_token` / `assert_status` /
`poll_status` / `warm_up_gateway`. No copy, no edit to EH4, and EH4's own
suite never runs. The CH5d `x-keese-limit-source` header poll
(`fire_request_headers` / `limit_source_of` / `poll_limit_source`) is defined
locally in `enforce.sh`, mirroring EH4's curl shape.

## Steps

| File | Kind | Purpose |
|---|---|---|
| `00-apply.yaml` → `budget.yaml` | TestStep | Apply the workspace-scoped TokenBudget (small `totalTokens`, 1h window, hard mode) for `my-ws` / `claude-opus-4-7`. |
| `00-assert.yaml` | TestAssert | Prereq gates (`check-prereqs.sh` + `check-extauth.sh`); wait `my-ws` + TokenBudget Ready; assert `status.phase=Ready`. |
| `01-projection.yaml` → `assert-projection.sh` | TestStep | Assert the projected `BackendTrafficPolicy` reflects the budget. |
| `02-enforce.yaml` → `enforce.sh` | TestStep | In-budget 200 (always); over-budget 429 + `x-keese-limit-source: token-budget` + window reset (gated on `check-metering-fully-live.sh`). |

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
