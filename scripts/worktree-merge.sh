#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright (c) {{YEAR}} {{ORG_NAME}}
#
# Merge a completed agent worktree back into main.
# Usage:
#   scripts/worktree-merge.sh <branch> [--squash] [--keep-worktree] [--no-verify-green]

set -euo pipefail
IFS=$'\n\t'

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=scripts/lib/paths.sh
source "${HERE}/lib/paths.sh"
# shellcheck source=scripts/lib/log.sh
source "${HERE}/lib/log.sh"

if (( $# < 1 )); then
  log::err "usage: $(basename "$0") <branch> [--squash] [--keep-worktree] [--no-verify-green]"
  exit 2
fi

branch="$1"; shift
squash=0
keep_worktree=0
verify_green=1

for arg in "$@"; do
  case "${arg}" in
    --squash)           squash=1 ;;
    --keep-worktree)    keep_worktree=1 ;;
    --no-verify-green)  verify_green=0 ;;
    *) log::err "unknown flag: ${arg}"; exit 2 ;;
  esac
done

cd "${REPO_ROOT}"

# Locate the worktree for the branch.
wt_path="$(git worktree list --porcelain | awk -v br="${branch}" '
  /^worktree / {path=$2}
  /^branch refs\/heads\// {b=substr($0, index($0,"refs/heads/")+11); if (b==br) {print path; exit}}
')"
if [[ -z "${wt_path}" ]]; then
  log::err "no worktree tracked for branch ${branch}"
  exit 1
fi
log::info "worktree: ${wt_path}"

# Refuse to merge a branch that touches protected paths without explicit override.
protected_hits="$(git -C "${wt_path}" diff --name-only "main...${branch}" | \
  grep -E '^(CLAUDE\.md|MEMORY\.md|\.claude/rules/|\.claude/settings\.json)$' || true)"
if [[ -n "${protected_hits}" ]]; then
  log::err "branch ${branch} touches protected paths (edit on main instead):"
  echo "${protected_hits}" | sed 's/^/  /'
  exit 1
fi

# Verify green.
if (( verify_green )); then
  log::info "verifying green in worktree (lint + test)"
  ( cd "${wt_path}" && make lint && make test ) \
    || { log::err "verification failed; aborting merge"; exit 1; }
fi

# Sync origin, rebase, fast-forward or squash.
log::info "fetching origin"
git fetch origin main
git checkout main
git pull --ff-only origin main

if (( squash )); then
  log::info "squash-merging ${branch}"
  git merge --squash "${branch}"
  git diff --cached --quiet && { log::warn "nothing to commit after squash"; exit 0; }

  msg="$(git -C "${wt_path}" log --pretty=%B "main..${branch}" | head -1)"
  [[ -z "${msg}" ]] && msg="chore(${branch}): squash merge"
  git commit -m "${msg}"
else
  log::info "rebasing ${branch} on main"
  git -C "${wt_path}" rebase main || {
    log::err "rebase conflict in worktree; resolve in ${wt_path} and rerun merge"
    exit 1
  }
  log::info "fast-forward merge"
  git merge --ff-only "${branch}"
fi

# Cleanup unless asked to keep.
if (( ! keep_worktree )); then
  log::info "removing worktree ${wt_path}"
  git worktree remove "${wt_path}" --force
  log::info "deleting branch ${branch}"
  git branch -d "${branch}" || git branch -D "${branch}"
fi

log::ok "merged ${branch} at $(git rev-parse --short HEAD)"
