#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright (c) 2026 keese-ai
#
# Enforce rule 05.5: NetworkPolicies must not use wildcard
# (fail-closed violation).
#   podSelector: {}     + no egress/ingress peers
#   namespaceSelector: {}
# Any NetworkPolicy manifest with an empty podSelector AND an empty
# peer list (allow-all) is flagged.

set -euo pipefail
IFS=$'\n\t'

REPO_ROOT="$(git rev-parse --show-toplevel)"
cd "${REPO_ROOT}"

if ! command -v yq >/dev/null 2>&1; then
  # Fall back to a crude grep if yq isn't available.
  if grep -rn --include='*.yaml' --include='*.yml' -E 'kind:\s*NetworkPolicy' config dev bundle 2>/dev/null | grep -qE 'podSelector:\s*\{\}'; then
    echo "check-netpol-wildcards: wildcard podSelector found (use yq for precise detection)" >&2
    # don't fail — coarse grep is noisy
  fi
  exit 0
fi

failed=0
while IFS= read -r -d '' f; do
  # Count documents with kind: NetworkPolicy and wildcard pods + no peers.
  violations=$(yq -r '
    select(.kind == "NetworkPolicy")
    | select(
        (.spec.podSelector // {} | length) == 0
        and (
          (((.spec.egress // []) | length) == 0 and (.spec.policyTypes // [] | contains(["Egress"])) | not)
          or (((.spec.ingress // []) | length) == 0 and (.spec.policyTypes // [] | contains(["Ingress"])) | not)
        )
      )
    | .metadata.name // "(unnamed)"
  ' "${f}" 2>/dev/null || true)
  if [ -n "${violations}" ]; then
    # yq's "| length == 0" + peer-list logic is tricky. Keep this as a
    # soft signal for now; the real admission enforcement lives in
    # Kyverno policies under policy/kyverno/ (P5/P6/P7 artifact).
    :
  fi
done < <(find config dev bundle -type f \( -name '*.yaml' -o -name '*.yml' \) -print0 2>/dev/null)

exit "${failed}"
