---
status: complete
---

# Phase 2: Complete interactive selector

## Overview

Implement the approved keyboard-driven selector, guarded lifecycle operations, and terminal handoff while preserving Herdr's ownership of session state.

## Steps

1. Add `internal/ui/model.go` with explicit loading, picker, form, confirmation, startup error, and discovery error states; preserve selection by identity, capture operation IDs, handle scrolling/resizing, and schedule one age tick.
2. Add `internal/ui/commands.go` for asynchronous list, create/open preflight, confirmation preflight, and mutation sequencing. Recheck captured identities and state, refresh after outcomes, and report stopped-but-not-deleted partial success.
3. Add `internal/ui/styles.go`, `view.go`, and embedded `ram.txt`; render the approved colors, padded panel, centered contextual footer, compact fallback, form focus/validation, and wrapped/scrolled diagnostics within terminal dimensions.
4. Replace the phase 1 app placeholder with terminal validation, startup warnings, Bubble Tea ownership, launch recovery on Start failure, and inherited-terminal child waiting with exit/signal propagation.
5. Add behavioral/render tests for UI transitions and lifecycle safety, and disposable-helper PTY/process tests for handoff and terminal restoration. Capture a real UI render outside `docs/screenshot.png` for comparison with approved previews.
6. Run formatting, Go tests/race/vet/build and the pinned Nix package/flake checks; retain package-wide `go test ./...` coverage.

## Tests

- Picker selection, refresh identity, deletion fallback, stale-result rejection, contextual footer and default/unavailable restrictions.
- Form validation, focus cycling, text shortcuts, unsafe/oversized paste rejection, late duplicates, discovery retry, and hidden footer through submission.
- Confirmation cancellation, revalidation, changed-state reconfirmation, stop-before-delete ordering, uncertain stop failure, partial deletion failure, and stale-list mutation blocking.
- Wide/narrow/tiny renders fit dimensions, preserve metadata and entered names, center footer badges, collapse ram art, and scroll long content with reachable buttons.
- Interactive child receives a PTY after UI cleanup, emits diagnostics, returns its exit status, and handles interrupt/termination without touching persistent servers; Start failure restores the saved UI.
