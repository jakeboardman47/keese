#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright (c) 2026 Aviz Networks, Inc.
#
# PreToolUse hook: require Conventional Commits format on `git commit -m` and
# `git commit -F`. Does not replace the pre-commit commitizen hook — this fires
# before the command runs so Claude does not waste a turn on a rejected commit.

set -euo pipefail

input="$(cat)"

tool_name="$(jq -r '.tool_name // empty' <<<"$input")"
command="$(jq -r '.tool_input.command // empty' <<<"$input")"

if [[ "$tool_name" != "Bash" ]]; then exit 0; fi
if [[ ! "$command" =~ ^[[:space:]]*git[[:space:]]+commit ]]; then exit 0; fi
# Only enforce on inline messages. `git commit` (opens editor) and `-F <file>` are fine.
if [[ ! "$command" =~ -m[[:space:]]+ ]] && [[ ! "$command" =~ --message= ]]; then
  exit 0
fi

# Extract the message literal. Best-effort; tolerates single or double quotes.
msg=""
if [[ "$command" =~ -m[[:space:]]+\"([^\"]+)\" ]]; then
  msg="${BASH_REMATCH[1]}"
elif [[ "$command" =~ -m[[:space:]]+\'([^\']+)\' ]]; then
  msg="${BASH_REMATCH[1]}"
fi

if [[ -z "$msg" ]]; then exit 0; fi

# Conventional Commits regex — allow optional `!` for breaking changes.
conv_re='^(feat|fix|docs|style|refactor|perf|test|chore|ci|build)(\([a-z0-9._/-]+\))?!?: .+'

if [[ ! "$msg" =~ $conv_re ]]; then
  jq -n --arg reason "commit message must follow Conventional Commits: <type>(<scope>): <subject>. Allowed types: feat, fix, docs, style, refactor, perf, test, chore, ci, build." '{
    hookSpecificOutput: {
      hookEventName: "PreToolUse",
      permissionDecision: "deny",
      permissionDecisionReason: $reason
    }
  }'
  exit 2
fi

# Subject length
subject="${msg#*: }"
if (( ${#subject} > 72 )); then
  jq -n --arg reason "commit subject is ${#subject} chars; keep it ≤ 72" '{
    hookSpecificOutput: {
      hookEventName: "PreToolUse",
      permissionDecision: "deny",
      permissionDecisionReason: $reason
    }
  }'
  exit 2
fi

exit 0
