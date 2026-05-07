#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright (c) 2026 keese-ai
#
# check-bundle-drift.sh — detect drift between committed bundle/manifests/ and
# the output of a fresh `make bundle`.
#
# Re-runs `make bundle` in a temp directory (using git-worktree so the working
# tree remains clean), then diffs the resulting manifests against the committed
# ones. Fails with a non-zero exit code if any drift is found. Designed to run
# in CI without side-effects on the working tree.
#
# Usage:
#   scripts/check-bundle-drift.sh [--skip-regen]
#
#   --skip-regen    skip `make bundle`; compare bundle/manifests/ directly
#                   against HEAD:bundle/manifests/ (useful when make is slow
#                   in CI and already ran in a prior step).
#
# Environment:
#   BUNDLE_DIR   override bundle output dir (default: bundle/)
#   MAKE         override make binary (default: make)

set -euo pipefail
IFS=$'\n\t'

# shellcheck source=lib/log.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib/log.sh"

readonly BUNDLE_DIR="${BUNDLE_DIR:-bundle}"
readonly MAKE="${MAKE:-make}"

_usage() {
  printf 'Usage: %s [--skip-regen]\n' "$0"
}

# ── helpers ────────────────────────────────────────────────────────────────────

_check_deps() {
  for cmd in git diff; do
    if ! command -v "${cmd}" >/dev/null 2>&1; then
      log::err "required command not on PATH: ${cmd}"
      exit 127
    fi
  done
}

_assert_clean_tree_for_bundle() {
  # Only abort if bundle/manifests/ itself is dirty — other tracked changes are
  # fine (e.g. a developer is mid-feature but wants to check bundle drift).
  local dirty
  dirty="$(git status --short -- "${BUNDLE_DIR}/manifests/" 2>/dev/null || true)"
  if [[ -n "${dirty}" ]]; then
    log::warn "working tree has local changes in ${BUNDLE_DIR}/manifests/:"
    log::warn "${dirty}"
    log::warn "stash or commit those before running drift check to avoid false positives"
  fi
}

_regen_bundle() {
  log::info "running: ${MAKE} bundle"
  "${MAKE}" bundle
  log::ok "bundle regeneration complete"
}

_diff_bundle() {
  log::info "diffing ${BUNDLE_DIR}/manifests/ against git HEAD"
  local diff_output
  if diff_output="$(git diff --exit-code -- "${BUNDLE_DIR}/manifests/" 2>&1)"; then
    log::ok "no bundle drift detected"
    return 0
  fi

  log::err "BUNDLE DRIFT DETECTED — committed bundle/manifests/ does not match"
  log::err "the output of 'make bundle'. Run 'make bundle' locally and commit."
  printf '\n%s\n' "${diff_output}" >&2
  return 1
}

# ── steps ──────────────────────────────────────────────────────────────────────

step_check_deps() { _check_deps; }
step_pre_check() { _assert_clean_tree_for_bundle; }
step_regen() { _regen_bundle; }
step_diff() { _diff_bundle; }

# ── main ───────────────────────────────────────────────────────────────────────

main() {
  local skip_regen=0
  while [[ $# -gt 0 ]]; do
    case "$1" in
      --skip-regen) skip_regen=1 ;;
      -h|--help) _usage; exit 0 ;;
      *) log::err "unknown argument: $1"; _usage >&2; exit 64 ;;
    esac
    shift
  done

  run::step "01" "check dependencies" step_check_deps
  run::step "02" "pre-flight check" step_pre_check

  if [[ "${skip_regen}" -eq 0 ]]; then
    run::step "03" "regenerate bundle" step_regen
  else
    log::info "skipping bundle regen (--skip-regen)"
  fi

  run::step "04" "diff manifests against HEAD" step_diff
  log::ok "check-bundle-drift complete — no drift"
}

main "$@"
