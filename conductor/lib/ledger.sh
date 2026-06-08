# SPDX-License-Identifier: Apache-2.0
# Copyright (c) 2026 keese-ai
#
# Conductor run ledger: the sole cross-restart truth. JSON at
# ${LEDGER} (.plan-logs/conduct/<run-id>/ledger.json). Every mutation is atomic
# (jq to a temp file, then rename). Only the conductor process writes it.
# Source me; do not execute.
# shellcheck shell=bash

if [[ -n "${__LIB_LEDGER_SH_LOADED:-}" ]]; then
  return 0
fi
__LIB_LEDGER_SH_LOADED=1

# Requires conductor-utils.sh (cu::atomic_write, cu::now_iso) and jq.
HERE_LEDGER="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=conductor/lib/conductor-utils.sh
source "${HERE_LEDGER}/conductor-utils.sh"

# ledger::init <ledger-path> <run-id> <config-json> — create a fresh ledger.
ledger::init() {
  local path="$1" run_id="$2" config="$3" now
  now="$(cu::now_iso)"
  LEDGER="${path}"
  mkdir -p "$(dirname "${path}")"
  jq -n \
    --arg run_id "${run_id}" \
    --arg now "${now}" \
    --argjson config "${config:-{\}}" \
    '{run_id:$run_id, started_at:$now, updated_at:$now, status:"running", wave:0, config:$config, slots:{}}' \
    | cu::atomic_write "${path}"
  export LEDGER
}

# ledger::get <jq-filter> [jq-args...] — read a value (raw).
ledger::get() {
  local filter="$1"
  shift
  jq -r "$@" "${filter}" "${LEDGER:?ledger not initialised}"
}

# ledger::patch <jq-expr> [jq-args...] — atomically apply a jq transform to the
# whole ledger object. updated_at is refreshed automatically.
ledger::patch() {
  local expr="$1"
  shift
  local now out
  now="$(cu::now_iso)"
  out="$(jq --arg __now "${now}" "$@" "(.updated_at=\$__now) | (${expr})" "${LEDGER:?ledger not initialised}")" || return 1
  printf '%s\n' "${out}" | cu::atomic_write "${LEDGER}"
}

# ledger::set <jq-path-expr> <value> — set a top-level field (string).
ledger::set() {
  ledger::patch "$1 = \$__v" --arg __v "$2"
}

# ledger::slot_init <phase-id> <slot-json> — create/replace a slot.
ledger::slot_init() {
  local phase="$1" slot="$2"
  ledger::patch ".slots[\$__p] = \$__slot" --arg __p "${phase}" --argjson __slot "${slot}"
}

# ledger::slot_set <phase-id> <key> <value> — set one slot field (string).
ledger::slot_set() {
  ledger::patch ".slots[\$__p][\$__k] = \$__v" \
    --arg __p "$1" --arg __k "$2" --arg __v "$3"
}

# ledger::slot_set_json <phase-id> <key> <json-value> — set one slot field (json).
ledger::slot_set_json() {
  ledger::patch ".slots[\$__p][\$__k] = \$__v" \
    --arg __p "$1" --arg __k "$2" --argjson __v "$3"
}

# ledger::slot_status <phase-id> <status> — convenience for the status field
# plus an updated_at stamp on the slot.
ledger::slot_status() {
  local now
  now="$(cu::now_iso)"
  ledger::patch ".slots[\$__p].status = \$__s | .slots[\$__p].updated_at = \$__now" \
    --arg __p "$1" --arg __s "$2" --arg __now "${now}"
}

# ledger::slot_get <phase-id> <key> — read one slot field (raw, empty if absent).
ledger::slot_get() {
  ledger::get ".slots[\"$1\"][\"$2\"] // \"\""
}

# ledger::slots_by_status <status> — list phase ids in a status (newline-sep).
ledger::slots_by_status() {
  ledger::get ".slots | to_entries[] | select(.value.status==\"$1\") | .key"
}

# ledger::active_slots — phase ids that are not in a terminal/idle state
# (includes merge-pending, which is "done, awaiting the merge gate").
# shellcheck disable=SC2016  # jq program; $s is a jq variable, not a shell one.
ledger::active_slots() {
  ledger::get '.slots | to_entries[] | select(.value.status as $s | ["dispatching","running","stuck","merge-pending"] | index($s)) | .key'
}

# ledger::inprogress_slots — phase ids whose agent is still working
# (dispatching/running/stuck). Excludes merge-pending so the poll loop can exit
# to the completion gate once no agent is still running.
# shellcheck disable=SC2016  # jq program; $s is a jq variable, not a shell one.
ledger::inprogress_slots() {
  ledger::get '.slots | to_entries[] | select(.value.status as $s | ["dispatching","running","stuck"] | index($s)) | .key'
}

# ledger::all_slots — every phase id with a slot.
ledger::all_slots() {
  ledger::get '.slots | keys[]'
}

# ledger::count_status <status> — how many slots are in a status.
ledger::count_status() {
  ledger::get "[.slots[] | select(.status==\"$1\")] | length"
}
