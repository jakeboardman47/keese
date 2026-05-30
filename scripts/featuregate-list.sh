#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright (c) 2026 keese-ai
#
# featuregate-list.sh — print every keese FeatureGate with its stage,
# default, override, effective value, and consumer list. The
# operator-facing companion to `kubectl get featuregate`. Output is a
# table; pipe to grep / awk for scripting.
#
# Usage:
#   scripts/featuregate-list.sh           # all gates
#   scripts/featuregate-list.sh --json    # raw JSON for tooling

set -euo pipefail
IFS=$'\n\t'

# shellcheck source=lib/log.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib/log.sh"

main() {
  local mode="table"
  if [[ "${1:-}" == "--json" ]]; then
    mode="json"
  elif [[ -n "${1:-}" ]]; then
    log::err "unknown argument: $1"
    log::err "Usage: $0 [--json]"
    exit 64
  fi

  if ! command -v kubectl >/dev/null 2>&1; then
    log::err "kubectl not on PATH"
    exit 127
  fi
  if ! command -v jq >/dev/null 2>&1; then
    log::err "jq not on PATH"
    exit 127
  fi

  local raw
  if ! raw=$(kubectl get featuregates.policy.keese.ai -o json 2>/dev/null); then
    log::err "could not list featuregates — is the CRD installed?"
    exit 1
  fi

  if [[ "${mode}" == "json" ]]; then
    printf '%s\n' "${raw}"
    return 0
  fi

  printf '%-36s %-12s %-9s %-9s %-9s %-9s %s\n' \
    NAME STAGE DEFAULT OVERRIDE EFFECTIVE RESTART OWNERS
  jq -r '
    .items[] |
      [
        .metadata.name,
        .spec.stage,
        (
          if .spec.stage == "beta" or .spec.stage == "ga"
          then "true"
          else "false"
          end
        ),
        (.spec.override // "—" | tostring),
        (.status.effective // "—" | tostring),
        (.spec.restartRequired // false | tostring),
        ((.spec.owners // []) | join(","))
      ] | @tsv
  ' <<<"${raw}" |
    awk -F'\t' '{ printf "%-36s %-12s %-9s %-9s %-9s %-9s %s\n",
      $1, $2, $3, $4, $5, $6, $7 }'
}

main "$@"
