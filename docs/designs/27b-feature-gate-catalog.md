<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: design
category: lifecycle
depends:
  - 27-feature-gates-openfeature.md
related_skills: [doc-authoring, controller-authoring]
status: current
last_verified: 2026-05-06
---

# 27b — Feature gate catalog

Companion to [27-feature-gates-openfeature.md](27-feature-gates-openfeature.md).
This is the source-of-truth list of every keese feature gate, owned
by the kind that consumes it. When you add a gate, append a row;
when you promote a stage, edit the row; when you remove a gate,
strike-through the row and keep it for one minor release.

## Catalog

| Gate (CR name) | Stage | Default | Owners | Restart | Description |
|---|---|---|---|---|---|
| `cosign-installplan-verify` | alpha | off | `keese-cosign-webhook` | no | Pre-install ValidatingWebhook on OLM InstallPlans verifies cosign keyless OIDC signatures on every keese-published image. Off → handler short-circuits. |
| `cosign-installplan-failclosed` | alpha | off | `keese-cosign-webhook` | no | When verify is on, controls whether verification failures result in deny (true) or warning + Allowed (false). Lets operators stage rollouts log-only → enforcing. |

## How to add a gate

1. Pick a name. DNS-1035; lowercase + hyphens; namespaced by binary
   prefix when ambiguous (e.g. `cosign-…`, `authz-…`, `otel-…`).
2. Add a stable `Gate` const in
   [internal/featuregate/featuregate.go](../../internal/featuregate/featuregate.go).
3. Author a seed CR in
   [config/featuregates/](../../config/featuregates/) and add it to
   the kustomization.
4. Append a row above. State stage (start at alpha), default
   (alpha → off, beta → on), owner binaries, restart-required,
   one-line description.
5. In the consumer binary, wrap the entry point in
   `if !gates.Enabled(ctx, X) { return passThrough() }` per
   [27 §10](27-feature-gates-openfeature.md).
6. If the gate alters operational risk (security, supply chain,
   tenancy), open a tech-debt row in
   [docs/plans/demo/tech-debt.md](../plans/demo/tech-debt.md)
   tracking promotion to beta.

## How to promote

- **alpha → beta:** flip the row's Default from off to on. Bump the
  CR's `spec.stage` field. Run a soak in candidate channel (≥1
  week) before merging.
- **beta → ga:** delete the conditional in code. Set the CR
  `spec.stage` to `deprecated`. Keep the deprecated row in this
  catalog for one minor release; remove next minor.
- **deprecated → removed:** delete the seed CR + the const + the row
  here. Audit consumers via `make featuregate-list` first to confirm
  zero readers.

## See also

- [27-feature-gates-openfeature.md](27-feature-gates-openfeature.md) — the design.
- [27-ii-iter-log.md](27-ii-iter-log.md) — rubric scoring.
- [../plans/td-feature-gates-openfeature.md](../plans/td-feature-gates-openfeature.md) — implementation plan (phases A+B).
