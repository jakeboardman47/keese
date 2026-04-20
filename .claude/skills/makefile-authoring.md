---
name: makefile-authoring
description: Author Makefiles with self-documenting grouped help targets and two-stage signal-safe recipes
type: skill
depends: []
options: []
model: sonnet
---

<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) {{YEAR}} {{ORG_NAME}} -->

# Makefile Authoring

## When to use

Editing the root `Makefile`, any `*.mk` include, or a script a recipe invokes
to run a long-lived process.

## Invariants

1. Every user-facing target has a `## description` on the target line.
2. Targets are grouped by `##@ GroupName` lines.
3. `make help` prints grouped targets and is the default goal.
4. Recipes are signal-safe: first SIGINT/SIGTERM → graceful shutdown (or
   immediate stop if no graceful path exists); second signal → force
   terminate. No orphaned children.

SIGSTOP and SIGKILL are uncatchable, so we approximate their contract at the
first catchable layer (INT/TERM). The *second* signal maps to an in-process
SIGKILL of children — "I said stop; now really stop."

## Preamble (required)

```make
SHELL        := /usr/bin/env bash
.SHELLFLAGS  := -Eeuo pipefail -c
MAKEFLAGS    += --no-print-directory --warn-undefined-variables
.DEFAULT_GOAL := help
.DELETE_ON_ERROR:
.ONESHELL:
```

- `.ONESHELL:` — one shell per recipe, required for `trap` to span lines.
- `.DELETE_ON_ERROR:` — remove partial artifacts on failure.
- `-E` — propagate `ERR` traps into subshells; `-u` — catch var typos.

## Self-documenting targets & groups

```make
##@ Development

build: ## Build the binary
	go build -o bin/app ./cmd/app

test: ## Run unit + integration with race detector
	$(MAKE) test-unit test-integration

##@ Release

release: ## Cut a release
	...
```

- `##` after the colon, same line. Description ≤ 72 chars, imperative.
- `##@ GroupName` on its own line, blank line above. Targets inherit the
  nearest preceding group.
- Internal targets omit `##` and stay out of help.

## The help target (include `mk/help.mk` across sub-Makefiles)

```make
##@ General

.PHONY: help
help: ## Show this help
	@awk 'BEGIN {FS = ":.*##"; \
	  printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} \
	  /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5); next } \
	  /^[a-zA-Z0-9_\/-]+:.*?##/ { printf "  \033[36m%-24s\033[0m %s\n", $$1, $$2 }' \
	  $(MAKEFILE_LIST)
```

Respect `NO_COLOR=1` if present — strip the `\033[...m` sequences by setting
them to empty strings in the `BEGIN` block.

## Signal-safe recipes

**Contract.** First signal = graceful shutdown + grace timer. Second signal
= SIGKILL to child, exit 137. SIGKILL to Make itself is uncatchable; we
mitigate by launching long children through a supervising script so the
kernel reaps the child tree when Make's shell dies.

**Makefile:** never put trap logic in a recipe. Call a supervised script:

```make
##@ Development

dev-run: ## Run the app against $$CONFIG
	@$(SCRIPTS)/dev-run.sh
```

**`scripts/dev-run.sh`:**

```bash
#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright (c) {{YEAR}} {{ORG_NAME}}
set -Eeuo pipefail
IFS=$'\n\t'

source "$(dirname "$0")/lib/log.sh"
source "$(dirname "$0")/lib/signals.sh"

log::info "starting app"
go run ./cmd/app &
signals::supervise $!
```

**`scripts/lib/signals.sh`** (author once; source everywhere):

```bash
# shellcheck shell=bash
[[ -n "${__LIB_SIGNALS_SH_LOADED:-}" ]] && return 0
__LIB_SIGNALS_SH_LOADED=1

: "${SIGNALS_GRACE_SECONDS:=15}"
__signals_stage=0
__signals_child_pid=

__signals_on_signal() {
  if (( __signals_stage == 0 )); then
    __signals_stage=1
    log::warn "graceful shutdown (SIGTERM -> pid ${__signals_child_pid})"
    kill -TERM "${__signals_child_pid}" 2>/dev/null || true
    ( sleep "${SIGNALS_GRACE_SECONDS}"
      kill -KILL "${__signals_child_pid}" 2>/dev/null || true ) &
  else
    log::err "force quit (SIGKILL -> pid ${__signals_child_pid})"
    kill -KILL "${__signals_child_pid}" 2>/dev/null || true
    exit 137
  fi
}

# signals::supervise <child_pid>
signals::supervise() {
  __signals_child_pid="$1"
  trap __signals_on_signal INT TERM
  # Loop-wait: a trap interrupts `wait`; we must re-wait after handling.
  while kill -0 "${__signals_child_pid}" 2>/dev/null; do
    wait "${__signals_child_pid}" 2>/dev/null || true
  done
  trap - INT TERM
}
```

Tools that already handle signals well (`go test`, compilers) do not need the
supervisor — Make forwards SIGINT to the shell, which forwards to the tool.
Only use `signals::supervise` for long-lived / daemonizing children.

## Anti-patterns

- `trap` on a single recipe line without `.ONESHELL:` — trap covers one line only.
- Bare long-running command in a recipe — trap won't fire until it exits;
  use `cmd & signals::supervise $!`.
- `kill -9` as the *first* response — breaks the two-stage contract.
- Hand-rolled help parsing in each sub-Makefile — `include mk/help.mk`.
- Secrets in `Makefile` vars — use `include .env.local` (gitignored).
- `.ONESHELL:` inconsistently applied — it's global; pick one model per file.

## Verification

- `make help` renders grouped targets.
- One Ctrl-C → graceful shutdown within `SIGNALS_GRACE_SECONDS`.
- Two Ctrl-C → immediate termination; no orphans in `ps -ef`.
- `shellcheck scripts/lib/signals.sh scripts/<script>.sh` clean.
- `shfmt -i 2 -ci -bn -d` no-op.

## References

- [../rules/01-conventions.md](../rules/01-conventions.md) — Bash conventions.
- `scripts/lib/log.sh` — `log::` helpers and `run::step` mutation boundary.
