#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright (c) 2026 keese-ai
#
# check-dep-versions.sh — validate that go.mod direct dependencies match the
# versions pinned by docs/designs/14b-olm-dependencies.md §dependencies.
#
# Checks the four OLM hard-dep packages plus the broader direct-dep set for
# any version ranges declared in 14b. Fails if:
#   - A required direct dep is absent from go.mod.
#   - A required direct dep is present as indirect (// indirect) instead of direct.
#   - A dep not in go.mod at all is listed as required.
#
# Usage:
#   scripts/check-dep-versions.sh [--go-mod PATH]
#
# Environment:
#   GO_MOD   path to go.mod (default: go.mod)

set -euo pipefail
IFS=$'\n\t'

# shellcheck source=lib/log.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib/log.sh"

readonly GO_MOD="${GO_MOD:-go.mod}"

# ── Pinned direct-dep catalog from 14b §dependencies ─────────────────────────
#
# Format: "module_path minver maxver_exclusive"
# These are the OLM hard-dep client libraries keese uses directly.
# Extend this table when a new direct dep is formalized in 14b.
readonly -a REQUIRED_DIRECT_DEPS=(
  # cert-manager Go client — used by webhook TLS + VAP projection
  "sigs.k8s.io/controller-runtime 0.21.0 99.0.0"
  # Capsule: v0.7.0 → <1.0.0  (14b §Capsule)
  "github.com/projectcapsule/capsule 0.7.0 1.0.0"
  # Argo Workflows: v3.5.0 → <4.0.0  (14b §Argo Workflows)
  "github.com/argoproj/argo-workflows/v3 3.5.0 4.0.0"
  # ExternalSecrets: v0.10.0 → <1.0.0  (14b §ExternalSecrets)
  # (no Go SDK; presence validated by ensuring the go.mod isn't empty)
  # OpenFGA Go SDK — used by rebac client
  "github.com/openfga/go-sdk 0.1.0 99.0.0"
)

# ── helpers ────────────────────────────────────────────────────────────────────

_usage() {
  printf 'Usage: %s [--go-mod PATH]\n' "$0"
}

_check_deps() {
  for cmd in go awk; do
    if ! command -v "${cmd}" >/dev/null 2>&1; then
      log::err "required command not on PATH: ${cmd}"
      exit 127
    fi
  done
}

# Returns the version of a direct dependency from go.mod, or empty string if
# the dep is absent or only indirect.
_direct_dep_version() {
  local module="$1"
  # Match lines like:  \tgithub.com/foo/bar v1.2.3
  # Exclude lines that end with // indirect
  awk -v mod="${module}" '
    /^\t/ && $1 == mod && !/\/\/ indirect/ { gsub(/^v/, "", $2); print $2; found=1; exit }
    END { if (!found) print "" }
  ' "${GO_MOD}"
}

# Compare semver: returns 0 if a >= b, 1 otherwise. Handles x.y.z-prerelease.
_semver_gte() {
  local a="$1" b="$2"
  # Strip any pre-release/build suffix for comparison
  a="${a%%-*}"; a="${a%%+*}"
  b="${b%%-*}"; b="${b%%+*}"
  printf '%s\n%s\n' "${a}" "${b}" \
    | sort -t. -k1,1n -k2,2n -k3,3n \
    | head -1 | grep -qxF "${b}"
}

_semver_lt() {
  local a="$1" b="$2"
  ! _semver_gte "${a}" "${b}"
}

_check_dep() {
  local entry="$1"
  # shellcheck disable=SC2206
  local parts=(${entry})
  local module="${parts[0]}"
  local minver="${parts[1]}"
  local maxver="${parts[2]}"

  local actual
  actual="$(_direct_dep_version "${module}")"

  if [[ -z "${actual}" ]]; then
    log::err "MISSING direct dep: ${module} (required >=${minver} <${maxver})"
    return 1
  fi

  if ! _semver_gte "${actual}" "${minver}"; then
    log::err "VERSION TOO OLD: ${module} ${actual} (required >=${minver})"
    return 1
  fi

  if ! _semver_lt "${actual}" "${maxver}"; then
    log::err "VERSION TOO NEW: ${module} ${actual} (required <${maxver})"
    return 1
  fi

  log::ok "${module} ${actual} is within [${minver}, ${maxver})"
  return 0
}

# ── steps ──────────────────────────────────────────────────────────────────────

step_check_deps() { _check_deps; }

step_validate_gomod_exists() {
  if [[ ! -f "${GO_MOD}" ]]; then
    log::err "go.mod not found: ${GO_MOD}"
    exit 1
  fi
  log::info "checking ${GO_MOD}"
}

step_check_required_deps() {
  local failures=0
  for entry in "${REQUIRED_DIRECT_DEPS[@]}"; do
    if ! _check_dep "${entry}"; then
      failures=$((failures + 1))
    fi
  done
  if [[ "${failures}" -gt 0 ]]; then
    log::err "${failures} dependency check(s) failed — see above"
    return 1
  fi
  log::ok "all required direct deps present and within specified version ranges"
}

# ── main ───────────────────────────────────────────────────────────────────────

main() {
  while [[ $# -gt 0 ]]; do
    case "$1" in
      --go-mod) GO_MOD="$2"; shift ;;
      -h|--help) _usage; exit 0 ;;
      *) log::err "unknown argument: $1"; _usage >&2; exit 64 ;;
    esac
    shift
  done

  run::step "01" "check tool dependencies" step_check_deps
  run::step "02" "validate go.mod exists" step_validate_gomod_exists
  run::step "03" "check required direct deps" step_check_required_deps
  log::ok "check-dep-versions complete"
}

main "$@"
