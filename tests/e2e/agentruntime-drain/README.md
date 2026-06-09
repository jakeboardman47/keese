<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

# agentruntime-drain kuttl test

Exercises the **real `keese-drain` binary** through a real SIGTERM and asserts
the real drain contract (TD-P1-02, rule 06-signal-handling). EH10 replaced the
pre-existing busybox stand-ins and fixed the orphan `drain-test-ws` prereq.

## What it validates

| Step | What happens |
|---|---|
| 00 | Provisions the self-contained fixture — Tenant → AgentRuntime → Workspace (`drain-test-ws`) → WorkspaceSession + namespace + session PVC — then asserts the Workspace reaches `Running`. No dependency on a prior suite. |
| 01 | Seeds the pre-drain goose session SQLite on the PVC (the file that must survive). |
| 02 | Runs the **real** `/usr/local/bin/keese-drain` (baked into the goose-runtime image) as PID 1, delivers a **real SIGTERM** via `kubectl delete pod` honouring `terminationGracePeriodSeconds`, and asserts the structured `shutdown` event (`reason` / `drain_duration_ms` / `checkpoint_location`, rule 06 §4) appears in the pod's logs. |
| 03 | A fresh pod mounts the same PVC and asserts the real checkpoint marker (`sessions/<uid>/draining`, with keese-drain's `version: v1` / `workspace_uid` / `sqlite_ref` shape), the `draining-active` sentinel, and the session SQLite all survived the pod-replacement cycle. |

The real binary is wired in production as the agent container's **preStop** exec
hook (`workspacesession_controller.go`) with identical flags
(`--pvc-root=/var/run/keese/session --timeout=25s`). Step 02 runs it as PID 1
instead so a real SIGTERM hits the binary and its shutdown event lands in
`kubectl logs` (preStop-hook stdout is not surfaced there).

## Image gate (skips cleanly when not loaded)

The real-binary steps require the goose-runtime image to be loaded into the
cluster:

```sh
make goose-runtime-load
```

If it is not loaded, `check-drain-image.sh` writes a SKIP sentinel and steps 02
(real drain) and 03 (checkpoint assertions) self-skip — while the self-contained
fixture (00, 01) and SQLite-survival assertion still run. The real-binary
assertions activate automatically once the image is live.

## Prerequisites

- A kind cluster with the `kind-keese-*` context (`make kind-up`).
- The keese operator running (so `drain-test-ws` reconciles to `Running`).
- `make goose-runtime-load` for the real-binary path (optional; gated).
- `kuttl` (`kubectl-kuttl`) on PATH.

## Run

```sh
kubectl kuttl test tests/e2e/agentruntime-drain \
  --config tests/e2e/kuttl-config.yaml
```

## Acceptance criteria

- Workspace `drain-test-ws` reaches `phase: Running` (no orphan assert).
- With the image loaded: the real `keese-drain` emits the structured `shutdown`
  event, and the checkpoint marker + session SQLite survive pod replacement.
- Without the image: the suite still passes; real-binary steps report skipped.
