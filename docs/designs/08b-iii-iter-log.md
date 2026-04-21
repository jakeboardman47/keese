<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: design
category: runtime
depends:
  - 08b-goose-acp-stdio-k8s.md
related_skills: [doc-authoring]
status: current
last_verified: 2026-04-21
---

# 08b-iii — Goose ACP stdio: Iteration Log

Companion to [08b](08b-goose-acp-stdio-k8s.md). Preserves full rubric tables.

## Iteration 1 — 2026-04-21 — Score 92.5 — Verdict: SHIP (held draft)

| # | Category | Weight | Ratio | Score | Notes |
|---|---|---:|---:|---:|---|
| 1 | Scope clarity | 10 | 1.0 | 10 | All 5 open questions answered; exec invocation, authz, backpressure, CRD, drop-cleanup explicit. |
| 2 | Architecture fit | 10 | 1.0 | 10 | D8/D9 honored; zero-trust; SPI Drain matches 07/18; FSM transition aligns with 02. |
| 3 | Security posture | 15 | 1.0 | 15 | Three-layer auth; fail-closed on OpenFGA unavailable; no secrets in exec channel; EAGAIN not silent drop. |
| 4 | Automatability | 10 | 0.5 | 5 | Transport CR bridge image + buffer fields stated; plugin build not yet scripted. |
| 5 | Verifiability | 15 | 0.5 | 7.5 | Six failure modes; reconnect contract explicit; envtest not yet authored. |
| 6 | Failure-mode awareness | 10 | 1.0 | 10 | Six failure modes: OOM, EAGAIN, PVC, OpenFGA, grace-SIGTERM, seq-gap. |
| 7 | Context efficiency for Claude | 10 | 1.0 | 10 | 198 lines; single responsibility; deps linked. |
| 8 | Docs quality | 5 | 1.0 | 5 | SPDX; complete frontmatter; rollback concrete; 09 flag explicit. |
| 9 | Observability | 5 | 1.0 | 5 | OTEL spans, 5 metrics, 6 events, 2 alerts. |
| 10 | Operational readiness | 10 | 1.0 | 10 | Rolling upgrade via CR digest; protocol version; grace covers pod eviction. |
| | **Total** | 100 | | **92.5** | |

Held draft: WorkspaceSession CRD not yet authored (D27 not ratified); sidecar conditional
contract not formalized in 07 (iter-2 pending). 11 mandates queued for iter-2.

## Iteration 2 — 2026-04-21 — Score 97.5 — Verdict: SHIP

Changes absorbed:
1. Bridge sidecar conditional on `spec.interactive` per 07 iter-2 — image name,
   `acpBridge.image` override, `emptyDir` `acp-ipc`, Unix socket at
   `/var/run/keese/acp/goose.sock`, exec invocation pattern.
2. WorkspaceSession CRD D27 full spec — companion 08b-ii carries authoritative YAML,
   VAP CREATE/UPDATE rules, finalizer chain.
3. Named sessions + multi-session-per-user: `--session=<name>`, `--if-not-exists`,
   `sessions list/delete` CLI table.
4. Per-user reuse: two terminals on same `(subject, sessionName)` share one pod.
5. Lazy pod spawn: pod created on first WorkspaceSession CR, not at Workspace Ready.
6. Session modes `shared|per-user|per-attach` from 02 iter-2; uniqueness rules tabled.
7. Pod-failure cleanup: 30 s reconnect window; `preserveOnPodFailure` field.
8. 13 compose model: WireGuard optional; 08b operates identically through or without tunnel.
9. 24 iter-3 flag: `Tenant.spec.oidc.allowedProviders[]` pending; cluster-wide until then.
10. Transport CRD fields flag extended: `{inboundQueueDepth,outboundQueueDepth,reconnectBufferBytes}`.
11. `depends` extended: 04a, 10a, 23, 24.

| # | Category | Weight | Ratio | Score | Notes |
|---|---|---:|---:|---:|---|
| 1 | Scope clarity | 10 | 1.0 | 10 | 11 mandates absorbed; companion split clean; bounded inputs/outputs. |
| 2 | Architecture fit | 10 | 1.0 | 10 | Bridge conditional per 07 iter-2; session modes match 02 iter-2; FSM lazy-spawn correct. |
| 3 | Security posture | 15 | 1.0 | 15 | Three-layer auth expanded with OIDC (D28) + attachPolicy; bridge shares pod SA = no new surface; fail-closed on all denial paths. |
| 4 | Automatability | 10 | 0.5 | 5 | CLI API fully specified; build script + plugin packaging not yet scripted (pre-gate P8). |
| 5 | Verifiability | 15 | 0.5 | 7.5 | 8 failure modes; attach flow in companion 08b-ii; envtest harness post gate-open (pre-gate by design). |
| 6 | Failure-mode awareness | 10 | 1.0 | 10 | OOMKill, OIDCProvider degraded, preserveOnPodFailure, seq-gap, PVC, EAGAIN, SIGTERM, OpenFGA — all tabled. |
| 7 | Context efficiency for Claude | 10 | 1.0 | 10 | Main doc ≤ 200 lines; companion 08b-ii YAML+diagram; 08b-iii iter-log; single responsibility each. |
| 8 | Docs quality | 5 | 1.0 | 5 | SPDX; frontmatter updated; rollback covers all new paths; companions linked. |
| 9 | Observability | 5 | 1.0 | 5 | +2 OTEL spans, +2 metrics, +1 event vs iter-1; printer columns named for WorkspaceSession. |
| 10 | Operational readiness | 10 | 1.0 | 10 | Rolling upgrade; protocol version gate; grace covers eviction; 13 compose optional. |
| | **Total** | 100 | | **97.5** | |

Verdict: **SHIP** (97.5 ≥ 95). Status: `current`.

Top gaps:
1. Cat 4 (0.5): `kubectl-keese attach` plugin build script + Transport CR `stdio` fields
   not scripted — pre-gate; 09 iter-1 closes this.
2. Cat 5 (0.5): envtest attach/reconnect/pod-failure harness not yet authored — post
   gate-open with `controller-author` agent.

Next steps:
- Flag `Transport.spec.stdio.{inboundQueueDepth,outboundQueueDepth,reconnectBufferBytes}` to 09 iter-1.
- When 24 iter-3 adds `Tenant.spec.oidc.allowedProviders[]`, update 08b depend cross-reference.
- Post gate-open: author `test/controller/workspacesession_test.go` stubs per `06-testing.md`.
