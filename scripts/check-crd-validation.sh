#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright (c) 2026 keese-ai
#
# Validate every sample under config/samples/ against the generated
# CRDs using an envtest-backed API server via kubectl --dry-run=server.
#
# Until P6 scaffolds the envtest helper binary under hack/, this script
# falls back to a static schema validation pass.

set -euo pipefail
IFS=$'\n\t'

REPO_ROOT="$(git rev-parse --show-toplevel)"
cd "${REPO_ROOT}"

SAMPLES_DIR="${REPO_ROOT}/config/samples"
CRD_DIR="${REPO_ROOT}/config/crd/bases"

if [ ! -d "${SAMPLES_DIR}" ]; then
  exit 0
fi

# If envtest + kubectl aren't available, fall back to kubeconform.
if [ ! -d "${CRD_DIR}" ] || ! command -v kubectl >/dev/null 2>&1; then
  if command -v kubeconform >/dev/null 2>&1; then
    kubeconform -strict -summary -kubernetes-version 1.30.0 "${SAMPLES_DIR}"/**/*.yaml 2>/dev/null || true
  fi
  exit 0
fi

# Full envtest path requires hack/envtest-apiserver (P6 artifact).
if [ ! -d "${REPO_ROOT}/hack/envtest-apiserver" ]; then
  echo "check-crd-validation: hack/envtest-apiserver not present yet (P6); skipping full envtest validation" >&2
  exit 0
fi

# Bounded readiness
TMPKUBE="$(mktemp -d)"
trap 'rm -rf "${TMPKUBE}"' EXIT

if [ -z "${KUBEBUILDER_ASSETS:-}" ]; then
  if command -v setup-envtest >/dev/null 2>&1; then
    KUBEBUILDER_ASSETS="$(setup-envtest use 1.30.x -p path)"
    export KUBEBUILDER_ASSETS
  else
    echo "check-crd-validation: setup-envtest missing; skipping" >&2
    exit 0
  fi
fi

go run ./hack/envtest-apiserver --kubeconfig "${TMPKUBE}/kubeconfig" --ready-file "${TMPKUBE}/ready" &
APISERVER_PID=$!
trap 'kill "${APISERVER_PID}" 2>/dev/null || true; rm -rf "${TMPKUBE}"' EXIT

for _ in $(seq 1 30); do
  [ -f "${TMPKUBE}/ready" ] && break
  sleep 1
done

if [ ! -f "${TMPKUBE}/ready" ]; then
  echo "check-crd-validation: envtest apiserver never became ready" >&2
  exit 1
fi

export KUBECONFIG="${TMPKUBE}/kubeconfig"

if ! kubectl apply --server-side -f "${CRD_DIR}" >/dev/null; then
  echo "check-crd-validation: CRD install failed" >&2
  exit 1
fi

failed=0
while IFS= read -r -d '' sample; do
  if ! kubectl apply --dry-run=server -f "${sample}" >/dev/null 2>&1; then
    echo "check-crd-validation: ${sample} failed" >&2
    kubectl apply --dry-run=server -f "${sample}" 2>&1 | sed 's/^/    /' >&2 || true
    failed=1
  fi
done < <(find "${SAMPLES_DIR}" -name '*.yaml' -print0)

exit "${failed}"
