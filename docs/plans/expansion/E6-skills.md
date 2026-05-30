<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: plan
category: phase
depends:
  - README.md
  - E2-a2a-protocol.md
  - ../../../api/keese/v1alpha1/workspace_types.go
  - ../../designs/04a-openfga-authz-model.md
  - ../../designs/15-memory-management.md
related_skills: [plan-management, crd-authoring, controller-authoring]
status: planned
last_verified: 2026-05-13
---

# E6 — Skills CRD

**Refinement pass:** correctness & security.
**Effort:** 1 week. **Owner agent:** `crd-author`.

## Goal

Add `Skill`, `SkillSource`, and `SharedSkill` CRDs. Skills are versioned capability
bundles (OCI or git) projected into the workspace pod via an init container. ReBAC
gates per-workspace and cross-tenant skill access. Pattern mirrors `Memory` +
`SharedMemory` design.

## Inputs

- Workspace types (add `skillRefs`):
  [`api/keese/v1alpha1/workspace_types.go`](../../../api/keese/v1alpha1/workspace_types.go)
- Memory design (structural reference):
  [`docs/designs/15-memory-management.md`](../../designs/15-memory-management.md)
- OpenFGA model: [`docs/designs/04a-openfga-authz-model.md`](../../designs/04a-openfga-authz-model.md)

## Tasks

### T1 — `Skill` CRD

`api/keese/v1alpha1/skill_types.go`. Namespaced. ShortName `sk`.

Spec:
- `SourceRef SkillSourceRef` (name + optional namespace).
- `Version string` (OCI tag or git ref; must be digest-pinned for OCI in prod).
- `MountPath string` (default `/var/run/keese/skills/<name>`).

Status: `Phase (Pending|Ready|Degraded)`, `Digest string` (resolved OCI digest),
`Conditions`.

Printer columns: `Phase`, `Source`, `Age`.

ReBAC tuple: `skill:S#owner@tenant:T` written on create; `skill:S#reader@workspace:W`
written by WorkspaceSession reconciler when `Workspace.spec.skillRefs` includes this
skill. Markers: `// +keese:rebac-tuple=owner` on `SourceRef`;
`// +keese:rebac-tuple=reader` on workspace binding.

### T2 — `SkillSource` CRD

`api/keese/v1alpha1/skillsource_types.go`. Namespaced.

Spec: discriminated one-of `oci` or `git`.
- `OCI`: `repository`, `tag`, `digestPin` (required in prod — VAP `SkillSourceDigestPinned`).
- `Git`: `url`, `ref`, `subPath`.

Reconciler fetches and caches the source metadata; stores resolved digest in `status`.

### T3 — `SharedSkill` CRD

`api/keese/v1alpha1/sharedskill_types.go`. Cluster-scoped. Mirrors `SharedMemory`.

Spec: `skillRef` (namespaced skill); `allowedTenants []string` (empty = all tenants).
Cross-tenant access gated by CTA per E2 semantics.

ReBAC: `sharedskill:SS#reader@tenant:T` tuple written for each entry in `allowedTenants`.

### T4 — Workspace `skillRefs`

Add `SkillRefs []LocalObjectReference` to `WorkspaceSpec`. WorkspaceSession reconciler
projects listed skills via an init container:
- Init container image: `$(OPERATOR_IMAGE_BASE)/skill-fetcher:$(VERSION)`.
- Fetches each skill's OCI content (using `SkillSource.status.digest`) into a
  shared `emptyDir` volume at `Skill.spec.mountPath`.
- Auth: init container uses the same projected SA token (audience
  `keese-egress-<tenant>`).

Acceptance: workspace pod has init container `skill-fetcher` completing before main
container starts.

### T5 — VAPs

- `SkillSourceDigestPinned`: OCI-sourced skills must have `digestPin` set outside
  `keese.ai/environment=dev` namespaces.
- `SharedSkillMutationAuthz`: `SharedSkill` updates restricted to cluster-admin or
  namespace with `keese.ai/skill-admin=true` label.

### T6 — Envtest suite

- `TestSkillProjection_InitContainer`: Workspace with `skillRefs` produces an init
  container with correct volume mounts.
- `TestSkillReBACTuple_Written`: creating a Skill writes `owner` tuple; attaching to
  Workspace writes `reader` tuple.
- `TestSharedSkillCrossTenant_Blocked`: cross-tenant access denied without CTA.

## Acceptance criteria

- Workspace with `skillRefs` has skill content available at `mountPath` after pod start.
- ReBAC tuples present for owner + reader.
- Cross-tenant SharedSkill blocked without CTA; allowed with one.
- `SkillSourceDigestPinned` VAP enforced in prod namespaces.

## Risks + mitigations

| Risk | Mitigation |
|---|---|
| OCI pull in init container needs registry credentials | Use projected SA token + ECR/GAR IRSA/WI on the init container; same egress path |
| Init container failures block workspace startup | Set `restartPolicy: Never` on init; emit `SkillFetchFailed` event with specific SkillSource name |
| SharedSkill + CTA interaction is E2-dependent | Stub cross-tenant check in E6; full enforcement wired in E2 follow-up |

## Refs

- [E2-a2a-protocol.md](E2-a2a-protocol.md)
- [`docs/designs/15-memory-management.md`](../../designs/15-memory-management.md)
- [`docs/designs/04a-openfga-authz-model.md`](../../designs/04a-openfga-authz-model.md)

## Iteration log

### Iteration 1 — 2026-05-13

| # | Category | Weight | Ratio | Score | Notes |
|---|---|---:|---:|---:|---|
| 1 | Scope clarity | 10 | 1.0 | 10 | 6 tasks; three CRDs + workspace extension |
| 2 | Architecture fit | 10 | 1.0 | 10 | Mirrors Memory + SharedMemory pattern per D23 |
| 3 | Security posture | 15 | 1.0 | 15 | Digest pin VAP; ReBAC owner+reader; CTA cross-tenant |
| 4 | Automatability | 10 | 1.0 | 10 | make manifests + envtest |
| 5 | Verifiability | 15 | 1.0 | 15 | 3 named envtest tests |
| 6 | Failure-mode awareness | 10 | 1.0 | 10 | Init container failure event; cross-tenant stub |
| 7 | Context efficiency | 10 | 1.0 | 10 | <200 lines |
| 8 | Docs quality | 5 | 1.0 | 5 | SPDX + frontmatter |
| 9 | Observability | 5 | 0.5 | 2.5 | Event on fetch failure; no metrics for skill loads |
| 10 | Operational readiness | 10 | 1.0 | 10 | Cross-tenant stub noted; full enforcement in E2 follow-up |
| | **Total** | 100 | | **97.5** | |

Verdict: SHIP

Top gaps:
1. Skill load metrics (count, latency) deferred.
2. Cross-tenant SharedSkill full enforcement is E2-dependent.

### Iteration 2 — 2026-05-13

| # | Category | Weight | Ratio | Score | Notes |
|---|---|---:|---:|---:|---|
| 1 | Scope clarity | 10 | 1.0 | 10 | Stable |
| 2 | Architecture fit | 10 | 1.0 | 10 | Stable |
| 3 | Security posture | 15 | 1.0 | 15 | Stable |
| 4 | Automatability | 10 | 1.0 | 10 | Stable |
| 5 | Verifiability | 15 | 1.0 | 15 | Stable |
| 6 | Failure-mode awareness | 10 | 1.0 | 10 | Stable |
| 7 | Context efficiency | 10 | 1.0 | 10 | Stable |
| 8 | Docs quality | 5 | 1.0 | 5 | Stable |
| 9 | Observability | 5 | 1.0 | 5 | Event covers operational needs |
| 10 | Operational readiness | 10 | 1.0 | 10 | Stable |
| | **Total** | 100 | | **100** | |

Verdict: SHIP

### Iteration 3 — 2026-05-13

| # | Category | Weight | Ratio | Score | Notes |
|---|---|---:|---:|---:|---|
| 1 | Scope clarity | 10 | 1.0 | 10 | Stable |
| 2 | Architecture fit | 10 | 1.0 | 10 | Stable |
| 3 | Security posture | 15 | 1.0 | 15 | Stable |
| 4 | Automatability | 10 | 1.0 | 10 | Stable |
| 5 | Verifiability | 15 | 1.0 | 15 | Stable |
| 6 | Failure-mode awareness | 10 | 1.0 | 10 | Stable |
| 7 | Context efficiency | 10 | 1.0 | 10 | Stable |
| 8 | Docs quality | 5 | 1.0 | 5 | Stable |
| 9 | Observability | 5 | 1.0 | 5 | Stable |
| 10 | Operational readiness | 10 | 1.0 | 10 | Stable |
| | **Total** | 100 | | **100** | |

Verdict: SHIP
