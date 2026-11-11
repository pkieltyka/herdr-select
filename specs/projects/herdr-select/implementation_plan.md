---
status: complete
---

# Implementation Plan: Herdr Select

Build against the [architecture](architecture.md), [functional spec](functional_spec.md), and [UI design](ui_design.md). The user approved the architecture and this plan for implementation.

## Phases

- [x] **Phase 1: Core and build foundation.** Pin the latest-major dependencies, add the executable entry point and Herdr adapter, implement name validation/recency ordering/error handling, and verify them with fake CLI fixtures. Complete the Nix package's vendor hash and obtain a real passing package build. Reuse the MIT license, flake scaffold, and README prepared during planning.
- [x] **Phase 2: Complete interactive selector.** Implement the approved picker, ram art, centered contextual footer, New Session form, confirmations, refresh/scroll/resize behavior, and asynchronous lifecycle sequencing. Add terminal handoff and behavioral/PTY coverage; validate against the approved previews and run the full Go/Nix checks.
- [ ] **Phase 3: Nix integration and release readiness.** Prepare the pinned GitHub flake input, package overlay, Home Manager package entry, and Ghostty keybinding change in the Nix configuration; validate with a local input override until published. Finish user documentation and installation verification. Publishing and system activation remain explicit external steps; report any such step still needed to make the shortcut live. Add the user's `docs/screenshot.png` when supplied, without blocking a working app on that optional image.

Each implementation phase includes code review before commit. Keep cross-repository changes separately reviewable and preserve unrelated work. Phase plans are written at implementation time, not during this planning flow.
