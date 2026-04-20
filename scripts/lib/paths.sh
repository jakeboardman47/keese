# SPDX-License-Identifier: Apache-2.0
# Copyright (c) {{YEAR}} {{ORG_NAME}}
#
# Path helpers. Source me; do not execute.
# shellcheck shell=bash

if [[ -n "${__LIB_PATHS_SH_LOADED:-}" ]]; then
  return 0
fi
__LIB_PATHS_SH_LOADED=1

# REPO_ROOT: absolute path to repo root, regardless of cwd.
REPO_ROOT="$(git -C "$(dirname "${BASH_SOURCE[0]}")" rev-parse --show-toplevel 2>/dev/null || true)"
if [[ -z "${REPO_ROOT}" ]]; then
  # Fallback when not inside a git repo (e.g. nix build sandbox).
  REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
fi
export REPO_ROOT

PLAN_LOGS="${PLAN_LOGS:-${REPO_ROOT}/.plan-logs}"
mkdir -p "${PLAN_LOGS}"
export PLAN_LOGS

# Convenience helpers.
paths::plan_log() {
  local name="$1"
  echo "${PLAN_LOGS}/${name}"
}

paths::worktree_base() {
  # Default worktree base is a sibling directory to the repo.
  # Override by exporting WORKTREE_BASE=... in your env or .env.local.
  echo "${WORKTREE_BASE:-$(dirname "${REPO_ROOT}")/$(basename "${REPO_ROOT}")-worktrees}"
}
