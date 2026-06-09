#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright (c) 2026 keese-ai
#
# EH8 — assert a FeatureGate's status settled correctly after a reconcile.
#
# Two facts native kuttl can't assert cleanly on their own:
#   - status.effective matches the expected value, AND
#   - status.observedGeneration == metadata.generation (proves the
#     reconciler caught up with the latest spec edit; observedGeneration is
#     a dynamic int so it can't be a literal in a TestAssert), AND
#   - the Ready condition's observedGeneration also tracks the current
#     generation (rule 04.4 — status reflects the observed spec).
#
# Usage:
#   assert-status.sh <gate-name> <expected-effective: true|false>
#
# Env: KUBECTL_CONTEXT (default: current-context). Skips with no context.

set -euo pipefail
IFS=$'\n\t'

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../../.." && pwd)"

# shellcheck source=../../../scripts/lib/log.sh
source "${REPO_ROOT}/scripts/lib/log.sh"

GATE="${1:?usage: assert-status.sh <gate-name> <expected-effective: true|false>}"
EXPECTED="${2:?usage: assert-status.sh <gate-name> <expected-effective: true|false>}"

CONTEXT="${KUBECTL_CONTEXT:-$(kubectl config current-context 2>/dev/null || true)}"
if [[ -z "${CONTEXT}" ]]; then
  log::warn "[status] no kubectl context — skipping (structural validation only)"
  exit 0
fi

get() {
  kubectl --context="${CONTEXT}" get featuregate "${GATE}" \
    -o "jsonpath=${1}" 2>/dev/null || true
}

# Poll until observedGeneration catches generation AND effective matches, or
# we time out. Reconcile after a spec edit is async.
deadline=$((SECONDS + 90))
while ((SECONDS < deadline)); do
  gen="$(get '{.metadata.generation}')"
  obs="$(get '{.status.observedGeneration}')"
  eff="$(get '{.status.effective}')"
  cond_obs="$(get '{.status.conditions[?(@.type=="Ready")].observedGeneration}')"
  cond_status="$(get '{.status.conditions[?(@.type=="Ready")].status}')"

  if [[ -n "${gen}" && "${obs}" == "${gen}" && "${eff}" == "${EXPECTED}" &&
    "${cond_status}" == "True" && "${cond_obs}" == "${gen}" ]]; then
    log::ok "[status] ${GATE}: effective=${eff} observedGeneration=${obs}==generation Ready=True"
    exit 0
  fi
  sleep 2
done

log::err "[status] ${GATE} did not settle within timeout"
log::dim "  generation=${gen:-<none>} observedGeneration=${obs:-<none>} effective=${eff:-<none>} (want ${EXPECTED})"
log::dim "  Ready.status=${cond_status:-<none>} Ready.observedGeneration=${cond_obs:-<none>}"
exit 1
