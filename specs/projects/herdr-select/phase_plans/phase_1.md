---
status: complete
---

# Phase 1: Core and build foundation

## Overview

Build and verify the domain model and bounded Herdr CLI adapter, pin the approved current-major libraries, and deliver a real Nix executable package. The interactive UI and lifecycle orchestration remain phase 2 work.

## Steps

1. Pin exact stable Charm dependency releases in `go.mod` and generate `go.sum`, using the locked Nixpkgs Go toolchain.
2. Add `internal/herdr/session.go` with `Session`, `ValidateNewName(name string, existing []Session) error`, `SortSessions([]Session)`, and `RelativeAge(modified, now time.Time) string`. Preserve identity and whitespace; model unavailable connection state and unknown timestamps explicitly.
3. Add `internal/herdr/client.go` with `Backend`, a resolved-path `Client`, and `List`, `Stop`, and `Delete` methods. Capture inherited environment and working directory, construct literal argv, enforce operation deadlines/output limits, and expose typed errors with uncertain mutation outcomes.
4. Strictly decode required JSON fields, default identity, duplicate names, acknowledgements, and trailing data. Stat regular `session.json` files and sort discovery results without treating metadata failures as discovery failures.
5. Add `cmd/herdr-select/main.go` and minimal `internal/app/run.go` startup scaffold with terminal checks and a clear interim phase-2 message. Keep interactive terminal ownership and screens for phase 2. Pin UI imports in a documented temporary dependency anchor so the vendor closure is reproducible before UI implementation.
6. Replace the flake vendor placeholder with the real fixed-output hash, preserving `pkgs.go`, and update README build-status wording.

## Tests

- `TestValidateNewName`: ASCII boundaries, whitespace, reserved names, special option-like names, case, exact duplicates, and length limits.
- `TestSortSessions` and `TestRelativeAge`: known/unknown/future/tied timestamps and age-unit boundaries.
- `TestClientList`: real helper executable argv, inherited environment/directory, metadata, unavailable rows, empty lists, and malformed/missing/null/duplicate/inconsistent JSON.
- `TestClientMutations`: exact names including help and option-like values; matching acknowledgements; malformed results; nonzero exits; structured/plain stderr.
- `TestClientBounds`: cancellation, timeout, output overflow, inherited pipe delays, and uncertain mutation reporting using disposable fixtures.
- Run formatting, Go build/vet/tests/race tests, `nix build`, and `nix flake check` with the pinned toolchain.
