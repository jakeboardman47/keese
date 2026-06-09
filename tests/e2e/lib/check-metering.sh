#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright (c) 2026 keese-ai
#
# Additive companion to check-prereqs.sh, used by the EH7 TokenBudget
# enforcement suite. Gates the token-cost METERING pipeline that the
# TokenBudget controller depends on to drive over-budget enforcement:
# the OTEL processor that records per-(tenant|workspace,model) token
# consumption as the Prometheus series `keese_token_budget_consumed_total`,
# which `internal/controller/policy/tokenbudget_controller.go` queries via
# `increase(...)` to compute consumedCurrent and project the rate-limit
# ceiling.
#
# Unlike check-prereqs.sh / check-extauth.sh (fail-closed: a missing
# prerequisite fails the gate), this gate is *skip-on-absent*: the local
# bootstrap does NOT wire the metering pipeline (no Prometheus, no OTEL
# token-cost processor — the controller falls back to FakePrometheusQuerier,
# which reports zero consumption). When metering is absent we cannot drive
# consumed tokens past the limit, so the over-budget (429) and window-reset
# steps cannot assert anything real. Rather than silently pass, those steps
# call this gate and SKIP cleanly when it reports absent.
#
# Exit-code convention:
#   0  metering pipeline live    → caller proceeds with over-budget assertions
#   2  metering pipeline absent   → caller SKIPS the over-budget step
#                                   (revisit_when_token_metering_live)
#   2  no kubectl context         → caller SKIPS (cannot probe)
#
# A live pipeline requires BOTH:
#   - a Prometheus query endpoint reachable as Service ${PROM_SVC} in
#     ${PROM_NS} (the controller's PrometheusQuerier target), AND
#   - the `keese_token_budget_consumed_total` series present (non-empty),
#     proving the OTEL token-cost processor is actually emitting.
#
# Override with KUBECTL_CONTEXT / PROM_NS / PROM_SVC / METRIC_NAME.
#
# Refs: internal/controller/policy/tokenbudget_controller.go (queryConsumed)
#       internal/controller/policy/ratelimit.go (RemainingTokens projection)
#       docs/plans/e2e-hardening/EH7-token-budget-e2e.md

set -euo pipefail
IFS=$'\n\t'

# Caller-visible skip code (distinct from a hard prereq failure).
METERING_SKIP=2

CONTEXT="${KUBECTL_CONTEXT:-$(kubectl config current-context 2>/dev/null || true)}"
PROM_NS="${PROM_NS:-keese-system}"
PROM_SVC="${PROM_SVC:-prometheus}"
METRIC_NAME="${METRIC_NAME:-keese_token_budget_consumed_total}"

if [[ -z "${CONTEXT}" ]]; then
  echo "[metering] no kubectl context — SKIP over-budget assertions" >&2
  exit "${METERING_SKIP}"
fi

# 1. Prometheus Service must exist (the controller's query target).
if ! kubectl --context="${CONTEXT}" -n "${PROM_NS}" \
  get svc "${PROM_SVC}" >/dev/null 2>&1; then
  cat <<EOF >&2
[metering] SKIP: token-cost metering pipeline not live.

  Service ${PROM_NS}/${PROM_SVC} (the TokenBudget controller's Prometheus
  query target) is absent. The local bootstrap does not wire the OTEL
  token-cost processor or Prometheus, so consumed-token counters stay at
  zero and over-budget enforcement cannot be driven.

  Tracking: revisit_when_token_metering_live.
EOF
  exit "${METERING_SKIP}"
fi

# 2. The consumed-token series must be present (proves the OTEL processor
#    is actually emitting, not just that Prometheus is up).
SERIES="$(kubectl --context="${CONTEXT}" -n "${PROM_NS}" \
  exec "svc/${PROM_SVC}" -- \
  wget -qO- "http://localhost:9090/api/v1/series?match[]=${METRIC_NAME}" 2>/dev/null || true)"

if [[ -z "${SERIES}" || "${SERIES}" == *'"data":[]'* ]]; then
  cat <<EOF >&2
[metering] SKIP: ${METRIC_NAME} series is empty.

  Prometheus is reachable but the OTEL token-cost processor is not emitting
  ${METRIC_NAME}. Without it the controller reads zero consumption and
  over-budget enforcement cannot be exercised.

  Tracking: revisit_when_token_metering_live.
EOF
  exit "${METERING_SKIP}"
fi

echo "[metering] OK: ${PROM_NS}/${PROM_SVC} reachable; ${METRIC_NAME} series present"
