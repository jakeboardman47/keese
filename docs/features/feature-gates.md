<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: feature
category: feature
depends:
  - docs/designs/27-feature-gates-openfeature.md
  - docs/designs/27b-feature-gate-catalog.md
implements_specs: []
implements_plans: [docs/plans/td-feature-gates-openfeature.md]
source_refs:
  - api/policy/v1alpha1/featuregate_types.go:1-147
  - internal/controller/policy/featuregate_controller.go:1-255
  - internal/featuregate/featuregate.go:1-315
  - internal/featuregate/loader.go:1-148
related_skills: [controller-authoring]
status: implemented
implemented_in_phase: td-feature-gates-openfeature-A
last_verified: 2026-05-29
---

# Feature Gates

## Summary

Every keese capability ships behind a cluster-scoped `FeatureGate`
(`policy.keese.ai/v1alpha1`). The `FeatureGateReconciler` projects
effective boolean values for all gates into ConfigMap
`keese-system/keese-features` (`gates.json`). Each keese binary mounts
that ConfigMap at `/etc/keese/features/gates.json` and evaluates it
via the `internal/featuregate` package, which wires an in-process
OpenFeature provider backed by an `atomic.Pointer` map and an `fsnotify`
file watcher. Stage lifecycle follows Kubernetes conventions:
`alpha` defaults off, `beta` defaults on, `ga` is unconditional in code,
`deprecated` defaults off and emits warnings.

## Behavior

- Creating a `FeatureGate` CR causes the reconciler to rebuild the full
  projection from the complete list of CRs and SSA-patch
  `keese-system/keese-features` (featuregate_controller.go:59-84).
  Deleting a CR removes it from the projection on the next reconcile;
  no stale entries remain.
- Effective value rule: `spec.override ?? DefaultEffective(spec.stage)`
  (featuregate_types.go:137-146; featuregate_controller.go:108-113).
  `ga` and `deprecated` gates may not carry `spec.override` — enforced
  by XValidation at featuregate_types.go:28-29.
- Binaries call `featuregate.New(ctx, opts)` once at startup. The loader
  performs an initial synchronous read then watches the parent directory
  for fsnotify events, debounced 500 ms to absorb ConfigMap symlink-swap
  bursts (loader.go:30-147).
- If the projection file is absent or malformed at startup, per-gate
  `opts.Defaults` take effect; the binary continues without error
  (featuregate.go:78-98; loader.go:83-103).
- `spec.restartRequired=true` on a gate that transitions causes a
  `RestartRequired` Kubernetes event within 30 s; the controller does
  not auto-restart the binary (featuregate_controller.go:86-101).

## Configuration surface

Key fields at api/policy/v1alpha1/featuregate_types.go:

- `spec.stage` — `alpha | beta | ga | deprecated`; determines the
  default effective value (line 41).
- `spec.override` — optional bool; overrides the stage default; forbidden
  on `ga` and `deprecated` (lines 47-49).
- `spec.owners` — free-form binary names for drift alerts and
  `make featuregate-list` output (lines 54-58).
- `spec.restartRequired` — marks gates that cannot be hot-reloaded
  (lines 63-68).

Gate IDs used in code are typed constants declared in
`internal/featuregate/featuregate.go:28-29`.
**Only two gates are currently registered:**

| Gate name | Constant | Stage | Default |
|---|---|---|---|
| `cosign-installplan-verify` | `CosignInstallPlanVerify` | `alpha` | off |
| `cosign-installplan-failclosed` | `CosignInstallPlanFailClosed` | `alpha` | off |

The full catalog (authoritative list of all gates, past and future):
`docs/designs/27b-feature-gate-catalog.md`.

## Observability

**Status fields** (featuregate_types.go:72-101):

- `status.effective` — the projected boolean (refreshed every reconcile).
- `status.lastTransitionTime` — time of the last effective-value flip.
- `status.observedGeneration` — tracks spec convergence.

**Conditions** (`status.conditions[]`):

| Type | Meaning |
|---|---|
| `Ready` | `True/Projected` once effective value is in the ConfigMap |
| `RestartRequired` | Set via Event (not a condition); see Event below |

**Events** (recorder in featuregate_controller.go:97-100):

| Reason | Type | Trigger |
|---|---|---|
| `RestartRequired` | Normal | Gate with `spec.restartRequired=true` transitions within 30 s |

**Printer columns** (shortname `fg`): `Age`, `Stage`, `Effective`,
`Override`, `Restart`.

**Prometheus metrics** emitted by `internal/featuregate` per-process
(featuregate.go:105-118):

| Metric | Labels | Meaning |
|---|---|---|
| `keese_featuregate_eval_total` | `gate, value, binary` | Evaluation counter |
| `keese_featuregate_state` | `gate` | Current effective value (0/1) |

## Known limitations

- `status.consumers` is defined in the CRD status schema
  (featuregate_types.go:83-88) but the controller never populates it;
  the field is inert. The intent was to collect binary read telemetry
  via an OpenFeature hook, but that hook is not implemented. Future
  consumer plans are tracked under
  `docs/plans/td-feature-gates-openfeature.md` §"follow-ons (other
  consumers per design 27 §10)".
- Only boolean evaluation is supported by the in-process OpenFeature
  provider; string, float, int, and object evaluations return
  `ErrorReason` (featuregate.go:264-314).

## Change history

- `docs/plans/td-feature-gates-openfeature.md` phases A + B: landed
  `FeatureGate` CRD, projection controller, `internal/featuregate` eval
  package with fsnotify reload, Prometheus metrics, and cosign-webhook
  retrofit as first consumer.

## References

- Design: `docs/designs/27-feature-gates-openfeature.md`
- Design: `docs/designs/27b-feature-gate-catalog.md`
- Plan: `docs/plans/td-feature-gates-openfeature.md`
- Source: `api/policy/v1alpha1/featuregate_types.go`
- Source: `internal/controller/policy/featuregate_controller.go`
- Source: `internal/featuregate/featuregate.go`
- Source: `internal/featuregate/loader.go`
