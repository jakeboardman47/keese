#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright (c) 2026 keese-ai
#
# EH11 recipe-source: assert the spec/status coupling on the ConfigMap
# RecipeSource that kuttl's declarative assert cannot express.
#
#   1. status.observedGeneration == metadata.generation
#      (rule 04.4 — reconcileConfigMap sets ObservedGeneration = Generation).
#   2. status.resolvedDigest starts with "configmap:" (the digest is
#      "configmap:<UID>" — proves a real ConfigMap was read, not a stub).
#
# No sleep-as-assertion: 00-assert.yaml has already gated the object to
# Synced before this step runs, so the fields are populated; this step is a
# one-shot read. Fail-closed on mismatch.
#
# Overrides: KUBECTL_CONTEXT, RS_NS, RS_NAME.

set -euo pipefail
IFS=$'\n\t'

CONTEXT="${KUBECTL_CONTEXT:-$(kubectl config current-context 2>/dev/null || true)}"
RS_NS="${RS_NS:-recipe-source-e2e}"
RS_NAME="${RS_NAME:-rs-configmap-e2e}"

if [[ -z "${CONTEXT}" ]]; then
  echo "[observed-gen] no kubectl context — skipping" >&2
  exit 0
fi

GEN="$(kubectl --context="${CONTEXT}" -n "${RS_NS}" \
  get recipesource "${RS_NAME}" -o jsonpath='{.metadata.generation}' 2>/dev/null || true)"
OBS="$(kubectl --context="${CONTEXT}" -n "${RS_NS}" \
  get recipesource "${RS_NAME}" -o jsonpath='{.status.observedGeneration}' 2>/dev/null || true)"
DIGEST="$(kubectl --context="${CONTEXT}" -n "${RS_NS}" \
  get recipesource "${RS_NAME}" -o jsonpath='{.status.resolvedDigest}' 2>/dev/null || true)"

if [[ -z "${GEN}" || -z "${OBS}" ]]; then
  echo "[observed-gen] FAIL: generation=${GEN:-<none>} observedGeneration=${OBS:-<none>}" >&2
  exit 1
fi
if [[ "${GEN}" != "${OBS}" ]]; then
  echo "[observed-gen] FAIL: observedGeneration ${OBS} != generation ${GEN}" >&2
  exit 1
fi
if [[ "${DIGEST}" != configmap:* ]]; then
  echo "[observed-gen] FAIL: resolvedDigest ${DIGEST:-<none>} lacks the 'configmap:' prefix" >&2
  exit 1
fi

echo "[observed-gen] OK: observedGeneration=${OBS}==generation; resolvedDigest=${DIGEST}"
