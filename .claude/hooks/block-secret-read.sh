#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright (c) 2026 keese-ai
#
# PreToolUse hook: block Bash commands that would read secrets from disk.
#
# Input JSON on stdin:
#   { "tool_name": "Bash", "tool_input": { "command": "<cmd>" }, ... }
# Exit 2 with a JSON body to deny the tool call.

set -euo pipefail

input="$(cat)"

tool_name="$(jq -r '.tool_name // empty' <<<"$input")"
command="$(jq -r '.tool_input.command // empty' <<<"$input")"

if [[ "$tool_name" != "Bash" ]]; then
  exit 0
fi

# Patterns that indicate reading/echoing secrets we forbid.
# Add project-specific env var names to the echo/printenv patterns below.
forbidden_patterns=(
  'cat[[:space:]]+.*\.env\.local'
  'cat[[:space:]]+.*\.env([^.]|$)'
  'cat[[:space:]]+.*\.pem'
  'cat[[:space:]]+.*\.key([^s]|$)'
  'cat[[:space:]]+.*kubeconfig'
  'printenv[[:space:]]'
  'env[[:space:]]*$'
  # Add project-specific token env var names here, e.g.:
  # 'echo[[:space:]]+.*\$MY_API_TOKEN'
)

for pat in "${forbidden_patterns[@]}"; do
  if [[ "$command" =~ $pat ]]; then
    jq -n --arg reason "blocked: command would read or print a secret (pattern: $pat)" '{
      hookSpecificOutput: {
        hookEventName: "PreToolUse",
        permissionDecision: "deny",
        permissionDecisionReason: $reason
      }
    }'
    exit 2
  fi
done

exit 0
