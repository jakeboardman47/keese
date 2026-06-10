<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: reference
category: testing
status: current
last_verified: 2026-05-07
---

# tests/e2e/

End-to-end test suite for keese. Driven by [kuttl](https://kuttl.dev),
runs against an existing `kind-keese-*` cluster — does **not** spin up
its own. Use `make kind-up && make bootstrap-infra` first.

## Run

```sh
# Standard suite (workspace-progression + aigw-defense):
make test-e2e

# Extended suite (all four suites including multi-tenant + chaos-network):
make test-e2e-extended
```

The Makefile targets require `kuttl` (or `kubectl-kuttl`) on PATH. Add
to the dev shell via `flake.nix` or install upstream:

```sh
go install github.com/kudobuilder/kuttl/cmd/kubectl-kuttl@latest
```

`make test-e2e-extended` fails fast if no `kind-keese-dev` cluster is
present — it does NOT spin up its own kind cluster.

## Layout

```
tests/e2e/
├── kuttl-config.yaml          # TestSuite (timeouts, serial execution)
├── README.md                  # this file
└── <case-name>/
    └── NN-step.yaml           # numbered TestStep YAMLs
```

Cases run in directory-name order. Each case directory contains one or
more numbered TestStep YAMLs that are executed in order.

## Cases

| Case | What it asserts | TD |
|---|---|---|
| `aigw-defense/` | AI Gateway ext_authz strips hostile auth headers; four shapes must return HTTP 200 with BSP-injected credential. | TD-P1-07 |
| `workspace-progression/` | Tenant → Workspace → WorkspaceSession lifecycle reaches Active/Ready. | TD-P1-07 |
| `agentruntime-drain/` | Real `keese-drain` under SIGTERM → 3-field shutdown event + checkpoint/SQLite survival; self-contained fixture. (in-cluster drain run gated on goose-runtime image.) | TD-P1-02 · EH10 |
| `multi-tenant/` | Two concurrent tenants Active; cross-tenant egress blocked by NetworkPolicy (probe exits Failed). | TD-P3-07 |
| `chaos-network/` | Deny-all NetworkPolicy triggers EgressUnavailable within 30s; restore clears it; controller restart leaves session pod running. | TD-P3-08 |
| `rebac-decision/` | Live ext_authz CRD-driven decision: granted tool → HTTP 200, ungranted → 403 (fail-closed), token-free deny audit (rule 05.10), allow→deny revoke flip within cache TTL. | EH4 |
| `authz-guardrails/` | GuardrailBinding default-inherit deny-union + finite event reasons; WorkspaceTool tool allow→200 / revoke→403. (guardrail ext_proc gated.) | EH5 |
| `cross-tenant/` | CrossTenantAgreement trust-tuple write + expiry-driven removal; OIDCProvider Ready. (cross-tenant request decision gated.) | EH6 |
| `token-budget/` | TokenBudget Ready + BackendTrafficPolicy rate-limit projection + in-budget→200. (over-budget 429 gated on metering.) | EH7 |
| `feature-gate/` | FeatureGate flip observable via the `keese-features` projection ConfigMap (enable→true/disable→false) + status. (cosign admission-outcome flip gated on OLM/webhook.) | EH8 |
| `workflow/` | Workflow→Argo `WorkflowTemplate`/`CronJob`; WorkflowRun→Argo→Succeeded (live argosay); `concurrencyPolicy=Forbid`; finalizer-cascade GC. (runCount gated.) | EH9 |
| `recipe-source/` | RecipeSource (ConfigMap/OCI) → Synced + observedGeneration + finalizer cache-evict. (real OCI registry gated; FakeOCIFetcher.) | EH11 |
| `runtime-extension/` | RuntimeExtension reconcile → `ExtensionTupleWritten` owner tuple + `Degraded`/`ExtensionRuntimeRefInvalid` negative path. (enabled_in unwired.) | EH11 |

## Adding a case

1. Create a directory under `tests/e2e/<case-name>/`.
2. Add numbered `TestStep` YAMLs (`00-foo.yaml`, `01-bar.yaml`, …).
3. For assertions that can be read from `kubectl get`, use kuttl's native
   `apiVersion: kuttl.dev/v1beta1, kind: TestAssert`. For request-time
   behaviors (Envoy filters, BSP injection, etc.) use a `TestStep` with
   `commands:` invoking a Bash script in `scripts/dev/`.
4. Update the table above and link the script.

## Adding kuttl to the dev shell

Tracked in TD-P1-07. Until then, `make test-e2e` errors with `kuttl
missing`.
