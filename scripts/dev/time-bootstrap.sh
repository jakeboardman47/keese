#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright (c) 2026 keese-ai
#
# Measures wall-clock time for `make kind-up bootstrap-infra`.
# Fails (exit 1) if the combined time exceeds MAX_SECS (default: 300).
#
# Usage: scripts/dev/time-bootstrap.sh [max-seconds]
#   max-seconds defaults to 300 (5 minutes).

set -euo pipefail
IFS=$'\n\t'

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"

# shellcheck source=../lib/log.sh
source "${REPO_ROOT}/scripts/lib/log.sh"

MAX_SECS="${1:-300}"

log::info "time-bootstrap: timing 'make kind-up bootstrap-infra' (max ${MAX_SECS}s)"

start=$(date +%s)

make -C "${REPO_ROOT}" kind-up bootstrap-infra

elapsed=$(($(date +%s) - start))
log::info "time-bootstrap: completed in ${elapsed}s"

if [[ ${elapsed} -gt ${MAX_SECS} ]]; then
  log::err "time-bootstrap: FAILED — ${elapsed}s exceeds limit of ${MAX_SECS}s"
  log::err "Tune helmfile concurrency or pre-pull images to improve boot time."
  exit 1
fi

log::ok "time-bootstrap: PASSED — ${elapsed}s <= ${MAX_SECS}s"
