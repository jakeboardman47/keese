# SPDX-License-Identifier: Apache-2.0
# Copyright (c) 2026 keese-ai

# Build the keese-token-meter binary — the stateless OTEL/metrics processor
# (ADR 30) that relabels the Envoy AI Gateway's per-response token usage into the
# keese_token_budget_consumed_total{tenant,workspace,model,direction} contract
# series and dedups on Envoy x-request-id.
#
# Build context is the repository root (needs go.mod / go.sum):
#   docker build -f cmd/token-meter/Dockerfile -t keese-token-meter .
#
# Both base images are pinned by digest (rule 02 supply-chain + rule 05.12). The
# resulting image carries no secrets and is cosign-signable via keyless OIDC in
# CI (rule 05.12); CI re-pins these digests at release time.

# golang:1.25
FROM golang@sha256:fb6b5fe6aca5fb55b7238c2fadaac64bd955ceeb6f305cb0ac3fd28b377e4415 AS builder
ARG TARGETOS
ARG TARGETARCH

WORKDIR /workspace

# Cache deps before copying source.
COPY go.mod go.mod
COPY go.sum go.sum
RUN go mod download

COPY cmd/token-meter/ cmd/token-meter/

# CGO_ENABLED=0 → fully static binary that runs in distroless/static. -trimpath
# + reproducible flags keep the build deterministic for cosign attestation.
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags="-s -w" -a -o token-meter ./cmd/token-meter

# gcr.io/distroless/static:nonroot — no shell, no libc, no package manager, runs
# as uid 65532. The meter holds no durable state and reads/writes no secrets, so
# a static distroless base is the minimal viable runtime.
FROM gcr.io/distroless/static@sha256:963fa6c544fe5ce420f1f54fb88b6fb01479f054c8056d0f74cc2c6000df5240
WORKDIR /
COPY --from=builder /workspace/token-meter /token-meter
USER 65532:65532
# 8080: ingest + /metrics + /healthz + /readyz (single HTTP listener).
EXPOSE 8080
ENTRYPOINT ["/token-meter"]
