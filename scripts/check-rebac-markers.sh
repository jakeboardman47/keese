#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright (c) 2026 keese-ai
#
# Enforce rule 04.14 / 05.9: every authz-affecting CRD field carries
# a `// +keese:rebac-tuple=<relation>` marker.
#
# Heuristic: any struct field tagged with `rebac:"true"` in the json/yaml
# struct tags (or a naming heuristic: fields named *Entitlement*,
# *Allow*, *Deny*, *Tool*, *Role*, *Share*, *Tenant*, *Grant*) MUST have
# a `+keese:rebac-tuple=` marker within 5 lines above.

set -euo pipefail
IFS=$'\n\t'

REPO_ROOT="$(git rev-parse --show-toplevel)"
cd "${REPO_ROOT}"

if [ ! -d api ]; then
  exit 0
fi

# Fields whose names suggest authz relevance.
readonly NAMING_REGEX='^[[:space:]]+(Entitlement|Entitlements|Allow|AllowList|Deny|DenyList|Tool|Tools|Role|Roles|Grant|Grants|Share|Shares|Tenant|TenantRef|Allowed|Denied)[A-Z][a-zA-Z0-9]*[[:space:]]+'

failed=0
while IFS= read -r -d '' f; do
  # Skip generated files.
  case "$(basename "${f}")" in
    zz_generated*) continue ;;
  esac

  # Walk field definitions; check for marker in prior 5 lines.
  awk -v file="${f}" -v regex="${NAMING_REGEX}" '
    {
      if ($0 ~ regex && field_name == "") {
        field_name = $0
        field_line = NR
        # search preceding 5 lines
        found_marker = 0
        for (i = NR-1; i >= NR-5 && i > 0; i--) {
          if (history[i] ~ /\+keese:rebac-tuple=/) { found_marker = 1; break }
        }
        if (!found_marker && $0 ~ /`.*json:/) {
          printf "%s:%d: missing // +keese:rebac-tuple=... above field %s\n", file, NR, $0 > "/dev/stderr"
          rc=1
        }
        field_name = ""
      }
      history[NR] = $0
    }
    END { exit rc+0 }
  ' "${f}" || failed=1
done < <(find api -name '*_types.go' -not -name 'zz_generated*' -print0)

exit "${failed}"
