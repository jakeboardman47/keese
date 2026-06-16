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
# shellcheck source=../../../scripts/lib/log.sh
source "${SCRIPT_DIR}/../../../scripts/lib/log.sh"

BAO_ADDR="${BAO_ADDR:-http://localhost:8200}"
# Dev-mode default — values/openbao.yaml runs OpenBao with `dev.enabled=true`
# and `devRootToken: "root"` so the auto-unsealed in-memory server is reachable
# with the well-known token. Production overlays must export BAO_TOKEN
# explicitly after manual `bao operator init` + `bao operator unseal`.
BAO_TOKEN="${BAO_TOKEN:-root}"
KV_MOUNT="keese"

# ── Preconditions ──────────────────────────────────────────────────────────────

if [[ -z "${BAO_TOKEN}" ]]; then
  log::err "BAO_TOKEN is not set and no dev default available."
  log::err "  Production: kubectl exec -n openbao openbao-0 -- bao operator init"
  log::err "             kubectl exec -n openbao openbao-0 -- bao operator unseal <key>"
  log::err "  Dev:       export BAO_TOKEN=root  (matches values/openbao.yaml devRootToken)"
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
  # tenant-a: Anthropic API key. If ANTHROPIC_API_KEY is exported (e.g. from
  # .env.local) this seeds the live value so the AI Gateway BSP works
  # immediately. Otherwise an empty placeholder is written and the operator
  # populates it later via `bao kv put`.
  if [[ -n "${ANTHROPIC_API_KEY:-}" ]]; then
    log::info "Seeding live ANTHROPIC_API_KEY at ${KV_MOUNT}/tenants/tenant-a/anthropic"
    bao kv put -mount="${KV_MOUNT}" "tenants/tenant-a/anthropic" "api_key=${ANTHROPIC_API_KEY}"
  else
    _write_placeholder "tenants/tenant-a/anthropic" "api_key"
  fi
  # tenant-a: GitHub PAT placeholder (for recipe sources)
  _write_placeholder "tenants/tenant-a/github" "pat"
  # tenant-b: Anthropic API key placeholder
  _write_placeholder "tenants/tenant-b/anthropic" "api_key"
}

run::step "02" "write placeholder secrets" _seed_placeholders

log::ok "OpenBao seeding complete."
