# SPDX-License-Identifier: Apache-2.0
# Copyright (c) 2026 keese-ai

{
  description = "keese — reproducible dev environment";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs = { self, nixpkgs, flake-utils }:
    flake-utils.lib.eachDefaultSystem (system:
      let
        pkgs = import nixpkgs { inherit system; };
        isLinux = pkgs.lib.hasSuffix "-linux" system;
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
            nodePackages.mermaid-cli

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

            # ===== Add your language toolchain here =====
            # Examples — uncomment what you need:
            # go
            # golangci-lint
            # gopls
            # nodejs_20
            # python312
            # rustup
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
