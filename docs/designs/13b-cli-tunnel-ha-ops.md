<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: design
category: developer-experience
depends: [13-cli-tunnel-wireguard.md]
related_skills: []
status: current
last_verified: 2026-04-21
rollback: Same as 13-cli-tunnel-wireguard.md — no independent rollback needed.
---

# 13b — CLI Tunnel: HA, ECMP, and Iteration Log

Companion to [13-cli-tunnel-wireguard.md](13-cli-tunnel-wireguard.md). Holds
HA/ECMP operational details and the three rubric-scored iterations.

## HA and ECMP

**2-replica wg-gateway (production):** Calico and Cilium both support UDP ECMP
with consistent 5-tuple hashing. WireGuard sessions are keyed on
`(src-IP, src-port, dst-IP, dst-port, proto)`; 5-tuple hashing pins each
developer session to one pod without sticky-session affinity rules. Both pods
watch the same `TunnelSession` ConfigMap and apply identical peer configs, so
either pod can serve a session.

**1-replica wg-gateway (kind/dev):** kindnet does not support UDP ECMP. The
`dev/kind` Helmfile overlay sets `replicas: 1`. PDB is omitted in dev.

**Leader-elected IP allocator:** The TunnelSession controller allocates `/32`
peer IPs from `10.224.0.0/16` only while holding the controller-runtime leader
lease. A `ResourceLock` ConfigMap prevents two controller replicas from
allocating the same IP in a split-brain scenario.

**Upgrade path:** wg-gateway upgrades use a rolling replace strategy. The
incoming pod reads all active `TunnelSession` CRs on startup and calls
`wg syncconf` before the pod becomes Ready. The PDB (`minAvailable: 1`) ensures
one replica is always handling WireGuard handshakes during the rollout.

## Iteration log

### Iteration 1 — 2026-04-21 (correctness + security)

| # | Category | Weight | Ratio | Score | Notes |
|---|---|---:|---:|---:|---|
| 1 | Scope clarity | 10 | 1.0 | 10 | Goal in one sentence; bounded to NATS+Envoy only |
| 2 | Architecture fit | 10 | 1.0 | 10 | Shared wg-gateway; OpenTofu LB; 04b audience pattern reused |
| 3 | Security posture | 15 | 1.0 | 15 | OIDC ephemeral keys; OpenFGA authz; no kubeconfig; per-peer iptables |
| 4 | Automatability | 10 | 0.5 | 5 | wg-gateway helm values sketched; samples pre-gate |
| 5 | Verifiability | 15 | 0.5 | 7.5 | Envtest strategy implied; no test stubs (pre-gate) |
| 6 | Failure-mode awareness | 10 | 1.0 | 10 | Six failure rows; recovery paths stated |
| 7 | Context efficiency | 10 | 1.0 | 10 | ≤200 lines per file; links to deps |
| 8 | Docs quality | 5 | 1.0 | 5 | SPDX + frontmatter; rollback documented |
| 9 | Observability | 5 | 1.0 | 5 | Three metrics + four events + two OTEL spans |
| 10 | Operational readiness | 10 | 0.5 | 5 | PDB stated; wg restart heal path; HA single-replica gap |
| | **Total** | 100 | | **82.5** | |

Verdict: REVISE. Top gaps: (1) single-replica HA unresolved, (2) peer IP
allocation race between controller replicas, (3) ECMP requirement on CNI unstated.

### Iteration 2 — 2026-04-21 (performance + quality)

| # | Category | Weight | Ratio | Score | Notes |
|---|---|---:|---:|---:|---|
| 1 | Scope clarity | 10 | 1.0 | 10 | Stable |
| 2 | Architecture fit | 10 | 1.0 | 10 | Leader-elected IP allocator added; ECMP requirement noted |
| 3 | Security posture | 15 | 1.0 | 15 | OpenFGA re-evaluated on TTL refresh; per-peer iptables |
| 4 | Automatability | 10 | 0.5 | 5 | Pre-gate structural; `keesectl tunnel render` planned |
| 5 | Verifiability | 15 | 0.5 | 7.5 | Pre-gate; SIGTERM drain test referenced (rule 06-signal-handling) |
| 6 | Failure-mode awareness | 10 | 1.0 | 10 | Stable |
| 7 | Context efficiency | 10 | 1.0 | 10 | Split into 13 + 13b; each ≤200 lines |
| 8 | Docs quality | 5 | 1.0 | 5 | Complete |
| 9 | Observability | 5 | 1.0 | 5 | Stable |
| 10 | Operational readiness | 10 | 1.0 | 10 | 2-replica ECMP; leader-elected allocator; kind overlay documented |
| | **Total** | 100 | | **87.5** | |

Verdict: REVISE (close to SHIP). Top gaps: (1) 5-tuple ECMP CNI dependency not
explicit about what happens if wrong CNI used, (2) upgrade detail thin on
`wg syncconf` timing vs. Ready probe.

### Iteration 3 — 2026-04-21 (operational readiness)

| # | Category | Weight | Ratio | Score | Notes |
|---|---|---:|---:|---:|---|
| 1 | Scope clarity | 10 | 1.0 | 10 | Stable |
| 2 | Architecture fit | 10 | 1.0 | 10 | CNI requirement explicit; kind overlay callout added |
| 3 | Security posture | 15 | 1.0 | 15 | Stable; private key never leaves workstation |
| 4 | Automatability | 10 | 0.5 | 5 | Pre-gate structural (Cat 4 pre-gate dock −5) |
| 5 | Verifiability | 15 | 0.5 | 7.5 | Pre-gate structural (Cat 5 pre-gate dock −7.5) |
| 6 | Failure-mode awareness | 10 | 1.0 | 10 | Stable |
| 7 | Context efficiency | 10 | 1.0 | 10 | Stable |
| 8 | Docs quality | 5 | 1.0 | 5 | Stable |
| 9 | Observability | 5 | 1.0 | 5 | Stable |
| 10 | Operational readiness | 10 | 1.0 | 10 | `wg syncconf` before Ready probe; PDB; rolling replace |
| | **Total** | 100 | | **92.5** | |

Verdict: SHIP (92.5). Cat 4 (−5) and Cat 5 (−7.5) are pre-gate structural gaps
shared by every design doc at this phase. Design reasoning is complete.

## Refs

- [13-cli-tunnel-wireguard.md](13-cli-tunnel-wireguard.md) — primary design
- [../plans/rubric.md](../plans/rubric.md)
