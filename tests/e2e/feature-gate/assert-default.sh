#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright (c) 2026 keese-ai
#
# EH8 step 0 — assert the gate's DOCUMENTED DEFAULT with no CR applied.
#
# Catalog (docs/designs/27b-feature-gate-catalog.md): cosign-installplan-verify
# is stage=alpha → default OFF. api/policy/v1alpha1.DefaultEffective(alpha)
# returns false. With no FeatureGate CR for this gate, the projection
# ConfigMap must NOT carry the gate with value `true`: either the key is
# absent (so consumers fall back to their compiled-in alpha default = off)
# or, if a seed CR with the default exists, it is projected as `false`.
# Both outcomes mean the consumer evaluates the gate OFF — which is the
# documented default this step proves observably.
#
# Pre-condition this step enforces: the gate is not currently pinned ON by
# a stray CR. If a CR with override=true is present from a prior run the
# step fails loudly (the suite's own teardown should have removed it).
#
# Env: KUBECTL_CONTEXT / FEATURES_NS / FEATURES_CM / FEATURES_KEY (see
# assert-projection.sh). Skips cleanly with no kube context.

set -euo pipefail
IFS=$'\n\t'

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../../.." && pwd)"

# shellcheck source=../../../scripts/lib/log.sh
source "${REPO_ROOT}/scripts/lib/log.sh"

GATE="cosign-installplan-verify"

CONTEXT="${KUBECTL_CONTEXT:-$(kubectl config current-context 2>/dev/null || true)}"
if [[ -z "${CONTEXT}" ]]; then
  log::warn "[default] no kubectl context — skipping (structural validation only)"
  exit 0
fi

FEATURES_NS="${FEATURES_NS:-keese-system}"
FEATURES_CM="${FEATURES_CM:-keese-features}"
FEATURES_KEY="${FEATURES_KEY:-gates.json}"

# 1. No FeatureGate CR for this gate may be pinning it ON. A leftover
#    override=true CR would invalidate the default assertion.
override="$(kubectl --context="${CONTEXT}" \
  get featuregate "${GATE}" \
  -o jsonpath='{.spec.override}' 2>/dev/null || true)"
if [[ "${override}" == "true" ]]; then
  log::err "[default] FeatureGate ${GATE} has spec.override=true — not the default state."
  log::err "  A prior run did not clean up. Delete it: kubectl delete featuregate ${GATE}"
  exit 1
fi

# 2. The projection must not carry the gate as `true`. Absent or false both
#    satisfy the alpha=off default.
gates_json="$(kubectl --context="${CONTEXT}" -n "${FEATURES_NS}" \
  get configmap "${FEATURES_CM}" \
  -o "jsonpath={.data.${FEATURES_KEY//./\\.}}" 2>/dev/null || true)"

if [[ -z "${gates_json}" ]]; then
  # No projection CM yet (no gates reconciled). Consumers fall back to
  # their compiled-in defaults — alpha=off. The documented default holds.
  log::ok "[default] no projection ConfigMap yet — consumers use compiled-in default (alpha=off) for '${GATE}'"
  exit 0
fi

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

if [[ "${actual}" == "true" ]]; then
  log::err "[default] gate '${GATE}' projected as true with no override CR — default (alpha=off) violated"
  log::dim "  gates.json = ${gates_json}"
  exit 1
fi

log::ok "[default] gate '${GATE}' default observed OFF (projected '${actual}', alpha→off)"
