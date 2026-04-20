# SPDX-License-Identifier: Apache-2.0
# Copyright (c) 2026 keese-ai
#
# .env.local loader. Source me; do not execute.
# shellcheck shell=bash

if [[ -n "${__LIB_ENV_SH_LOADED:-}" ]]; then
  return 0
fi
__LIB_ENV_SH_LOADED=1

env::load_local() {
  local path="${1:-${REPO_ROOT:-.}/.env.local}"
  if [[ -f "${path}" ]]; then
    # shellcheck disable=SC1090
    set -a
    source "${path}"
    set +a
    return 0
  fi
  return 1
}
