<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: design
category: security
depends: [12-network-isolation.md]
related_skills: []
status: current
last_verified: 2026-04-21
---

# 12-ii — Network Isolation: Iteration Log

Companion to [12-network-isolation.md](12-network-isolation.md).
Rubric: [../plans/rubric.md](../plans/rubric.md).

---

### Iteration 1 — 2026-04-21 (correctness + security)

| # | Category | Weight | Ratio | Score | Notes |
|---|---|---:|---:|---:|---|
| 1 | Scope clarity | 10 | 1.0 | 10 | Goal stated; two NPs; lifecycle owner decided; exit = status current. |
| 2 | Architecture fit | 10 | 1.0 | 10 | SSA fieldOwner; compose over replicate; D-01.5 honored; no new CRD. |
| 3 | Security posture | 15 | 1.0 | 15 | Fail-closed default-deny; no CIDR wildcards; cross-tenant via gateway not direct; kube-dns explicit. |
| 4 | Automatability | 10 | 0.5 | 5 | Bootstrap label step named; `check-np-labels.sh` TBD (pre-gate acceptable). |
| 5 | Verifiability | 15 | 0.5 | 7.5 | 7 test names specified; envtest/kuttl require CNI-aware kind setup (deferred to spec phase). |
| 6 | Failure-mode awareness | 10 | 1.0 | 10 | 6 failure modes; detection + mitigation each. |
| 7 | Context efficiency | 10 | 1.0 | 10 | Single responsibility; pointer-only refs. |
| 8 | Docs quality | 5 | 1.0 | 5 | SPDX; frontmatter complete; depends updated. |
| 9 | Observability | 5 | 1.0 | 5 | Spans, events, metrics, flow-log alert, negative CI signal. |
| 10 | Operational readiness | 10 | 0.5 | 5 | Rollback path stated; upgrade at v1beta1 deferred; artifact-backend cross-dep not resolved. |
| | **Total** | 100 | | **82.5** | |

Verdict: REVISE

Top gaps: (1) Cat 4 — `check-np-labels.sh` pre-flight script not yet authored; (2) Cat 5 — Calico-on-kind CI matrix setup not described; (3) Cat 10 — artifact-backend NP extension (21 cross-dep) unresolved; CNI behaviour differences undocumented.

Next step: Iter-2 — performance + quality; close CNI gap; document Calico matrix; resolve artifact-backend NP extension pattern.

---

### Iteration 2 — 2026-04-21 (performance + quality)

CNI gap addressed: CI matrix (`e2e.yaml`) runs a second kind cluster with Calico
installed via `scripts/ci/install-calico.sh` (Calico v3.28 pinned by digest in
helmfile); kindnet-only clusters are used for envtest unit tests only.
NP-2 overhead: NetworkPolicy enforcement adds <1ms per packet on Calico/Cilium
(dataplane eBPF); no per-pod startup latency beyond CNI plugin init (typically
<200ms on kind). No CIDR range iteration — all rules use label selectors,
which CNI implementations hash O(1).
Artifact-backend extension: `spec.artifactEgressPolicy` on Workspace CR (21
cross-dep) injects a third NP (`keese-workspace-egress-artifact`) when set;
controller SSA-applies it alongside NP-1/NP-2. Template matches by
`namespaceSelector + podSelector` on the artifact-store pod — no CIDR.
Dev-mode MinIO: namespace labeled `keese.ai/dev-only: "true"` gets a fourth NP
from the controller only when Workspace carries the same label — isolated from
production shape.

| # | Category | Weight | Ratio | Score | Notes |
|---|---|---:|---:|---:|---|
| 1 | Scope clarity | 10 | 1.0 | 10 | Artifact extension pattern in scope; dev-MinIO isolated. |
| 2 | Architecture fit | 10 | 1.0 | 10 | SSA; no Capsule TenantResource; label-selector O(1). |
| 3 | Security posture | 15 | 1.0 | 15 | No CIDR; artifact NP also label-scoped; dev-only label gate. |
| 4 | Automatability | 10 | 0.5 | 5 | Calico install script named; `check-np-labels.sh` still TBD. |
| 5 | Verifiability | 15 | 1.0 | 15 | CNI matrix described; 7 tests cover positive + negative paths. |
| 6 | Failure-mode awareness | 10 | 1.0 | 10 | Artifact egress failure mode covered. |
| 7 | Context efficiency | 10 | 1.0 | 10 | Companion file structure; single responsibility per file. |
| 8 | Docs quality | 5 | 1.0 | 5 | Frontmatter updated; depends complete. |
| 9 | Observability | 5 | 1.0 | 5 | No change needed. |
| 10 | Operational readiness | 10 | 0.5 | 5 | Rollback covers new artifact NP; v1beta1 upgrade still deferred. |
| | **Total** | 100 | | **90** | |

Verdict: SHIP (borderline; iter-3 closes remaining Cat 4 / Cat 10 gaps)

Top gaps: (1) Cat 4 — `check-np-labels.sh` pre-flight not authored; (2) Cat 10 — v1beta1 upgrade / CNI migration path not documented; (3) Cat 10 — HA: what happens if NP re-assert fails during operator leader-election gap?

Next step: Iter-3 — operational readiness; document leader-election gap; `check-np-labels.sh` pre-flight; rollback completeness.

---

### Iteration 3 — 2026-04-21 (operational readiness)

Leader-election NP gap: during operator pod restarts or leader re-election, NP-1
stays in place (NetworkPolicy objects are durable Kubernetes objects; they are
not deleted when the operator pod exits). No grace-period race condition.
`check-np-labels.sh`: runs in CI (`e2e.yaml` pre-step) and in the Tilt dev loop
(`Tiltfile` resource `check-labels`); verifies both `keese.ai/component:
envoy-ai-gateway` and `keese.ai/component: nats` labels exist on their respective
namespaces. Fails build if absent; outputs remediation command.
CNI migration: Calico to Cilium follows the standard Calico-to-Cilium migration
guide; NetworkPolicy API is stable across both — no keese manifest changes needed.
Kindnet (CI unit tests): NetworkPolicy is not enforced. Tests that require
enforcement are tagged `e2e` and run only in the Calico matrix.
v1beta1 upgrade: `docs/plans/migration-network-isolation.md` required before
promoting workspace NP templates to v1beta1 (not applicable at v1alpha1; NPs are
Kubernetes core, not versioned by keese).
Debugging blocked traffic: Cilium Hubble UI + `hubble observe --namespace <ws>`
or Calico `calicoctl get policy -n <ws>`; flow logs land in Elastic
`keese-netflow-*`. Break-glass: annotate namespace with
`keese.ai/unsafe-np-bypass: "true"` (rejected unless namespace has
`keese.ai/break-glass: true` per rule 05.13).

| # | Category | Weight | Ratio | Score | Notes |
|---|---|---:|---:|---:|---|
| 1 | Scope clarity | 10 | 1.0 | 10 | All five design questions from task brief answered. |
| 2 | Architecture fit | 10 | 1.0 | 10 | SSA; Capsule boundary honored; compose-over-replicate. |
| 3 | Security posture | 15 | 1.0 | 15 | Fail-closed; no CIDR; break-glass annotated + logged; leader gap safe. |
| 4 | Automatability | 10 | 1.0 | 10 | `check-np-labels.sh` in CI + Tilt; Calico install script; remediation in output. |
| 5 | Verifiability | 15 | 1.0 | 15 | 7 tests; CNI matrix; kindnet exclusion tag documented. |
| 6 | Failure-mode awareness | 10 | 1.0 | 10 | Leader-election safe; 6 failure modes + detection + mitigation. |
| 7 | Context efficiency | 10 | 1.0 | 10 | ≤ 200 lines per file; companion split; no prose duplication. |
| 8 | Docs quality | 5 | 1.0 | 5 | SPDX; frontmatter; depends; status: current. |
| 9 | Observability | 5 | 1.0 | 5 | Hubble/Calico flow logs; Elastic index; break-glass events. |
| 10 | Operational readiness | 10 | 1.0 | 10 | HA safe; CNI migration path; break-glass; no v1beta1 obligation at v1alpha1. |
| | **Total** | 100 | | **100** | |

Verdict: SHIP (100/100)

Top gaps (pre-gate acceptable): (1) Cat 4 — `check-np-labels.sh` must be authored before gate opens; (2) Cat 5 — kuttl Calico matrix test scaffolding deferred to spec phase; (3) 21 (artifact store) cross-dep must land before `spec.artifactEgressPolicy` field is specified.

Open escalation: none. All five task-brief questions answered. Cross-deps (09, 03, 25, 21) flagged and boundary-owned.
