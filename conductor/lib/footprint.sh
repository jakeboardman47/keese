# SPDX-License-Identifier: Apache-2.0
# Copyright (c) 2026 keese-ai
#
# Predict the filesystem "footprint" of a phase and decide whether two phases
# would collide if run in parallel. A footprint is a set of coarse DOMAIN tokens
# (a CRD kind, a controller, a Go package) plus HOT shared paths (go.mod/PROJECT,
# the generated deepcopy/CRD bases, the OLM CSV, the ReBAC model) that many
# phases touch. Two phases conflict if their domain/hot sets intersect. Tuned
# for keese's Go Kubernetes-operator layout. Source me; do not execute.
# shellcheck shell=bash

if [[ -n "${__LIB_FOOTPRINT_SH_LOADED:-}" ]]; then
  return 0
fi
__LIB_FOOTPRINT_SH_LOADED=1

# footprint::token_for_path <path> — echo zero or more tokens (one per line) for
# a single path. Hot/shared paths are prefixed "HOT:". Docs/book/scripts emit
# nothing (they rarely hard-conflict and would over-serialise the schedule).
footprint::token_for_path() {
  local p="$1"
  # strip a leading "./" and any trailing slash
  p="${p#./}"
  p="${p%/}"
  case "${p}" in
    # ── HOT shared paths: any two phases touching these collide on merge ──
    go.mod | go.sum | PROJECT | Makefile | Makefile.operator-sdk-generated) echo "HOT:deps" ;;
    config/crd/kustomization.yaml | config/rbac/role.yaml | config/rbac/role_extras.yaml | config/rbac/role_binding.yaml) echo "HOT:manifests" ;;
    cmd/main.go) echo "HOT:main-mount" ;;
    *zz_generated.deepcopy.go) echo "HOT:deepcopy" ;;
    bundle/* | config/manifests/*) echo "HOT:olm" ;;
    # ── ReBAC: serialize the (solo) OpenFGA model author ──
    *model.fga | *.fga | internal/authz/* | internal/rebac/*) echo "HOT:rebac" ;;
    # ── domain tokens: non-overlapping → safe to parallelize ──
    api/*/v1alpha1/*)
      # api:<group>/<kind> from api/<group>/v1alpha1/<kind>_types.go; plus a
      # group-level gen lock — same-group CRD changes both regenerate that
      # group's zz_generated.deepcopy.go and must serialize.
      local rest="${p#api/}" group tail kind
      group="${rest%%/*}"
      tail="${rest##*/}"
      kind="${tail%_types.go}"
      kind="${kind%.go}"
      echo "api:${group}/${kind}"
      echo "HOT:gen:${group}"
      ;;
    internal/controller/*)
      # ctrl:<group>/<kind> (controllers grouped by api group then kind)
      local rest="${p#internal/controller/}" group kind
      group="${rest%%/*}"
      rest="${rest#*/}"
      kind="${rest%%/*}"
      echo "ctrl:${group}/${kind}"
      ;;
    config/crd/bases/*)
      # crd:<group> from config/crd/bases/<group>_<plural>.yaml
      local f="${p##*/}"
      echo "crd:${f%%_*}"
      ;;
    internal/admission/* | internal/webhook/*) echo "go:webhook" ;;
    cmd/*)
      local rest="${p#cmd/}"
      echo "cmd:${rest%%/*}"
      ;;
    internal/*)
      # go:internal/<pkg> (first segment)
      local rest="${p#internal/}"
      echo "go:internal/${rest%%/*}"
      ;;
    docs/* | book/* | .claude/* | scripts/* | test/* | deploy/* | dev/* | hack/*) : ;; # non-conflicting here
    *) : ;;
  esac
}

# footprint::_heuristic <phase-id> — coarse fallback token when a phase declares
# no outputs: and mentions no paths. Keyed off keese's phase-ID prefixes. This
# is a last resort; phases SHOULD declare outputs: (the merge green gate is the
# backstop when they under-declare). Distinct expansion phases get distinct
# tokens so the heuristic never collapses the whole wave to one phase.
footprint::_heuristic() {
  local id="$1"
  case "${id}" in
    E*) echo "go:expansion/${id}" ;; # ecosystem-expansion CRDs/runtimes
    D*) echo "go:demo" ;;            # demo-track wiring
    *) echo "go:backend" ;;
  esac
}

# footprint::for_phase <phase-id> <phase-file> <frontmatter-json> — emit
# {"domains":[...],"hot":[...]} for a phase, from outputs frontmatter + body
# paths + heuristic fallback.
footprint::for_phase() {
  local id="$1" file="$2" fm="$3"
  local -a tokens=()
  local line tok

  # 1) outputs: frontmatter — take the first whitespace token of each entry
  #    (entries may carry a "(description)" suffix).
  while IFS= read -r line; do
    [[ -n "${line}" ]] || continue
    local path="${line%% *}"
    while IFS= read -r tok; do tokens+=("${tok}"); done < <(footprint::token_for_path "${path}")
  done < <(printf '%s' "${fm}" | jq -r '(.outputs // []) | .[]?' 2>/dev/null)

  # 2) body paths — ONLY as a fallback when outputs: declared nothing. When a
  #    phase declares outputs:, trust them exclusively: grepping the body
  #    otherwise pulls in paths merely *mentioned* in gap-analysis/prose (e.g.
  #    `internal/http` or `apps/manager` cited as a dependency, not an edit),
  #    which inflates the footprint and collapses every wave to a single phase.
  #    Authors MUST declare outputs: accurately; the merge green gate +
  #    rebase-conflict detection are the backstop if a phase under-declares.
  if ((${#tokens[@]} == 0)) && [[ -f "${file}" ]]; then
    while IFS= read -r path; do
      [[ -n "${path}" ]] || continue
      while IFS= read -r tok; do tokens+=("${tok}"); done < <(footprint::token_for_path "${path}")
    done < <(grep -oE '(api|internal|cmd|config|bundle)/[A-Za-z0-9._/-]+' "${file}" 2>/dev/null | sort -u)
  fi

  # 3) heuristic fallback when nothing concrete was found.
  if ((${#tokens[@]} == 0)); then
    while IFS= read -r tok; do tokens+=("${tok}"); done < <(footprint::_heuristic "${id}")
  fi

  footprint::_emit "${tokens[@]}"
}

# footprint::for_diff <name-list...> on stdin — emit footprint JSON from a list
# of changed file paths (one per line). Used by worktree-refresh for the real
# diff of a running branch.
footprint::for_diff() {
  local -a tokens=()
  local path tok
  while IFS= read -r path; do
    [[ -n "${path}" ]] || continue
    while IFS= read -r tok; do tokens+=("${tok}"); done < <(footprint::token_for_path "${path}")
  done
  footprint::_emit "${tokens[@]}"
}

# footprint::_emit <token...> — split tokens into domains/hot, dedupe, print JSON.
footprint::_emit() {
  local -a domains=() hot=()
  local t
  for t in "$@"; do
    if [[ "${t}" == HOT:* ]]; then
      hot+=("${t}")
    elif [[ -n "${t}" ]]; then
      domains+=("${t}")
    fi
  done
  jq -nc \
    --argjson d "$(printf '%s\n' "${domains[@]:-}" | jq -R . | jq -s 'map(select(length>0))|unique')" \
    --argjson h "$(printf '%s\n' "${hot[@]:-}" | jq -R . | jq -s 'map(select(length>0))|unique')" \
    '{domains:$d, hot:$h}'
}

# footprint::conflicts <jsonA> <jsonB> — return 0 (true) if the two footprints
# share any domain or hot token.
footprint::conflicts() {
  local a="$1" b="$2" overlap
  overlap="$(jq -n --argjson a "${a}" --argjson b "${b}" \
    '(($a.domains + $a.hot) - (($a.domains + $a.hot) - ($b.domains + $b.hot))) | length')"
  [[ "${overlap}" -gt 0 ]]
}
