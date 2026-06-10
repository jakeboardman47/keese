<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: plan
category: phase
depends:
  - README.md
  - ../../specs/keese.ai-v1alpha1-runtime.md
  - ../../../internal/controller/keese/runtime_rebac.go
  - ../../../internal/controller/keese/workspace_rebac.go
related_skills: [plan-management, controller-authoring]
status: planned
last_verified: 2026-06-10
phase: CH3
model_tier: sonnet
depends_on: []
agent: controller-author
outputs:
  - internal/controller/keese
---

# CH3 — Wire the enabled_in tuple on workspace bind

**Goal.** `WriteExtensionEnabledIn` (`internal/controller/keese/runtime_rebac.go`)
writes the `tool:<ext>#enabled_in@workspace:<ws>` tuple, but **no reconciler calls
it** (only tests do — EH11 found this). The runtime spec promises
"`RuntimeExtension_enabled_in` tuple written on workspace create / deleted on
teardown" — so the extension→workspace binding is currently unwired.

## Deliverables

1. **Find the binding linkage** — determine from the API how a `Workspace` enables
   a `RuntimeExtension` (e.g. `Workspace.spec.runtimeExtensions` / a selector / the
   AgentRuntime's extensions). Read `workspace_types.go` +
   `runtimeextension_controller.go` + the runtime spec; document the linkage you
   find.
2. **Write on bind** — in the workspace reconcile path (`workspace_rebac.go`), for
   each enabled `RuntimeExtension`, call `WriteExtensionEnabledIn(ctx, ext, ws)`;
   emit the `ExtensionTupleWritten` event. Make it idempotent (re-running writes the
   same tuple, no error).
3. **Delete on teardown** — on workspace finalize, remove the `enabled_in` tuples
   (alongside the existing rebac cleanup); emit `ExtensionTupleDeleted`.
4. Reflect the bound count in `RuntimeExtension.status.boundWorkspaces` if the field
   exists (EH11 asserts `boundWorkspaces`).

## Acceptance

- Envtest (in the now-consolidated keese suite): create a workspace enabling an
  extension → assert the `enabled_in` tuple is written (fake rebac) + the event;
  delete the workspace → assert the tuple is removed; idempotent over ≥ 3 reconciles.
- `CGO_ENABLED=0 go test -race -count=1 -tags=integration ./internal/controller/keese/...`
  green; `make lint` clean. SSA-only for any object writes (rule 04.7).

## Notes for the agent

- Unblocks EH11's `revisit_when_enabled_in_wired`. Stay inside
  `internal/controller/keese/`. Do NOT regress CH6's runCount / CH9's suite.
- **Never run bare `git stash`/`pop`/`reset`/`checkout <branch>`** — they hit the
  shared checkout, not your worktree. macOS gotcha: `CGO_ENABLED=0`.
