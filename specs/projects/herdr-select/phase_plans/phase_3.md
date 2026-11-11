---
status: complete
---

# Phase 3: Nix integration and release readiness

## Overview

Finish documentation for the working selector and prepare a separately reviewable Nix configuration integration without publishing, activating, or modifying the user's live configuration.

## Steps

1. Update `README.md` to describe the working application, local and published installation, terminal requirements, session behavior, and the remaining publication/activation steps. Preserve the centered `docs/logo.png` and optional screenshot placeholder.
2. Replace obsolete scaffold instructions in `docs/nix.md` with current package outputs, reproducible toolchain details, installation commands, consumer configuration, and publication/lock/activation sequencing.
3. Snapshot the current `~/.nix-config` working files into an isolated Git checkout, preserving its staged and unstaged content as the validation baseline. Record read-only baseline hashes for the live working files and index.
4. Prepare `docs/nix-config.patch` containing only the GitHub flake input, auto-discovered overlay, Home Manager package, and existing Ghostty shortcut change. Keep the selector's Nixpkgs input independent; exclude temporary absolute-path locks.
5. Apply that exact patch to an isolated baseline checkout, use a local input override for validation, and run the consumer's evaluation/quality checks and cooldown inspection. Document the final remote lock step and its cooldown requirement after publication.
6. Verify the built package's install layout and noninteractive startup behavior using disposable state, run the project checks, and compare the live configuration's files/index with the recorded baseline.

## Tests

- Existing Go unit, race, and vet checks pass in the pinned development environment; Go source remains formatted.
- `nix build` and `nix flake check` validate the working package and app outputs.
- A temporary Nix profile installs the package and its executable reports the expected noninteractive-terminal error without touching personal sessions.
- The consumer patch applies cleanly to the copied current baseline; only the intended four files change.
- Consumer `make test`, `make check`, and `make cooldown` run in the isolated integration; investigate and report baseline/environment failures without changing unrelated files.
- Targeted consumer evaluations confirm the package is in Home Manager and the Ghostty binding invokes `herdr-select` on the configured Linux hosts.
- Live Nix configuration working-file and Git-index hashes are unchanged after verification.
