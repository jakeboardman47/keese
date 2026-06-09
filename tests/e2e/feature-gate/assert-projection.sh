#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright (c) 2026 keese-ai
#
# EH8 — assert the FeatureGateReconciler projected the gate's effective
# value into the canonical projection ConfigMap.
#
# The reconciler (internal/controller/policy/featuregate_controller.go)
# rewrites keese-system/keese-features (key gates.json) from the full
# FeatureGate list on every reconcile: gates.json is a JSON object mapping
# `<gate-name> -> <effective bool>`. That ConfigMap is what every keese
# binary mounts at /etc/keese/features/gates.json — so its content is the
# OBSERVABLE controller behavior the gate flip changes (NOT the CR status).
#
# This is the artifact assertion that a native kuttl TestAssert can't make:
# the projection key/value isn't a field on the FeatureGate CR.
#
# Usage:
#   assert-projection.sh <gate-name> <expected: true|false>
#
# Env:
#   KUBECTL_CONTEXT   override the kube context (default: current-context)
#   FEATURES_NS       projection namespace   (default: keese-system)
#   FEATURES_CM       projection ConfigMap   (default: keese-features)
#   FEATURES_KEY      projection JSON key     (default: gates.json)
#
# Skips cleanly (exit 0) when there is no kube context — the suite is then
# being structurally validated, not run against a cluster.

set -euo pipefail
IFS=$'\n\t'

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../../.." && pwd)"

# shellcheck source=../../../scripts/lib/log.sh
source "${REPO_ROOT}/scripts/lib/log.sh"

GATE="${1:?usage: assert-projection.sh <gate-name> <expected: true|false>}"
EXPECTED="${2:?usage: assert-projection.sh <gate-name> <expected: true|false>}"

case "${EXPECTED}" in
  true | false) ;;
  *)
    log::err "[projection] expected value must be 'true' or 'false', got '${EXPECTED}'"
    exit 2
    ;;
esac

CONTEXT="${KUBECTL_CONTEXT:-$(kubectl config current-context 2>/dev/null || true)}"
if [[ -z "${CONTEXT}" ]]; then
  log::warn "[projection] no kubectl context — skipping (structural validation only)"
  exit 0
fi

FEATURES_NS="${FEATURES_NS:-keese-system}"
FEATURES_CM="${FEATURES_CM:-keese-features}"
FEATURES_KEY="${FEATURES_KEY:-gates.json}"

# Poll: the reconcile that follows a spec change is async, so the CM may
# lag the CR by a beat. Bounded retry; fail-closed on timeout.
deadline=$((SECONDS + 60))
gates_json=""
while ((SECONDS < deadline)); do
  gates_json="$(kubectl --context="${CONTEXT}" -n "${FEATURES_NS}" \
    get configmap "${FEATURES_CM}" \
    -o "jsonpath={.data.${FEATURES_KEY//./\\.}}" 2>/dev/null || true)"
  if [[ -n "${gates_json}" ]]; then
    break
  fi
  sleep 2
done

if [[ -z "${gates_json}" ]]; then
  log::err "[projection] ConfigMap ${FEATURES_NS}/${FEATURES_CM} key ${FEATURES_KEY} not found"
  log::err "  The FeatureGateReconciler should have created it. Is the operator running?"
  exit 1
fi

# Extract the gate's projected value. jq if present (clean), else a grep
# fallback so the suite doesn't hard-depend on jq on every CI runner.
actual=""
if command -v jq >/dev/null 2>&1; then
  actual="$(printf '%s' "${gates_json}" | jq -r --arg g "${GATE}" '.[$g] // "absent"')"
else
  if printf '%s' "${gates_json}" | grep -qE "\"${GATE}\"[[:space:]]*:[[:space:]]*true"; then
    actual="true"
  elif printf '%s' "${gates_json}" | grep -qE "\"${GATE}\"[[:space:]]*:[[:space:]]*false"; then
    actual="false"
  else
    actual="absent"
  fi
fi

deadline=$((SECONDS + 60))
while [[ "${actual}" != "${EXPECTED}" ]] && ((SECONDS < deadline)); do
  sleep 2
  gates_json="$(kubectl --context="${CONTEXT}" -n "${FEATURES_NS}" \
    get configmap "${FEATURES_CM}" \
    -o "jsonpath={.data.${FEATURES_KEY//./\\.}}" 2>/dev/null || true)"
  if command -v jq >/dev/null 2>&1; then
    actual="$(printf '%s' "${gates_json}" | jq -r --arg g "${GATE}" '.[$g] // "absent"')"
  else
    if printf '%s' "${gates_json}" | grep -qE "\"${GATE}\"[[:space:]]*:[[:space:]]*${EXPECTED}"; then
      actual="${EXPECTED}"
    fi
  fi
done

if [[ "${actual}" != "${EXPECTED}" ]]; then
  log::err "[projection] gate '${GATE}' projected as '${actual}', expected '${EXPECTED}'"
  log::dim "  gates.json = ${gates_json}"
  exit 1
fi

log::ok "[projection] gate '${GATE}' = ${EXPECTED} in ${FEATURES_NS}/${FEATURES_CM}"
