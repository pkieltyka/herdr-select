---
status: complete
---

# Implementation Plan: Herdr Select v2

Build against the [architecture](architecture.md) and [functional spec](functional_spec.md), on the `next` branch. Each phase includes code review before commit. Keep v1 behavior unchanged throughout: the existing v1 tests must pass after every phase.

## Phases

- [x] **Phase 1: Shared session package, no behavior change.** Move the protocol-neutral domain (`Session`, sorting, ages, `CommandError`, the bounded subprocess runner) into `internal/session`, extend `Backend` to the eight methods, and make the v1 `herdr` client implement them with today's behavior (`NewClient(path)`, v1 hint, nil `CheckLaunch`, `ClientName` "Herdr"). Switch the UI to the interface for validation, the hint, `CheckLaunch` in create/open, and the client name. Move tests with their code; add UI tests with a fake backend for the new interface points.
- [ ] **Phase 2: herdr-core backend.** Add `internal/hc`: path resolution (with go-toml for the fixed-socket check), socket probe, listing, v2 name rules and hint, stop through `hc server stop`, guarded delete, launch command, and the herdr-app single-instance preflight. Full unit tests in temp folders with a fake `hc`; never touch the real core.
- [ ] **Phase 3: Startup and client selection.** Add `ParseArgs` and `Choose`, move `--version` into `app.Run`, apply the run order (terminal check, missing-program warnings, nesting refusal for `HERDR_PANE_ID` and `HC_TERMINAL_ID`, fixed-socket refusal), and build the chosen backend. Add the argument/selection tests and the v2 PTY handoff, nesting and missing-hc tests.
- [ ] **Phase 4: Docs and packaging.** Update the README (both protocols, `--app`, defaults, herdr-app single-instance limit, v2 delete), the flake descriptions and `vendorHash`, and confirm `go test`, `go vet`, `nix build` and `nix flake check` pass. Then an end-to-end QA gate driven through hc, then a manual check by the user against their real sessions (for example the leftover `probe` session).

## End-to-end QA gate (Phase 4)

Drive the real binary through hc, never against the user's sessions:

- Create an isolated hc environment: a fresh `HC_SESSION` name and temp `HOME`, `XDG_STATE_HOME`, `XDG_RUNTIME_DIR` and `XDG_CONFIG_HOME`, with a few throwaway hc sessions (some running, one stopped, one with a stale socket).
- Start the built binary in an hc terminal with `hc run --bg`, using `env -u HC_TERMINAL_ID` (hc sets it in every terminal, and herdr-select would otherwise refuse) and the isolated environment, for example `--app=rodeo` with a stub client that prints its `HC_SESSION`.
- Drive it with `hc keys` and check each screen with `hc read`: the listing (Running, Stopped, Unavailable), the v2 naming hint and errors, stop, delete (folder and sockets gone), the herdr-app "already open" error with a fake live `herdr-app.sock`, and the launched client receiving the right `HC_SESSION`.
- Also run v1 mode the same way with a fake `herdr`, to confirm the picker is unchanged.
- Stop every isolated core with `hc server stop` in the same environment, and remove the temp folders, when done.
