<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: reference
category: testing
status: shipped-with-stubs
last_verified: 2026-06-10
---

# tests/e2e/runtime-extension/ — RuntimeExtension e2e (EH11)

First e2e coverage for the shipped
`internal/controller/keese/runtimeextension_controller.go`. The reconciler
validates `spec.runtimeRef` against a cluster-scoped `AgentRuntime`, writes an
OpenFGA **owner** tuple, converges to `Ready` (reporting `boundWorkspaces`), and
removes all tuples behind a finalizer on delete.

## What it asserts

| Step | File | Assertion |
|---|---|---|
| 00 | `00-setup.yaml` / `00-assert.yaml` | A cluster-scoped `AgentRuntime` (`agentruntime-e2e`) + a tenant-labeled namespace (`keese.ai/tenant=ext-tenant`, so `resolveTenantName` yields a real tenant). The AgentRuntime exists so `runtimeRef` resolves. |
| 01 | `01-extension.yaml` / `01-assert.yaml` | Two `RuntimeExtension`s. **Happy path** `rext-e2e` → `phase: Ready`, `boundWorkspaces: 0`, `Ready=True/ExtensionTupleWritten`, finalizer `finalizers.runtimeextension.keese.ai/rebac-cleanup` present. **Negative** `rext-badref-e2e` (missing runtimeRef) → `phase: Degraded`, `Ready=False/ExtensionRuntimeRefInvalid` — proves the runtimeRef validation gates. |
| 02 | `02-observed-gen.yaml` | `check-observed-gen.sh`: `status.observedGeneration == metadata.generation` (rule 04.4). `../lib/check-extension-tuple.sh MODE=present`: reads `extension:rext-e2e#owner@tenant:ext-tenant` from the **live** OpenFGA store — proves the real writer ran, not the `RuntimeNoopRebacWriter` fallback. This is the owner-LINK assertion (see Plan vs. reality). |
| 03 | `03-teardown.yaml` / `03-assert.yaml` | Delete both extensions with a **blocking** `kubectl delete --timeout`; `reconcileDelete` runs `DeleteAllExtensionTuples`, emits `ExtensionTupleDeleted`, removes the finalizer. `../lib/check-extension-tuple.sh MODE=absent` confirms the owner tuple is gone from the live store. Clean removal within the timeout **is** the finalizer assertion. |

## Shipped-with-stubs

- **Live owner-tuple read/delete** — `../lib/check-extension-tuple.sh` requires a
  seeded OpenFGA (`keese-system/openfga-config.store_id`). On placeholder infra
  the controller falls back to `RuntimeNoopRebacWriter` and the helper **skips
  cleanly**; the CR-status layer (Ready / observedGeneration / finalizer
  completion) is still asserted natively by kuttl.
  **revisit_when_openfga_seeded**: run `kubectl apply -f
  dev/bootstrap/openfga/seed.yaml` then rerun — the tuple present/absent gates
  go live.
- **`enabled_in` (workspace-binding) tuple** — EH11 named an
  "extension-enabled OpenFGA tuple … on workspace create". The shipped
  `RuntimeExtensionReconciler` writes only the `owner` tuple on reconcile; the
  `enabled_in` tuple (`extension:<n>#enabled_in@workspace:<w>`) has a writer
  method (`WriteExtensionEnabledIn`) but **no caller in a shipped reconciler** —
  the workspace-create binding path is not wired yet. This suite asserts the
  owner tuple + `boundWorkspaces=0`. **revisit_when_enabled_in_wired**: when a
  reconciler writes `enabled_in` on workspace bind, add a workspace + assert
  `boundWorkspaces` increments and the `enabled_in` tuple appears.

## Run

```sh
make kind-up && make bootstrap-infra
kubectl-kuttl test --config tests/e2e/kuttl-config.yaml --test runtime-extension
```

## Plan vs. shipped reality

EH11 asked for an **owner-ref to the AgentRuntime**. The shipped controller
deliberately sets **no** formal `ownerReference`: AgentRuntime is cluster-scoped
and RuntimeExtension is namespaced, and K8s GC does not support a cross-namespace
ownerRef (`runtimeextension_controller.go §105-109`). Ownership is tracked via
the OpenFGA **owner tuple** + a manual reference check on delete (rule 04.10), so
this suite asserts the owner **tuple** (step 02) as the owner-link, not a
`metadata.ownerReferences` entry. Follow-up: if a formal in-namespace ownerRef or
GC-annotation approach is later adopted, add a `metadata.ownerReferences` assert.
