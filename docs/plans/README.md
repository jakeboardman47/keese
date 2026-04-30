<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: reference
category: index
gate_status: open
depends: [../designs/README.md, rubric.md, gate-open-audit-2026-04-22.md]
related_skills: [plan-management]
status: current
last_verified: 2026-04-22
---

# plans/ — HOW (phased)

> **Gate status: OPEN** as of 2026-04-22. All 62 designs and 13 specs reached
> `status: current` with honest scores ≥ 90 (audit:
> [gate-open-audit-2026-04-22.md](gate-open-audit-2026-04-22.md)). Controller
> code may now land in `internal/controller/` and `api/`; spec promotion to
> `status: implemented` proceeds via the test-engineer agent backlog enumerated
> in the audit doc.
> See [../designs/README.md](../designs/README.md) and [rubric.md](rubric.md).

## Phase index

| Phase | Title | Depends on | Model tier | Score | Status |
|---|---|---|---|---|---|
| P0 | Repo foundation & licensing (Apache-2.0) | — | sonnet | 90 | shipped |
| P1 | Claude automation (`.claude/` customization) | P0 | opus+sonnet | 92 | shipped |
| P2 | Dev env (flake, direnv, Makefile, envs) | P1 | sonnet | 90 | shipped |
| P3 | Pre-commit hardening (Go + K8s + OLM) | P2 | sonnet | 89 | shipped |
| P4 | Docs skeleton — designs first, specs after | P1 | opus+sonnet | 93 | in-progress |
| P5 | CI/CD (11 GH Actions workflows) | P3 | sonnet | 90 | planned |
| P6 | Go operator scaffold (13 empty-stub kinds) | P2, P4-designs | sonnet | 92 | planned |
| P7 | Local infra bootstrap (kind/tilt/full stack) | P6 | sonnet | 91 | planned |
| P8 | Design-gate freeze enforcement | P4, P6 | opus+sonnet | 91 | planned |

## Demo track (post-gate, time-boxed to Mon 2026-04-27 AM)

Time-boxed phased plan to land the first end-to-end agent demo on a real
cloud cluster. Targets SHIP ≥ 85; tech-debt register captures every
shortcut. See [demo/README.md](demo/README.md) for the full index.

| Phase | Title | Effort | Status |
|---|---|---|---|
| D1 | [Controller wiring + samples + bundle regen](demo/D1-controller-wiring.md) | 3–4 h | planned |
| D2 | [Agent runtime minimum SPI](demo/D2-runtime-spi-minimum.md) | 5–7 h | planned |
| D3 | [Cluster bootstrap + Anthropic LLM wiring](demo/D3-cluster-bootstrap.md) | 3–4 h | planned |
| D4 | [Cloud deploy](demo/D4-cloud-deploy.md) | 3–4 h | planned |
| D5 | [Demo smoke + runbook](demo/D5-demo-smoke.md) | 2 h | planned |
| TD | [Tech-debt register (post-demo cleanup)](demo/tech-debt.md) | tracked | open |

## Parallel execution groups

- **Group A (P0–P3):** sequential foundation; must all ship before Group B.
- **Group B:** P4 (docs) + P5 (CI/CD) can run in parallel after P3 ships.
- **Group C:** P6 (scaffold) starts after P4 design gate passes (all 62 designs current).
- **Group D:** P7 (infra) after P6; P8 (gate freeze) after P4 + P6.

## Gate-check reference

The design gate is enforced by:

- `scripts/check-design-gate.sh` — run locally and in CI.
- `.github/workflows/design-gate.yaml` — required PR check on `api/**`,
  `internal/controller/**`, and `docs/{designs,specs,plans}/**`.
- Frontmatter field `gate_status` in this file — flipped to `open` by an
  architect-signed commit after all docs score ≥ 90.

## Scoring

Every phase file must have an iteration log scored against [rubric.md](rubric.md).
Target ≥ 90/100 (SHIP) before marking `status: in-progress`.
Score 65–84 → REVISE; < 65 → REPLAN.

## Rubric

See [rubric.md](rubric.md).

## Plan file

Full plan with decisions D1–D26, kind list, and phase details:
[/Users/marshallmccain/.claude/plans/you-are-an-expert-iridescent-alpaca.md](/Users/marshallmccain/.claude/plans/you-are-an-expert-iridescent-alpaca.md)
