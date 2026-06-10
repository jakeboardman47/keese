#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright (c) 2026 keese-ai
#
# CH5d gate — is the FULL token-metering enforcement path live end-to-end?
#
# check-metering.sh (its sibling) answers a narrower question: "is the
# consumed series in Prometheus?" That is necessary but NOT sufficient for the
# CH5d over-budget assertion, which exercises the long-window TokenBudget loop
# all the way to the gateway's ext_authz local_reply:
#
#   gateway response usage
#     → OTLP exporter → meter /ingest  (collector OTLP→/ingest SHAPING — CH5b stub)
#     → keese-token-meter relabels      (the :dev meter IMAGE — CH5b stub)
#     → keese_token_budget_consumed_total in Prometheus
#     → TokenBudget reconciler increase()[window] crossover (CH5c, complete)
#     → NATS KV keese-budget-exceeded boolean
#     → keese-authz ext_authz push-watch → Envoy local_reply
#     → HTTP 429 + x-keese-limit-source: token-budget   (ADR 30 / 05a / 10b)
#
# Per ADR 30 §"Implementation phases" the loop is sequential CH5a→CH5b→CH5c→
# CH5d. CH5c (reconciler) is COMPLETE; CH5d (this e2e) is wired. But CH5b
# (dev/bootstrap) shipped-with-stubs with TWO open triggers that gate the live
# series, and therefore the whole downstream 429:
#
#   1. revisit_when_meter_image_live      — the ghcr.io/keese-ai/keese-token-
#      meter:dev image is built+kind-loaded (`make token-meter-load`). Until
#      then the keese-token-meter Deployment in `monitoring` has no runnable
#      image → 0/N ready → /ingest never relabels → consumed series stays empty.
#   2. revisit_when_collector_ingest_shaping — the Tier-1 OTEL collector's
#      OTLP token-cost datapoint → meter /ingest JSON ADAPTATION. Until the
#      live collector image performs that shaping, the meter receives nothing
#      to relabel even when its own pod is up.
#
# This gate collapses both into the single umbrella precondition the EH7/CH5d
# docs track as `revisit_when_metering_fully_live`. When ANY precondition is
# unmet we SKIP (exit 2) — never a fake pass (rule 06: do not paper over a
# missing dependency; assert real behavior or skip cleanly with a trigger).
#
# Exit-code convention (mirrors check-metering.sh):
#   0  full live path up         → caller asserts 429 + x-keese-limit-source
#   2  any precondition unmet    → caller SKIPS the over-budget step
#                                  (revisit_when_metering_fully_live)
#   2  no kubectl context        → caller SKIPS (cannot probe)
#
# Overrides: KUBECTL_CONTEXT / METER_NS / METER_DEPLOY / EXTAUTHZ_NS /
#            EXTAUTHZ_LABEL / PROM_NS / PROM_SVC / METRIC_NAME.
#
# Refs: docs/designs/30-token-metering-pipeline.md (the metering hop + phases)
#       docs/plans/controller-hardening/CH5b-meter-bootstrap-wiring.md
#         (revisit_when_meter_image_live + revisit_when_collector_ingest_shaping)
#       dev/bootstrap/token-meter/kustomization.yaml (the meter Deployment)
#       dev/bootstrap/values/otel-collector.yaml (the OTLP→/ingest shaping)
#       internal/controller/policy/tokenbudget_controller.go (NATS-KV signal)

set -euo pipefail
IFS=$'\n\t'

# Caller-visible skip code (distinct from a hard prereq failure), matching the
# convention enforce.sh already keys on for check-metering.sh.
METERING_SKIP=2

CONTEXT="${KUBECTL_CONTEXT:-$(kubectl config current-context 2>/dev/null || true)}"
# CH5b lands the meter + dev Prometheus in `monitoring` (Makefile bootstrap-infra).
METER_NS="${METER_NS:-monitoring}"
METER_DEPLOY="${METER_DEPLOY:-keese-token-meter}"
PROM_NS="${PROM_NS:-${METER_NS}}"
PROM_SVC="${PROM_SVC:-prometheus}"
METRIC_NAME="${METRIC_NAME:-keese_token_budget_consumed_total}"
# ext_authz (keese-authz) holds the NATS-KV exceeded watch that emits the
# local_reply 429; it must be up for the budget signal to reach Envoy.
EXTAUTHZ_NS="${EXTAUTHZ_NS:-keese-system}"
EXTAUTHZ_LABEL="${EXTAUTHZ_LABEL:-app.kubernetes.io/name=keese-authz}"

skip() {
  # $1 = human-readable reason block on stderr.
  printf '%s\n' "$1" >&2
  exit "${METERING_SKIP}"
}

if [[ -z "${CONTEXT}" ]]; then
  skip "[metering-live] no kubectl context — SKIP over-budget assertions"
fi

kc() { kubectl --context="${CONTEXT}" "$@"; }

# ── Precondition 1: the :dev meter image is live (revisit_when_meter_image_live)
# The Deployment must exist AND report >=1 ready replica. A pod stuck
# ImagePullBackOff (image never `make token-meter-load`-ed) leaves
# readyReplicas empty/0 — the exact signal CH5b's stub note describes.
READY="$(kc -n "${METER_NS}" get deploy "${METER_DEPLOY}" \
  -o jsonpath='{.status.readyReplicas}' 2>/dev/null || true)"
if [[ -z "${READY}" || "${READY}" -lt 1 ]]; then
  skip "[metering-live] SKIP: keese-token-meter not live (readyReplicas=${READY:-<none>}).

  The ghcr.io/keese-ai/keese-token-meter:dev image is not running in
  ${METER_NS}/${METER_DEPLOY}. Run 'make token-meter-load' to build+kind-load
  it (CH5b: revisit_when_meter_image_live). Until the meter pod is ready it
  cannot relabel /ingest records, so the consumed series stays empty and the
  over-budget 429 cannot be driven.

  Tracking: revisit_when_metering_fully_live."
fi

# ── Precondition 2: ext_authz (NATS-KV watch → local_reply 429) is up ─────────
# The token-budget 429 (and its x-keese-limit-source header) is emitted by
# keese-authz's Envoy local_reply, driven by the NATS-KV exceeded boolean
# (ADR 30 / 05a). Without it the over-budget signal never becomes a 429.
EXTAUTHZ_POD="$(kc -n "${EXTAUTHZ_NS}" get pod -l "${EXTAUTHZ_LABEL}" \
  -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || true)"
if [[ -z "${EXTAUTHZ_POD}" ]]; then
  skip "[metering-live] SKIP: keese-authz ext_authz pod absent in ${EXTAUTHZ_NS}
  (label ${EXTAUTHZ_LABEL}). It holds the NATS-KV budget-exceeded watch that
  emits the token-budget local_reply 429; without it the over-budget step
  cannot observe 429 + x-keese-limit-source.

  Tracking: revisit_when_metering_fully_live."
fi

# ── Precondition 3: the collector OTLP→/ingest shaping actually emitted ───────
# (revisit_when_collector_ingest_shaping). The proof the shaping works is the
# consumed series materializing in Prometheus — same evidence check-metering.sh
# uses, re-asserted here so the umbrella gate is self-contained.
if ! kc -n "${PROM_NS}" get svc "${PROM_SVC}" >/dev/null 2>&1; then
  skip "[metering-live] SKIP: Prometheus ${PROM_NS}/${PROM_SVC} absent — the
  collector OTLP→/ingest shaping has nowhere to land the consumed series.

  Tracking: revisit_when_metering_fully_live (collector_ingest_shaping)."
fi

SERIES="$(kc -n "${PROM_NS}" exec "svc/${PROM_SVC}" -- \
  wget -qO- "http://localhost:9090/api/v1/series?match[]=${METRIC_NAME}" 2>/dev/null || true)"
if [[ -z "${SERIES}" || "${SERIES}" == *'"data":[]'* ]]; then
  skip "[metering-live] SKIP: ${METRIC_NAME} series empty.

  The meter pod is up but no consumed datapoints have arrived — the Tier-1
  OTEL collector is not yet shaping its OTLP token-cost datapoint into the
  meter's /ingest JSON record (CH5b: revisit_when_collector_ingest_shaping).
  Without the series the reconciler reads consumed=0 and never trips the
  NATS-KV exceeded signal, so the over-budget 429 cannot be driven.

  Tracking: revisit_when_metering_fully_live."
fi

echo "[metering-live] OK: meter Deployment ready in ${METER_NS}; ext_authz up in" \
  "${EXTAUTHZ_NS}; ${METRIC_NAME} series present — full token-budget enforcement" \
  "path is live; asserting 429 + x-keese-limit-source: token-budget."
