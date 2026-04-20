<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: plan
category: rubric
depends: [README.md]
related_skills: [plan-management]
status: current
last_verified: 2026-04-19
---

# Plan / Spec / Implementation Rubric

Score plans, specs, and implementation iterations against this rubric. Total 100 points.
Verdicts: **SHIP ≥ 85**, **REVISE 65-84**, **REPLAN < 65**.

## Categories and weights

| # | Category | Weight | Full points require |
|---|---|---|---|
| 1 | **Scope clarity** | 10 | Goal in one sentence. Bounded inputs, outputs, exit criteria. |
| 2 | **Architecture fit** | 10 | Aligns with key beliefs; does not violate any `.claude/rules/`. |
| 3 | **Security posture** | 15 | Threat model named; secrets/least-priv; privileged operations justified. |
| 4 | **Automatability** | 10 | Every step runnable via make/script; no manual hand-waving. |
| 5 | **Verifiability** | 15 | Concrete tests: unit + integration + e2e where applicable. |
| 6 | **Failure-mode awareness** | 10 | Rollback plan; partial failure handling; known gotchas listed. |
| 7 | **Context efficiency for Claude** | 10 | Uses CLAUDE.md routing; ≤ 200-line docs; skill pointers instead of inline. |
| 8 | **Docs quality** | 5 | SPDX headers; frontmatter complete; no broken links. |
| 9 | **Observability** | 5 | Metrics, traces, events declared. |
| 10 | **Operational readiness** | 10 | HA correct; upgrade/rollback path; resource ceilings stated. |

## Scoring rules

- Full credit: every bullet under that category's rubric is fully satisfied.
- Half credit: most bullets satisfied; one notable gap.
- Zero: gap blocks safe progress.

Total = Σ(category_weight × score_ratio), where score_ratio ∈ {0, 0.5, 1}.

## Iteration cap

Maximum 3 iterations on the same decomposition. If iteration 3 still lands below SHIP,
**split** the phase or **rescope** — do not iterate a fourth time on the same plan.

Split when: the diff is growing unbounded, or unrelated risks are coupled.
Rescope when: the core problem turned out to be different from what the plan assumed.

## Using the rubric

1. Copy the template below into the target doc's `## Iteration log`.
2. Walk each category; record 0 / 0.5 / 1 and a one-line note.
3. Total, verdict, top-3 gaps, proposed next step.

### Template

```
### Iteration N — YYYY-MM-DD

| # | Category | Weight | Ratio | Score | Notes |
|---|---|---:|---:|---:|---|
| 1 | Scope clarity | 10 | 1.0 | 10 | ... |
| 2 | Architecture fit | 10 |  |  |  |
| 3 | Security posture | 15 |  |  |  |
| 4 | Automatability | 10 |  |  |  |
| 5 | Verifiability | 15 |  |  |  |
| 6 | Failure-mode awareness | 10 |  |  |  |
| 7 | Context efficiency for Claude | 10 |  |  |  |
| 8 | Docs quality | 5 |  |  |  |
| 9 | Observability | 5 |  |  |  |
| 10 | Operational readiness | 10 |  |  |  |
| | **Total** | 100 | | **NN** | |

Verdict: SHIP | REVISE | REPLAN

Top gaps:
1. ...
2. ...
3. ...

Next step: ...
```

## Refinement passes

Each iteration chooses one emphasis:

1. **Correctness & security** — does it do the right thing safely?
2. **Performance & quality** — is it efficient and idiomatic?
3. **Operational readiness** — can it be deployed, observed, rolled back?

Iterations focus sharpens the review. Do not try to polish all three in one pass.
