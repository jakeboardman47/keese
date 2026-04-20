#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright (c) {{YEAR}} {{ORG_NAME}}
#
# PostToolUse hook for Edit/Write: verify new or modified source files carry
# the SPDX + copyright header. Issues a block so the model can self-correct.
#
# Customize YEAR and ORG_NAME below to match your project.

set -euo pipefail

input="$(cat)"

tool_name="$(jq -r '.tool_name // empty' <<<"$input")"
file_path="$(jq -r '.tool_input.file_path // empty' <<<"$input")"

if [[ -z "$file_path" ]]; then exit 0; fi
if [[ ! -f "$file_path" ]]; then exit 0; fi

# Skip file types that do not need the header.
case "$file_path" in
  */zz_generated*.go|*.pb.go) exit 0 ;;
  */LICENSE|*/CODEOWNERS) exit 0 ;;
  */go.sum|*/go.mod|*/flake.lock) exit 0 ;;
  */.secrets.baseline|*/.commitlintrc.json) exit 0 ;;
  *.json|*.toml|*.svg|*.png|*.jpg|*.jpeg|*.gif|*.pdf|*.ico) exit 0 ;;
  */book/site/*|*/.plan-logs/*|*/vendor/*|*/.go/*|*/node_modules/*) exit 0 ;;
esac

# Target extensions / basenames that should have a header.
should_check=false
case "$file_path" in
  *.go|*.sh|*.bash|*.py|*.yaml|*.yml|*.nix|*.tf|*.hcl|*.mk|*.proto|*.ts|*.js)
    should_check=true ;;
  */Makefile|*.Makefile|*/Dockerfile|*.Dockerfile|*/Containerfile)
    should_check=true ;;
  *.md|*.markdown)
    should_check=true ;;
esac

if [[ "$should_check" != true ]]; then exit 0; fi

# Required substrings — edit to match your project's copyright line.
: "${SPDX_LINE:=SPDX-License-Identifier: Apache-2.0}"
: "${COPYRIGHT_LINE:=Copyright (c)}"

# Check the top of the file. 20 lines clears typical YAML frontmatter blocks.
header_scan_lines=20
first_lines="$(head -n "$header_scan_lines" "$file_path")"

spdx_ok=false
cpr_ok=false

if grep -qF "$SPDX_LINE" <<<"$first_lines"; then
  spdx_ok=true
fi
if grep -qF "$COPYRIGHT_LINE" <<<"$first_lines"; then
  cpr_ok=true
fi

if [[ "$spdx_ok" == true && "$cpr_ok" == true ]]; then
  exit 0
fi

reason="$file_path is missing SPDX and/or copyright header in the first ${header_scan_lines} lines. Required substrings: '${SPDX_LINE}' and '${COPYRIGHT_LINE}'."

jq -n --arg r "$reason" '{
  hookSpecificOutput: {
    hookEventName: "PostToolUse",
    decision: "block",
    reason: $r
  }
}'
exit 2
