#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright (c) 2026 keese-ai
#
# Validate every rendered kustomize overlay + bundle manifest against
# kubeconform schemas for the k8s versions we target.

set -euo pipefail
IFS=$'\n\t'

REPO_ROOT="$(git rev-parse --show-toplevel)"
cd "${REPO_ROOT}"

if ! command -v kubeconform >/dev/null 2>&1; then
  echo "check-kubeconform: kubeconform missing; skipping" >&2
  exit 0
fi

if ! command -v kustomize >/dev/null 2>&1; then
  echo "check-kubeconform: kustomize missing; skipping" >&2
  exit 0
fi

# Target versions
readonly K8S_VERSIONS=("1.30.0" "1.31.0")

# Datree CRDs catalog (for our own CRDs we'd point at config/crd/bases)
schema_locations=(
  -schema-location default
  -schema-location "https://raw.githubusercontent.com/datreeio/CRDs-catalog/main/{{ .ResourceKind }}{{ .KindSuffix }}.json"
)

# Check every overlay under config/ + bundle/manifests.
failed=0
while IFS= read -r -d '' dir; do
  if [ ! -f "${dir}/kustomization.yaml" ] && [ ! -f "${dir}/kustomization.yml" ]; then
    continue
  fi
  rendered="$(kustomize build "${dir}" 2>/dev/null || true)"
  [ -z "${rendered}" ] && continue
  for v in "${K8S_VERSIONS[@]}"; do
    if ! echo "${rendered}" | kubeconform -strict -kubernetes-version "${v}" "${schema_locations[@]}" /dev/stdin; then
      echo "check-kubeconform: ${dir} failed against k8s ${v}" >&2
      failed=1
    fi
  done
done < <(find config bundle/manifests -type d -print0 2>/dev/null)

# Bundle manifests are not a kustomization; scan files directly.
if [ -d bundle/manifests ]; then
  for v in "${K8S_VERSIONS[@]}"; do
    if ! kubeconform -strict -kubernetes-version "${v}" "${schema_locations[@]}" bundle/manifests/*.yaml 2>/dev/null; then
      echo "check-kubeconform: bundle/manifests failed against k8s ${v}" >&2
      failed=1
    fi
  done
fi

exit "${failed}"
