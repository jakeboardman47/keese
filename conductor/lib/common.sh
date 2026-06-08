# SPDX-License-Identifier: Apache-2.0
# Copyright (c) 2026 keese-ai
#
# Conductor shared basics: repo paths + colorized logging. Self-contained so the
# conductor/ tree has NO dependency on scripts/lib and can be copied to another
# repo wholesale. Source me; do not execute.
# shellcheck shell=bash

if [[ -n "${__CONDUCTOR_COMMON_SH_LOADED:-}" ]]; then
  return 0
fi
__CONDUCTOR_COMMON_SH_LOADED=1

# REPO_ROOT: absolute repo root regardless of cwd.
REPO_ROOT="$(git -C "$(dirname "${BASH_SOURCE[0]}")" rev-parse --show-toplevel 2>/dev/null || true)"
if [[ -z "${REPO_ROOT}" ]]; then
  REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
fi
export REPO_ROOT

PLAN_LOGS="${PLAN_LOGS:-${REPO_ROOT}/.plan-logs}"
mkdir -p "${PLAN_LOGS}"
export PLAN_LOGS

# CONDUCTOR_HOME: the conductor/ root (one level up from this lib dir).
CONDUCTOR_HOME="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
export CONDUCTOR_HOME

paths::plan_log() { echo "${PLAN_LOGS}/$1"; }

paths::worktree_base() {
  # Sibling of the repo, e.g. <parent>/<repo>-worktrees. Override via WORKTREE_BASE.
  echo "${WORKTREE_BASE:-$(dirname "${REPO_ROOT}")/$(basename "${REPO_ROOT}")-worktrees}"
}

# --- logging (stdout for info/ok/dim; stderr for warn/err) -------------------
if [[ -t 1 ]]; then
  __LOG_RED=$'\e[31m' __LOG_YEL=$'\e[33m' __LOG_GRN=$'\e[32m' __LOG_BLU=$'\e[34m' __LOG_DIM=$'\e[2m' __LOG_RST=$'\e[0m'
else
  __LOG_RED="" __LOG_YEL="" __LOG_GRN="" __LOG_BLU="" __LOG_DIM="" __LOG_RST=""
fi

log::info() { printf '%s[info]%s %s\n' "${__LOG_BLU}" "${__LOG_RST}" "$*"; }
log::ok() { printf '%s[ ok ]%s %s\n' "${__LOG_GRN}" "${__LOG_RST}" "$*"; }
log::warn() { printf '%s[warn]%s %s\n' "${__LOG_YEL}" "${__LOG_RST}" "$*" >&2; }
log::err() { printf '%s[err ]%s %s\n' "${__LOG_RED}" "${__LOG_RST}" "$*" >&2; }
log::dim() { printf '%s%s%s\n' "${__LOG_DIM}" "$*" "${__LOG_RST}"; }
