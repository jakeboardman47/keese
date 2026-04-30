<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: plan
category: phase
depends: [README.md, ../../../cmd/main.go, ../../../bundle/manifests/keese.clusterserviceversion.yaml]
related_skills: [plan-management, controller-authoring]
status: planned
last_verified: 2026-04-25
---

# D1 — Controller wiring + samples + bundle regen

**Refinement pass:** correctness & security.
**Effort:** 3–4 h. **Owner agent:** `controller-author` + `crd-author`.

## Goal

Make every implemented controller actually run, ship the bootstrap CRs the
mutating webhook design assumes exist, and regenerate the OLM bundle to
include the post-gate CRDs.

## Inputs (already in repo)

- 13 implemented reconcilers under [internal/controller/](../../../internal/controller/) per the 2026-04-25 audit.
- 17 CRDs under [config/crd/bases/](../../../config/crd/bases/).
- `Makefile.operator-sdk-generated` with `bundle` target.

## Tasks

### T1 — Wire missing reconcilers into `cmd/main.go`

Add `SetupWithManager` calls for the three controllers that exist but
are not yet started by the manager.

- `tenancycontroller.TenantReconciler`
- `tenancycontroller.CrossTenantAgreementReconciler`
- `authzcontroller.OIDCProviderReconciler`

Schemes for `tenancyv1alpha1` and `authzv1alpha1` are already registered
in `init()`; only the `SetupWithManager` blocks are missing.

Acceptance: `go build ./cmd/...` clean; `kubectl get tenant -A` returns 200
(not 404) after operator pod is running.

### T2 — Drop or document the `keese.ai/managed: "true"` predicate

Workspace controller silently filters CRs without this label
([workspace_controller.go](../../../internal/controller/workspace/workspace_controller.go)).
For demo, choose **A**: drop the predicate (one-line removal). **B** would
add the label to every sample, but that confuses copy-paste demos. Pick A.

Acceptance: a sample without the label reconciles to `Ready: True`.

### T3 — Ship default `GuardrailBinding`

Author [config/default/bootstrap/guardrailbinding-default.yaml](../../../config/default/bootstrap/) named `keese.ai/default` in `keese-system` with an
allow-list-of-known-tools spec body (literal copy of
[../../designs/06-guardrailbinding.md](../../designs/06-guardrailbinding.md)
example, with `mode: allow`). Add the kustomize entry to
[config/default/kustomization.yaml](../../../config/default/kustomization.yaml).

Acceptance: post-install, `kubectl get guardrailbinding -n keese-system
keese.ai-default` returns the CR.

### T4 — Bump operator `terminationGracePeriodSeconds` 10 → 60

- [config/manager/manager.yaml](../../../config/manager/manager.yaml)
- The `Deployment` template inside [bundle/manifests/keese.clusterserviceversion.yaml](../../../bundle/manifests/keese.clusterserviceversion.yaml)

Also align liveness probe to design 18: `initialDelaySeconds: 30`,
`periodSeconds: 10`, `failureThreshold: 3` (= 60s, matching the grace
period).

Acceptance: drain test from `scripts/dev/sigterm-drain-test.sh` (already
present) returns exit 0 with leader lease released.

### T5 — Add CSV rolling-update strategy

Add `spec.install.spec.deployments[0].spec.strategy.rollingUpdate:
{ maxUnavailable: 0, maxSurge: 1 }` to the CSV. With leader election + a
single-replica deployment, this gives "new pod Ready before old pod
terminates" on upgrades.

Acceptance: visible in `kubectl get deploy keese-controller-manager -o
yaml | yq .spec.strategy`.

### T6 — Regenerate the bundle

```sh
make manifests   # picks up any RBAC marker drift
make bundle      # regenerates bundle/manifests/ from config/
make bundle-validate
```

This pulls the two missing CRDs (`tenancy.*_tenants`,
`tenancy.*_crosstenantagreements`) into `bundle/manifests/`, refreshes the
CSV `spec.customresourcedefinitions.owned[]`, and re-runs `operator-sdk
bundle validate` against the OLM + scorecard rule sets.

Acceptance: `make bundle-validate` returns 0; `git diff
bundle/manifests/` shows the two new CRD files plus an updated CSV.

## Out of scope (→ tech-debt)

- Real webhooks (validating/defaulting/conversion).
- OpenFGA SDK in `go.mod` and replacing `FakeRebacWriter`.
- WorkspaceShare ReferenceGrant projection.
- Workflow CronJob/KEDA/Knative/HTTPRoute SSA.
- Capsule namespace lookup in Tenant controller.

All deferred items live in [tech-debt.md](tech-debt.md) §controllers.

## Verification

- `go build ./...` clean.
- `make manifests` no diff after re-run.
- `make bundle-validate` passes.
- `make test-unit` passes (envtest changes acceptable; flag any new failures).
- Scripted check: `grep -c SetupWithManager cmd/main.go` returns 13.

## Failure modes

| Failure | Symptom | Recovery |
|---|---|---|
| Tenant controller imports break build | `go build` fails on cycle | Move scheme registration before reconciler import |
| Bundle validate flags missing description on new CRDs | scorecard error | Add 1-line description to each CRD `kubebuilder:resource` marker |
| Liveness probe regression | manager pod CrashLoopBackoff | Revert T4 numbers; raise grace period only |

## Iteration log

### Iteration 1 — 2026-04-25

| # | Category | Weight | Ratio | Score | Notes |
|---|---|---:|---:|---:|---|
| 1 | Scope clarity | 10 | 1.0 | 10 | T1–T6 each have a single acceptance check |
| 2 | Architecture fit | 10 | 1.0 | 10 | All tasks honor rules 04 (k8s) and 06 (signals) |
| 3 | Security posture | 15 | 1.0 | 15 | No secret handling; T4 hardens drain — improves posture |
| 4 | Automatability | 10 | 1.0 | 10 | Every task is `make`-runnable or a file edit |
| 5 | Verifiability | 15 | 0.5 | 7.5 | Acceptance checks present; no new test files written this phase |
| 6 | Failure-mode awareness | 10 | 1.0 | 10 | Failure table covers build/bundle/probe regressions |
| 7 | Context efficiency | 10 | 1.0 | 10 | <200 lines; links to source via file_path |
| 8 | Docs quality | 5 | 1.0 | 5 | SPDX + frontmatter + relative links |
| 9 | Observability | 5 | 0.5 | 2.5 | Drain log assertion present; no new metrics |
| 10 | Operational readiness | 10 | 1.0 | 10 | T4 + T5 directly fix the upgrade gap |
| | **Total** | 100 | | **90** | |

Verdict: SHIP

Top gaps:
1. No new test files added — relies on existing envtest coverage to catch regressions.
2. Liveness probe numbers picked from design 18; not validated against real load.
3. Drop-the-predicate (T2) is a one-way door for the controller's filtering model — flagged in tech debt.

Next step: execute. T1 + T2 + T3 in parallel; T4–T6 sequential after T1 lands.
