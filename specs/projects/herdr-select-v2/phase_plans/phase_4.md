---
status: complete
---

# Phase 4: Docs and packaging

## Overview

Finish v2 for users and packagers. The README describes both protocols, `--app`, how the client is chosen by default, the herdr-app single-instance limit and how v2 delete works. The Nix setup guide mentions herdr-core. The flake gets the new descriptions and the `vendorHash` for the go-toml dependency added in phase 2. Then the full check suite (`go test`, `go test -race`, `go vet`, `nix build`, `nix flake check`) runs, followed by the end-to-end QA gate from the implementation plan, driven through an isolated hc core. No Go code changes are planned; a defect found by the QA gate is fixed with a regression test.

## Steps

1. **`README.md`**:
   - Tagline and intro: a session picker for herdr (v1) and herdr-core (v2) sessions.
   - "How it works": the two protocols (v1 through the `herdr` CLI; v2 through herdr-core's state folders and sockets, with `hc` only to stop), and that the UI is the same for both.
   - New "Choosing herdr or herdr-core" section: `--app=<client>` / `--app <client>`, `--app=herdr` (base name `herdr`) is v1, anything else is a herdr-core client; the default table (herdr, else herdr-app, else an error); `rodeo` only through `--app`; v2 needs `hc` too; usage errors exit 2.
   - v2 details: Running/Stopped/Unavailable from the socket probe; recency from the newest file in the session's state folder; listing never starts a core; `HC_SESSION` set for the client; `HC_SOCKET` / fixed socket refused.
   - herdr-app single-instance limit and its "already open" error.
   - Delete: v1 through `herdr`; v2 removes the stopped session's state folder and leftover sockets after a recheck; `default` is never deletable.
   - Names: v1 rules unchanged; v2 rules (spaces inside allowed, no `/ \ : * ? " < > |`, no leading/trailing spaces, socket-path length).
   - Nesting: refuses inside a herdr pane or an hc terminal (rodeo/herdr-app panes).
   - Installation: herdr and/or herdr-core (`hc` plus a client).
   - Development: link the v2 spec folder next to the v1 one.
2. **`docs/nix.md`**: the install line names herdr or herdr-core, and running outside any herdr pane or hc terminal; mention that the shortcut can pass `--app`.
3. **`flake.nix`**: `description` and `meta.description` become "A terminal session picker for herdr and herdr-core"; the app's `meta.description` becomes "Choose a herdr or herdr-core session"; recompute `vendorHash` (build with a fake hash, take the reported one).
4. **Checks**: `gofmt -l`, `go vet ./...`, `go test ./...`, `go test -race ./...`, `nix build`, `nix flake check`.
5. **End-to-end QA gate** (per the implementation plan): isolated `HOME`, `XDG_STATE_HOME`, `XDG_RUNTIME_DIR`, `XDG_CONFIG_HOME`, `HERDR_APP_HOME` and a fresh `HC_SESSION`, throwaway hc sessions (running, stopped, stale socket); run the built binary in an hc terminal with `hc run --bg` and `env -u HC_TERMINAL_ID`; drive with `hc keys`, check with `hc read`: listing, v2 hint and name errors, stop, delete (folder and sockets gone), herdr-app "already open" with a fake live `herdr-app.sock`, and a stub `rodeo` client printing its `HC_SESSION`; v1 mode with a fake `herdr`; then stop every isolated core and remove the temp folders.

## Tests

- No new Go tests are planned: this phase changes docs and packaging. The existing suite must pass unchanged, including in `nix build` (which runs `go test ./...`).
- Any defect the QA gate finds gets a regression test in the package that owns it.
