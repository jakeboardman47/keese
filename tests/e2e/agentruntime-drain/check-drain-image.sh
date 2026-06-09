#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright (c) 2026 keese-ai
#
# Gate for the real-keese-drain steps of the agentruntime-drain suite.
#
# The drain steps (02-drain, 03-resume) run the REAL /usr/local/bin/keese-drain
# baked into the goose-runtime image (Dockerfile.goose-runtime). That image must
# be built and loaded into the kind cluster first:
#
#     make goose-runtime-load
#
# When the image is NOT loaded, those steps would fail with ImagePullBackOff
# (the dev tag is never pushed to a registry — rule 05.15 denies docker push).
# Rather than fail the suite, this gate detects the missing image and writes a
# SKIP sentinel; the real-binary steps source it and exit 0 early.
#
# This keeps the real-binary WIRING committed and exercised the moment the image
# is live, while letting the self-contained prereq steps (00, 01) always run.
#
# Exit status is ALWAYS 0 — the gate's job is to set DRAIN_SKIP, not to fail.
#
# Outputs (written to the suite's per-run namespace tmp via $SKIP_SENTINEL):
#   $SKIP_SENTINEL present  → real-binary steps skip (image absent)
#   $SKIP_SENTINEL absent   → real-binary steps run  (image present)
#
# Override the image ref with GOOSE_RUNTIME_IMG; the cluster context with
# KUBECTL_CONTEXT.

set -euo pipefail
IFS=$'\n\t'

IMG="${GOOSE_RUNTIME_IMG:-ghcr.io/keese-ai/goose-runtime:dev}"
SKIP_SENTINEL="${SKIP_SENTINEL:-/tmp/keese-drain-skip}"
CONTEXT="${KUBECTL_CONTEXT:-$(kubectl config current-context 2>/dev/null || true)}"

# Start clean: a stale sentinel from a previous run must not leak in.
rm -f "${SKIP_SENTINEL}"

skip() {
  printf '[drain-image] SKIP: %s\n' "$1" >&2
  printf '[drain-image] real-keese-drain steps will be skipped; ' >&2
  # shellcheck disable=SC2016  # backticks are literal markdown, not a subshell.
  printf 'run `make goose-runtime-load` then re-run to exercise the real binary.\n' >&2
  : >"${SKIP_SENTINEL}"
  exit 0
}

if [[ -z "${CONTEXT}" ]]; then
  skip "no kubectl context — cannot probe for ${IMG}"
fi

# The image is "available" if at least one node reports it in status.images.
# kind-loaded images appear there; never-loaded dev tags do not.
NODES_WITH_IMG="$(
  kubectl --context="${CONTEXT}" get nodes \
    -o jsonpath='{range .items[*]}{range .status.images[*]}{.names[*]}{"\n"}{end}{end}' \
    2>/dev/null | grep -cF "${IMG}" || true
)"

if [[ "${NODES_WITH_IMG}" -lt 1 ]]; then
  skip "goose-runtime image ${IMG} not loaded on any node"
fi

printf '[drain-image] OK: %s present on %s node(s); real keese-drain will run.\n' \
  "${IMG}" "${NODES_WITH_IMG}" >&2
exit 0
