#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright (c) 2026 keese-ai
#
# E2E: WorkspaceTool tuple -> live ext_authz allow/deny (EH5 deliverable 2).
#
# Ties the CRD layer to the live decision. The namespaced WorkspaceTool
# `eh5-search` (alpha) resolves a `GET /eh5-search` request to the OpenFGA
# object `tool:alpha.eh5-search` (resolver.go composeTool). The AUTHORIZING
# tuple is written by the WORKSPACE controller from
# spec.egress.allowedTools — there is no WorkspaceTool reconciler (see the
# suite README + SUMMARY); WorkspaceTool/ToolBinding are request-time
# catalogue objects consumed by ext_authz, not reconciled into tuples.
#
# Cases:
#   1. GRANT  — add `alpha.eh5-search` to my-ws.spec.egress.allowedTools.
#               The Workspace controller writes
#               `tool:alpha.eh5-search#allowed_in@workspace:my-ws`.
#               Fire GET /eh5-search -> poll for HTTP 200 (allowed) within
#               the ext_authz cache TTL.
#   2. REVOKE — remove the grant. The tuple is deleted; re-firing flips the
#               decision allow->deny -> HTTP 403 (fail-closed).
#   3. GUARDRAIL-EXTPROC (conditional) — if the gateway-side guardrail
#               enforcement (Presidio / LlamaGuard ext_proc) is live, assert
#               a denied-tool request is blocked. When NOT live in the
#               bootstrap (the common case), this case is SKIPPED and the
#               suite is shipped-with-stubs (revisit_when_guardrail_extproc_live).
#               The CRD-reconcile + tuple layer above is covered in full.
#
# Reuses the EH4 request-firing pattern via the sourceable primitives in
# ../lib/fire-request.sh (NOT a copy of EH4's script — see that file's
# header for why the shared primitives live in lib/).
#
# Idempotent (rule 01): the grant/revoke patches are declarative merges;
# re-running converges. A trap restores my-ws to its pre-test allowlist so
# the suite leaves no residue for other suites sharing my-ws.
#
# Usage: tests/e2e/authz-guardrails/test-tool-allow-deny.sh
#        KUBECTL_CONTEXT=kind-keese-demo tests/e2e/authz-guardrails/test-tool-allow-deny.sh
#
# Refs: tests/e2e/lib/fire-request.sh                       (firing primitives)
#       internal/authz/extauth/resolver.go                  (path->tool: resolve)
#       internal/controller/keese/workspace_controller.go   (allowed_in tuple)

set -euo pipefail
IFS=$'\n\t'

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../../.." && pwd)"

# shellcheck source=../../../scripts/lib/log.sh
source "${REPO_ROOT}/scripts/lib/log.sh"
# shellcheck source=../lib/fire-request.sh
source "${SCRIPT_DIR}/../lib/fire-request.sh"

CONTEXT="${KUBECTL_CONTEXT:-$(kubectl config current-context 2>/dev/null || true)}"
NS="${WORKSPACE_NS:-alpha}"
ALLOW_WS="${ALLOW_WS:-my-ws}"

# The namespaced WorkspaceTool resolves to `tool:<ns>.<toolName>`; the grant
# string the Workspace controller turns into the allowed_in tuple is the
# bare `<ns>.<toolName>` (the `tool:` prefix is added controller-side).
TOOL_GRANT="${TOOL_GRANT:-${NS}.eh5-search}"
TOOL_PATH="${TOOL_PATH:-/eh5-search}"

# ext_authz caches its OpenFGA result per gateway pod; allow/deny flips poll
# up to this long. Generous so a warm cluster is non-flaky while a real
# never-propagates failure still fails the test.
FLIP_TIMEOUT_S="${FLIP_TIMEOUT_S:-90}"

# Whether gateway-side guardrail ext_proc (Presidio / LlamaGuard) is live.
# Default off — the bootstrap ships the CRD-reconcile + tuple layer but not
# the ext_proc filter. Set GUARDRAIL_EXTPROC=1 to enable case 3 once the
# filter is deployed (revisit_when_guardrail_extproc_live).
GUARDRAIL_EXTPROC="${GUARDRAIL_EXTPROC:-0}"

FAILURES=0

log::info "test-tool-allow-deny: cluster=${CONTEXT} ns=${NS} ws=${ALLOW_WS} tool=${TOOL_GRANT}"

# Snapshot my-ws's current allowedTools so we can restore on exit (leave no
# residue — other suites share my-ws).
ORIG_TOOLS_JSON="$(kubectl --context="${CONTEXT}" -n "${NS}" \
  get workspace "${ALLOW_WS}" -o jsonpath='{.spec.egress.allowedTools}' 2>/dev/null || echo '[]')"
[[ -n "${ORIG_TOOLS_JSON}" ]] || ORIG_TOOLS_JSON='[]'

restore_tools() {
  kubectl --context="${CONTEXT}" -n "${NS}" patch workspace "${ALLOW_WS}" \
    --type=merge -p "{\"spec\":{\"egress\":{\"allowedTools\":${ORIG_TOOLS_JSON}}}}" \
    >/dev/null 2>&1 || true
  log::dim "[cleanup] restored ${ALLOW_WS}.spec.egress.allowedTools"
}
trap restore_tools EXIT

# grant_tool — append TOOL_GRANT to allowedTools via a JSON-patch `add` to
# the END of the array (`/-`). This preserves my-ws's pre-existing grants
# (no python3/jq dependency) and is idempotent at the suite level because
# the EXIT trap restores the original list regardless. The `add` to a
# non-existent egress object is guarded by a prior merge that ensures the
# egress + allowedTools path exists.
grant_tool() {
  # Ensure spec.egress.allowedTools exists (no-op when already present —
  # the snapshot is restored as-is, so this only matters when the original
  # list was empty/absent).
  kubectl --context="${CONTEXT}" -n "${NS}" patch workspace "${ALLOW_WS}" \
    --type=merge -p "{\"spec\":{\"egress\":{\"allowedTools\":${ORIG_TOOLS_JSON}}}}" >/dev/null 2>&1 || true
  kubectl --context="${CONTEXT}" -n "${NS}" patch workspace "${ALLOW_WS}" \
    --type=json -p "[{\"op\":\"add\",\"path\":\"/spec/egress/allowedTools/-\",\"value\":\"${TOOL_GRANT}\"}]" \
    >/dev/null 2>&1
}

# revoke_tool — restore the original allowedTools (drops TOOL_GRANT).
revoke_tool() {
  kubectl --context="${CONTEXT}" -n "${NS}" patch workspace "${ALLOW_WS}" \
    --type=merge -p "{\"spec\":{\"egress\":{\"allowedTools\":${ORIG_TOOLS_JSON}}}}" >/dev/null 2>&1
}

TOKEN="$(fr::mint_sa_token "${ALLOW_WS}" "${NS}")" || {
  log::err "could not mint SA token for ${NS}/${ALLOW_WS}"
  exit 1
}

# ── Case 1 — GRANT -> allow (200) ────────────────────────────────────────────
log::info "[grant] adding ${TOOL_GRANT} to ${ALLOW_WS}.spec.egress.allowedTools"
if grant_tool; then
  fr::warm_up "${NS}" "${TOKEN}" "${TOOL_PATH}"
  if fr::poll_status "grant-allow" "${NS}" "${TOKEN}" "200" "${FLIP_TIMEOUT_S}" "${TOOL_PATH}" "GET" "{}"; then
    log::ok "[grant-allow] WorkspaceTool tuple grants ${TOOL_GRANT} -> 200"
  else
    FAILURES=$((FAILURES + 1))
  fi
else
  log::err "[grant-allow] could not patch ${ALLOW_WS} egress.allowedTools"
  FAILURES=$((FAILURES + 1))
fi

# ── Case 2 — REVOKE -> deny (403, fail-closed) ───────────────────────────────
log::info "[revoke] removing ${TOOL_GRANT} grant from ${ALLOW_WS}"
if revoke_tool; then
  if fr::poll_status "revoke-deny" "${NS}" "${TOKEN}" "403" "${FLIP_TIMEOUT_S}" "${TOOL_PATH}" "GET" "{}"; then
    log::ok "[revoke-deny] removing the tuple flips ${TOOL_GRANT} -> 403 (fail-closed)"
  else
    FAILURES=$((FAILURES + 1))
  fi
else
  log::err "[revoke-deny] could not patch ${ALLOW_WS} egress.allowedTools"
  FAILURES=$((FAILURES + 1))
fi

# ── Case 3 — GUARDRAIL ext_proc (conditional / shipped-with-stubs) ───────────
if [[ "${GUARDRAIL_EXTPROC}" == "1" ]]; then
  # When the Presidio / LlamaGuard ext_proc filter is live, a request that
  # trips a guardrail (e.g. a denied tool from the GuardrailBinding deny set)
  # is blocked at the gateway. Re-grant the tool so the ReBAC layer ALLOWS,
  # isolating the guardrail decision, then assert a guardrail-tripping body
  # is still blocked (403/422 depending on the ext_proc verdict mapping).
  log::info "[guardrail-extproc] ext_proc enabled — asserting guardrail block"
  grant_tool || true
  fr::warm_up "${NS}" "${TOKEN}" "${TOOL_PATH}"
  # A body that trips the guardrail. Mapped to a non-2xx by the ext_proc.
  if fr::poll_status "guardrail-block" "${NS}" "${TOKEN}" "403" "${FLIP_TIMEOUT_S}" \
    "${TOOL_PATH}" "POST" '{"prompt":"__guardrail_trip__"}'; then
    log::ok "[guardrail-block] ext_proc blocked the guardrail-tripping request"
  else
    FAILURES=$((FAILURES + 1))
  fi
else
  log::warn "[guardrail-extproc] SKIPPED: gateway-side guardrail ext_proc (Presidio/LlamaGuard) not live"
  log::warn "[guardrail-extproc] revisit_when_guardrail_extproc_live — set GUARDRAIL_EXTPROC=1 once deployed"
fi

if [[ "${FAILURES}" -gt 0 ]]; then
  log::err "test-tool-allow-deny: ${FAILURES} case(s) failed"
  exit 1
fi

log::ok "test-tool-allow-deny: WorkspaceTool tuple allow(200)->revoke-deny(403) passed"
