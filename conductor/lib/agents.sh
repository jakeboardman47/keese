# SPDX-License-Identifier: Apache-2.0
# Copyright (c) 2026 keese-ai
#
# Single adapter over .claude/agents/*.md so BOTH dispatch modes resolve the same
# persona / model / effort / tools from one source: chat /conduct (Agent tool
# subagent_type, honored via CLAUDE_CODE_SUBAGENT_MODEL=inherit) and program
# conductor.sh (`claude -p --agent <name>`). Replaces the hardcoded
# tier->model->effort map (model-effort.sh) and the inline persona heredoc.
# Source me; do not execute. See docs/designs/29-conductor-orchestration.md.
# shellcheck shell=bash

if [[ -n "${__LIB_AGENTS_SH_LOADED:-}" ]]; then
  return 0
fi
__LIB_AGENTS_SH_LOADED=1

# Usable standalone or sourced after common.sh (which also sets REPO_ROOT).
: "${REPO_ROOT:=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)}"

AGENTS_DIR="${REPO_ROOT}/.claude/agents"
AGENTS_FM_PARSER="${REPO_ROOT}/conductor/lib/frontmatter.py"
AGENTS_STAGES_FILE="${REPO_ROOT}/conductor/config/stages.json"

# agents::file <name> — absolute path to the agent definition.
agents::file() {
  printf '%s/%s.md\n' "${AGENTS_DIR}" "$1"
}

# agents::exists <name> — succeeds iff the agent definition file exists.
agents::exists() {
  [[ -f "$(agents::file "$1")" ]]
}

# agents::_fm <name> — emit the agent frontmatter as JSON ({} on error).
agents::_fm() {
  python3 "${AGENTS_FM_PARSER}" "$(agents::file "$1")" 2>/dev/null || echo '{}'
}

# agents::resolve <name> — echo "model<TAB>effort" from the agent frontmatter.
# Mirrors what `claude -p --agent <name>` applies; haiku / no-effort agents emit
# an empty effort field. Falls back to sonnet when the file declares no model.
agents::resolve() {
  local fm model effort
  fm="$(agents::_fm "$1")"
  model="$(jq -r '.model // "sonnet"' <<<"${fm}")"
  effort="$(jq -r '.effort // ""' <<<"${fm}")"
  printf '%s\t%s\n' "${model}" "${effort}"
}

# agents::system_prompt <name> — emit the agent's Markdown body (the persona /
# system prompt: everything after the closing frontmatter fence). The CLI loads
# this itself under `--agent`; exposed here for tests and non-CLI callers.
agents::system_prompt() {
  awk 'BEGIN{f=0} /^---[[:space:]]*$/{f++; next} f>=2{print}' "$(agents::file "$1")"
}

# agents::for_stage <stage> — agent name mapped to a pipeline stage (stages.json).
agents::for_stage() {
  jq -r --arg s "$1" '.stages[$s] // empty' "${AGENTS_STAGES_FILE}" 2>/dev/null || true
}

# agents::for_tier <tier> — agent name mapped to a model tier (stages.json). Used
# by the conductor to pick the dispatch persona from a phase's model_tier.
agents::for_tier() {
  jq -r --arg t "$1" '.tiers[$t] // empty' "${AGENTS_STAGES_FILE}" 2>/dev/null || true
}

# agents::for_phase <agent-field> <stage-field> <tier-field> — resolve the single
# dispatch persona for a phase with precedence agent: > stage: > model_tier.
# keese routes phases to SPECIALIZED personas (a new-CRD phase to crd-author, a
# reconciler phase to controller-author, the OLM bundle to olm-author, the
# OpenFGA model to rebac-modeler, …) via the phase doc's `agent:` frontmatter;
# falls back to the stage map, then the tier map, then `implementer`. An unknown
# explicit agent: is ignored (falls through) rather than dispatched blindly.
agents::for_phase() {
  local agent="${1:-}" stage="${2:-}" tier="${3:-}" out=""
  if [[ -n "${agent}" ]] && agents::exists "${agent}"; then
    out="${agent}"
  fi
  [[ -z "${out}" && -n "${stage}" ]] && out="$(agents::for_stage "${stage}")"
  [[ -z "${out}" && -n "${tier}" ]] && out="$(agents::for_tier "${tier}")"
  printf '%s\n' "${out:-implementer}"
}
