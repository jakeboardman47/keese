---
name: controller-authoring
description: Authoring keese reconcilers (load before editing internal/controller/)
status: current
last_verified: 2026-04-19
---

<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

# Controller authoring (on-demand skill)

Load this before writing or revising a reconciler. Pairs with the
`controller-author` agent and
`docs/references/envtest-kuttl-harness.md`.

## Reconcile idiom

```go
func (r *WorkspaceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
    var ws workspacev1alpha1.Workspace
    if err := r.Get(ctx, req.NamespacedName, &ws); err != nil {
        return ctrl.Result{}, client.IgnoreNotFound(err)
    }
    orig := ws.DeepCopy()

    // Handle deletion first (finalizers).
    if !ws.DeletionTimestamp.IsZero() {
        return r.cleanup(ctx, &ws)
    }

    // Compute desired state, apply via SSA.
    desired := buildDesired(&ws)
    if err := r.Apply(ctx, desired, client.FieldOwner("keese-workspace-controller"),
        client.ForceOwnership); err != nil {
        meta.SetStatusCondition(&ws.Status.Conditions, progressing(err))
        return ctrl.Result{RequeueAfter: backoff}, r.Status().Patch(ctx, &ws, client.MergeFrom(orig))
    }

    // Write ReBAC tuples.
    if err := r.rebac.Sync(ctx, rebacTuplesFor(&ws)); err != nil {
        return ctrl.Result{}, err
    }

    // Update status.
    ws.Status.ObservedGeneration = ws.Generation
    meta.SetStatusCondition(&ws.Status.Conditions, ready())
    return ctrl.Result{}, r.Status().Patch(ctx, &ws, client.MergeFrom(orig))
}
```

## Invariants

- **Idempotency**: 3 reconciles with no spec change → stable status
  (envtest asserts).
- **No spec writes from status.** Spec is user intent; status is
  observation.
- **`observedGeneration`** set to `ws.Generation` on every successful
  reconcile.
- **No `time.Sleep`** — use `ctrl.Result{RequeueAfter: d}` or
  `wait.PollUntilContextCancel`.
- **No `panic`/`log.Fatal`/`os.Exit`** — return
  `(ctrl.Result{}, err)`.

## Finalizers

- Add on create if external resources touched; remove only after
  cleanup succeeds; double-check `DeletionTimestamp.IsZero()`.
- ID format: `finalizers.<kind>.keese.ai/<purpose>` (rule 04.10).
- Envtest: write a deletion test covering failure path (external
  cleanup fails → finalizer sticks → retry converges).

## Conditions + events

- Conditions: `Ready`, `Progressing`, plus domain
  (`ExtAuthzConfigured`, `GatewayRouted`, `MemoryBound`).
- Events: const reason table in `events.go`. Never log tokens or
  request bodies; OpenFGA tuple references only.

## Predicates + rate limiting

- `predicate.GenerationChangedPredicate{}` to skip status-only
  updates.
- Label selector `keese.ai/managed=true` on watched objects.
- `DefaultControllerRateLimiter` (exponential, max 1000s); custom
  limiters need an ADR.

## SSA fieldOwner

- Every write: `client.FieldOwner("keese-<kind>-controller")` +
  `client.ForceOwnership` (yes: we claim ownership; conflicts from
  user edits land as events for admin review).

## Signal handling

- controller-runtime's Manager handles SIGTERM (drain queue, release
  leader lease). The reconciler just has to be ctx-aware — every
  blocking operation takes `ctx`.

## Envtest patterns

- `suite_test.go` bootstraps CRDs from `config/crd/bases/`.
- Per-test namespace: `testutil.MustCreateNamespace(t, ctx)`.
- `Eventually(...).Should(...)` with 10s timeout / 250ms interval by
  default.
- Table-driven tests use fakes under `internal/controller/fake/`.

## Checklist before commit

1. `make fmt vet lint test-unit test-integration` green.
2. Idempotency test (`TestReconcileIdempotent_<Kind>`) present.
3. Deletion + finalizer test present.
4. Events + conditions exercised in tests.
5. Rubric iter-logged in the spec doc.
