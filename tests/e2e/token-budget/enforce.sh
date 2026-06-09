#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright (c) 2026 keese-ai
#
# EH7 steps 2-4 — ENFORCEMENT through the live Envoy AI Gateway.
#
# Reuses EH4's request-firing helper (tests/e2e/rebac-decision/
# test-rebac-decision.sh) BY SOURCING — not copying. EH4 defines the proven
# fire_request / mint_sa_token / assert_status / poll_status / warm_up_gateway
# functions that curl the canonical /anthropic/v1/messages path from inside a
# workspace session pod (mounted gateway CA + projected SA token, identical to
# a real agent). EH4's script has no source-guard and runs its own ReBAC cases
# at the bottom, so we source ONLY its function block (from the first function
# to its "── Run ──" marker) via process substitution. This imports EH4's real
# helper text (zero duplication) without its preamble or its suite run; we
# supply the env vars + logging those functions expect ourselves.
#
# Steps:
#   2. IN-BUDGET (always run): fire one request under the cap → assert HTTP
#      200. Proves a budgeted request flows through the gateway while the
#      projected BackendTrafficPolicy ceiling is > 0 (RemainingTokens > 0).
#   3. OVER-BUDGET (metering-gated): drive consumed past limitTokens and assert
#      the gateway returns HTTP 429 + the TokenBudget status reflects
#      exhaustion (phase=Exhausted). The token-cost metering pipeline (OTEL
#      processor → keese_token_budget_consumed_total → controller queryConsumed
#      → RemainingTokens 0 → 0-rps ceiling) is NOT wired in the local
#      bootstrap; check-metering.sh reports absent and this step SKIPS cleanly
#      (revisit_when_token_metering_live).
#   4. WINDOW RESET (metering-gated): after exhaustion, assert the budget
#      recovers — status.windowStart advances and the gateway returns 200.
#
# Refs: tests/e2e/rebac-decision/test-rebac-decision.sh (sourced helper)
#       internal/controller/policy/tokenbudget_controller.go (resetWindow)

set -euo pipefail
IFS=$'\n\t'

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../../.." && pwd)"
EH4_HELPER="${SCRIPT_DIR}/../rebac-decision/test-rebac-decision.sh"
LIB_DIR="${SCRIPT_DIR}/../lib"

# shellcheck source=../../../scripts/lib/log.sh
source "${REPO_ROOT}/scripts/lib/log.sh"

# ── Env the sourced EH4 functions expect ─────────────────────────────────────
# (EH4 sets these in its own preamble, which we deliberately do NOT source.)
CONTEXT="${KUBECTL_CONTEXT:-$(kubectl config current-context 2>/dev/null || true)}"
BUDGET_NS="${BUDGET_NS:-alpha}"
BUDGET_NAME="${BUDGET_NAME:-my-ws-budget}"
WORKSPACE_NS="${WORKSPACE_NS:-${BUDGET_NS}}"
SESSION_LABEL="${SESSION_LABEL:-keese.ai/session}"
GATEWAY_HOST="${GATEWAY_HOST:-envoy-ai-gateway.keese-system.svc:443}"
CA_PATH="${CA_PATH:-/var/run/keese/ca/ca.crt}"
MESSAGES_PATH="${MESSAGES_PATH:-/anthropic/v1/messages}"
# Model under test — keep in lockstep with budget.yaml's limits[].model.
MODEL="${MODEL:-claude-opus-4-7}"
# The ALLOW workspace whose SA token we mint (scope of the TokenBudget).
ALLOW_WS="${ALLOW_WS:-my-ws}"
export CONTEXT WORKSPACE_NS SESSION_LABEL GATEWAY_HOST CA_PATH MESSAGES_PATH MODEL

if [[ ! -f "${EH4_HELPER}" ]]; then
  log::err "[enforce] EH4 helper not found at ${EH4_HELPER}"
  exit 1
fi

# Source ONLY EH4's helper function block: from its first function definition
# through (but excluding) its "── Run ──" marker. Reuse, not copy; no EH4
# preamble (we set env + log ourselves) and no EH4 suite run.
# shellcheck disable=SC1090  # dynamic source of a trimmed sibling helper.
source <(sed -n '/^mint_sa_token() {/,/^# ── Run /p' "${EH4_HELPER}" | sed '$d')

for fn in mint_sa_token fire_request assert_status poll_status warm_up_gateway; do
  if ! declare -F "${fn}" >/dev/null; then
    log::err "[enforce] EH4 helper did not provide function ${fn} (sourcing drifted?)"
    exit 1
  fi
done

# A workspace session pod is the curl client (same identity as a real agent).
SESSION_POD="${SESSION_POD:-$(kubectl --context="${CONTEXT}" -n "${WORKSPACE_NS}" \
  get pod -l "${SESSION_LABEL}" -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || true)}"
if [[ -z "${SESSION_POD}" ]]; then
  log::err "[enforce] no workspace session pod in ${WORKSPACE_NS} (label ${SESSION_LABEL})"
  log::err "  Apply dev/demo/hello-keese.yaml first, or set WORKSPACE_NS / SESSION_LABEL."
  exit 1
fi
export SESSION_POD

kc() { kubectl --context="${CONTEXT}" "$@"; }

FAILURES=0

# ── Step 2: in-budget → 200 (always) ─────────────────────────────────────────
ALLOW_TOKEN="$(mint_sa_token "${ALLOW_WS}")" || {
  log::err "[enforce] could not mint SA token for ${ALLOW_WS}"
  exit 1
}

warm_up_gateway "${ALLOW_TOKEN}"

# Under the cap, RemainingTokens > 0 → projected ceiling > 0 → request flows.
assert_status "in-budget-200" "${ALLOW_TOKEN}" "200" \
  || FAILURES=$((FAILURES + 1))

# ── Steps 3-4: over-budget + reset (metering-gated) ──────────────────────────
# The local bootstrap doesn't wire token-cost metering, so we can't drive
# consumed past limitTokens. Gate on check-metering.sh: skip cleanly when the
# pipeline is absent (exit 2), proceed only when it's live.
METERING_RC=0
"${LIB_DIR}/check-metering.sh" || METERING_RC=$?

if [[ "${METERING_RC}" -eq 2 ]]; then
  log::warn "[enforce] SKIP steps 3-4 (over-budget 429 + window reset): token-cost"
  log::warn "          metering pipeline not live. revisit_when_token_metering_live."
elif [[ "${METERING_RC}" -ne 0 ]]; then
  log::err "[enforce] check-metering.sh errored (rc=${METERING_RC})"
  FAILURES=$((FAILURES + 1))
else
  # Step 3 — OVER-BUDGET. Drive consumption past the cap, then assert the
  # gateway flips to 429 within the controller's reconcile + cache window.
  log::info "[enforce] driving consumption past the budget cap …"
  OVER_TIMEOUT_S="${OVER_TIMEOUT_S:-120}"
  over_deadline=$(($(date +%s) + OVER_TIMEOUT_S))
  while [[ $(date +%s) -lt ${over_deadline} ]]; do
    code="$(fire_request "${ALLOW_TOKEN}")"
    [[ "${code}" == "429" ]] && break
    # keep spending; each iteration is a real request (rule 06 "Eventually").
  done
  poll_status "over-budget-429" "${ALLOW_TOKEN}" "429" "${OVER_TIMEOUT_S}" \
    || FAILURES=$((FAILURES + 1))

  # Status must reflect exhaustion (phase=Exhausted).
  PHASE="$(kc -n "${BUDGET_NS}" get tokenbudget "${BUDGET_NAME}" \
    -o jsonpath='{.status.phase}' 2>/dev/null || true)"
  if [[ "${PHASE}" != "Exhausted" ]]; then
    log::err "[enforce] status.phase=${PHASE:-<empty>} (expected Exhausted)"
    FAILURES=$((FAILURES + 1))
  else
    log::ok "[enforce] TokenBudget status reflects exhaustion (phase=Exhausted)"
  fi

  # Step 4 — WINDOW RESET. Capture windowStart and assert it advances
  # (recovery), then a request 200s again.
  WS_BEFORE="$(kc -n "${BUDGET_NS}" get tokenbudget "${BUDGET_NAME}" \
    -o jsonpath='{.status.windowStart}' 2>/dev/null || true)"
  log::info "[enforce] windowStart before reset: ${WS_BEFORE}"
  RESET_TIMEOUT_S="${RESET_TIMEOUT_S:-180}"
  reset_deadline=$(($(date +%s) + RESET_TIMEOUT_S))
  ws_advanced=""
  while [[ $(date +%s) -lt ${reset_deadline} ]]; do
    ws_now="$(kc -n "${BUDGET_NS}" get tokenbudget "${BUDGET_NAME}" \
      -o jsonpath='{.status.windowStart}' 2>/dev/null || true)"
    if [[ -n "${ws_now}" && "${ws_now}" != "${WS_BEFORE}" ]]; then
      ws_advanced="${ws_now}"
      break
    fi
  done
  if [[ -n "${ws_advanced}" ]]; then
    log::ok "[enforce] windowStart advanced ${WS_BEFORE} → ${ws_advanced} (reset)"
    poll_status "post-reset-200" "${ALLOW_TOKEN}" "200" 60 \
      || FAILURES=$((FAILURES + 1))
  else
    log::err "[enforce] windowStart never advanced within ${RESET_TIMEOUT_S}s"
    FAILURES=$((FAILURES + 1))
  fi
fi

if [[ "${FAILURES}" -gt 0 ]]; then
  log::err "[enforce] ${FAILURES} case(s) failed"
  exit 1
fi
log::ok "[enforce] enforcement assertions passed"
