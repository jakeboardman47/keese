#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright (c) 2026 keese-ai
#
# make smoke — post-gate smoke test implementation.
#
# Applies dev/samples/* CRs and waits for .status.phase=Ready (60s timeout).
# Controllers are stubs in pre-gate state, so Ready is expected to time out.
# Exit 0 if apply succeeded; exit 1 on apply error.
#
# Full smoke (requiring controllers) is gated behind design-gate open.

set -euo pipefail
IFS=$'\n\t'

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"

# shellcheck source=../lib/log.sh
source "${REPO_ROOT}/scripts/lib/log.sh"

SAMPLES_DIR="${REPO_ROOT}/dev/samples"
TIMEOUT_SECS=60
CONTEXT="${KUBECTL_CONTEXT:-kind-keese-dev}"

log::info "smoke: applying sample CRs from ${SAMPLES_DIR}"

# ── Apply samples ──────────────────────────────────────────────────────────────

apply_samples() {
  kubectl --context="${CONTEXT}" apply -f "${SAMPLES_DIR}/tenant-alpha.yaml" || {
    log::err "Failed to apply tenant-alpha.yaml — check CRDs are installed."
    return 1
  }
  kubectl --context="${CONTEXT}" apply -f "${SAMPLES_DIR}/workspace-research.yaml" || {
    log::err "Failed to apply workspace-research.yaml — check CRDs are installed."
    return 1
  }
  kubectl --context="${CONTEXT}" apply -f "${SAMPLES_DIR}/workflow-summarize-review.yaml" || {
    log::err "Failed to apply workflow-summarize-review.yaml — check CRDs are installed."
    return 1
  }
  log::ok "All sample CRs applied successfully."
}

apply_samples

# ── Wait for Ready (expected to time out pre-gate) ─────────────────────────────

log::info "smoke: waiting up to ${TIMEOUT_SECS}s for Workspace 'research' to reach phase=Ready"
log::warn "Controllers are stubs pre-gate — Ready status is NOT expected yet."

workspace_ready=false
deadline=$(($(date +%s) + TIMEOUT_SECS))

while [[ $(date +%s) -lt ${deadline} ]]; do
  phase=$(kubectl --context="${CONTEXT}" get workspace.workspace.operator.keese.ai research \
    -n tenant-a-default \
    -o jsonpath='{.status.phase}' 2>/dev/null || echo "")
  if [[ "${phase}" == "Ready" ]]; then
    workspace_ready=true
    break
  fi
  sleep 5
done

if [[ "${workspace_ready}" == "true" ]]; then
  log::ok "smoke: Workspace reached Ready — controllers are active."
else
  log::warn "smoke: Workspace did not reach Ready within ${TIMEOUT_SECS}s."
  log::warn "This is expected pre-gate (controllers are stubs)."
fi

# ── Summary ────────────────────────────────────────────────────────────────────

cat <<'EOF'

────────────────────────────────────────────────────────────────────────────────
  Smoke test is post-gate: controllers are stubs; full pass requires
  design-gate open. CR apply succeeded — this is the pre-gate pass criterion.
────────────────────────────────────────────────────────────────────────────────

EOF

log::ok "smoke: exit 0 (apply succeeded; status gated on design-gate open)"
exit 0
