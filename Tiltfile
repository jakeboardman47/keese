# SPDX-License-Identifier: Apache-2.0
# Copyright (c) 2026 keese-ai
#
# Tiltfile — keese operator local development loop.
#
# Sections:
#   1. Preflight  — assert kind cluster context is reachable
#   2. Infra      — helmfile bootstrap (cert-manager → full stack)
#   3. Seeds      — OpenFGA model + NATS streams + OpenBao placeholders
#   4. Operator   — hot-reload Go build + live_update + dlv port-forward
#   5. CRDs/RBAC  — kustomize config/overlays/dev applied after infra
#
# Components intentionally NOT observed by Tilt (still installed by helmfile):
#   kyverno, argo-workflows, qdrant, eck APM Server — defer to later phases.
#
# Refs: docs/references/tilt-local-loop.md
#       dev/bootstrap/README.md

load('ext://restart_process', 'docker_build_with_restart')

# ── 1. Preflight ──────────────────────────────────────────────────────────────

local_resource(
    "kind-ready",
    cmd = "kubectl cluster-info --context kind-keese-dev",
    labels = ["preflight"],
)

# ── 2. Infrastructure (helmfile) ──────────────────────────────────────────────

local_resource(
    "bootstrap-infra",
    cmd = "helmfile -f dev/bootstrap/helmfile.yaml sync",
    deps = [
        "dev/bootstrap/helmfile.yaml",
        "dev/bootstrap/values",
    ],
    resource_deps = ["kind-ready"],
    labels = ["infra"],
)

# ── 3a. OpenFGA seed ──────────────────────────────────────────────────────────

k8s_yaml(kustomize("dev/bootstrap/openfga"))

k8s_resource(
    "openfga-seed",
    resource_deps = ["bootstrap-infra"],
    labels = ["seeds"],
)

# ── 3b. NATS streams ─────────────────────────────────────────────────────────

k8s_yaml(kustomize("dev/bootstrap/nats"))

# ── 3c. OpenBao seed (local script — requires manual unseal first) ─────────────

local_resource(
    "openbao-seed",
    cmd = "scripts/dev/seed-openbao.sh",
    resource_deps = ["bootstrap-infra"],
    labels = ["seeds"],
)

# ── 4. Operator hot-reload ────────────────────────────────────────────────────

# Build the manager binary on the host with debug symbols (no inlining).
# Tilt rebuilds whenever Go source files change.
local_resource(
    "compile-manager",
    cmd = "CGO_ENABLED=0 GOOS=linux go build -gcflags='all=-N -l' -o bin/manager ./cmd/main.go",
    deps = [
        "cmd",
        "internal",
        "api",
    ],
    labels = ["operator"],
)

# Build the operator container image using the pre-compiled binary.
# live_update syncs the binary into the running container and restarts it,
# giving ~5–12s feedback loops without a full image rebuild.
docker_build_with_restart(
    "keese-ai/keese-operator:dev",
    ".",
    dockerfile = "Dockerfile.dev",
    entrypoint = ["/manager"],
    live_update = [
        sync("./bin/manager", "/manager"),
    ],
    only = ["bin/manager"],
)

# ── 5. CRDs + operator deployment ────────────────────────────────────────────

k8s_yaml(kustomize("config/overlays/dev"))

k8s_resource(
    "keese-controller-manager",
    new_name = "keese-operator",
    port_forwards = [
        "2345:2345",  # dlv debugger
        "8080:8080",  # health / metrics
    ],
    resource_deps = ["bootstrap-infra", "openfga-seed", "compile-manager"],
    labels = ["operator"],
)
