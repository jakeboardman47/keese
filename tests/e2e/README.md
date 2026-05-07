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
| `agentruntime-drain/` | Drain → pod-delete → Resume round-trip; checkpoint survives across pod restart. | TD-P1-02 |
| `multi-tenant/` | Two concurrent tenants Active; cross-tenant egress blocked by NetworkPolicy (probe exits Failed). | TD-P3-07 |
| `chaos-network/` | Deny-all NetworkPolicy triggers EgressUnavailable within 30s; restore clears it; controller restart leaves session pod running. | TD-P3-08 |

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
