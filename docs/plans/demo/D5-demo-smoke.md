<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: plan
category: phase
depends:
  - README.md
  - D4-cloud-deploy.md
  - ../../references/e2e-smoke.md
related_skills: [plan-management, test-engineer]
status: planned
last_verified: 2026-04-25
---

# D5 — Demo smoke + runbook

**Refinement pass:** operational readiness.
**Effort:** 2 h. **Owner agent:** `test-engineer`.

## Goal

Prove the end-to-end happy path on the cloud cluster from D4, capture the
five-step demo runbook, and pre-rehearse the upgrade story so Monday's
demo doesn't surprise anyone.

## Inputs

- D4 cloud cluster up, all CRs Ready.
- Existing kind smoke harness at
  [scripts/dev/e2e-smoke.sh](../../../scripts/dev/e2e-smoke.sh) — adapt for
  cloud target.
- [docs/references/e2e-smoke.md](../../references/e2e-smoke.md) — smoke
  reference doc.

## Tasks

### T1 — End-to-end happy path

From a developer laptop with `kubectl --context gke-keese-demo`:

```sh
SESSION=$(kubectl get pod -n alpha -l keese.ai/session=my-session -o name | head -1)
kubectl exec -n alpha $SESSION -- /usr/local/bin/goose run \
  --text 'Write a Python function that returns the nth Fibonacci number.' \
  --quiet
```

Expected:

1. The pod's stdout returns a Python function.
2. `kubectl logs -n keese-system deploy/envoy-ai-gateway` shows a
   POST `/v1/messages` round-trip with status 200.
3. `kubectl exec -n alpha $SESSION -- ls -la /var/run/keese/memory/`
   shows a `session.db` file with non-zero size.
4. Subsequent prompts in the same session reference earlier turns
   (memory persistence works).

Acceptance: capture the session transcript as
`.plan-logs/D5-happy-path-<timestamp>.txt`.

### T2 — Memory persistence across pod restart

```sh
kubectl delete pod -n alpha $SESSION
# wait for new pod
kubectl wait -n alpha --for=condition=Ready pod -l keese.ai/session=my-session --timeout=60s
NEW_SESSION=$(kubectl get pod -n alpha -l keese.ai/session=my-session -o name | head -1)
kubectl exec -n alpha $NEW_SESSION -- /usr/local/bin/goose run \
  --text 'What was my previous question?' --quiet
```

Expected: the agent's response references the Fibonacci prompt, proving
memory survives pod restart (D2-T5 best-effort drain hook).

If it doesn't, capture as a known limitation in the runbook — full
Drain/Resume SPI is tech debt.

### T3 — Operator restart smoke

```sh
kubectl rollout restart deploy/keese-controller-manager -n keese-system
kubectl rollout status deploy/keese-controller-manager -n keese-system
```

Expected: workspace + session pods are not killed; `kubectl get pod -n
alpha` shows both pods continuously Running. After the operator returns,
edit `Workspace.spec.description` and confirm the new operator
re-reconciles.

Acceptance: pod ages on workspace + session pods exceed the operator
deployment age.

### T4 — Manual InstallPlan upgrade dry-run

Bump a no-op annotation in the CSV, build a `v0.0.1-demo.2` bundle, push,
and approve the InstallPlan. Verify:

- `kubectl get csv -n keese-system` shows `Replacing` then
  `Succeeded` for the new CSV.
- `Replacing` phase does not kill workspace pods.
- `terminationGracePeriodSeconds: 60` (D1-T4) is observed in operator
  pod's eviction timing.

Acceptance: capture timing as `.plan-logs/D5-upgrade-<timestamp>.txt`.

### T5 — Author the demo runbook

Create [docs/references/demo-runbook.md](../../references/) — the
literal kubectl sequence to run during the live demo, in five copy-paste
blocks:

1. `kubectl --context gke-keese-demo get nodes` — sanity.
2. `kubectl apply -f config/samples/tenancy/tenant-minimal.yaml` —
   provision Tenant.
3. `kubectl apply -f config/samples/{runtime,workspace,memory,workspace/workspacesession-minimal}.yaml` — provision the workspace stack.
4. `kubectl wait --for=condition=Ready workspacesession my-session -n
   alpha --timeout=120s` — block until ready.
5. `kubectl exec -n alpha $(kubectl get pod -n alpha -l
   keese.ai/session=my-session -o name | head -1) -- goose run --text
   '<live demo prompt>'` — the money shot.

Plus an "if anything breaks" backup section with the three most likely
failure modes from D3+D4 failure tables and their one-line recoveries.

Cap at 200 lines, frontmatter + SPDX, link from
[CLAUDE.md task table](../../../CLAUDE.md) under a new "Run the demo"
row.

### T6 — Rollback drill

Practice deleting and recreating the demo from scratch in <10 min:

```sh
kubectl delete -f config/samples/workspace/ -n alpha
kubectl delete -f config/samples/{tenancy,runtime,memory}/ -n alpha
operator-sdk cleanup keese --namespace keese-system
make bootstrap-infra-clean   # add this Makefile target if missing
```

Acceptance: from a clean cluster, the full demo path replays in
<10 min using only the runbook.

## Out of scope (→ tech-debt §verification)

- Automated kuttl e2e under `tests/e2e/`.
- Chaos / network-partition tests.
- Multi-LLM smoke.
- Multi-tenant smoke.
- Performance / latency budget tests.

## Verification

- `.plan-logs/D5-happy-path-<timestamp>.txt` shows successful Anthropic
  round-trip.
- `.plan-logs/D5-upgrade-<timestamp>.txt` shows zero workspace-pod
  restarts during operator upgrade.
- The runbook reads cleanly to a stranger (have someone unfamiliar with
  the project execute it on Sunday evening).
- `MEMORY.md` updated with date + outcome of demo dry-run.

## Failure modes

| Failure | Symptom | Recovery |
|---|---|---|
| Anthropic returns 200 but content is empty | gateway stripped headers | check `BackendSecurityPolicy` template injection; D3-T2 |
| Memory persists across pod restart but is corrupted | sqlite WAL + RWO mid-write | accept as known issue; flag tech debt; advise restart-and-retry in runbook |
| Operator rolling-update kills workspace pod | reconciler too aggressive | confirm SSA fieldOwner; investigate predicate that drops `keese.ai/managed` filter (D1-T2) |
| Demo cluster runs out of quota | Pending Pods | reduce replica counts in helmfile values; OpenFGA, eck-operator are common culprits |

## Iteration log

### Iteration 1 — 2026-04-25

| # | Category | Weight | Ratio | Score | Notes |
|---|---|---:|---:|---:|---|
| 1 | Scope clarity | 10 | 1.0 | 10 | Four smoke checks + runbook + rollback drill |
| 2 | Architecture fit | 10 | 1.0 | 10 | Validates rules 04 (k8s), 06-signal (drain) end-to-end |
| 3 | Security posture | 15 | 0.5 | 7.5 | Validates LLM call path; doesn't validate ext_authz (permit-all in demo) |
| 4 | Automatability | 10 | 0.5 | 5 | Smoke is scripted; runbook authoring is a manual write |
| 5 | Verifiability | 15 | 1.0 | 15 | Each task has an acceptance check + log artifact |
| 6 | Failure-mode awareness | 10 | 1.0 | 10 | 4-row table; rollback drill mandatory |
| 7 | Context efficiency | 10 | 1.0 | 10 | <200 lines; runbook is a separate file |
| 8 | Docs quality | 5 | 1.0 | 5 | SPDX + frontmatter + links |
| 9 | Observability | 5 | 0.5 | 2.5 | Logs captured; no metrics dashboard |
| 10 | Operational readiness | 10 | 1.0 | 10 | Upgrade dry-run + rollback drill + runbook |
| | **Total** | 100 | | **85** | |

Verdict: SHIP

Top gaps:
1. ext_authz path not exercised — relies on D3 tech-debt P1 unblocking.
2. Smoke is manual; CI doesn't run it. Tech-debt §verification.
3. Demo runbook depends on the live cluster — no offline rehearsal artifact.

Next step: T1 → T2 → T3 → T5 (runbook) sequential; T4 + T6 same evening before demo.
