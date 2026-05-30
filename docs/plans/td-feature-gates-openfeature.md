<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: plan
category: execution
depends:
  - ../designs/27-feature-gates-openfeature.md
  - ../designs/27-ii-iter-log.md
  - demo/tech-debt.md
related_skills: [plan-management, controller-authoring, crd-authoring]
status: current
last_verified: 2026-05-06
---

# TD — Feature gates via OpenFeature

## Goal

Land design [27](../designs/27-feature-gates-openfeature.md) end-to-end:
the `policy.keese.ai/v1alpha1.FeatureGate` CRD, its projection
controller, the `internal/featuregate` eval package, the kyverno
tamper-resistance policy, and the Makefile/script tooling. Retrofit
the just-shipped cosign-webhook (TD-P1-04) onto two gates so the
plumbing has a real consumer the day it lands.

Out of scope: keese-authz / OTEL / recipe-source / guardrail / image
rewriting retrofits — each gets its own follow-on plan.

## Phases

### Phase A — Gate plumbing

| # | Step | File / target |
|---|---|---|
| A1 | CRD types + deepcopy | [api/policy/v1alpha1/featuregate_types.go](../../api/policy/v1alpha1/featuregate_types.go), `zz_generated.deepcopy.go` |
| A2 | Eval API — `Enabled(ctx, Gate) bool` over OpenFeature | [internal/featuregate/](../../internal/featuregate/) |
| A3 | CM loader (fsnotify, atomic.Value) + tests | [internal/featuregate/loader.go](../../internal/featuregate/loader.go) |
| A4 | FeatureGateController — projects CRs into ConfigMap `keese-system/keese-features`, status patches | [internal/controller/policy/featuregate_controller.go](../../internal/controller/policy/featuregate_controller.go) |
| A5 | CRD manifest | [config/crd/bases/policy.keese.ai_featuregates.yaml](../../config/crd/bases/policy.keese.ai_featuregates.yaml) |
| A6 | Seed CRs + kyverno ClusterPolicy + kustomization | [config/featuregates/](../../config/featuregates/) |
| A7 | RBAC for the controller (CR + status + CM SSA) | rolled into `+kubebuilder:rbac` markers in A4 |
| A8 | `make featuregate-list` + `make featuregate-diff` + `scripts/featuregate-list.sh` | [Makefile](../../Makefile), [scripts/featuregate-list.sh](../../scripts/featuregate-list.sh) |
| A9 | 27b — feature-gate catalog doc | [docs/designs/27b-feature-gate-catalog.md](../designs/27b-feature-gate-catalog.md) |

### Phase B — Cosign retrofit (first consumer)

| # | Step | File |
|---|---|---|
| B1 | Wire `featuregate.Enabled(ctx, CosignInstallPlanVerify)` short-circuit in [Handle](../../internal/admission/cosign/handler.go) | `internal/admission/cosign/handler.go` |
| B2 | Wire `CosignInstallPlanFailClosed` — verify failure → Allowed+Warning when off | `internal/admission/cosign/handler.go` |
| B3 | Mount projected CM `keese-features` into cosign-webhook Deployment | [config/cosign-webhook/deployment.yaml](../../config/cosign-webhook/deployment.yaml) |
| B4 | Add `configmaps/keese-features` `get,watch` to cosign-webhook RBAC | [config/cosign-webhook/rbac.yaml](../../config/cosign-webhook/rbac.yaml) |
| B5 | Bootstrap the gate loader in [main](../../cmd/keese-cosign-webhook/main.go) | `cmd/keese-cosign-webhook/main.go` |
| B6 | Update tests for gate-aware paths | `internal/admission/cosign/handler_test.go` |
| B7 | Tech-debt entry: TD-P1-04 closure follow-ons (a)–(c) extended with feature-gate dependency | [docs/plans/demo/tech-debt.md](demo/tech-debt.md) |

## Verification

- **Unit:** `go test -race ./internal/featuregate/... ./internal/controller/policy/... ./internal/admission/cosign/...` — gate eval, loader fsnotify reload, controller status convergence, cosign handler with verify-off / failClosed-off paths.
- **Build:** `go vet ./... && go build ./...`.
- **Manifests:** `kustomize build config/featuregates/` + `kustomize build config/cosign-webhook/` parse clean; `kubectl apply --dry-run=client` accepts both.
- **Lint:** `shellcheck -x scripts/featuregate-list.sh`.

## Failure modes

| Failure | Recovery |
|---|---|
| OpenFeature SDK breaks with k8s deps | Hand-roll a minimal provider — public API stable |
| fsnotify race on CM rewrite | Loader debounces 500 ms after first event |
| Kyverno not installed | Operator emits `KyvernoMissing` event; CM still works (RBAC still binds) |
| Webhook starts before CM exists | Loader serves per-stage defaults; alpha gates off, beta on |

## Rollback

`kubectl delete cm -n keese-system keese-features` then scale
`keese-feature-gate-controller` to 0 (per [27 §11](../designs/27-feature-gates-openfeature.md)).
The CRD itself is non-destructive — leaving it in place lets the
design ship in the next release without re-applying CRs.

## Iteration log

### Iteration 1 — 2026-05-06 (Correctness & Security)

| # | Category | Weight | Ratio | Score | Notes |
|---|---|---:|---:|---:|---|
| 1 | Scope clarity | 10 | 1.0 | 10 | Two phases, table-driven, out-of-scope explicit |
| 2 | Architecture fit | 10 | 1.0 | 10 | Aligns with [27](../designs/27-feature-gates-openfeature.md), rule 04, 05.12 |
| 3 | Security posture | 15 | 1.0 | 15 | Kyverno + cosign trust root + projected CM + apiserver-free read path |
| 4 | Automatability | 10 | 1.0 | 10 | Two `make` targets; no manual steps |
| 5 | Verifiability | 15 | 1.0 | 15 | Per-package tests + manifest dry-run + shellcheck |
| 6 | Failure-mode awareness | 10 | 1.0 | 10 | 4-row table + plumbing rollback |
| 7 | Context efficiency | 10 | 1.0 | 10 | Under 200 lines; routes via 27 |
| 8 | Docs quality | 5 | 1.0 | 5 | SPDX, frontmatter, links resolve |
| 9 | Observability | 5 | 1.0 | 5 | Inherits 27 §7 metrics + transition events |
| 10 | Operational readiness | 10 | 1.0 | 10 | Rollback identical to 27 §11; phased delivery preserves green build |
| | **Total** | 100 | | **100** | |

Verdict: SHIP

No top-3 gaps. Implementation begins immediately; follow-ons (other
consumers per [27 §10](../designs/27-feature-gates-openfeature.md))
land as separate plans after Phase B closes.
