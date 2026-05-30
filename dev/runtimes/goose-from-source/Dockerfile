# SPDX-License-Identifier: Apache-2.0
# Copyright (c) 2026 keese-ai
#
# keese goose runtime built from source — pinned to a tagged release
# of block/goose because ghcr.io/block/goose hasn't published a tag
# newer than 1.30.0 (`:latest`, `:main`, `:stable` all alias it). The
# `custom_providers` JSON-file loader landed after 1.30.0; it's the
# only known config-only workaround for envoyproxy/ai-gateway#1894
# (streaming + Buffered EOF on the AI Gateway extProc translator).
#
# Build:
#   docker buildx build --platform=linux/arm64 --build-arg GOOSE_REF=v1.33.1 \
#     -t keese-goose:1.33.1 -f dev/runtimes/goose-from-source/Dockerfile \
#     dev/runtimes/goose-from-source
#   kind load docker-image keese-goose:1.33.1 --name keese-demo
#
# This is mechanically equivalent to the upstream goose Dockerfile —
# we just clone at a known tag rather than COPY a working tree, so
# this image is reproducible from a Dockerfile-only checkout.

# syntax=docker/dockerfile:1.4

ARG GOOSE_REF=v1.33.1

# ── Build stage ──────────────────────────────────────────────────────
FROM rust:1.82-bookworm AS builder

RUN apt-get update && apt-get install -y --no-install-recommends \
      build-essential \
      cmake \
      pkg-config \
      libssl-dev \
      libdbus-1-dev \
      libclang-dev \
      protobuf-compiler \
      libprotobuf-dev \
      ca-certificates \
      git \
    && rm -rf /var/lib/apt/lists/*

ARG GOOSE_REF
WORKDIR /build
RUN git clone --depth=1 --branch="${GOOSE_REF}" https://github.com/block/goose.git . \
    && git rev-parse HEAD > /build/GOOSE_COMMIT

ENV CARGO_REGISTRIES_CRATES_IO_PROTOCOL=sparse \
    CARGO_PROFILE_RELEASE_LTO=true \
    CARGO_PROFILE_RELEASE_CODEGEN_UNITS=1 \
    CARGO_PROFILE_RELEASE_OPT_LEVEL=z \
    CARGO_PROFILE_RELEASE_STRIP=true

RUN cargo build --release --package goose-cli

# ── Runtime stage ────────────────────────────────────────────────────
FROM debian:bookworm-slim

RUN apt-get update && apt-get install -y --no-install-recommends \
      ca-certificates \
      libssl3 \
      libdbus-1-3 \
      libgomp1 \
      libxcb1 \
      curl \
      git \
    && apt-get clean \
    && rm -rf /var/lib/apt/lists/*

# Match the upstream image's user (uid/gid 1000) so keese pod
# `runAsUser: 1000` mounts and PVCs already-set-up for the goose
# session pod work without permission churn.
RUN groupadd -g 1000 goose && useradd -u 1000 -g 1000 -s /bin/bash -m goose

COPY --from=builder /build/target/release/goose /usr/local/bin/goose
COPY --from=builder /build/GOOSE_COMMIT /usr/local/share/goose/COMMIT
RUN chmod 0755 /usr/local/bin/goose

USER 1000:1000
WORKDIR /home/goose

ENV PATH=/usr/local/bin:/usr/local/sbin:/usr/sbin:/usr/bin:/sbin:/bin

ENTRYPOINT ["/usr/local/bin/goose"]
CMD ["--help"]
