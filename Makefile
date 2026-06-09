# SPDX-License-Identifier: Apache-2.0
# Copyright (c) 2026 keese-ai
#
# keese — wrapper Makefile.
#
# Convention: targets that CI depends on by NAME are marked "CI" below.
# Renaming these is a cross-cutting change; update .github/workflows/*.yaml
# in the same commit.
#
# Targets whose body is "sdk" delegate to Makefile.operator-sdk-generated
# (emitted by `operator-sdk init` during P6). Until P6 lands, those targets
# are stubbed with informative errors.

SHELL := bash
.SHELLFLAGS := -euo pipefail -c
.ONESHELL:
.DEFAULT_GOAL := help

# Directories
REPO_ROOT        := $(shell git rev-parse --show-toplevel)
SCRIPTS_DIR      := $(REPO_ROOT)/scripts
BIN_DIR          := $(REPO_ROOT)/bin
DEV_DIR          := $(REPO_ROOT)/dev
DEPLOY_OPENTOFU  := $(REPO_ROOT)/deploy/opentofu

# Images (override via .env.local)
IMG             ?= ghcr.io/keese-ai/keese:dev
GOOSE_RUNTIME_IMG ?= ghcr.io/keese-ai/goose-runtime:dev
BUNDLE_IMG      ?= ghcr.io/keese-ai/keese-bundle:dev
COSIGN_WEBHOOK_IMG ?= ghcr.io/keese-ai/keese-cosign-webhook:dev

# Kubernetes versions (used by envtest + kubeconform + pluto)
K8S_VERSION     ?= 1.30.x
KIND_CLUSTER    ?= keese-dev

# Guard so targets don't accidentally run against prod contexts
GUARD_CONTEXT   := $(SCRIPTS_DIR)/guard-kube-context.sh

# ==== Informational =====================================================

.PHONY: help
help:  ## Show this help — grouped by section, source order
	@awk ' \
		BEGIN { FS = ":.*## " } \
		/^# ==== / { \
			line = $$0; \
			sub(/^# ==== /, "", line); \
			sub(/[= ]+$$/, "", line); \
			printf "\n\033[1m%s\033[0m\n", line; \
			next; \
		} \
		/^[a-zA-Z0-9_-]+:.*## / { \
			printf "  %-24s %s\n", $$1, $$2; \
		} \
	' $(MAKEFILE_LIST)

.PHONY: version
version:  ## Print tool versions
	@echo "go:             $$(go version 2>/dev/null || echo 'missing')"
	@echo "kubectl:        $$(kubectl version --client=true -o json 2>/dev/null | jq -r .clientVersion.gitVersion || echo 'missing')"
	@echo "kustomize:      $$(kustomize version 2>/dev/null || echo 'missing')"
	@echo "operator-sdk:   $$(operator-sdk version 2>/dev/null | head -n1 || echo 'missing')"
	@echo "controller-gen: $$(controller-gen --version 2>/dev/null || echo 'missing')"
	@echo "helm:           $$(helm version --short 2>/dev/null || echo 'missing')"
	@echo "helmfile:       $$(helmfile --version 2>/dev/null || echo 'missing')"
	@echo "kind:           $$(kind version 2>/dev/null || echo 'missing')"
	@echo "tilt:           $$(tilt version 2>/dev/null || echo 'missing')"
	@echo "tofu:           $$(tofu version 2>/dev/null | head -n1 || echo 'missing')"

# ==== Go hygiene (keese-owned) ==========================================

.PHONY: fmt
fmt:  ## gofumpt + goimports -local on api/ internal/ cmd/
	@if command -v gofumpt >/dev/null; then gofumpt -w api internal cmd 2>/dev/null || true; fi
	@if command -v goimports >/dev/null; then goimports -w -local github.com/keese-ai/keese api internal cmd 2>/dev/null || true; fi

.PHONY: vet
vet:  ## go vet ./...
	@if [ -f go.mod ]; then go vet ./...; else echo "go.mod not present yet (P6)"; fi

.PHONY: tidy
tidy:  ## go mod tidy (fails on drift)
	@if [ -f go.mod ]; then go mod tidy -diff; else echo "go.mod not present yet (P6)"; fi

.PHONY: vuln
vuln:  ## govulncheck ./...
	@if command -v govulncheck >/dev/null && [ -f go.mod ]; then govulncheck ./...; else echo "govulncheck or go.mod not present"; fi

# ==== Lint (CI) =========================================================

.PHONY: lint
lint:  ## golangci-lint + yamllint + markdownlint + shellcheck (CI)
	@pre-commit run --all-files

# ==== Test (CI) =========================================================

.PHONY: envtest-setup
envtest-setup:  ## setup-envtest install for $(K8S_VERSION)
	@if command -v setup-envtest >/dev/null; then \
		mkdir -p $(BIN_DIR); \
		setup-envtest use $(K8S_VERSION) --bin-dir $(BIN_DIR); \
	else \
		echo "setup-envtest missing; install via: go install sigs.k8s.io/controller-runtime/tools/setup-envtest@latest"; \
		exit 1; \
	fi

.PHONY: test-unit
test-unit:  ## go test -short ./... (CI)
	@if [ -f go.mod ]; then go test -short -race ./...; else echo "go.mod not present yet (P6)"; fi

.PHONY: test-integration
test-integration: envtest-setup  ## envtest-backed integration tests (CI) — requires //go:build integration
	@if [ -f go.mod ]; then \
		KUBEBUILDER_ASSETS="$$(setup-envtest use $(K8S_VERSION) -p path)" \
		go test -v -race -tags=integration ./internal/controller/... -timeout=20m; \
	else \
		echo "go.mod not present yet (P6)"; \
	fi

.PHONY: test-e2e
test-e2e:  ## kuttl against kind-$(KIND_CLUSTER)
	@$(GUARD_CONTEXT)
	@if command -v kuttl >/dev/null; then kubectl-kuttl test --config tests/e2e/kuttl-config.yaml; else echo "kuttl missing"; exit 1; fi

.PHONY: test-e2e-extended
test-e2e-extended:  ## kuttl extended suites: workspace-progression + agentruntime-drain + multi-tenant + chaos-network + cross-workspace + non-interactive-launcher + memory-backend-auth (requires live kind cluster + seeded OpenFGA + OpenBao)
	@$(GUARD_CONTEXT)
	@if ! command -v kubectl-kuttl >/dev/null 2>&1 && ! command -v kuttl >/dev/null 2>&1; then \
		echo "ERROR: kuttl (kubectl-kuttl) not found — install via Nix flake or brew install kuttl"; \
		exit 1; \
	fi
	@if ! kind get clusters 2>/dev/null | grep -q "$(KIND_CLUSTER)"; then \
		echo "ERROR: kind cluster '$(KIND_CLUSTER)' not found — run 'make kind-up && make bootstrap-infra' first"; \
		exit 1; \
	fi
	@bash tests/e2e/lib/check-prereqs.sh || { echo "ERROR: e2e prereqs not satisfied — see message above"; exit 1; }
	@KUTTL=$$(command -v kubectl-kuttl || command -v kuttl); \
	echo "Running extended e2e suites (workspace-progression, agentruntime-drain, multi-tenant, chaos-network, cross-workspace, non-interactive-launcher, memory-backend-auth)..."; \
	$${KUTTL} test tests/e2e/workspace-progression --config tests/e2e/kuttl-config.yaml && \
	$${KUTTL} test tests/e2e/agentruntime-drain --config tests/e2e/kuttl-config.yaml && \
	$${KUTTL} test tests/e2e/multi-tenant --config tests/e2e/kuttl-config.yaml && \
	$${KUTTL} test tests/e2e/chaos-network --config tests/e2e/kuttl-config.yaml && \
	$${KUTTL} test tests/e2e/cross-workspace --config tests/e2e/kuttl-config.yaml && \
	$${KUTTL} test tests/e2e/non-interactive-launcher --config tests/e2e/kuttl-config.yaml && \
	$${KUTTL} test tests/e2e/memory-backend-auth --config tests/e2e/kuttl-config.yaml

.PHONY: test-e2e-olm-upgrade
test-e2e-olm-upgrade:  ## kuttl OLM upgrade suite: install v1, upgrade to v2, assert cross-version stability (requires kind cluster + pre-loaded bundle images)
	@$(GUARD_CONTEXT)
	@if ! command -v kubectl-kuttl >/dev/null 2>&1 && ! command -v kuttl >/dev/null 2>&1; then \
		echo "ERROR: kuttl (kubectl-kuttl) not found — install via Nix flake or brew install kuttl"; \
		exit 1; \
	fi
	@if ! kind get clusters 2>/dev/null | grep -q "$(KIND_CLUSTER)"; then \
		echo "ERROR: kind cluster '$(KIND_CLUSTER)' not found — run 'make kind-up && make bootstrap-infra' first"; \
		exit 1; \
	fi
	@if ! command -v operator-sdk >/dev/null 2>&1; then \
		echo "ERROR: operator-sdk not found — install via Nix flake or https://sdk.operatorframework.io/docs/installation/"; \
		exit 1; \
	fi
	@KUTTL=$$(command -v kubectl-kuttl || command -v kuttl); \
	echo "Running OLM upgrade kuttl suite (tests/e2e/olm-upgrade)..."; \
	$${KUTTL} test tests/e2e/olm-upgrade --config tests/e2e/kuttl-config.yaml

.PHONY: coverage-check
coverage-check:  ## Per-package coverage gate vs test/coverage-targets.yaml (rule 06) (CI)
	@$(SCRIPTS_DIR)/coverage-check.sh

.PHONY: sigterm-drain-test
sigterm-drain-test:  ## SIGTERM drain contract for each long-running cmd/** pod (rule 06 §10); skips cleanly if no pod
	@$(GUARD_CONTEXT)
	@$(SCRIPTS_DIR)/dev/sigterm-drain-test.sh $(SIGTERM_TARGETS)

.PHONY: test
test: test-unit test-integration  ## Composed: unit + integration

.PHONY: conductor-test
conductor-test:  ## Conductor orchestrator unit tests (scheduler/parity/registry; needs bash>=4)
	@bash conductor/tests/run.sh

.PHONY: verify
verify: fmt vet lint test coverage-check bundle-validate  ## fmt+vet+lint+test+coverage-check+bundle-validate aggregator

# ==== Manifest + bundle generation (delegated) ==========================

.PHONY: manifests
manifests:  ## controller-gen: CRDs + RBAC + webhooks (CI)
	@if [ -f Makefile.operator-sdk-generated ]; then $(MAKE) -f Makefile.operator-sdk-generated manifests; else echo "Makefile.operator-sdk-generated not present yet (P6)"; fi

.PHONY: generate
generate:  ## controller-gen: deepcopy + zz_generated (CI)
	@if [ -f Makefile.operator-sdk-generated ]; then $(MAKE) -f Makefile.operator-sdk-generated generate; else echo "Makefile.operator-sdk-generated not present yet (P6)"; fi

.PHONY: bundle
bundle:  ## operator-sdk generate bundle (channels=alpha) (CI)
	@if [ -f Makefile.operator-sdk-generated ]; then $(MAKE) -f Makefile.operator-sdk-generated bundle CHANNELS=alpha DEFAULT_CHANNEL=alpha; else echo "Makefile.operator-sdk-generated not present yet (P6)"; fi

.PHONY: bundle-validate
bundle-validate:  ## operator-sdk bundle validate (CI)
	@if [ -d bundle ]; then \
		operator-sdk bundle validate ./bundle --select-optional suite=operatorframework; \
	else \
		echo "bundle/ not present yet (P6)"; \
	fi

.PHONY: bundle-build
bundle-build:  ## Build bundle container (CI)
	@if [ -f Makefile.operator-sdk-generated ]; then $(MAKE) -f Makefile.operator-sdk-generated bundle-build BUNDLE_IMG=$(BUNDLE_IMG); else echo "Makefile.operator-sdk-generated not present yet (P6)"; fi

.PHONY: bundle-sign-verify
bundle-sign-verify:  ## cosign verify keyless OIDC on $(BUNDLE_IMG) — required CI status check before catalog-push (CI)
	@bash $(SCRIPTS_DIR)/bundle-sign-verify.sh $(BUNDLE_IMG)

# ==== Feature gates (D27) ===============================================

.PHONY: featuregate-list
featuregate-list:  ## Print every keese FeatureGate with stage, override, effective value
	@bash $(SCRIPTS_DIR)/featuregate-list.sh

.PHONY: featuregate-diff
featuregate-diff:  ## Diff a candidate seed FeatureGate file (NEW=<file>) against current cluster state
	@if [ -z "$(NEW)" ]; then echo "usage: make featuregate-diff NEW=path/to/file.yaml"; exit 64; fi
	@kubectl diff -f $(NEW) || true

# ==== Docker image (CI) =================================================
#
# Local docker-build/push is a developer convenience that misses cosign
# signing. Production images publish via .github/workflows/image.yaml on
# tag push (CI-only credentials, GitHub OIDC keyless). The cosign-webhook
# fails-closed on any unsigned keese image reaching an InstallPlan, so
# locally-built images cannot land in a real cluster without a
# break-glass override (rule 05.13).
.PHONY: docker-build
docker-build:  ## buildx multi-arch operator image — local/dev only; CI runs image.yaml
	@docker buildx build --platform linux/amd64,linux/arm64 -t $(IMG) -f Dockerfile .

.PHONY: docker-push
docker-push:  ## push operator image — local/dev only; production uses CI tag flow
	@echo "WARN: local push is unsigned — production must use image.yaml on tag push"
	@docker push $(IMG)

.PHONY: cosign-webhook-build
cosign-webhook-build:  ## buildx multi-arch keese-cosign-webhook image (CI)
	@docker buildx build --platform linux/amd64,linux/arm64 -t $(COSIGN_WEBHOOK_IMG) -f Dockerfile.keese-cosign-webhook .

.PHONY: cosign-webhook-push
cosign-webhook-push:  ## push keese-cosign-webhook image — CI only
	@echo "WARN: local push is unsigned — production must use image.yaml on tag push"
	@docker push $(COSIGN_WEBHOOK_IMG)

.PHONY: goose-runtime-build
goose-runtime-build:  ## build goose-runtime image (block/goose + keese-drain) for local kind
	@docker build -t $(GOOSE_RUNTIME_IMG) -f Dockerfile.goose-runtime .

.PHONY: goose-runtime-load
goose-runtime-load: goose-runtime-build  ## kind load the goose-runtime image into the dev cluster
	@kind load docker-image $(GOOSE_RUNTIME_IMG) --name=$(KIND_CLUSTER)

# ==== Deploy / install (delegated) ======================================

.PHONY: deploy
deploy:  ## kustomize config/default | kubectl apply
	@$(GUARD_CONTEXT)
	@if [ -f Makefile.operator-sdk-generated ]; then $(MAKE) -f Makefile.operator-sdk-generated deploy IMG=$(IMG); else echo "Makefile.operator-sdk-generated not present yet (P6)"; fi

.PHONY: undeploy
undeploy:  ## kustomize config/default | kubectl delete
	@$(GUARD_CONTEXT)
	@if [ -f Makefile.operator-sdk-generated ]; then $(MAKE) -f Makefile.operator-sdk-generated undeploy; else echo "Makefile.operator-sdk-generated not present yet (P6)"; fi

.PHONY: install
install:  ## Install CRDs only
	@$(GUARD_CONTEXT)
	@if [ -f Makefile.operator-sdk-generated ]; then $(MAKE) -f Makefile.operator-sdk-generated install; else echo "Makefile.operator-sdk-generated not present yet (P6)"; fi

.PHONY: uninstall
uninstall:  ## Uninstall CRDs only
	@$(GUARD_CONTEXT)
	@if [ -f Makefile.operator-sdk-generated ]; then $(MAKE) -f Makefile.operator-sdk-generated uninstall; else echo "Makefile.operator-sdk-generated not present yet (P6)"; fi

# ==== Local dev (kind + tilt + helmfile) ================================

.PHONY: kind-up
kind-up:  ## ctlptl apply -f dev/kind/ctlptl.yaml
	@if command -v ctlptl >/dev/null; then ctlptl apply -f $(DEV_DIR)/kind/ctlptl.yaml; else kind create cluster --name=$(KIND_CLUSTER) --config=$(DEV_DIR)/kind/kind-config.yaml; fi

.PHONY: kind-down
kind-down:  ## delete kind cluster $(KIND_CLUSTER)
	@kind delete cluster --name=$(KIND_CLUSTER) || true

.PHONY: bootstrap-infra
bootstrap-infra:  ## helmfile sync dev/bootstrap/ + apply aigateway CRs
	@$(GUARD_CONTEXT)
	@echo "==> pre-applying chart-shipped CRDs (TD-P1-10)"
	@$(DEV_DIR)/bootstrap/install-crds.sh
	@helmfile -f $(DEV_DIR)/bootstrap/helmfile.yaml sync
	@echo "==> applying NATS streams"
	@kubectl apply -k $(DEV_DIR)/bootstrap/nats
	@echo "==> applying AI Gateway stack (Anthropic LLM path)"
	@kubectl apply -k $(DEV_DIR)/bootstrap/aigateway
	@echo "==> waiting for AIGatewayRoute Ready"
	@kubectl wait --for=condition=Accepted aigatewayroute/anthropic \
	  -n keese-system --timeout=180s || true
	@echo "==> bootstrap-infra complete"

.PHONY: tilt-up
tilt-up:  ## tilt up (hot-reload operator)
	@$(GUARD_CONTEXT)
	@TILT_HOST=$${TILT_HOST:-127.0.0.1} tilt up

.PHONY: tilt-down
tilt-down:  ## tilt down
	@tilt down

.PHONY: e2e-smoke
e2e-smoke:  ## End-to-end kind smoke (kind + bootstrap + operator + samples). Pass --no-keep to tear down.
	@bash $(SCRIPTS_DIR)/dev/e2e-smoke.sh $(MAKEFLAGS)

.PHONY: d5-smoke
d5-smoke:  ## D5 T1+T2 (Anthropic round-trip + memory persistence). Requires e2e-smoke --keep first.
	@bash $(SCRIPTS_DIR)/dev/d5-anthropic-smoke.sh

.PHONY: smoke
smoke:  ## Post-gate smoke test
	@$(GUARD_CONTEXT)
	@bash $(SCRIPTS_DIR)/dev/smoke.sh

# ==== OpenTofu (cloud) ==================================================

.PHONY: tofu-validate
tofu-validate:  ## tofu fmt -check + validate + conftest
	@for mod in $(DEPLOY_OPENTOFU)/aws $(DEPLOY_OPENTOFU)/gcp $(DEPLOY_OPENTOFU)/azure; do \
		if [ -d $$mod ]; then \
			(cd $$mod && tofu fmt -check && tofu init -backend=false >/dev/null && tofu validate); \
		fi; \
	done
	@if [ -d deploy/opentofu ] && [ -d policy/opentofu ]; then conftest test $(DEPLOY_OPENTOFU)/ -p policy/opentofu; fi

.PHONY: tofu-plan
tofu-plan:  ## tofu plan across aws/gcp/azure (read-only)
	@for mod in $(DEPLOY_OPENTOFU)/aws $(DEPLOY_OPENTOFU)/gcp $(DEPLOY_OPENTOFU)/azure; do \
		if [ -d $$mod ]; then \
			(cd $$mod && tofu init -backend=false >/dev/null && tofu plan -lock=false); \
		fi; \
	done

# ==== Doc tooling (delegated to template scripts) =======================

.PHONY: doc-check
doc-check:  ## lychee + markdownlint + frontmatter check
	@pre-commit run markdownlint --all-files
	@lychee --config lychee.toml --offline .

.PHONY: diagram-render
diagram-render:  ## Render D2 / Mermaid / Graphviz sources
	@$(SCRIPTS_DIR)/check-diagram-freshness.sh

.PHONY: plan-score
plan-score:  ## Pointer: use the plan-scorer agent on a doc path
	@echo "Dispatch the plan-scorer agent: /dispatch <phase-id> plan-scorer <path/to/doc.md>"

# ==== IDE configs =======================================================

.PHONY: ide-config
ide-config:  ## Stamp out .idea/ + .vscode/ debug configs from dev/ide/
	@if [ -d $(DEV_DIR)/ide/goland ]; then rsync -a $(DEV_DIR)/ide/goland/ .idea/; fi
	@if [ -d $(DEV_DIR)/ide/vscode ]; then rsync -a $(DEV_DIR)/ide/vscode/ .vscode/; fi

# ==== Design gate =======================================================

.PHONY: design-gate
design-gate:  ## Run the design-gate check locally
	@bash $(SCRIPTS_DIR)/check-design-gate.sh || true

.PHONY: verify-placeholders
verify-placeholders:  ## Fail if any {{TOKEN}} placeholder remains
	@bash $(SCRIPTS_DIR)/verify-placeholders.sh
