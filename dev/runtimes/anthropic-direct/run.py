#!/usr/bin/env python3
# SPDX-License-Identifier: Apache-2.0
# Copyright (c) 2026 keese-ai
"""
keese non-streaming agent runtime via the AI Gateway's OpenAI shim.

Reads a recipe (YAML at $KEESE_RECIPE_PATH) and runs a single
non-streaming OpenAI-style /v1/chat/completions call through the
in-cluster Envoy AI Gateway. Bypasses envoyproxy/ai-gateway#1894 (the
streaming/Buffered EOF bug on both translator paths).

This was previously the "anthropic-direct" image (Anthropic SDK,
/anthropic/v1/messages). 2026-05-05: switched to the OpenAI SDK +
/v1/chat/completions so it shares one canonical AIGatewayRoute with
the keese-built goose-from-source runtime — no more route-precedence
dance between native Anthropic and OpenAI shim AIGatewayRoutes.

Env:
  KEESE_RECIPE_PATH   path to the recipe YAML (default: /var/run/keese/recipes/recipe.yaml)
  OPENAI_BASE_URL     gateway base URL (default: https://envoy-ai-gateway.keese-system.svc:443/v1)
  OPENAI_API_KEY      placeholder ok; BSP injects the real x-api-key on the gateway
  GOOSE_MODEL         claude-* model ID (default: claude-opus-4-7)
  SSL_CERT_FILE       PEM with the gateway's CA (default: /var/run/keese/ca/ca.crt)
  KEESE_PROMPT        optional override of the recipe's `prompt` field
"""

from __future__ import annotations

import json
import os
import sys
from pathlib import Path

import yaml
from openai import OpenAI

RECIPE_PATH = os.environ.get("KEESE_RECIPE_PATH", "/var/run/keese/recipes/recipe.yaml")
BASE_URL = os.environ.get(
    "OPENAI_BASE_URL", "https://envoy-ai-gateway.keese-system.svc:443/v1"
)
MODEL_DEFAULT = os.environ.get("GOOSE_MODEL", "claude-opus-4-7")


def load_recipe() -> dict:
    p = Path(RECIPE_PATH)
    if not p.exists():
        print(f"recipe not found at {RECIPE_PATH}", file=sys.stderr)
        sys.exit(2)
    return yaml.safe_load(p.read_text())


def main() -> None:
    recipe = load_recipe()
    instructions = (recipe.get("instructions") or "").strip()
    prompt = os.environ.get("KEESE_PROMPT") or (recipe.get("prompt") or "").strip()
    if not prompt:
        print("recipe has no prompt and KEESE_PROMPT not set", file=sys.stderr)
        sys.exit(2)
    model = (recipe.get("settings") or {}).get("goose_model") or MODEL_DEFAULT

    print("=== keese non-streaming runtime (OpenAI shim) ===")
    print(f"recipe: {recipe.get('title')!r} v{recipe.get('version')}")
    print(f"model: {model}")
    print(f"base_url: {BASE_URL}")
    print(f"prompt: {prompt!r}")
    print()

    # Goose's gateway path strips client-supplied Authorization at the
    # listener Lua, then BSP injects x-api-key. Any non-empty value
    # for api_key is fine.
    client = OpenAI(base_url=BASE_URL, api_key="placeholder-bsp-injects-real-one")
    messages: list[dict[str, str]] = []
    if instructions:
        messages.append({"role": "system", "content": instructions})
    messages.append({"role": "user", "content": prompt})

    response = client.chat.completions.create(
        model=model,
        max_tokens=512,
        messages=messages,
        stream=False,
    )
    choice = response.choices[0]
    text = (choice.message.content or "").strip()
    print("--- response ---")
    print(text)
    print("--- usage ---")
    usage = response.usage
    print(
        json.dumps(
            {
                "prompt_tokens": getattr(usage, "prompt_tokens", None),
                "completion_tokens": getattr(usage, "completion_tokens", None),
                "model": response.model,
                "finish_reason": choice.finish_reason,
            },
            indent=2,
        )
    )


if __name__ == "__main__":
    main()
