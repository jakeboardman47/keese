<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: reference
category: testing
status: current
last_verified: 2026-05-07
---

# multi-tenant kuttl test suite (TD-P3-07)

Validates multi-tenant isolation: two concurrent tenants, per-tenant
Workspace + WorkspaceSession lifecycle, and fail-closed NetworkPolicy
cross-tenant denial.

## What it validates

| Step | File | What happens |
|---|---|---|
| 00 | `00-setup.yaml` | Two Tenants (`alpha`, `beta`) + Workspaces + WorkspaceSessions + a Memory CR in `beta` namespace |
| 01 | `01-assert.yaml` | Both Tenants `Active`, both Workspaces `Ready`, both Sessions `Active` |
| 02 | `02-cross-tenant-deny.yaml` | Probe pod in `alpha` namespace attempts TCP to `beta-memory` Service ClusterIP |
| 03 | `03-assert.yaml` | Probe pod exits `Failed` — NetworkPolicy blocked the cross-tenant egress |

### Cross-tenant denial (steps 02-03)

The probe pod runs `nc -z -w 5` from the `alpha` namespace toward the
Memory service in `beta`. The Workspace controller installs a default-deny
egress NetworkPolicy per rule 04.17 and rule 05.4-5. A successful TCP
connect would mean isolation has broken; the test expects `phase=Failed`.

### CrossTenantAgreement happy path

The CTA happy path (two tenants agreeing to share a SharedMemory) is
deferred from this suite. Reason: it requires a live OpenFGA instance for
the tuple negotiation, adding cluster bootstrap complexity beyond what a
self-contained kuttl test can gate. Tracked as a follow-on to TD-P3-07.

## Prerequisites

- A kind cluster with `kind-keese` context:
  `make kind-up && make bootstrap-infra`
- `kuttl` (`kubectl-kuttl`) on PATH (in Nix flake or `brew install kuttl`)
- Operator deployed (`make deploy` or `make install && make run`)
- **No internet access needed** — busybox:1.36 should be pre-pulled or
  available from a local registry mirror

## Run

```sh
# Via the extended target (runs all suites):
make test-e2e-extended

# Or individually:
kubectl kuttl test tests/e2e/multi-tenant \
  --config tests/e2e/kuttl-config.yaml
```

## Expected duration

Under 90 seconds on a warm cluster (images pre-pulled). CRD watch
latency dominates the first ~30s for Tenant + Workspace condition
propagation.

## Flake risk

None identified. The cross-tenant deny probe uses a 5s connect timeout
(`nc -w 5`) — well within the 30s step assertion window.
