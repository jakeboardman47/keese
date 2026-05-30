<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: design
category: lifecycle
depends:
  - 18-process-lifecycle.md
  - 20a-api-group-layout.md
related_skills: [doc-authoring, controller-authoring, crd-authoring]
status: current
last_verified: 2026-05-06
---

# 27 — Feature gates via OpenFeature

**Decision:** Every keese capability that is not a one-shot setup
step ships behind a named, staged feature gate. Gates are declared
as `policy.keese.ai/v1alpha1.FeatureGate` cluster-scoped CRs,
projected by the operator into a single `keese-system/keese-features`
ConfigMap, and read by every keese binary through the
[OpenFeature Go SDK](https://openfeature.dev) backed by a local
in-process provider. Cosign InstallPlan verification (TD-P1-04) is
the first consumer.

## Context

keese ships opinionated runtime behavior — cosign verify, ext_authz,
OpenFGA writes, OTEL export, recipe-source signing. Operators need
to flip these per-cluster (incident response, air-gapped Sigstore,
staged rollouts) without rebuilding images, restarting pods, or
forking. Kubernetes' `--feature-gates=` pattern + CNCF OpenFeature
give us the vocabulary and tooling.

## 1. CRD shape

```yaml
apiVersion: policy.keese.ai/v1alpha1
kind: FeatureGate
metadata:
  name: cosign-installplan-verify     # DNS-1035; cluster-scoped.
spec:
  description: "InstallPlan cosign verify (TD-P1-04)."
  stage: alpha                          # alpha|beta|ga|deprecated
  default: true
  override: null                        # bool; null = use default
  owners: [keese-cosign-webhook]
  restartRequired: false
status:
  effective: true                       # default ⊕ override
  observedGeneration: 1
  consumers: [keese-cosign-webhook]     # rolling window of readers
  lastTransitionTime: 2026-05-06T18:00:00Z
```

Stage semantics, cribbed from k8s:

| Stage | Default | Lifecycle |
|---|---|---|
| `alpha` | off | May change; no upgrade compat |
| `beta` | on | Stable shape; backwards-compatible flips ok |
| `ga` | n/a | Code path unconditional; CR flipped to `deprecated` for 1 minor |
| `deprecated` | frozen | Reads emit `Warning` event; removed next minor |

Promotion = CR edit + CSV bump. GA deletes the conditional in code.

## 2. Source of truth + projection

`FeatureGateController` (cluster-scoped) recomputes `effective =
override ?? defaultFor(stage)` on every Generation change, writes
the canonical `{name: bool}` map into ConfigMap
`keese-system/keese-features` key `gates.json` (SSA owner
`keese-feature-gate-controller`), and patches `status.effective`,
`status.observedGeneration`, `status.lastTransitionTime`, plus a
ring-buffer of consumer IDs from reader hooks.

Every binary mounts the CM as a projected volume at
`/etc/keese/features/gates.json`, watches via fsnotify, and reloads
an `atomic.Value[map[string]bool]` behind the OpenFeature provider.

CRD-via-CM (vs CRD-direct): pods already mount ConfigMaps,
projected volumes survive apiserver outages, and one CM means one
watch per binary instead of one informer. CRD owns schema +
audit; CM is delivery.

## 3. Eval API

```go
// internal/featuregate/featuregate.go
type Gate string

const (
    CosignInstallPlanVerify  Gate = "cosign.installplan.verify"
    CosignInstallPlanFailClosed Gate = "cosign.installplan.failClosed"
    // ...
)

// Enabled is the only call site path. Sub-µs.
func Enabled(ctx context.Context, g Gate) bool
```

Behind it: OpenFeature client with the keese-local provider; Hooks
emit `keese_featuregate_eval_total{gate, value}`. Standardizing on
OpenFeature buys a one-line swap to flagd or a SaaS provider later;
cost is one ~3 MB MIT dep.

## 4. Cosign as the first consumer

Two gates land with TD-P1-04. `cosign.installplan.verify` (alpha,
default-off; OLM seed CR sets `override: true` in prod): off →
handler short-circuits `admission.Allowed("…verify=false")`.
`cosign.installplan.failClosed` (alpha, default-off): with verify on
but failClosed off, verify failures become Allowed + warning +
`Event(BundleUnsignedAdmittedDryRun)` for staged rollouts.
Deployment + Service + WebhookConfig always exist; binary always
boots. Flips propagate in ~2s.

## 5. Restart-required gates

Flips that can't take effect mid-process (webhook registration,
leader election, listener ports) set `spec.restartRequired: true`.
Controller emits `Event(Reason=RestartRequired)` + counter
`keese_featuregate_restart_required_pending`. No auto-restart per
rule 06.

## 6. Bootstrap, RBAC + tamper resistance

Seed manifests in `config/featuregates/` ship with the OLM bundle
(cluster-scoped CRs). Operator ClusterRole gets
`featuregates.policy.keese.ai` `get,list,watch,patch/status` and
ConfigMap `keese-system/keese-features` `get,create,patch`.
Consumer binaries get only ConfigMap `get,watch` on the projected
CM — no FeatureGate access; read path is apiserver-free past boot.

A kyverno `ClusterPolicy` in `config/featuregates/policy.yaml`
denies writes to `keese-system/keese-features` from any SA other
than `keese-controller-manager`. The operator image's cosign
signature (rule 05.12 + TD-P1-04) is the trust root — unsigned
images can't install, and only the signed controller's SA can write
the CM.

`make featuregate-list` and `make featuregate-diff NEW=<file>`
(under `scripts/`) print + validate gates as kubectl/jq one-liners.

## 7. Observability

- `keese_featuregate_eval_total{gate,value,binary}` — every read.
- `keese_featuregate_state{gate}` — effective-value gauge.
- `keese_featuregate_drift_seconds{gate}` — CR→binary observe lag;
  alert > 30s.
- Structured event per transition: `{"event":"featuregate_transition","gate":"…","from":false,"to":true,"reason":"override_set"}`.

## 8. Failure modes

| # | Failure | Detection | Mitigation |
|---|---|---|---|
| F1 | CR override invalid (override=true on stage=deprecated) | Admission VAP rejects | CEL rule on the CRD; never reaches reconcile |
| F2 | CM projection lags > 30s | `drift_seconds` SLO | Alert; operator re-reconciles via `kubectl annotate featuregate <n> keese.ai/poke=$(date)` |
| F3 | Operator down → no projection updates | CM watch on consumers still serves last-good | Last-good cached in-process; consumers do not fail-closed on a stale CM (alert on age) |
| F4 | Consumer binary mounts no CM (volume misconfigured) | Startup probe: file open fails | Pod NotReady; falls back to per-stage default for every gate |
| F5 | OpenFeature provider panics | gate eval returns the per-stage default | recover() in the eval hot path; counter `keese_featuregate_provider_panics_total` |
| F6 | Two operators race on CM SSA | Field-owner conflict | Single `keese-feature-gate-controller` owner; HPA disabled on the operator |

## 9. Verification

- **Unit** (`internal/featuregate/`): table-driven on stage→default,
  override merge, fsnotify reload, OpenFeature provider adapter,
  panic recovery.
- **Integration** (envtest): FeatureGateController updates CM;
  status converges; VAP rejects bad CRs.
- **e2e** (kuttl `tests/e2e/featuregate/`): patch a FeatureGate,
  wait <30s, assert cosign-webhook reads the new value (InstallPlan
  toggles deny → allow).

## 10. Migration path

Per consumer: (1) wrap entry point in
`if !featuregate.Enabled(ctx, X) { return passThrough() }`; (2)
ship a seed CR at the right stage; (3) record the gate in 27b's
catalog. Order: cosign-webhook (this PR), keese-authz, OTEL
exporters, recipe-source signing, guardrail CEL, image rewriting.

## 11. Rollback

Plumbing regression (empty CM, leaked watches) → binaries fall back
to `defaultFor(stage)` per F4+F5: alpha off, beta on; GA paths
unchanged. Kill switch: `kubectl delete cm -n keese-system
keese-features` then `kubectl scale deploy -n keese-system
keese-feature-gate-controller --replicas=0`; redeploy from last
signed CSV. CRD survives; user overrides persist.

## Iteration log — see [27-ii-iter-log.md](27-ii-iter-log.md).

## See also

- [18-process-lifecycle.md](18-process-lifecycle.md) — no auto-restart on flip.
- [20a-api-group-layout.md](20a-api-group-layout.md) — `policy.keese.ai` placement.
- [../plans/demo/tech-debt.md](../plans/demo/tech-debt.md) — TD-P1-04 first consumer.
