#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright (c) 2026 keese-ai
#
# Wrapper around dev/bootstrap/openbao/seed.sh.
# Adds a cluster context guard (must be pointed at kind-keese-dev) before
# delegating to the seed script so we never accidentally seed a prod vault.
#
# Usage: scripts/dev/seed-openbao.sh
#        BAO_ADDR=http://localhost:8200 BAO_TOKEN=<root> scripts/dev/seed-openbao.sh

set -euo pipefail
IFS=$'\n\t'

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"

# shellcheck source=../lib/log.sh
source "${REPO_ROOT}/scripts/lib/log.sh"

# ── Cluster context guard ──────────────────────────────────────────────────────

CURRENT_CONTEXT="$(kubectl config current-context 2>/dev/null || echo "")"
# Accept the full-stack dev cluster and the minimal demo cluster. Anything
# else (incl. cloud contexts) is refused.
case "${CURRENT_CONTEXT}" in
  kind-keese-dev|kind-keese-demo) ;;
  *)
    log::err "Refusing to seed: current kubectl context is '${CURRENT_CONTEXT}'."
    log::err "Expected one of: kind-keese-dev, kind-keese-demo."
    log::err "Switch context: kubectl config use-context kind-keese-dev"
    exit 1
    ;;
esac

log::info "Context check passed: ${CURRENT_CONTEXT}"

# ── Source .env.local for ANTHROPIC_API_KEY (gitignored, dev-only) ────────────
# The inner seed script reads ANTHROPIC_API_KEY from env. .env.local is the
# canonical place for dev secrets per .env.local.example. If the var is already
# set in the calling shell, it wins.

if [[ -f "${REPO_ROOT}/.env.local" ]]; then
  log::info "Sourcing ${REPO_ROOT}/.env.local for dev secrets"
  set -a
  # shellcheck source=/dev/null
  source "${REPO_ROOT}/.env.local"
  set +a
fi

if [[ -z "${ANTHROPIC_API_KEY:-}" ]]; then
  log::warn "ANTHROPIC_API_KEY is not set; will write empty placeholder."
  log::warn "  Set ANTHROPIC_API_KEY in .env.local for the demo Anthropic round-trip to work."
fi

# ── Delegate to the seed script ───────────────────────────────────────────────

exec "${REPO_ROOT}/dev/bootstrap/openbao/seed.sh" "$@"
