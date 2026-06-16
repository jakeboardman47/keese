# SPDX-License-Identifier: Apache-2.0
# Copyright (c) 2026 keese-ai

{
  description = "keese — reproducible dev environment";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-25.05";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs = { self, nixpkgs, flake-utils }:
    flake-utils.lib.eachDefaultSystem (system:
      let
        pkgs = import nixpkgs { inherit system; };
        isLinux = pkgs.lib.hasSuffix "-linux" system;

        # Upstream nixpkgs ships open-policy-agent 1.6.0 with a broken test
        # suite (v1/server/server_bench_test.go references undefined symbols
        # `newReqV1` and `fixture`). Skip checks until the next bump.
        open-policy-agent = pkgs.open-policy-agent.overrideAttrs (_: {
          doCheck = false;
        });
      in
      {
        devShells.default = pkgs.mkShell {
          name = "keese";

          packages = with pkgs; [
            # ===== Supply-chain / signing =====
            cosign
            syft
            trivy
            gitleaks
            detect-secrets
            sops
            age

            # ===== Pre-commit & commits =====
            pre-commit
            commitizen
            nodejs_20           # for commitlint
            git-lfs

            # ===== Shell & doc tooling =====
            bashInteractive
            shellcheck
            shfmt
            yq-go
            jq
            markdownlint-cli
            lychee

            # ===== Diagrams (text → SVG) =====
            d2
            graphviz
            mermaid-cli

            # ===== Documentation (mkdocs) =====
            python312
            python312Packages.mkdocs
            python312Packages.mkdocs-material

            # ===== Release =====
            gh

            # ===== Dev niceties =====
            direnv
            gnumake
            tree
            curl
            wget

            # ===== Go toolchain =====
            # Some of these may lag in nixpkgs stable; if a build fails,
            # see nix/overlays/operator-tools.nix for the `go install`
            # fallback. Unverified-in-nixpkgs packages are marked below.
            go                 # 1.24+ expected from nixos-unstable
            gopls
            delve
            golangci-lint
            gotools            # goimports, stringer, etc.
            govulncheck
            gofumpt

            # ===== Kubernetes / Operator SDK tooling =====
            kubectl
            kubernetes-helm
            helmfile
            kustomize
            kubebuilder
            kind
            tilt
            stern
            k9s
            kubeconform
            pluto
            kuttl                # nixpkgs attr `kuttl` ships the
                                 # `kubectl-kuttl` binary (TD-P1-07).
            # operator-sdk       # unverified; overlay fallback
            # setup-envtest      # unverified; go-install fallback
            # controller-gen     # unverified nixpkgs naming
            ctlptl
            cfssl
            # cmctl              # unverified nixpkgs naming

            # ===== OpenTofu / policy =====
            opentofu
            # tflint             # unverified naming in nixpkgs
            # terraform-ls       # works with tofu; verify naming
            conftest
            open-policy-agent

            # ===== Container tooling =====
            crane
            skopeo

            # ===== Argo Workflows CLI =====
            # argo               # unverified nixpkgs name (may be argo-workflows-cli)
          ]
          ++ pkgs.lib.optionals isLinux [
            iproute2
            iputils
            tcpdump
          ]
          ++ pkgs.lib.optionals (!isLinux) [
            pkgs.apple-sdk
          ];

          shellHook = ''
            echo ""
            echo "  keese dev shell"
            echo ""
            echo "  Next steps:"
            echo "    1. cp .env.local.example .env.local    # fill in secrets"
            echo "    2. pre-commit install --install-hooks"
            echo "    3. git lfs install"
            echo "    4. make help"
            echo ""
          '';
        };
      });
}
