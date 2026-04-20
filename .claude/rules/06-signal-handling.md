<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

# Signal handling + process lifecycle (always loaded)

Every long-running process in keese — the operator, agent runtimes,
sidecars, init containers — must shut down cleanly on SIGTERM and
survive SIGKILL by checkpointing to durable stores. SIGSTOP is
uncatchable and out of scope.

## SIGTERM

1. **Install a SIGTERM handler.** Every `main.go` under `cmd/` calls
   `signal.NotifyContext(ctx, syscall.SIGTERM, syscall.SIGINT)` before
   starting the manager / agent loop.
2. **Drain in-flight work.** On SIGTERM:
   - Operator: release leader lease, drain reconcile queue, flush
     metrics, close OTEL exporters.
   - Agent runtime (goose): write the current session state to SQLite
     on the workspace PVC, emit a final OTEL trace span, close the
     ACP transport.
   - Gateway sidecars: use Envoy native draining (`preStop:
     sleep 30` + `/healthcheck/fail`).
3. **Exit 0 within `terminationGracePeriodSeconds`.** Default
   budgets: operator 60s, agent runtime 120s, infra sidecars 30s.
4. **Log a structured `shutdown` event** with
   `(reason, drain_duration_ms, checkpoint_location)` before exit.

## SIGKILL (uncatchable)

5. **All state required to resume lives in durable stores.** SQLite
   on PVC (goose sessions), NATS JetStream (in-flight messages), ES
   (logs), OpenBao (secrets), OpenFGA (tuples). In-memory state is
   lost on SIGKILL; that loss is acceptable.
6. **Restart is idempotent.** Every reconciler converges the world to
   the spec in ≤ 3 reconciles. Every agent recipe is re-runnable
   without side-effect doubling.

## SIGSTOP

7. **Not caught — not our contract.** SIGSTOP is a supervisor/OS
   tool; keese processes assume they are not stopped arbitrarily
   outside of planned debugging (`kubectl debug`).

## Probes

8. **Liveness probes accommodate drain.** `initialDelaySeconds +
   (periodSeconds × failureThreshold) ≥ terminationGracePeriodSeconds`.
   Otherwise the kubelet SIGKILLs mid-drain.
9. **Readiness probes flip to NotReady on SIGTERM** so the Service
   stops routing before the process stops listening.

## Testing

10. Every `cmd/` binary has an envtest / e2e test that sends SIGTERM
    and asserts: (a) non-zero work is drained, (b) exit code 0, (c)
    structured shutdown event present. Smoke harness runs this in P7
    (`scripts/dev/sigterm-drain-test.sh`).

## Enforcement

11. Pre-commit hook `scripts/check-signal-handling.sh` (P3) greps every
    `cmd/**/main.go` for a `signal.Notify(*, syscall.SIGTERM*)` call;
    fails if absent.
