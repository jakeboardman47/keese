<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: reference
category: testing
status: shipped-with-stubs
last_verified: 2026-06-09
---

# tests/e2e/feature-gate/ — FeatureGate behavior e2e (EH8)

Proves a `policy.keese.ai/FeatureGate` flip changes **observable controller
behavior**, not just CR status. The gate under test is the shipped alpha gate
**`cosign-installplan-verify`** (design
[27b](../../../docs/designs/27b-feature-gate-catalog.md) catalog; consumed by
`keese-cosign-webhook`).

## The behavior the flip changes

The `FeatureGateReconciler`
(`internal/controller/policy/featuregate_controller.go`) rewrites the projection
ConfigMap `keese-system/keese-features` (key `gates.json`) from the full
FeatureGate list on every reconcile — a JSON map `<gate> -> <effective bool>`.
That ConfigMap is what **every keese binary mounts** at
`/etc/keese/features/gates.json` (e.g. the cosign webhook Deployment,
`config/cosign-webhook/deployment.yaml`). Flipping `spec.override` flips the
projected value, which is an observable cluster artifact a consumer reads — not
a field on the CR.

## What it asserts

| Step | Case | Assertion | Live by default? |
|---|---|---|---|
| 0 | `default` | No CR for the gate → documented alpha default (OFF): the gate is not projected as `true` in `keese-features`. `DefaultEffective(alpha)=false`. | yes |
| 1 | `enable` | Apply gate with `override=true` → FeatureGate reaches `Ready=True/Projected`, `status.effective=true`, `status.observedGeneration==metadata.generation`; **and** the projection ConfigMap maps `cosign-installplan-verify -> true`. | yes |
| 2 | `disable` | Re-apply with `override=false` → projection flips to `cosign-installplan-verify -> false`; status follows (`effective=false`, observedGeneration advances). Proves the behavior **follows** the spec, not a one-way set. | yes |
| 3 | `behavior` | Downstream admission flip: gate ON → unsigned keese-image InstallPlan **DENIED** (`BundleUnsigned`); gate OFF → **ADMITTED** (`AllowedFeatureGateOff`). | **webhook/OLM-gated** |

`99-teardown` deletes the CR so a rerun's step-0 default holds and the
projection self-heals to alpha=off.

## Shipped-with-stubs: the admission-outcome gate

The deepest observable effect — the cosign webhook's admission **outcome** on an
OLM `InstallPlan` (`internal/admission/cosign/handler.go`: `AllowedFeatureGateOff`
short-circuit vs. fail-closed `BundleUnsigned` deny) — is **not observable in the
local bootstrap**. `make bootstrap-infra` deploys neither OLM
(`operators.coreos.com/v1alpha1 InstallPlan`) nor the `keese-cosign-webhook`
Deployment + `ValidatingWebhookConfiguration` (`config/cosign-webhook/` ships the
manifests but no overlay/bootstrap applies them).

`assert-admission-flip.sh` detects the precondition (InstallPlan CRD + webhook
config + Deployment Available) and **skips cleanly** when absent. Tracking
trigger: **`revisit_when_featuregate_effect_observable`**. Gate to assert once
live: **`cosign-installplan-verify`** (deny unsigned InstallPlan when on; admit
with `AllowedFeatureGateOff` when off). Steps 0-2 already assert a real,
observable flip today (the projection ConfigMap the webhook consumes), so the
suite meets EH8's "≥ 1 on/off flip that changes observable behavior" without
this step.

## Steps

| File | Kind | Purpose |
|---|---|---|
| `00-default.yaml` → `assert-default.sh` | TestStep | No CR → gate not projected `true` (alpha=off). |
| `01-enable.yaml` → `featuregate.yaml` | TestStep | Apply gate `override=true`. |
| `01-assert.yaml` → `assert-status.sh` / `assert-projection.sh` | TestAssert | Ready + effective=true + observedGeneration; projection `true`. |
| `02-disable.yaml` → `featuregate-disabled.yaml` | TestStep | Re-apply `override=false`. |
| `02-assert.yaml` → `assert-status.sh` / `assert-projection.sh` | TestAssert | effective=false; projection flips to `false`. |
| `03-behavior.yaml` → `assert-admission-flip.sh` | TestStep | Admission-outcome flip (webhook/OLM-gated; skips). |
| `99-teardown.yaml` | TestStep | Delete the CR; projection self-heals. |

## Prerequisites

Standard bootstrap (operator + FeatureGate CRD + FeatureGateReconciler running):

```sh
make kind-up
make bootstrap-infra
```

No OpenFGA/OpenBao/gateway dependency — the suite asserts only the
FeatureGate → projection ConfigMap path, which the operator reconciles directly.
Every script skips cleanly with no kube context (structural validation only).

## Run

```sh
make test-e2e            # includes this suite (kuttl globs tests/e2e/)
# or just this case:
kubectl-kuttl test --config tests/e2e/kuttl-config.yaml --test feature-gate
```
