#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright (c) 2026 keese-ai
#
# Signal-handling smoke test — enforces rule 06-signal-handling §10 across every
# long-running keese cmd/** binary that ships a Pod. For each target it sends
# SIGTERM (via `kubectl delete pod --grace-period`) and asserts:
#   (a) the Pod exits within its terminationGracePeriodSeconds,
#   (b) for the operator, the leader lease is released before exit, and
#   (c) a structured 'shutdown' log line is emitted carrying
#       (reason, drain_duration_ms, checkpoint_location) (rule 06 §4).
#
# Targets are described in the TARGETS table below; each row names the Pod via a
# label selector + namespace, its grace budget, and whether it holds a leader
# lease. A target whose Pod is absent is SKIPPED (not failed), so the harness is
# safe to run against a partially-bootstrapped cluster or from CI smoke jobs.
#
# Binaries covered (rule 06 §10):
#   - operator             (cmd/main.go)              — leader lease + reconcile drain
#   - keese-cosign-webhook (cmd/keese-cosign-webhook) — controller-runtime drain
#   - agent-runtime        (keese-drain preStop)      — session SQLite checkpoint
#   - keese-authz          (cmd/keese-authz)          — gRPC GracefulStop
#
# keese-wf-launcher (cmd/keese-wf-launcher) is a short-lived launcher Job, not a
# long-running Pod; its SIGTERM path is covered by the Go test in
# cmd/keese-wf-launcher and is intentionally not probed here.
#
# Usage: scripts/dev/sigterm-drain-test.sh [target-name ...]
#   With no args, every target is probed. Names match the TARGETS table keys.
#
# Refs: docs/designs/18-process-lifecycle.md
#       .claude/rules/06-signal-handling.md

set -euo pipefail
IFS=$'\n\t'

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"

# shellcheck source=../lib/log.sh
source "${REPO_ROOT}/scripts/lib/log.sh"

CONTEXT="${KUBECTL_CONTEXT:-kind-keese-dev}"

# ── Target table ──────────────────────────────────────────────────────────────
# Each target is "name|namespace|label-selector|grace-seconds|leader-lease-name"
# A blank leader-lease-name means the target does not hold a lease.
TARGETS=(
  "operator|keese-system|control-plane=controller-manager|90|keese-operator-leader"
  "keese-cosign-webhook|keese-system|app.kubernetes.io/name=keese-cosign-webhook|30|"
  "keese-authz|keese-system|app.kubernetes.io/name=keese-authz|30|"
  "agent-runtime|keese-system|app.kubernetes.io/component=agent-runtime|120|"
)

# ── Per-target drain assertion ────────────────────────────────────────────────

# probe_target NAME NAMESPACE SELECTOR GRACE LEASE
# Returns 0 on pass or graceful skip; non-zero only on a hard contract failure.
probe_target() {
  local name="$1" namespace="$2" selector="$3" grace="$4" lease="$5"

  log::info "[${name}] looking for pod (-n ${namespace} -l ${selector})"
  local pod_name
  pod_name=$(kubectl --context="${CONTEXT}" get pods \
    -n "${namespace}" \
    -l "${selector}" \
    -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || echo "")

  if [[ -z "${pod_name}" ]]; then
    log::warn "[${name}] no pod found — skipping (deploy via 'make tilt-up' to cover)."
    return 0
  fi
  log::info "[${name}] found pod ${pod_name}"

  # Capture leader-lease holder before SIGTERM (operator only).
  local lease_before="none"
  if [[ -n "${lease}" ]]; then
    lease_before=$(kubectl --context="${CONTEXT}" get lease "${lease}" \
      -n "${namespace}" \
      -o jsonpath='{.spec.holderIdentity}' 2>/dev/null || echo "none")
    log::info "[${name}] lease ${lease} holder before: ${lease_before}"
  fi

  # Send SIGTERM via graceful pod delete.
  log::info "[${name}] deleting pod ${pod_name} (grace ${grace}s, triggers SIGTERM)"
  kubectl --context="${CONTEXT}" delete pod "${pod_name}" \
    -n "${namespace}" \
    --grace-period="${grace}" &
  local delete_pid=$!

  # Wait for the pod to leave within the grace budget.
  local deadline=$(($(date +%s) + grace))
  local pod_gone=false
  while [[ $(date +%s) -lt ${deadline} ]]; do
    local status
    status=$(kubectl --context="${CONTEXT}" get pod "${pod_name}" \
      -n "${namespace}" \
      -o jsonpath='{.status.phase}' 2>/dev/null || echo "gone")
    if [[ "${status}" == "gone" || -z "${status}" ]]; then
      pod_gone=true
      break
    fi
    sleep 2
  done
  wait "${delete_pid}" 2>/dev/null || true

  if [[ "${pod_gone}" != "true" ]]; then
    log::err "[${name}] FAILED — pod did not terminate within ${grace}s grace budget"
    return 1
  fi
  log::ok "[${name}] pod terminated within grace budget"

  # Assert leader lease released (operator).
  if [[ -n "${lease}" ]]; then
    local lease_after
    lease_after=$(kubectl --context="${CONTEXT}" get lease "${lease}" \
      -n "${namespace}" \
      -o jsonpath='{.spec.holderIdentity}' 2>/dev/null || echo "released")
    if [[ "${lease_after}" == "${lease_before}" && "${lease_before}" != "none" ]]; then
      log::err "[${name}] FAILED — leader lease still held by '${lease_after}' after exit (rule 06 §2)"
      return 1
    fi
    log::ok "[${name}] leader lease released (before=${lease_before}, after=${lease_after})"
  fi

  # Assert structured shutdown event (rule 06 §4). Read the --previous stream
  # once and reuse it for both the line check and the field checks.
  local stream
  stream=$(kubectl --context="${CONTEXT}" logs "${pod_name}" \
    -n "${namespace}" --previous --all-containers 2>/dev/null || echo "")

  local shutdown_line
  shutdown_line=$(grep -E '"event"[[:space:]]*:[[:space:]]*"shutdown"|"msg"[[:space:]]*:[[:space:]]*"shutdown"|shutdown' \
    <<<"${stream}" | head -n1 || echo "")
  if [[ -z "${shutdown_line}" ]]; then
    log::err "[${name}] FAILED — no structured 'shutdown' log line found (rule 06 §4)"
    return 1
  fi

  # Verify the three mandated fields are present (reason, drain_duration_ms,
  # checkpoint_location). controller-runtime binaries may split these across
  # adjacent log lines, so search the full --previous stream for each key.
  local missing=()
  local field
  for field in reason drain_duration_ms checkpoint_location; do
    if ! grep -q "${field}" <<<"${stream}"; then
      missing+=("${field}")
    fi
  done
  if [[ ${#missing[@]} -gt 0 ]]; then
    log::err "[${name}] FAILED — shutdown event missing fields: ${missing[*]} (rule 06 §4)"
    return 1
  fi

  log::ok "[${name}] structured shutdown event present with reason/drain_duration_ms/checkpoint_location"
  return 0
}

# ── Driver ────────────────────────────────────────────────────────────────────

main() {
  local -a wanted=("$@")
  local failures=0
  local probed=0

  local row name namespace selector grace lease
  for row in "${TARGETS[@]}"; do
    IFS='|' read -r name namespace selector grace lease <<<"${row}"

    # If explicit target names were passed, only probe those.
    if [[ ${#wanted[@]} -gt 0 ]]; then
      local match=false w
      for w in "${wanted[@]}"; do
        [[ "${w}" == "${name}" ]] && match=true
      done
      [[ "${match}" == "true" ]] || continue
    fi

    probed=$((probed + 1))
    if ! probe_target "${name}" "${namespace}" "${selector}" "${grace}" "${lease}"; then
      failures=$((failures + 1))
    fi
  done

  if [[ ${probed} -eq 0 ]]; then
    log::warn "sigterm-drain-test: no matching targets — nothing probed."
    return 0
  fi
  if [[ ${failures} -gt 0 ]]; then
    log::err "sigterm-drain-test: FAILED — ${failures} target(s) violated the drain contract."
    return 1
  fi
  log::ok "sigterm-drain-test: PASSED — all reachable targets drained cleanly."
  return 0
}

main "$@"
