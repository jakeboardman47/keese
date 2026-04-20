#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright (c) 2026 keese-ai
#
# Idempotent OpenBao seed script for local dev.
# Enables kv-v2 at keese/ if not already mounted.
# Writes empty-value placeholder secrets for dev.
#
# Prerequisites:
#   - OpenBao pod is running and UNSEALED (manual unseal required — auto-unseal
#     is disabled for dev parity with prod; see dev/bootstrap/values/openbao.yaml)
#   - BAO_ADDR and BAO_TOKEN exported in the calling environment
#
# Usage: scripts/dev/seed-openbao.sh
#        (wrapper adds cluster context guard before calling this script)

set -euo pipefail
IFS=$'\n\t'

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../../scripts/lib/log.sh
source "${SCRIPT_DIR}/../../scripts/lib/log.sh"

BAO_ADDR="${BAO_ADDR:-http://localhost:8200}"
BAO_TOKEN="${BAO_TOKEN:-}"
KV_MOUNT="keese"

# ── Preconditions ──────────────────────────────────────────────────────────────

if [[ -z "${BAO_TOKEN}" ]]; then
  log::err "BAO_TOKEN is not set. Unseal OpenBao and export the root token."
  log::err "  kubectl exec -n openbao openbao-0 -- bao operator init"
  log::err "  kubectl exec -n openbao openbao-0 -- bao operator unseal <key>"
  exit 1
fi

export BAO_ADDR BAO_TOKEN

log::info "OpenBao seed starting — addr=${BAO_ADDR}"

# ── Step 1: Enable kv-v2 at keese/ (idempotent) ───────────────────────────────

_enable_kv() {
  local existing
  existing=$(bao secrets list -format=json 2>/dev/null | jq -r --arg m "${KV_MOUNT}/" 'keys[] | select(. == $m)' || echo "")

  if [[ -n "${existing}" ]]; then
    log::info "kv-v2 mount '${KV_MOUNT}/' already exists — skipping enable."
  else
    log::info "Enabling kv-v2 at ${KV_MOUNT}/..."
    bao secrets enable -path="${KV_MOUNT}" kv-v2
    log::ok "kv-v2 enabled at ${KV_MOUNT}/."
  fi
}

run::step "01" "enable kv-v2 at ${KV_MOUNT}/" _enable_kv

# ── Step 2: Write placeholder secrets (idempotent) ─────────────────────────────

_write_placeholder() {
  local path="$1" key="$2"
  local existing
  existing=$(bao kv get -mount="${KV_MOUNT}" -format=json "${path}" 2>/dev/null | jq -r '.data.data // empty' || echo "")

  if [[ -n "${existing}" ]]; then
    log::info "Secret '${KV_MOUNT}/${path}' already exists — skipping write."
  else
    log::info "Writing placeholder at ${KV_MOUNT}/${path}..."
    # Empty-value placeholder — real values filled by operator or human.
    bao kv put -mount="${KV_MOUNT}" "${path}" "${key}="
    log::ok "Placeholder written at ${KV_MOUNT}/${path}."
  fi
}

_seed_placeholders() {
  # tenant-a: Anthropic API key placeholder
  _write_placeholder "tenants/tenant-a/anthropic" "api_key"
  # tenant-a: GitHub PAT placeholder (for recipe sources)
  _write_placeholder "tenants/tenant-a/github" "pat"
  # tenant-b: Anthropic API key placeholder
  _write_placeholder "tenants/tenant-b/anthropic" "api_key"
}

run::step "02" "write placeholder secrets" _seed_placeholders

log::ok "OpenBao seeding complete."
