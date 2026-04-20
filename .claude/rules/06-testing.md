---
description: Testing discipline (loaded when touching tests)
paths:
  - "**/*_test.*"
  - "test/**"
  - "**/testdata/**"
  - "Makefile"
  - ".github/workflows/*test*.yaml"
---

<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) {{YEAR}} {{ORG_NAME}} -->

# Testing discipline (loaded when touching tests)

Testing is not optional. If a rule here conflicts with another instruction,
this rule wins for test code.

## Always

- Race detector (or equivalent) enabled in CI for every unit and integration package. No opt-out.
- Table-driven tests. One subtest per case.
- Assertions via the language's standard `require`/`assert` idioms (fail fast where a
  cascading assertion would be misleading).
- Async assertions via `Eventually`/poll helpers. **Never** `sleep` in a test. It is a bug,
  not a convenience.
- Deterministic seeds: fix and log the seed if the test uses randomness.
- Tests own their state. No shared globals. Parallel execution encouraged where isolated.

## Coverage

- Per-package thresholds in `test/coverage-targets.yaml`.
- `scripts/coverage-check.sh` fails CI below threshold.
- Dropping a threshold is a rubric-scored change with a justification in the
  plan iteration log.

## Flake handling

- Two flakes on `main` in a week → quarantine via a dedicated build tag or skip guard,
  logged in `docs/plans/flake-log.md` with owner and due date.
- CI retry budget: at most 2 reruns. More reruns == real bug.

## Never

- Do not mock a type you do not own without a thin adapter owned by your code.
- Do not test log output. Test behavior, events, and status.
- Do not skip without a linked issue or flake-log entry.
- Do not commit golden / `-update` output from a machine-specific run (locale, path,
  timestamp). Normalize test output before diffing.

## Build tags / test tiers

| Tier | Purpose |
|---|---|
| unit | Pure logic with fakes. Default runs. |
| integration | Hits real subsystems (database, API server). Opt-in. |
| e2e | End-to-end flows against a local or ephemeral environment. Excluded from default runs. |
| flake | Quarantined. Excluded from CI; runs only in flake-triage jobs. |
