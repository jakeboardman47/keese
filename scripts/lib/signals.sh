# SPDX-License-Identifier: Apache-2.0
# Copyright (c) 2026 keese-ai
#
# Two-stage signal supervisor. Source me; do not execute.
# shellcheck shell=bash

[[ -n "${__LIB_SIGNALS_SH_LOADED:-}" ]] && return 0
__LIB_SIGNALS_SH_LOADED=1

: "${SIGNALS_GRACE_SECONDS:=15}"
__signals_stage=0
__signals_child_pid=

__signals_on_signal() {
  if (( __signals_stage == 0 )); then
    __signals_stage=1
    log::warn "graceful shutdown (SIGTERM -> pid ${__signals_child_pid})"
    kill -TERM "${__signals_child_pid}" 2>/dev/null || true
    ( sleep "${SIGNALS_GRACE_SECONDS}"
      kill -KILL "${__signals_child_pid}" 2>/dev/null || true ) &
  else
    log::err "force quit (SIGKILL -> pid ${__signals_child_pid})"
    kill -KILL "${__signals_child_pid}" 2>/dev/null || true
    exit 137
  fi
}

# signals::supervise <child_pid>
signals::supervise() {
  __signals_child_pid="$1"
  trap __signals_on_signal INT TERM
  # Loop-wait: a trap interrupts `wait`; we must re-wait after handling.
  while kill -0 "${__signals_child_pid}" 2>/dev/null; do
    wait "${__signals_child_pid}" 2>/dev/null || true
  done
  trap - INT TERM
}
