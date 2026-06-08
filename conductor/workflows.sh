#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright (c) 2026 keese-ai
#
# /workflows — one surface over every parallel run: conductor program runs +
# phases (the run registry + .plan-logs/conduct ledgers), chat/Workflow-tool runs
# (~/.claude/projects/<slug>/**/workflows journals), and git worktrees. Read-only
# board + per-run tail; control (pause/resume/kill) for shell-owned runs only
# (conductor + issue) — chat/Workflow runs are harness-managed (use TaskStop).
#
# Usage:
#   conductor/workflows.sh [board]            # default: list everything
#   conductor/workflows.sh tail   <id>        # follow a run's log
#   conductor/workflows.sh kill   <id>        # SIGTERM a program/issue run
#   conductor/workflows.sh pause  <id>        # hold new dispatch (conductor/issue)
#   conductor/workflows.sh resume <id>        # clear the pause flag

set -euo pipefail
IFS=$'\n\t'

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
LIB="${HERE}/lib"
# shellcheck source=conductor/lib/common.sh
source "${LIB}/common.sh"
# shellcheck source=conductor/lib/conductor-utils.sh
source "${LIB}/conductor-utils.sh"
# shellcheck source=conductor/lib/registry.sh
source "${LIB}/registry.sh"

cu::require jq >/dev/null || {
  log::err "jq required"
  exit 1
}

SUB="${1:-board}"
shift || true

# wf::live <pid> — succeeds iff the pid is a live process.
wf::live() {
  local pid="${1:-0}"
  [[ -n "${pid}" && "${pid}" != "0" && "${pid}" != "null" ]] && kill -0 "${pid}" 2>/dev/null
}

# wf::run <id> — the registry run object for <id> (empty if none).
wf::run() {
  registry::current | jq -c --arg id "$1" 'map(select(.id == $id)) | first // empty'
}

# wf::claude_project_dir — the ~/.claude transcript dir for this repo (best-effort).
wf::claude_project_dir() {
  printf '%s/.claude/projects/%s\n' "${HOME}" "$(printf '%s' "${REPO_ROOT}" | sed 's#[/.]#-#g')"
}

wf::board() {
  echo "== program / issue runs (registry) =="
  local rows count
  rows="$(registry::current)"
  count="$(jq 'length' <<<"${rows}" 2>/dev/null || echo 0)"
  if ((count > 0)); then
    printf '  %-9s %-22s %-14s %-5s %-8s %s\n' KIND ID STATUS LIVE PID UPDATED
    local kind id status pid updated live
    while IFS=$'\t' read -r kind id status pid updated; do
      if wf::live "${pid}"; then live="live"; else live="dead"; fi
      printf '  %-9s %-22s %-14s %-5s %-8s %s\n' "${kind}" "${id}" "${status}" "${live}" "${pid}" "${updated}"
    done < <(jq -r '.[] | [.kind, .id, .status, (.pid | tostring), (.updated_at // "")] | @tsv' <<<"${rows}")
  else
    echo "  (none)"
  fi

  echo
  echo "== git worktrees (agent branches) =="
  local wl
  wl="$(git -C "${REPO_ROOT}" worktree list 2>/dev/null | grep -E 'agent/|worktree-agent-' || true)"
  if [[ -n "${wl}" ]]; then printf '%s\n' "${wl}" | sed 's/^/  /'; else echo "  (none)"; fi

  echo
  echo "== workflow runs (chat / Workflow tool, newest) =="
  local proj
  proj="$(wf::claude_project_dir)"
  if [[ -d "${proj}" ]]; then
    local f
    while IFS= read -r f; do
      [[ -n "${f}" ]] || continue
      jq -r 'select(.workflowName) | "  " + (.status // "?") + "  " + .workflowName + "  (" + (.runId // "?") + ")"' "${f}" 2>/dev/null || true
    done < <(find "${proj}" -path '*/workflows/wf_*.json' -type f 2>/dev/null | tail -12)
    local live_j
    live_j="$(find "${proj}" -path '*/workflows/*/journal.jsonl' -type f 2>/dev/null | wc -l | tr -d ' ')"
    echo "  live workflow journals: ${live_j}"
  else
    echo "  (no ~/.claude project dir)"
  fi
}

wf::tail() {
  local id="${1:-}" run dir f
  [[ -n "${id}" ]] || {
    log::err "usage: workflows.sh tail <id>"
    exit 2
  }
  run="$(wf::run "${id}")"
  [[ -n "${run}" ]] || {
    log::err "no run '${id}' in the registry (see: workflows.sh board)"
    exit 1
  }
  dir="$(jq -r '.dir // empty' <<<"${run}")"
  [[ -d "${dir}" ]] || {
    log::err "run dir not found: ${dir}"
    exit 1
  }
  f="$(find "${dir}" \( -name session.log -o -name stream.jsonl \) -type f 2>/dev/null | head -1)"
  [[ -n "${f}" ]] || {
    log::err "no session.log / stream.jsonl under ${dir}"
    exit 1
  }
  log::info "tail -f ${f}  (Ctrl-C to stop)"
  tail -f "${f}"
}

wf::kill() {
  local id="${1:-}" run pid
  [[ -n "${id}" ]] || {
    log::err "usage: workflows.sh kill <id>"
    exit 2
  }
  run="$(wf::run "${id}")"
  [[ -n "${run}" ]] || {
    log::err "no run '${id}' in the registry"
    exit 1
  }
  pid="$(jq -r '.pid // 0' <<<"${run}")"
  if ! wf::live "${pid}"; then
    log::warn "run '${id}' has no live pid (${pid}) — nothing to kill"
    exit 0
  fi
  kill -TERM "${pid}" && log::ok "sent SIGTERM to ${id} (pid ${pid})"
  registry::status "${id}" killed "via /workflows"
}

# wf::_pause_resume <id> <on|off>
wf::_pause_resume() {
  local id="$1" mode="$2" run kind dir
  [[ -n "${id}" ]] || {
    log::err "usage: workflows.sh ${mode} <id>"
    exit 2
  }
  run="$(wf::run "${id}")"
  [[ -n "${run}" ]] || {
    log::err "no run '${id}' in the registry"
    exit 1
  }
  kind="$(jq -r '.kind // ""' <<<"${run}")"
  dir="$(jq -r '.dir // empty' <<<"${run}")"
  [[ "${kind}" == "conductor" || "${kind}" == "issue" ]] || {
    log::err "pause/resume only applies to conductor/issue runs (got '${kind}'); chat/Workflow runs are harness-managed (use TaskStop)"
    exit 2
  }
  [[ -d "${dir}" ]] || {
    log::err "run dir not found: ${dir}"
    exit 1
  }
  if [[ "${mode}" == "on" ]]; then
    touch "${dir}/PAUSED"
    registry::status "${id}" paused "via /workflows"
    log::ok "paused ${id} — conductor holds new dispatch each wave (running phases continue)"
  else
    rm -f "${dir}/PAUSED"
    registry::status "${id}" running "resumed via /workflows"
    log::ok "resumed ${id}"
  fi
}

case "${SUB}" in
  board | list) wf::board ;;
  tail) wf::tail "${1:-}" ;;
  kill) wf::kill "${1:-}" ;;
  pause) wf::_pause_resume "${1:-}" on ;;
  resume) wf::_pause_resume "${1:-}" off ;;
  *)
    log::err "usage: workflows.sh [board|tail <id>|kill <id>|pause <id>|resume <id>]"
    exit 2
    ;;
esac
