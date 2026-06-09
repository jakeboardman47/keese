#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright (c) 2026 keese-ai
#
# EH7 step 1 — PROJECTION. Proves the TokenBudget reconciler
# (internal/controller/policy/tokenbudget_controller.go) projects the budget
# onto the Envoy AI Gateway as the downstream artifact ratelimit_client.go
# writes: a BackendTrafficPolicy (gateway.envoyproxy.io/v1alpha1) named
# `keese-tb-<tokenbudget-uid>-<model>`, carrying a LocalRateLimit rule whose
# ceiling derives from RemainingTokens.
#
# Assertions (all against the live projected object):
#   (a) the BackendTrafficPolicy exists for (tokenbudget-uid, model);
#   (b) its keese.ai/tokenbudget-scope-id + keese.ai/tokenbudget-model
#       annotations reflect THIS budget's scope + model — i.e. the projection
#       is keyed to the budget, not a stray policy;
#   (c) its LocalRateLimit rule selects on x-keese-scope == scope-id and
#       carries a per-second Requests ceiling. Under the local bootstrap
#       (no metering) consumed is 0, so RemainingTokens == cap and the
#       ceiling is > 0 (in-budget). The over-budget 0-rps ceiling is asserted
#       by the metering-gated step 2 (02-enforce.yaml).
#
# This is a status-observable artifact assertion (rule 06 "test behavior,
# events, and status"), not a log scrape.
#
# Refs: internal/controller/policy/ratelimit_client.go (buildBackendTrafficPolicy)
#       internal/controller/policy/ratelimit.go        (rateLimitPolicyName)

set -euo pipefail
IFS=$'\n\t'

CONTEXT="${KUBECTL_CONTEXT:-$(kubectl config current-context 2>/dev/null || true)}"
BUDGET_NS="${BUDGET_NS:-alpha}"
BUDGET_NAME="${BUDGET_NAME:-my-ws-budget}"
SCOPE_NAME="${SCOPE_NAME:-my-ws}"
MODEL="${MODEL:-claude-opus-4-7}"
# Projected policies land in the gateway namespace; the controller writes
# them into the TokenBudget's own namespace (rlPolicy.Namespace = tb.Namespace).
BTP_NS="${BTP_NS:-${BUDGET_NS}}"

if [[ -z "${CONTEXT}" ]]; then
  echo "[projection] no kubectl context — skipping (suite surfaces its own failure)" >&2
  exit 0
fi

kc() { kubectl --context="${CONTEXT}" "$@"; }

# Resolve the runtime TokenBudget UID — the projected BackendTrafficPolicy
# name is keese-tb-<uid>-<model>.
TB_UID="$(kc -n "${BUDGET_NS}" get tokenbudget "${BUDGET_NAME}" \
  -o jsonpath='{.metadata.uid}' 2>/dev/null || true)"
if [[ -z "${TB_UID}" ]]; then
  echo "[projection] FAIL: TokenBudget ${BUDGET_NS}/${BUDGET_NAME} not found" >&2
  exit 1
fi

BTP_NAME="keese-tb-${TB_UID}-${MODEL}"
echo "[projection] expecting BackendTrafficPolicy ${BTP_NS}/${BTP_NAME}"

# Poll for the projected policy (the controller projects on its 10s reconcile
# interval). No sleep-as-assertion: each iteration re-GETs the live object.
DEADLINE=$(($(date +%s) + 120))
FOUND=""
while [[ $(date +%s) -lt ${DEADLINE} ]]; do
  if kc -n "${BTP_NS}" get backendtrafficpolicy "${BTP_NAME}" >/dev/null 2>&1; then
    FOUND="yes"
    break
  fi
  echo "[projection] waiting for ${BTP_NAME} …" >&2
done

if [[ -z "${FOUND}" ]]; then
  echo "[projection] FAIL: BackendTrafficPolicy ${BTP_NS}/${BTP_NAME} never projected" >&2
  exit 1
fi

# jsonpath getter (same toolchain as EH4 / the rest of the suite — no jq
# dependency on the kuttl runner image).
btp() { kc -n "${BTP_NS}" get backendtrafficpolicy "${BTP_NAME}" -o jsonpath="$1" 2>/dev/null || true; }

fail=0

# (b) annotations reflect this budget's scope + model.
ANN_SCOPE="$(btp '{.metadata.annotations.keese\.ai/tokenbudget-scope-id}')"
ANN_MODEL="$(btp '{.metadata.annotations.keese\.ai/tokenbudget-model}')"
if [[ "${ANN_SCOPE}" != "${SCOPE_NAME}" ]]; then
  echo "[projection] FAIL: scope-id annotation=${ANN_SCOPE:-<empty>} (expected ${SCOPE_NAME})" >&2
  fail=1
fi
if [[ "${ANN_MODEL}" != "${MODEL}" ]]; then
  echo "[projection] FAIL: model annotation=${ANN_MODEL:-<empty>} (expected ${MODEL})" >&2
  fail=1
fi

# (c) LocalRateLimit rule keyed to the scope, with a numeric ceiling.
RL_TYPE="$(btp '{.spec.rateLimit.type}')"
SCOPE_HDR="$(btp '{.spec.rateLimit.local.rules[0].clientSelectors[0].headers[?(@.name=="x-keese-scope")].value}')"
RPS="$(btp '{.spec.rateLimit.local.rules[0].limit.requests}')"
RPS_UNIT="$(btp '{.spec.rateLimit.local.rules[0].limit.unit}')"

if [[ "${RL_TYPE}" != "Local" ]]; then
  echo "[projection] FAIL: rateLimit.type=${RL_TYPE:-<empty>} (expected Local)" >&2
  fail=1
fi
if [[ "${SCOPE_HDR}" != "${SCOPE_NAME}" ]]; then
  echo "[projection] FAIL: x-keese-scope selector=${SCOPE_HDR:-<empty>} (expected ${SCOPE_NAME})" >&2
  fail=1
fi
# Ceiling must be a non-negative integer. Under the no-metering bootstrap
# consumed=0 → ceiling == cap (>0); the metering-gated step asserts the
# 0-rps over-budget ceiling.
if ! [[ "${RPS}" =~ ^[0-9]+$ ]]; then
  echo "[projection] FAIL: ceiling requests=${RPS} is not a non-negative integer" >&2
  fail=1
fi
if [[ "${RPS_UNIT}" != "Second" ]]; then
  echo "[projection] FAIL: ceiling unit=${RPS_UNIT:-<empty>} (expected Second)" >&2
  fail=1
fi

if [[ "${fail}" -ne 0 ]]; then
  echo "[projection] FAIL: projected BackendTrafficPolicy does not reflect the budget" >&2
  exit 1
fi

echo "[projection] OK: ${BTP_NAME} reflects budget (scope=${ANN_SCOPE}, model=${ANN_MODEL}, ceiling=${RPS}/Second)"
