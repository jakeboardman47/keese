# SPDX-License-Identifier: Apache-2.0
# Copyright (c) 2026 keese-ai
#
# Conductor shared utilities: portable date/stat, UUIDs, atomic writes, and a
# flock-free singleton lock. Source me; do not execute.
# shellcheck shell=bash

if [[ -n "${__LIB_CONDUCTOR_UTILS_SH_LOADED:-}" ]]; then
  return 0
fi
__LIB_CONDUCTOR_UTILS_SH_LOADED=1

__CU_OS="$(uname -s)"

# --- time -------------------------------------------------------------------

# cu::now_iso — current UTC time as ISO-8601 (e.g. 2026-06-05T16:53:00Z).
cu::now_iso() { date -u +"%Y-%m-%dT%H:%M:%SZ"; }

# cu::epoch_now — current unix epoch seconds.
cu::epoch_now() { date -u +%s; }

# cu::epoch_from_iso <iso> — convert an ISO-8601 UTC string to epoch seconds.
cu::epoch_from_iso() {
  local iso="$1"
  if [[ "${__CU_OS}" == "Darwin" ]]; then
    date -u -jf "%Y-%m-%dT%H:%M:%SZ" "${iso}" +%s 2>/dev/null
  else
    date -u -d "${iso}" +%s 2>/dev/null
  fi
}

# cu::iso_from_epoch <epoch> — convert epoch seconds to ISO-8601 UTC.
cu::iso_from_epoch() {
  local epoch="$1"
  if [[ "${__CU_OS}" == "Darwin" ]]; then
    date -u -r "${epoch}" +"%Y-%m-%dT%H:%M:%SZ" 2>/dev/null
  else
    date -u -d "@${epoch}" +"%Y-%m-%dT%H:%M:%SZ" 2>/dev/null
  fi
}

# cu::mtime <file> — file modification time in epoch seconds (0 if missing).
cu::mtime() {
  local f="$1"
  [[ -e "${f}" ]] || {
    echo 0
    return 0
  }
  if [[ "${__CU_OS}" == "Darwin" ]]; then
    stat -f %m "${f}" 2>/dev/null || echo 0
  else
    stat -c %Y "${f}" 2>/dev/null || echo 0
  fi
}

# --- identity ---------------------------------------------------------------

# cu::uuid — a lowercase RFC-4122 v4 UUID.
cu::uuid() {
  if command -v python3 >/dev/null 2>&1; then
    python3 -c 'import uuid; print(uuid.uuid4())'
  elif command -v uuidgen >/dev/null 2>&1; then
    uuidgen | tr '[:upper:]' '[:lower:]'
  else
    # Last resort: epoch + pid; not a real UUID but unique enough for a run id.
    printf '%s-%s\n' "$(cu::epoch_now)" "$$"
  fi
}

# cu::is_uuid <s> — true if the argument looks like a UUID.
cu::is_uuid() {
  [[ "$1" =~ ^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$ ]]
}

# --- atomic writes ----------------------------------------------------------

# cu::atomic_write <dest> — read stdin, write atomically (write tmp then rename).
# Rename within the same directory is atomic on POSIX filesystems, so a reader
# never sees a half-written file even while a writer is mid-flight.
cu::atomic_write() {
  local dest="$1" tmp
  tmp="${dest}.tmp.$$"
  cat >"${tmp}" && mv -f "${tmp}" "${dest}"
}

# cu::ensure_json <file> [seed] — create file with seed JSON ({} default) if absent.
cu::ensure_json() {
  local f="$1" seed="${2:-}"
  [[ -n "${seed}" ]] || seed='{}'
  [[ -f "${f}" ]] || {
    mkdir -p "$(dirname "${f}")"
    printf '%s\n' "${seed}" >"${f}"
  }
}

# --- singleton lock (flock is not in the macOS base system) -----------------

# cu::lock_acquire <lockfile> — succeed (0) only if no live holder exists.
# Uses noclobber for an atomic create-if-absent; clears a stale lock whose pid
# is dead. Returns 1 if a live process already holds it.
cu::lock_acquire() {
  local lock="$1" pid
  if [[ -f "${lock}" ]]; then
    pid="$(cat "${lock}" 2>/dev/null || true)"
    if [[ -n "${pid}" ]] && kill -0 "${pid}" 2>/dev/null; then
      return 1
    fi
    rm -f "${lock}"
  fi
  mkdir -p "$(dirname "${lock}")"
  (
    set -C
    echo "$$" >"${lock}"
  ) 2>/dev/null || return 1
  return 0
}

# cu::lock_release <lockfile> — remove the lock only if we own it.
cu::lock_release() {
  local lock="$1"
  [[ -f "${lock}" ]] || return 0
  [[ "$(cat "${lock}" 2>/dev/null)" == "$$" ]] && rm -f "${lock}"
  return 0
}

# --- misc -------------------------------------------------------------------

# cu::require <cmd...> — fail loudly if any required binary is missing.
cu::require() {
  local missing=()
  local c
  for c in "$@"; do
    command -v "${c}" >/dev/null 2>&1 || missing+=("${c}")
  done
  if ((${#missing[@]} > 0)); then
    echo "missing required commands: ${missing[*]}" >&2
    return 1
  fi
  return 0
}

# cu::clamp <value> <lo> <hi> — echo value clamped to [lo,hi] (floats ok via awk).
cu::clamp() {
  awk -v v="$1" -v lo="$2" -v hi="$3" 'BEGIN{if(v<lo)v=lo; if(v>hi)v=hi; print v}'
}

# cu::backoff_secs <attempt> [base=5] [cap=120] — exponential backoff with up to
# 50% jitter, in seconds (attempt 0→~base, 1→~2*base, … capped). Used before
# re-dispatching a failed / rate-limited agent so retries do not hammer the API.
cu::backoff_secs() {
  local attempt="${1:-0}" base="${2:-5}" cap="${3:-120}" exp delay jitter
  exp="${attempt}"
  ((exp > 6)) && exp=6 # cap the shift so the value can't explode
  delay=$((base * (1 << exp)))
  ((delay > cap)) && delay="${cap}"
  jitter=$((RANDOM % (delay / 2 + 1)))
  echo $((delay + jitter))
}
