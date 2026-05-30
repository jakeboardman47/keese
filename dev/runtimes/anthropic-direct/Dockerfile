# SPDX-License-Identifier: Apache-2.0
# Copyright (c) 2026 keese-ai
#
# Non-streaming agent runtime — Python container that runs a recipe
# against the keese in-cluster Envoy AI Gateway via the OpenAI SDK
# (non-streaming `/v1/chat/completions`). Sidesteps
# envoyproxy/ai-gateway#1894 (streaming + Buffered EOF) and shares a
# single canonical AIGatewayRoute with the keese-built
# goose-from-source runtime — everything goes through the OpenAI shim.
#
# The directory is still named `anthropic-direct` for git-history
# continuity; the runtime's effective semantics flipped 2026-05-05
# (Anthropic SDK -> OpenAI SDK, /anthropic/v1/messages ->
# /v1/chat/completions). See dev/runtimes/anthropic-direct/run.py.

FROM python:3.12-slim

# Non-root demo UID matches the goose pod's runAsUser (1000).
RUN groupadd -g 1000 agent && useradd -u 1000 -g 1000 -s /bin/bash -m agent

WORKDIR /app
COPY run.py /app/run.py

RUN pip install --no-cache-dir 'openai>=1.50' 'pyyaml>=6'

USER 1000:1000

ENV PYTHONUNBUFFERED=1 PYTHONDONTWRITEBYTECODE=1

ENTRYPOINT ["python3", "/app/run.py"]
