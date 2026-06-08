#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright (c) 2026 keese-ai
#
# Periodically refresh a still-running agent worktree onto an advanced main.
# main moves under long phases (autonomous merges + a concurrent human session),
# so a worktree can drift far from the tree it will eventually merge into.
#
# Refresh is ADVISED when main has advanced >= threshold commits AND the new
# main-side changes overlap this branch's predicted footprint (dependency /
# domain heuristic). It is only PERFORMED on a CLEAN worktree — never rebase a
# dirty in-flight edit — and aborts + escalates on conflict (never auto-resolve).
#
# Usage:
#   conductor/worktree-refresh.sh --branch B --worktree WT [--base main]
#       [--threshold N] [--check-only] [--force]
# Emits a JSON decision on stdout: {action, behind, overlap, branch}.

set -euo pipefail
IFS=$'\n\t'

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
LIB="${HERE}/lib"
# shellcheck source=conductor/lib/common.sh
source "${LIB}/common.sh"
# shellcheck source=conductor/lib/conductor-utils.sh
source "${LIB}/conductor-utils.sh"
# shellcheck source=conductor/lib/footprint.sh
source "${LIB}/footprint.sh"

BRANCH=""
WT=""
BASE="main"
THRESHOLD="${CONDUCT_REFRESH_THRESHOLD:-5}"
CHECK_ONLY=0
FORCE=0

while (($# > 0)); do
  case "$1" in
    --branch)
      BRANCH="$2"
      shift 2
      ;;
    --worktree)
      WT="$2"
      shift 2
      ;;
    --base)
      BASE="$2"
      shift 2
      ;;
    --threshold)
      THRESHOLD="$2"
      shift 2
      ;;
    --check-only)
      CHECK_ONLY=1
      shift
      ;;
    --force)
      FORCE=1
      shift
      ;;
    *)
      log::err "unknown flag: $1"
      exit 2
      ;;
  esac
done

emit() { # emit <action> <behind> <overlap>
  jq -nc --arg a "$1" --argjson b "${2:-0}" --argjson o "${3:-false}" --arg br "${BRANCH}" \
    '{action:$a, behind:$b, overlap:$o, branch:$br}'
}

[[ -n "${BRANCH}" && -n "${WT}" && -d "${WT}" ]] || {
  emit error 0 false
  exit 0
}

# How far has base advanced beyond this branch?
behind="$(git -C "${WT}" rev-list --count "${BRANCH}..${BASE}" 2>/dev/null || echo 0)"
behind="${behind:-0}"

if ((behind < THRESHOLD)) && ((!FORCE)); then
  emit none "${behind}" false
  exit 0
fi

# Footprint overlap: this branch's changes vs the base-side changes since the
# merge-base. If they touch the same domains/hot paths, drift matters.
branch_fp="$(git -C "${WT}" diff --name-only "${BASE}...${BRANCH}" 2>/dev/null | footprint::for_diff)"
base_fp="$(git -C "${WT}" diff --name-only "${BRANCH}...${BASE}" 2>/dev/null | footprint::for_diff)"
overlap=false
if footprint::conflicts "${branch_fp}" "${base_fp}"; then
  overlap=true
fi

if [[ "${overlap}" != "true" ]] && ((!FORCE)); then
  emit none "${behind}" false
  exit 0
fi

# Advised. In check-only mode, stop here.
if ((CHECK_ONLY)); then
  emit advised "${behind}" "${overlap}"
  exit 0
fi

# Safety interlock: never rebase a dirty worktree (an agent may be mid-edit).
if [[ -n "$(git -C "${WT}" status --porcelain 2>/dev/null)" ]]; then
  log::warn "refresh ${BRANCH}: worktree dirty — deferring rebase" >&2
  emit deferred-dirty "${behind}" "${overlap}"
  exit 0
fi

# Perform the rebase; abort + escalate on conflict.
log::info "refresh ${BRANCH}: rebasing onto ${BASE} (behind ${behind})" >&2
if git -C "${WT}" rebase "${BASE}" >/dev/null 2>&1; then
  log::ok "refresh ${BRANCH}: rebased clean" >&2
  emit refreshed "${behind}" "${overlap}"
else
  git -C "${WT}" rebase --abort >/dev/null 2>&1 || true
  {
    echo "# Refresh conflict — ${BRANCH}"
    echo
    echo "Rebase of ${BRANCH} onto ${BASE} hit conflicts and was aborted."
    echo "Resolve manually in ${WT}, or let the merge gate handle it at completion."
    echo
    echo "## git status"
    git -C "${WT}" status 2>&1 | sed 's/^/    /'
  } >"${WT}/.plan-logs/CONFLICT.md" 2>/dev/null || true
  log::err "refresh ${BRANCH}: rebase conflict — aborted, CONFLICT.md written" >&2
  emit conflict "${behind}" "${overlap}"
fi
