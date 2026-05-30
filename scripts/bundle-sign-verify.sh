#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright (c) 2026 keese-ai
#
# bundle-sign-verify.sh — verify cosign keyless OIDC signature on a
# keese bundle (or operator) image. Required CI status check before
# `make catalog-push` per design 14a §4. Exits non-zero on any
# failure; prints "FAIL: signature verification" so the cosign-tamper
# kuttl test can grep for it (design 14a-ii §"cosign Tamper Test").
#
# Usage:
#   scripts/bundle-sign-verify.sh <image-ref-with-digest>
#
# <image-ref-with-digest> must contain @sha256:… — tag-only refs are
# rejected pre-flight per rule 05.12.
#
# Identity + issuer pins anchored from rule 05.12:
#   --certificate-identity-regexp 'https://github.com/keese-ai/keese/.github/workflows/.*'
#   --certificate-oidc-issuer     'https://token.actions.githubusercontent.com'
#
# Override via env when verifying a fork's signed image:
#   KEESE_COSIGN_IDENTITY_REGEX=... KEESE_COSIGN_OIDC_ISSUER=...

set -euo pipefail
IFS=$'\n\t'

# shellcheck source=lib/log.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib/log.sh"

readonly DEFAULT_IDENTITY_REGEX='^https://github\.com/keese-ai/keese/\.github/workflows/.*$'
readonly DEFAULT_OIDC_ISSUER='https://token.actions.githubusercontent.com'

usage() {
  cat <<EOF
Usage: $0 <image-ref>

  <image-ref>   Container image reference with digest, e.g.
                ghcr.io/keese-ai/keese-bundle@sha256:<64-hex>

Environment overrides:
  KEESE_COSIGN_IDENTITY_REGEX   override --certificate-identity-regexp
  KEESE_COSIGN_OIDC_ISSUER      override --certificate-oidc-issuer
EOF
}

main() {
  if [[ $# -ne 1 ]]; then
    usage >&2
    exit 64 # EX_USAGE
  fi

  local image_ref="$1"

  if ! command -v cosign >/dev/null 2>&1; then
    log::err "cosign not on PATH — install via https://docs.sigstore.dev/cosign/installation/"
    exit 127
  fi

  if [[ "${image_ref}" != *"@sha256:"* ]]; then
    log::err "FAIL: signature verification — image ref is not digest-pinned (must contain @sha256:…)"
    log::err "  ref: ${image_ref}"
    exit 2
  fi

  local identity_regex="${KEESE_COSIGN_IDENTITY_REGEX:-${DEFAULT_IDENTITY_REGEX}}"
  local oidc_issuer="${KEESE_COSIGN_OIDC_ISSUER:-${DEFAULT_OIDC_ISSUER}}"

  log::info "verifying ${image_ref}"
  log::info "  identity-regexp: ${identity_regex}"
  log::info "  oidc-issuer:     ${oidc_issuer}"

  if cosign verify \
    --certificate-identity-regexp "${identity_regex}" \
    --certificate-oidc-issuer "${oidc_issuer}" \
    "${image_ref}" >/dev/null; then
    log::ok "signature verification passed: ${image_ref}"
    exit 0
  fi

  log::err "FAIL: signature verification — ${image_ref}"
  exit 1
}

main "$@"
