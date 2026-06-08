# SPDX-License-Identifier: Apache-2.0
# Copyright (c) 2026 keese-ai
#
# Append-only run registry (.plan-logs/registry.jsonl). Every shell dispatch path
# records a line so /workflows (conductor/workflows.sh) can list all parallel runs
# across modes from one index. Crash-safe: append-only, never rewritten; readers
# fold by id (last status wins). Lines are small (<PIPE_BUF) so concurrent appends
# from parallel phases stay atomic. Source me; do not execute.
# shellcheck shell=bash

if [[ -n "${__LIB_REGISTRY_SH_LOADED:-}" ]]; then
  return 0
fi
__LIB_REGISTRY_SH_LOADED=1

__rg_here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=conductor/lib/common.sh
source "${__rg_here}/common.sh"
# shellcheck source=conductor/lib/conductor-utils.sh
source "${__rg_here}/conductor-utils.sh"

REGISTRY_FILE="${CONDUCT_REGISTRY:-${PLAN_LOGS}/registry.jsonl}"

# registry::emit <json-object> — append one line atomically.
registry::emit() {
  mkdir -p "$(dirname "${REGISTRY_FILE}")"
  printf '%s\n' "$1" >>"${REGISTRY_FILE}"
}

# registry::record <kind> <id> <label> <pid> <dir> <status_path> — a run started.
registry::record() {
  registry::emit "$(jq -nc \
    --arg ts "$(cu::now_iso)" --arg kind "$1" --arg id "$2" --arg label "${3:-}" \
    --argjson pid "${4:-0}" --arg dir "${5:-}" --arg sp "${6:-}" \
    '{ts:$ts, event:"start", kind:$kind, id:$id, label:$label, pid:$pid,
      dir:$dir, status_path:$sp, status:"running"}')"
}

# registry::status <id> <status> [note] — a run changed state.
registry::status() {
  registry::emit "$(jq -nc \
    --arg ts "$(cu::now_iso)" --arg id "$1" --arg status "$2" --arg note "${3:-}" \
    '{ts:$ts, event:"status", id:$id, status:$status, note:$note}')"
}

# registry::current — fold the log into the latest state per id; print a JSON
# array. Each row = the last "start" record merged with the last status seen.
registry::current() {
  [[ -f "${REGISTRY_FILE}" ]] || {
    echo '[]'
    return
  }
  jq -s '
    map(select(.id != null))
    | group_by(.id)
    | map(
        ((map(select(.event == "start")) | last) // {id: .[0].id}) as $start
        | $start + {
            status: (last.status // $start.status // "unknown"),
            updated_at: (last.ts)
          }
      )
  ' "${REGISTRY_FILE}" 2>/dev/null || echo '[]'
}
