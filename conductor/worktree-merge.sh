#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright (c) 2026 keese-ai
#
# Merge a completed agent worktree back into main.
# Usage:
#   conductor/worktree-merge.sh <branch> [--squash] [--keep-worktree] [--no-verify-green]

set -euo pipefail
IFS=$'\n\t'

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=conductor/lib/common.sh
source "${HERE}/lib/common.sh"

if (($# < 1)); then
  log::err "usage: $(basename "$0") <branch> [--squash] [--keep-worktree] [--no-verify-green]"
  exit 2
fi

branch="$1"
shift
squash=0
keep_worktree=0
verify_green=1
skip_fetch=0

for arg in "$@"; do
  case "${arg}" in
    --squash) squash=1 ;;
    --keep-worktree) keep_worktree=1 ;;
    --no-verify-green) verify_green=0 ;;
    --skip-fetch) skip_fetch=1 ;;
    *)
      log::err "unknown flag: ${arg}"
      exit 2
      ;;
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

# Refuse to merge a branch that touches access-granting or policy-
# enforcing paths. These files control what dispatched agents are
# allowed to do; a compromised or confused agent editing them would
# undermine the entire sandbox. Any such change must be authored on
# main by the orchestrator, not landed via a worktree merge.
protected_patterns=(
  # Top-level docs that drive orchestrator behaviour.
  '^CLAUDE\.md$'
  '^MEMORY\.md$'
  # Rules + config read by every Claude session.
  '^\.claude/rules/'
  '^\.claude/settings\.json$'
  '^\.claude/settings\.local\.json$'
  # Hooks run on every tool call (block-secret-read, header enforcement, commitlint).
  '^\.claude/hooks/'
  # Agent definitions + skills the orchestrator dispatches.
  '^\.claude/agents/'
  '^\.claude/commands/'
  '^\.claude/skills/'
  # The conductor dispatch system: orchestrator, libs, hooks, config, and the
  # worktree create/merge/refresh gates themselves. Authored on main only.
  '^conductor/'
  # Shared shell libraries used across the repo.
  '^scripts/lib/'
  # Policy-enforcing CI + pre-commit config.
  '^\.pre-commit-config\.yaml$'
  '^\.gitignore$'
  # CI workflows + ownership (keese keeps CODEOWNERS at the repo root).
  '^\.github/'
  '^CODEOWNERS$'
  # The pinned Nix dev environment is supply-chain sensitive (rule 02/05).
  '^flake\.nix$'
  '^flake\.lock$'
)
protected_re="$(
  IFS='|'
  echo "${protected_patterns[*]}"
)"

protected_hits="$(git -C "${wt_path}" diff --name-only "main...${branch}" \
  | grep -E "${protected_re}" || true)"
if [[ -n "${protected_hits}" ]]; then
  log::err "branch ${branch} touches protected paths (edit on main instead):"
  # shellcheck disable=SC2001  # sed indent is clearer than ${var//search/replace}
  echo "${protected_hits}" | sed 's/^/  /'
  log::err "if these changes are legitimate, cherry-pick them onto main"
  log::err "manually after review, then retry the merge without them."
  exit 1
fi

# Sync local main to current origin first so the gate runs against the tree the
# branch will actually merge into (main is a moving target under the conductor +
# any concurrent human session).
if ((skip_fetch)); then
  log::info "skipping fetch (--skip-fetch)"
else
  log::info "fetching origin"
  git fetch origin main
fi
git checkout main
git pull --ff-only origin main 2>/dev/null || log::info "no origin/main to pull (local-only)"

# The green gate runs AFTER rebase/squash, against the merged result — not the
# stale pre-rebase tree. A branch that passed in isolation can still break once
# rebased onto an advanced main.
run_gate() { # run_gate <dir>
  ((verify_green)) || {
    log::warn "green gate skipped (--no-verify-green)"
    return 0
  }
  # keese green gate: `make lint` runs pre-commit --all-files (header, conventional
  # commits, controller-gen freshness, kubeconform, rebac markers, bundle-validate,
  # …) and `make test` runs the unit + envtest integration suites. No separate
  # plan-status checker — the plan status table lives in docs/plans/README.md and
  # is reconciled by the conductor on merge.
  log::info "green gate (make lint && make test) in $1"
  (cd "$1" && make lint && make test)
}

if ((squash)); then
  log::info "squash-merging ${branch}"
  git merge --squash "${branch}"
  git diff --cached --quiet && {
    log::warn "nothing to commit after squash"
    exit 0
  }
  # Gate the staged merge result on main before committing.
  run_gate "${REPO_ROOT}" || {
    log::err "green gate failed post-squash; reverting staged merge"
    git reset --hard HEAD
    exit 1
  }
  msg="$(git -C "${wt_path}" log --pretty=%B "main..${branch}" | head -1)"
  [[ -z "${msg}" ]] && msg="chore(${branch}): squash merge"
  git commit -m "${msg}"
else
  log::info "rebasing ${branch} on main"
  git -C "${wt_path}" rebase main || {
    git -C "${wt_path}" rebase --abort 2>/dev/null || true
    log::err "rebase conflict in worktree ${wt_path}; resolve and rerun merge"
    exit 1
  }
  # Gate the rebased branch before fast-forwarding main onto it.
  run_gate "${wt_path}" || {
    log::err "green gate failed post-rebase; aborting merge (no change to main)"
    exit 1
  }
  log::info "fast-forward merge"
  git merge --ff-only "${branch}"
fi

# Cleanup unless asked to keep.
if ((!keep_worktree)); then
  log::info "removing worktree ${wt_path}"
  # Agent harness locks the worktree for its lifetime; the lock file may
  # remain after the agent exits. Unlock-then-remove is idempotent.
  git worktree unlock "${wt_path}" 2>/dev/null || true
  git worktree remove "${wt_path}" --force
  log::info "deleting branch ${branch}"
  git branch -d "${branch}" || git branch -D "${branch}"
fi

log::ok "merged ${branch} at $(git rev-parse --short HEAD)"
