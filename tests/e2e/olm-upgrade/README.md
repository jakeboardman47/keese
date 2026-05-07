<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: reference
category: testing
depends:
  - docs/designs/14a-olm-channels-upgrades.md
status: current
last_verified: 2026-05-07
---

# olm-upgrade kuttl test suite (TD-P2-10)

Validates the OLM upgrade graph from `v0.0.1-demo.1` to `v0.0.1-demo.2`
against a live kind cluster: InstallPlan manual approval, CSV phase
transitions, cross-version session stability, and cleanup.

## What it validates

| Step | File | What happens |
|---|---|---|
| 00 | `00-setup.yaml` | Install bundle v1 via `operator-sdk run bundle`; create Tenant, Workspace, WorkspaceSession |
| 01 | `01-assert.yaml` | CSV Succeeded, Tenant Active, Workspace Ready, Session Active |
| 02 | `02-approve-install-plan.yaml` | Push bundle v2; approve the pending InstallPlan (Manual approval per TD-P2-16) |
| 03 | `03-assert.yaml` | CSV phase Replacing → Succeeded; old CSV cleaned up by OLM |
| 04 | `04-cross-version-stability-assert.yaml` | Session pod stays Running; new operator re-reconciles Workspace patch; grace period 90s observed |
| 05 | `05-cleanup.yaml` | Drain test resources; `operator-sdk cleanup keese` |

## Prerequisites

A bootstrapped kind cluster is required. This suite does NOT spin one up.

```sh
# 1. Create and bootstrap the cluster.
make kind-up
make bootstrap-infra

# 2. Build and load both bundle images into kind.
make docker-build bundle-build
BUNDLE_IMG=ghcr.io/keese-ai/keese-bundle:v0.0.1-demo.1 make bundle-build
kind load docker-image ghcr.io/keese-ai/keese-bundle:v0.0.1-demo.1 --name keese-dev
BUNDLE_IMG=ghcr.io/keese-ai/keese-bundle:v0.0.1-demo.2 make bundle-build
kind load docker-image ghcr.io/keese-ai/keese-bundle:v0.0.1-demo.2 --name keese-dev

# 3. Install operator-sdk CLI (available in Nix flake or brew).
operator-sdk version
```

## Run

```sh
# Via dedicated Makefile target (canonical — includes kind cluster guard):
make test-e2e-olm-upgrade

# Or directly via kuttl:
kubectl kuttl test tests/e2e/olm-upgrade \
  --config tests/e2e/kuttl-config.yaml
```

The suite runs tests serially (`parallel: 1` in `kuttl-config.yaml`).
Total duration is under 10 minutes on a warm cluster with pre-pulled images.

## Design references

- [docs/designs/14a-olm-channels-upgrades.md](../../../docs/designs/14a-olm-channels-upgrades.md)
  §1 channel map, §2 upgrade graph, §5 rollback runbook, F1/F6 failure modes.
- [config/subscription/](../../../config/subscription/) — the Manual-approval Subscription
  manifests (TD-P2-16) that this suite exercises.

## Flake risk and known limitations

- `operator-sdk run bundle-upgrade` requires the OLM and OperatorGroup resources to be
  healthy before the call. If OLM is degraded, step 02 will time out. Re-run with a
  fresh cluster bootstrap.
- The kuttl `commands:` executor runs each line as a separate shell invocation. The
  multi-line shell block in `02-approve-install-plan.yaml` is a single `command:` value
  using a YAML literal block scalar — kuttl passes it to `bash -c`.
- Cross-version session stability (step 04) relies on the `keese.ai/session=<name>`
  pod label being stable across operator versions. If a future operator version changes
  this label, update the pod selector in step 04.
