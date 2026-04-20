#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright (c) 2026 keese-ai
#
# Re-render every committed diagram source and compare to its sibling render.
# Fail when source and render have drifted. Invoked by pre-commit and CI.
#
# Supported sources:
#   *.d2    → rendered via d2
#   *.mmd   → rendered via mmdc (mermaid-cli)
#   *.dot   → rendered via dot (graphviz)
#
# Rendered files live next to sources with matching slug + .svg extension.

set -euo pipefail
IFS=$'\n\t'

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=scripts/lib/paths.sh
source "${HERE}/lib/paths.sh"
# shellcheck source=scripts/lib/log.sh
source "${HERE}/lib/log.sh"

cd "${REPO_ROOT}"

fail=0
check_one() {
  local src="$1" renderer="$2"
  local out="${src%.*}.svg"

  if [[ ! -f "$out" ]]; then
    log::err "missing render: $out (run: ${renderer} $src $out)"
    fail=1
    return
  fi

  if grep -q '^[[:space:]]*#.*status:[[:space:]]*stale' "$src" \
    || grep -q '^[[:space:]]*%%.*status:[[:space:]]*stale' "$src"; then
    log::warn "stale (skipped): $src"
    return
  fi

  local tmp
  tmp="$(mktemp -t diagram-check.XXXXXX.svg)"
  trap 'rm -f "$tmp"' RETURN

  case "$renderer" in
    d2) d2 "$src" "$tmp" >/dev/null 2>&1 || {
      log::err "d2 render failed: $src"
      fail=1
      return
    } ;;
    mmdc) mmdc -i "$src" -o "$tmp" -q >/dev/null 2>&1 || {
      log::err "mmdc render failed: $src"
      fail=1
      return
    } ;;
    dot) dot -Tsvg "$src" -o "$tmp" >/dev/null 2>&1 || {
      log::err "dot render failed: $src"
      fail=1
      return
    } ;;
    *)
      log::err "unknown renderer: $renderer"
      fail=1
      return
      ;;
  esac

  # Normalize whitespace-only differences so timestamps/comments don't trip drift.
  if ! diff -q <(sed -E 's/[[:space:]]+/ /g' "$tmp") \
    <(sed -E 's/[[:space:]]+/ /g' "$out") >/dev/null 2>&1; then
    log::err "drift: $out — source has changed since render"
    log::dim "  re-render: ${renderer} ${src} ${out}"
    fail=1
  fi
}

scan() {
  local ext="$1" renderer="$2"
  if ! command -v "$renderer" >/dev/null 2>&1; then
    log::warn "${renderer} not installed; skipping *.${ext} checks (nix develop provides it)"
    return
  fi
  while IFS= read -r -d '' src; do
    check_one "$src" "$renderer"
  done < <(find docs book -type f -name "*.${ext}" -print0 2>/dev/null || true)
}

scan d2 d2
scan mmd mmdc
scan dot dot

if ((fail)); then
  log::err "diagram freshness check FAILED — re-render and re-stage"
  exit 1
fi

log::ok "diagrams fresh"
