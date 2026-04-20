#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright (c) 2026 keese-ai
#
# Dispatch a Claude subagent into an isolated git worktree.
# Usage:
#   scripts/agent-dispatch.sh <phase-id> <agent-name> [--branch=<name>] [--base=<ref>]
#
# Examples:
#   scripts/agent-dispatch.sh phase-04 implementer
#   scripts/agent-dispatch.sh phase-08 architect --branch=agent/phase-08-redesign

set -euo pipefail
IFS=$'\n\t'

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=scripts/lib/paths.sh
source "${HERE}/lib/paths.sh"
# shellcheck source=scripts/lib/log.sh
source "${HERE}/lib/log.sh"

if (( $# < 2 )); then
  log::err "usage: $(basename "$0") <phase-id> <agent-name> [--branch=<name>] [--base=<ref>]"
  exit 2
fi

phase_id="$1"; shift
agent_name="$1"; shift

branch=""
base="main"
for arg in "$@"; do
  case "${arg}" in
    --branch=*) branch="${arg#--branch=}" ;;
    --base=*)   base="${arg#--base=}" ;;
    *) log::err "unknown flag: ${arg}"; exit 2 ;;
  esac
done

slug="$(echo "${phase_id}" | tr '[:upper:]/' '[:lower:]-')"
if [[ -z "${branch}" ]]; then
  branch="agent/${slug}-${agent_name}"
fi

wt_base="$(paths::worktree_base)"
wt_path="${wt_base}/${slug}-${agent_name}"

# Preconditions
git -C "${REPO_ROOT}" rev-parse --is-inside-work-tree >/dev/null 2>&1 || {
  log::err "not inside a git repo"; exit 1
}

# Validate agent name against known agents.
agent_file="${REPO_ROOT}/.claude/agents/${agent_name}.md"
if [[ ! -f "${agent_file}" ]]; then
  log::err "unknown agent ${agent_name} (no ${agent_file})"
  exit 1
fi

# Validate phase file exists for phase-* inputs.
if [[ "${phase_id}" == phase-* ]]; then
  phase_file="${REPO_ROOT}/docs/plans/${phase_id}.md"
  [[ -f "${phase_file}" ]] || log::warn "phase file ${phase_file} not found — agent will still run"
fi

# Ensure worktree base exists.
mkdir -p "${wt_base}"

# If branch already exists, attach; else create from base.
if git -C "${REPO_ROOT}" show-ref --verify --quiet "refs/heads/${branch}"; then
  log::info "attaching existing branch ${branch} at ${wt_path}"
  git -C "${REPO_ROOT}" worktree add "${wt_path}" "${branch}"
else
  log::info "creating branch ${branch} from ${base} at ${wt_path}"
  git -C "${REPO_ROOT}" worktree add -b "${branch}" "${wt_path}" "${base}"
fi

# Seed the worktree's .plan-logs/ with a prompt file for the agent.
mkdir -p "${wt_path}/.plan-logs"
cat > "${wt_path}/.plan-logs/prompt.md" <<EOF
<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

# Agent dispatch prompt

- Phase: \`${phase_id}\`
- Agent: \`${agent_name}\` (see .claude/agents/${agent_name}.md)
- Branch: \`${branch}\`
- Base: \`${base}\`
- Worktree: \`${wt_path}\`

## Start here

1. Read \`docs/plans/${phase_id}.md\` (if it exists) and follow the plan.
2. Obey all \`.claude/rules/*\`.
3. Commit using Conventional Commits; the hook will reject otherwise.
4. When complete, write \`\${PLAN_LOGS}/SUMMARY.md\` with:
   - what landed
   - what was deferred
   - any new entries for MEMORY.md (these are applied on merge back to main).
5. Exit. The parent will invoke \`scripts/worktree-merge.sh ${branch}\`.
EOF

log::ok "worktree ready: ${wt_path} (branch ${branch})"
echo "${wt_path}"
