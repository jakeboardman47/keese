<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: reference
category: testing
depends:
  - docs/references/tilt-local-loop.md
  - docs/references/nix-dev-env.md
related_skills: [test-engineer]
status: current
last_verified: 2026-04-25
---

# E2E Kind Smoke Harness

`make e2e-smoke` runs a 9-phase end-to-end smoke against a local kind cluster.
It is the gate between "operator compiles + unit/integration green" and
"the full stack reconciles CRs end-to-end."

## Pre-requisites

| Tool | Purpose |
|---|---|
| `kind` | creates the local Kubernetes cluster |
| `ctlptl` | idempotent cluster provisioning (falls back to bare `kind` if absent) |
| `kubectl` | cluster interaction |
| `helmfile` | installs bootstrap Helm charts |
| `tilt` | hot-reloads the operator image |
| `python3` | inline JSON counting in phase assertions |

Install via `nix develop` (see `docs/references/nix-dev-env.md`).

Optional: set `KUBEBUILDER_ASSETS` before running unit/integration tests.
The smoke does not require it but warns if it is absent.

## Phases

| ID | Name | Assertion |
|---|---|---|
| 01 | Pre-flight | All required tools on PATH; production context guard (rule 05.14). |
| 02 | Cluster up | `kind-keese-dev` nodes reach `Ready`. |
| 03 | Bootstrap dev deps | cert-manager, Capsule, Argo, ECK, OpenBao, ExternalSecrets, Envoy Gateway, NACK all Available. |
| 04 | Operator deploy | Tilt up; `keese-system/keese-controller-manager` Available. |
| 05 | OIDCProvider bootstrap | All 7 bootstrap OIDCProvider CRs reach `Active` or `Degraded` (placeholder-issuer → Degraded is expected). |
| 06 | Sample Tenant | `tenant-minimal` reaches `phase=Active`. |
| 07 | Sample Workspace | `workspace_v1alpha1_workspace` reaches `Provisioning` or `Running`; SA + ≥2 NetworkPolicies + PVC present in workspace namespace. |
| 08 | Sample WorkspaceSession | `workspacesession-minimal` reaches `phase=Active`; ≥1 Pod present. |
| 09 | Teardown | Samples deleted (finalizers cascade); Tilt down; cluster deleted only with `--no-keep`. |

## Usage

```
# Full run, keep cluster for debugging (default):
make e2e-smoke

# Tear everything down at the end:
make e2e-smoke -- --no-keep

# Resume from phase 05 after a phase-04 failure:
bash scripts/dev/e2e-smoke.sh --phase=05

# Override log directory:
bash scripts/dev/e2e-smoke.sh --logs-dir=/tmp/smoke-logs
```

## Interpreting results

- **Exit 0**: all phases passed.
- **Exit 1**: one phase failed. The failing phase ID and the failed assertion
  are printed to stderr. All output from that phase is already in the terminal.

## Debugging a failed phase

| Phase | Where to look |
|---|---|
| 03 (bootstrap) | `kubectl -n <ns> describe deploy/<name>` + Helm status: `helmfile -f dev/bootstrap/helmfile.yaml status` |
| 04 (operator) | Tilt UI at `http://localhost:10350`; log tail: `.plan-logs/e2e-smoke-tilt.log` |
| 05–08 (CRs) | `kubectl describe <kind> <name>` shown automatically on failure; operator logs: `kubectl -n keese-system logs deploy/keese-controller-manager` |

## Cleanup commands

```
# Tear down Tilt:
make tilt-down

# Delete the kind cluster:
make kind-down

# Delete only the sample CRs:
kubectl delete -f config/samples/workspace/workspacesession-minimal.yaml
kubectl delete -f config/samples/workspace_v1alpha1_workspace.yaml
kubectl delete -f config/samples/tenancy/tenant-minimal.yaml
```

## Files

- `scripts/dev/e2e-smoke.sh` — the harness (phases 01–09)
- `Makefile` target `e2e-smoke` — thin wrapper
- `.plan-logs/e2e-smoke-tilt.log` — Tilt stdout/stderr captured during phase 04
- `.plan-logs/state.json` — `run::step` breadcrumbs; inspect to see last completed step
