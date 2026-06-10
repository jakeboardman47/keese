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
#      the gateway returns HTTP 429 + the response carries
#      `x-keese-limit-source: token-budget` (the long-window TokenBudget signal
#      — ADR 30 / 05a / 10b — emitted by ext_authz's Envoy local_reply off the
#      NATS-KV exceeded boolean; distinct from the gateway's own short-window
#      `gateway-token-rate`) + the TokenBudget status reflects exhaustion
#      (phase=Exhausted). The FULL live path (OTEL collector OTLP→/ingest
#      shaping → keese-token-meter :dev image → keese_token_budget_consumed_total
#      → controller crossover → NATS-KV exceeded → ext_authz local_reply) still
#      depends on CH5b's two remaining stubs; check-metering-fully-live.sh gates
#      it and this step SKIPS cleanly when they are unmet
#      (revisit_when_metering_fully_live).
#   4. WINDOW RESET (metering-gated): after exhaustion, assert the budget
#      recovers — status.windowStart advances and the gateway returns 200.
#
# Refs: tests/e2e/rebac-decision/test-rebac-decision.sh (sourced helper)
#       tests/e2e/lib/check-metering-fully-live.sh (CH5d full-live gate)
#       docs/designs/30-token-metering-pipeline.md (the metering hop + phases)
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

# ── Limit-source header capture (CH5d) ────────────────────────────────────────
#
# EH4's sourced fire_request echoes ONLY the HTTP status code, not response
# headers — so it cannot distinguish the long-window TokenBudget 429
# (x-keese-limit-source: token-budget) from the gateway's short-window
# gateway-token-rate 429. We add a thin sibling that mirrors EH4's exact curl
# (same path / SA token / CA / model body) but dumps response HEADERS to stdout
# (-D -) with the body discarded (-o /dev/null). Per rule 02 + 05.10 we never
# emit the body; the caller greps only the single x-keese-limit-source header.
# We do NOT edit EH4 — this stays local to the enforcement step.
fire_request_headers() {
  local token="$1"
  kubectl --context="${CONTEXT}" -n "${WORKSPACE_NS}" \
    exec "${SESSION_POD}" -- bash -c "
      curl -s -o /dev/null -D - \
        --max-time 30 \
        --cacert ${CA_PATH} \
        -X POST 'https://${GATEWAY_HOST}${MESSAGES_PATH}' \
        -H 'Authorization: Bearer ${token}' \
        -H 'content-type: application/json' \
        -H 'anthropic-version: 2023-06-01' \
        -d '{\"model\":\"${MODEL}\",\"max_tokens\":8,\"messages\":[{\"role\":\"user\",\"content\":\"ok\"}]}'
    " 2>/dev/null || true
}

# Extract the x-keese-limit-source header value (lowercased name, trimmed) from
# a dumped header block. Echoes empty if absent.
limit_source_of() {
  # $1 = raw response header block.
  printf '%s\n' "$1" \
    | tr -d '\r' \
    | grep -i '^x-keese-limit-source:' \
    | head -n1 \
    | sed 's/^[^:]*:[[:space:]]*//'
}

# Poll the gateway until a 429 carries x-keese-limit-source: token-budget
# (the long-window budget signal — NOT gateway-token-rate). Each iteration is a
# real request (rule 06 "Eventually"; no sleep-as-assertion). Returns 0 on the
# first matching response, 1 on timeout.
poll_limit_source() {
  local case_id="$1" token="$2" want="$3" timeout="$4"
  local deadline=$(($(date +%s) + timeout))
  local hdrs code src last_src=""
  while [[ $(date +%s) -lt ${deadline} ]]; do
    hdrs="$(fire_request_headers "${token}")"
    code="$(printf '%s\n' "${hdrs}" | tr -d '\r' | grep -iE '^HTTP/' | tail -n1 | awk '{print $2}')"
    src="$(limit_source_of "${hdrs}")"
    if [[ "${code}" == "429" && "${src}" == "${want}" ]]; then
      log::ok "[${case_id}] 429 carries x-keese-limit-source: ${src}"
      return 0
    fi
    last_src="${src}"
    log::dim "[${case_id}] http=${code:-???} limit-source=${src:-<none>}; want 429/${want}"
  done
  log::err "[${case_id}] never saw 429 + x-keese-limit-source: ${want} within ${timeout}s (last source=${last_src:-<none>})"
  return 1
}

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
# The over-budget 429 with x-keese-limit-source: token-budget exercises the FULL
# live path: OTEL collector OTLP→/ingest shaping → keese-token-meter :dev image
# → consumed series → reconciler crossover → NATS-KV exceeded → ext_authz
# local_reply. That path still depends on CH5b's two remaining stubs (the :dev
# meter image via `make token-meter-load`, and the collector ingest shaping), so
# we gate on check-metering-fully-live.sh — the umbrella precondition the docs
# track as revisit_when_metering_fully_live. Skip cleanly when unmet (exit 2),
# proceed only when the full path is live (exit 0). No fake pass (rule 06).
METERING_RC=0
"${LIB_DIR}/check-metering-fully-live.sh" || METERING_RC=$?

if [[ "${METERING_RC}" -eq 2 ]]; then
  log::warn "[enforce] SKIP steps 3-4 (over-budget 429 + window reset): the full"
  log::warn "          token-metering enforcement path is not live (CH5b stubs)."
  log::warn "          revisit_when_metering_fully_live — see check-metering-fully-live.sh."
elif [[ "${METERING_RC}" -ne 0 ]]; then
  log::err "[enforce] check-metering-fully-live.sh errored (rc=${METERING_RC})"
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

  # The 429 must be attributed to the long-window TokenBudget, not the
  # gateway's own short-window token-rate cap: assert x-keese-limit-source:
  # token-budget (ADR 30 / 05a / 10b). This is what distinguishes the metering
  # loop (NATS-KV → ext_authz local_reply) from the projected BTP rate limit.
  poll_limit_source "over-budget-limit-source" "${ALLOW_TOKEN}" "token-budget" "${OVER_TIMEOUT_S}" \
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
