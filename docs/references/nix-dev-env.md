<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: reference
category: reference
depends: []
related_skills: []
status: current
last_verified: 2026-04-19
---

# Nix Dev Environment

The dev shell is the single source of truth for toolchains. CI uses the same
`flake.nix`; there is no "works on my laptop" path.

## Enter the Shell

```sh
nix develop           # interactive subshell
nix develop -c make test-integration   # one-shot
```

With direnv installed, `cd` into the repo auto-activates the shell via `.envrc`:

```
# .envrc
use flake
dotenv_if_exists .env.local
```

First entry warms the cache (~60s). Subsequent entries are instant.

## What's In It

See `flake.nix` for the authoritative list. Do not duplicate the list here —
it drifts.

## Adding a Tool

1. Edit `flake.nix`; add the package to `commonPkgs` or the platform-specific list.
2. If you bumped a flake **input** (e.g. nixpkgs channel):
   ```sh
   nix flake update
   ```
3. Reload the shell:
   ```sh
   direnv reload        # or: exit the subshell and re-run nix develop
   ```
4. Commit `flake.nix` + `flake.lock` in one commit: `build(nix): add <tool>`.

## Binary Caches

Use the public nixpkgs cache (default) or cachix for faster cold starts. Example
user-level cachix:

```sh
cachix use nix-community     # optional, user choice
```

Do not hard-code organization-specific caches in `flake.nix` — that stays a
per-user decision in `~/.config/nix/nix.conf`.

## Linux-Only Packages

Some tooling only builds on Linux. Gate them in `flake.nix`:

```nix
let
  isLinux = pkgs.stdenv.isLinux;
  linuxOnly = pkgs.lib.optionals isLinux [
    pkgs.qemu
    pkgs.libvirt
  ];
in
pkgs.mkShell {
  buildInputs = commonPkgs ++ linuxOnly;
}
```

## macOS Considerations

- `apple-sdk` is pulled in conditionally for CGo links against darwin-only libs.
- If `nix develop` fails with `unable to find apple-sdk`, update to nixpkgs
  `>= 24.11`.

## Troubleshooting

- **`nix develop` hangs on "evaluating"** — kill, `rm -rf ~/.cache/nix/eval-cache-v5`, retry.
- **Wrong toolchain version** — you're not in the shell. Run `which <tool>` and confirm path is under `/nix/store`.
- **Tool not found after `flake.nix` edit** — you forgot `direnv reload` / re-enter.

## Related

- Files: `flake.nix`, `flake.lock`, `.envrc`.
