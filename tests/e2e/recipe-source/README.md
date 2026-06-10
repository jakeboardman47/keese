<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: reference
category: testing
status: shipped-with-stubs
last_verified: 2026-06-10
---

# tests/e2e/recipe-source/ — RecipeSource e2e (EH11)

First e2e coverage for the shipped
`internal/controller/keese/recipesource_controller.go` (+ `recipesource_git.go`,
`recipe_oci.go`). The reconciler resolves a recipe bundle from one of three
sources (OCI / git / ConfigMap), caches it, and converges to `Synced`/`Ready`,
removing the cached artifact on delete via a finalizer.

## What it asserts

| Step | File | Assertion |
|---|---|---|
| 00 | `00-configmap-source.yaml` / `00-assert.yaml` | A dev-labeled namespace (`keese.ai/env=dev`) + ConfigMap + a `RecipeSource{configMap}`. `reconcileConfigMap` reaches `phase: Synced`, `sourceType: ConfigMap`, `cached: true`, `Ready=True/Synced`, and the `finalizers.recipesource.keese.ai/cache-cleanup` finalizer is present. This is the only source type that reaches Synced with **zero external infra**. |
| 01 | `01-observed-gen.yaml` (`check-observed-gen.sh`) | `status.observedGeneration == metadata.generation` (rule 04.4) and `status.resolvedDigest` carries the `configmap:` prefix (proves a real ConfigMap UID was read). Command step — kuttl can't compare two live fields. |
| 02 | `02-oci-source.yaml` / `02-assert.yaml` | A `RecipeSource{oci}`. The deployed operator wires the **default `FakeOCIFetcher`** (`cmd/main.go` leaves `Fetcher` nil → `SetupWithManager` defaults it), so `reconcileOCI` (Pull → cosign Verify → Synced) runs end-to-end and reaches `phase: Synced`, `sourceType: OCI`, `cached: true`, `Ready=True/Synced`, `Progressing=False/Synced`. `../lib/check-oci-registry.sh` gates the **real** registry/cosign assertion (skips by default). |
| 03 | `03-delete.yaml` / `03-assert.yaml` | Delete both sources with a **blocking** `kubectl delete --timeout`; `cleanup()` emits `RecipeCacheCleanup`, evicts the OCI cache entry (idempotent; OCI source only), removes the finalizer. A clean removal within the timeout **is** the finalizer assertion (`errors:` block fails if either object lingers in `Terminating`). |

## Shipped-with-stubs

- **Real OCI registry + cosign verify** — the default bootstrap wires the
  in-tree `FakeOCIFetcher` and ships no in-cluster OCI registry, so the OCI
  status path is asserted against the fake (genuine reconcile coverage), while
  the real pull/verify is skipped by `../lib/check-oci-registry.sh`.
  **revisit_when_oci_registry_live**: ship an in-cluster registry + build the
  operator with the real `OCIFetcher` (oras+cosign), then rerun with
  `OCI_REGISTRY_LIVE=1`.
- **Real git remote** — the git source uses `DefaultGitCloner` (real
  network). Not exercised here to keep the suite hermetic; the OCI + ConfigMap
  paths already cover the shared status/finalizer machinery.
  **revisit_when_git_remote_live**: point at a reachable in-cluster git remote
  and add a `RecipeSource{git}` step mirroring step 02.

## Run

```sh
make kind-up && make bootstrap-infra      # bootstrapped cluster + operator
kubectl-kuttl test --config tests/e2e/kuttl-config.yaml --test recipe-source
```

Skips cleanly on placeholder infra; the CR-reconcile + status + finalizer
layers always run.

## Plan vs. shipped reality

EH11 named a `finalizers.recipe.keese.ai/cache-cleanup`-style ID; the **actual**
shipped finalizer is `finalizers.recipesource.keese.ai/cache-cleanup`
(`recipesource_controller.go §28`). The OCI happy-path reaching Synced reflects
the default **fake** fetcher, not a real registry (see Shipped-with-stubs).
