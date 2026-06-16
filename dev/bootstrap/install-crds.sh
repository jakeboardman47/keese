#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright (c) 2026 keese-ai
#
# install-crds.sh — pre-apply chart-shipped CRDs server-side before
# helmfile sync. Closes TD-P1-10.
#
# Why this exists: Helm's CRD lifecycle (per the chart spec) installs
# CRDs from `crds/` only on first install and never upgrades them.
# Helm chart bumps therefore silently leave stale CRDs in place,
# which is what bit us twice in 2026 Q2 — first with EG v1.4 →
# v1.6 (BackendTLSPolicy moved from v1alpha3 → v1 GA) and again
# with EG v1.6 → v1.7 (additional API churn). Symptom: helmfile
# sync reports success while every kubectl apply targeting the
# new fields fails with "no matches for kind".
#
# This script extracts each chart's `crds/*.yaml` and applies via
# `kubectl apply --server-side --force-conflicts` BEFORE helmfile
# sync runs. SSA preserves the existing CRD's other-controller
# fields (annotations, status owners) and force-conflicts wins on
# the schema fields the chart owns.
#
# Idempotent: re-running just no-ops on unchanged CRDs.
# Resumable: each chart is a separate `run::step`.

set -euo pipefail
IFS=$'\n\t'

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"

# shellcheck source=../scripts/lib/log.sh
source "${REPO_ROOT}/scripts/lib/log.sh"

CONTEXT="${KUBECTL_CONTEXT:-$(kubectl config current-context 2>/dev/null || true)}"
if [[ -z "${CONTEXT}" ]]; then
  log::err "no kubectl context; set KUBECTL_CONTEXT or run kubectl config use-context"
  exit 1
fi

# Charts whose CRDs we pre-apply, in helmfile sync order. Each
# entry is `<oci-or-repo-ref>:<version>` — same shape as the
# helmfile.yaml `chart:` + `version:` pair. Update when helmfile
# pins move.
#
# We deliberately do NOT pre-apply CRDs for charts that own their
# CRDs via templates (not the `crds/` convention) — those upgrade
# fine via helmfile sync. Empirical list of "needs pre-apply":
CHARTS_NEEDING_PREAPPLY=(
  # Envoy Gateway: ships gateway.networking.k8s.io/* + gateway.envoyproxy.io/*
  "oci://docker.io/envoyproxy/gateway-helm:v1.7.0"
  # AI Gateway CRDs ship as a separate chart whose templates ARE the
  # CRDs (per its design). Helmfile-managed; this script does not
  # need to handle it. Listed for context only — leave commented.
  # "oci://docker.io/envoyproxy/ai-gateway-crds-helm:v0.6.0"
)

# Workdir for chart pulls.
WORK_DIR="$(mktemp -d -t keese-install-crds-XXXXXX)"
trap 'rm -rf "${WORK_DIR}"' EXIT

helm_pull() {
  local ref="$1" version="$2"
  log::info "pulling ${ref}@${version}"
  helm pull "${ref}" --version "${version}" -d "${WORK_DIR}" >/dev/null
}

apply_chart_crds() {
  local ref="$1" version="$2"
  helm_pull "${ref}" "${version}"

  # Find the produced .tgz and extract.
  local tgz extracted
  tgz="$(find "${WORK_DIR}" -maxdepth 1 -name '*.tgz' -print -quit)"
  if [[ -z "${tgz}" ]]; then
    log::err "no .tgz produced by helm pull ${ref}@${version}"
    return 1
  fi
  extracted="${WORK_DIR}/extracted-$(basename "${tgz}" .tgz)"
  mkdir -p "${extracted}"
  tar -xzf "${tgz}" -C "${extracted}"
  rm "${tgz}"

  # Find every yaml in the chart's crds/ subtree.
  local crd_root
  crd_root="$(find "${extracted}" -type d -name crds -print -quit)"
  if [[ -z "${crd_root}" ]]; then
    log::warn "${ref}@${version}: no crds/ dir; skipping"
    return 0
  fi

  log::info "apply CRDs from ${ref}@${version} (${crd_root})"
  # SSA + force-conflicts: chart owns the schema; we cleanly take
  # ownership on first apply and update on subsequent applies.
  if find "${crd_root}" -name '*.yaml' -print0 | head -c 1 | grep -q .; then
    find "${crd_root}" -name '*.yaml' -print0 | xargs -0 -I{} \
      kubectl --context="${CONTEXT}" apply --server-side --force-conflicts -f {}
  else
    log::warn "${ref}@${version}: crds/ dir empty"
  fi
}

main() {
  log::info "install-crds.sh: pre-apply chart-shipped CRDs (context=${CONTEXT})"
  for entry in "${CHARTS_NEEDING_PREAPPLY[@]}"; do
    local ref="${entry%:*}"
    local version="${entry##*:}"
    apply_chart_crds "${ref}" "${version}"
  done
  log::ok "install-crds.sh: complete"
}

main "$@"
