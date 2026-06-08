#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright (c) 2026 keese-ai
#
# conductor/intake.sh — turn a labelled GitHub issue into design→plan→implement→
# DRAFT PR, autonomously (no gates between stages) but never auto-merged. A human
# reviews and merges the draft. Idempotent/resumable: claim via assignee+label,
# each step checks its own prior marker. See docs/designs/29-conductor-orchestration.md.
#
# Usage:
#   conductor/intake.sh [--label LABEL] [--base REF] [--dry-run] [ISSUE]
#   (no ISSUE → poll the oldest unclaimed trusted-author LABEL issue)

set -euo pipefail
IFS=$'\n\t'

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
LIB="${HERE}/lib"
# shellcheck source=conductor/lib/common.sh
source "${LIB}/common.sh"
# shellcheck source=conductor/lib/conductor-utils.sh
source "${LIB}/conductor-utils.sh"
# shellcheck source=conductor/lib/agents.sh
source "${LIB}/agents.sh"
# shellcheck source=conductor/lib/registry.sh
source "${LIB}/registry.sh"

LABEL_TRIGGER="${INTAKE_LABEL:-agent:build}"
LABEL_ACTIVE="agent:in-progress"
LABEL_DONE="agent:done"
LABEL_FAILED="agent:failed"
BASE="main"
DRY_RUN=0
ISSUE=""

while (($# > 0)); do
  case "$1" in
    --label)
      LABEL_TRIGGER="$2"
      shift 2
      ;;
    --base)
      BASE="$2"
      shift 2
      ;;
    --dry-run)
      DRY_RUN=1
      shift
      ;;
    --*)
      log::err "unknown flag: $1"
      exit 2
      ;;
    *)
      ISSUE="$1"
      shift
      ;;
  esac
done

if ((DRY_RUN)); then
  cu::require jq >/dev/null || {
    log::err "jq required"
    exit 1
  }
else
  cu::require gh jq claude git >/dev/null || {
    log::err "gh + jq + claude + git required"
    exit 1
  }
fi

RUN_ID="intake-$(date -u +%Y%m%d-%H%M%S)"

# intake::do <description> -- <cmd...> — run a mutating command, or just print it
# under --dry-run.
intake::do() {
  local desc="$1"
  shift
  if ((DRY_RUN)); then
    local joined
    IFS=' ' joined="$*"
    log::info "[dry-run] ${desc}"
    log::dim "          \$ ${joined}"
    return 0
  fi
  log::info "${desc}"
  "$@"
}

# intake::pick — number of the oldest unclaimed issue from a trusted author.
intake::pick() {
  gh issue list --label "${LABEL_TRIGGER}" --search 'no:assignee sort:created-asc' \
    --json number,authorAssociation --limit 20 \
    | jq -r '[.[] | select(["OWNER","COLLABORATOR","MEMBER"] | index(.authorAssociation))]
             | (.[0].number // empty)'
}

# intake::agent_run <agent> <prompt> <cwd> — one foreground headless agent pass.
intake::agent_run() {
  local agent="$1" prompt="$2" cwd="$3"
  # shellcheck disable=SC2016  # $1/$2/$3 are intentionally expanded by the inner bash -c
  intake::do "stage ${agent} in ${cwd}" \
    bash -c 'cd "$1" && claude -p --agent "$2" --permission-mode dontAsk --fallback-model sonnet --output-format json "$3" >/dev/null' \
    _ "${cwd}" "${agent}" "${prompt}"
}

main() {
  # 1) resolve the issue (explicit, or poll).
  if [[ -z "${ISSUE}" ]]; then
    if ((DRY_RUN)); then
      log::info "[dry-run] would poll: gh issue list --label ${LABEL_TRIGGER} --search 'no:assignee sort:created-asc' and pick the oldest trusted-author issue"
      ISSUE="<N>"
    else
      ISSUE="$(intake::pick)"
      [[ -n "${ISSUE}" ]] || {
        log::ok "no unclaimed '${LABEL_TRIGGER}' issues from a trusted author"
        exit 0
      }
    fi
  fi

  # 2) fetch + gate on author association (write-access == trust).
  local title body assoc
  if ((DRY_RUN)); then
    title="(dry-run)"
    body="(dry-run)"
    assoc="OWNER"
  else
    local meta
    meta="$(gh issue view "${ISSUE}" --json number,title,body,authorAssociation)"
    assoc="$(jq -r '.authorAssociation' <<<"${meta}")"
    title="$(jq -r '.title' <<<"${meta}")"
    body="$(jq -r '.body' <<<"${meta}")"
  fi
  case "${assoc}" in
    OWNER | COLLABORATOR | MEMBER) ;;
    *)
      log::err "issue #${ISSUE} author association '${assoc}' is not trusted — refusing"
      exit 1
      ;;
  esac

  local slug="issue-${ISSUE}" branch wt
  branch="agent/${slug}"
  wt="$(paths::worktree_base)/${slug}"

  # 3) claim (idempotent lock).
  intake::do "claim #${ISSUE}: assign @me + label ${LABEL_ACTIVE}" \
    gh issue edit "${ISSUE}" --add-assignee "@me" --add-label "${LABEL_ACTIVE}"
  intake::do "ack comment on #${ISSUE}" \
    gh issue comment "${ISSUE}" --body "🤖 Conductor intake (${RUN_ID}) claimed this issue. Building design→plan→implement→draft PR; no auto-merge."

  # 4) worktree (resume if it already exists).
  if [[ -d "${wt}" ]]; then
    log::info "reusing existing worktree ${wt}"
  else
    intake::do "create worktree ${wt} (branch ${branch} from ${BASE})" \
      git -C "${REPO_ROOT}" worktree add -b "${branch}" "${wt}" "${BASE}"
  fi
  ((DRY_RUN)) || registry::record issue "${slug}" "issue #${ISSUE}: ${title}" "$$" "${wt}" ""

  # 5) design+plan (architect) then implement (implementer).
  local plan="docs/plans/phase-${slug}.md"
  intake::agent_run architect \
    "Handle GitHub issue #${ISSUE}: \"${title}\". Body:
${body}

Author an implementable plan phase at ${plan} (frontmatter MUST include \`issue: ${ISSUE}\`), scoped + scored against docs/plans/rubric.md. Commit it. Then stop — a separate implementer pass will build it." \
    "${wt}"
  intake::agent_run implementer \
    "Implement ${plan} in this worktree for issue #${ISSUE}. Commit per logical unit; run 'make lint' && 'make test'; fix failures. Follow your Conductor protocol. If you must ship a stub, declare it and set status: shipped-with-stubs." \
    "${wt}"

  # 6) draft PR (or fail comment).
  local commits=0
  ((DRY_RUN)) || commits="$(git -C "${wt}" rev-list --count "${BASE}..${branch}" 2>/dev/null || echo 0)"
  if ((DRY_RUN)) || ((commits > 0)); then
    intake::do "push ${branch}" git -C "${wt}" push -u origin "${branch}"
    intake::do "open draft PR (Closes #${ISSUE})" \
      gh pr create --draft --base "${BASE}" --head "${branch}" \
      --title "${title}" \
      --body "Closes #${ISSUE}

🤖 Opened by conductor intake (${RUN_ID}). Draft — review before merge."
    intake::do "label #${ISSUE} ${LABEL_DONE}" \
      gh issue edit "${ISSUE}" --remove-label "${LABEL_ACTIVE}" --add-label "${LABEL_DONE}"
    log::ok "issue #${ISSUE} → draft PR opened (no auto-merge)"
  else
    intake::do "comment manual-intervention on #${ISSUE}" \
      gh issue comment "${ISSUE}" --body "🤖 Conductor intake produced no commits for issue #${ISSUE}. Manual intervention required; branch ${branch} retained."
    intake::do "label #${ISSUE} ${LABEL_FAILED}" \
      gh issue edit "${ISSUE}" --remove-label "${LABEL_ACTIVE}" --add-label "${LABEL_FAILED}"
    ((DRY_RUN)) || registry::status "${slug}" failed "no commits"
    log::warn "issue #${ISSUE}: no commits — flagged for manual intervention"
  fi
}

main
