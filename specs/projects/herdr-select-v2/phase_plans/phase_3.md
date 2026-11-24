---
status: complete
---

# Phase 3: Startup and client selection

## Overview

Wire the v2 adapter into startup. `herdr-select` now parses `--app`, chooses the protocol and client from the argument or from what is on PATH, checks each protocol's required programs, refuses to run nested inside a herdr pane or an hc terminal, refuses herdr-core's fixed-socket setups, and builds either the v1 `herdr` client or the v2 `hc` backend for the unchanged UI loop. `--version` moves from `main.go` into `app.Run`. v1 behavior stays the same apart from the spec's new nesting text, which now names both herdr and hc.

## Steps

1. **`internal/app/args.go`**:

   ```go
   const Usage = "usage: herdr-select [--app=<client>] | --version"
   type Options struct {
       App     string
       Version bool
   }
   func ParseArgs(args []string) (Options, error)
   ```

   Accept exactly: no arguments; `--version` alone; `--app=<v>`; `--app <v>` (the next argument is the value, taken literally). Reject with an error: an empty value (`--app=`, `--app ""`), `--app` with no value, a repeated `--app`, `--version` with anything else, any other option and any extra argument.

2. **`internal/app/choose.go`**:

   ```go
   type Protocol int
   const (V1 Protocol = iota; V2)
   type Choice struct {
       Protocol           Protocol
       Client, ClientPath string // display name (base name) and resolved path
       HcPath             string // V2 only
   }
   func Choose(app string, lookPath func(string) (string, error)) (Choice, string)
   ```

   - `app != ""`: V1 when `filepath.Base(app) == "herdr"`, else V2; `lookPath(app)`. Not found: V1 gets v1's existing text ("Herdr is required. Install Herdr first, then run herdr-select again. https://github.com/herdrdev/herdr"), V2 gets "<app> was not found on PATH."
   - `app == ""`: `herdr` found → V1; else `herdr-app` found → V2 with herdr-app; else "Install herdr or herdr-app, or pass --app=<client>."
   - V2 then needs `lookPath("hc")`; missing → "hc (herdr-core) is required for <client>. Install herdr-core, then run herdr-select again."

3. **`internal/app/run.go`**:

   ```go
   func Run(args []string, version string) int
   func newBackend(app string, environ []string, lookPath func(string) (string, error)) (session.Backend, string)
   ```

   - `Run`: `ParseArgs` (error → `Usage` on stderr, return 2); `--version` → print `version`, return 0; the unchanged interactive terminal check (return 1); locale; `newBackend(opts.App, os.Environ(), exec.LookPath)`; a non-empty warning → `ui.Warning`, else `ui.New(backend)`; the unchanged UI/launch loop.
   - `newBackend` applies the run order and returns either a backend or a warning text: `Choose` (missing program); nesting (`HERDR_PANE_ID` or `HC_TERMINAL_ID` non-empty → "Detach from your current herdr or hc session before opening another session."); V1 → `herdr.NewClient(ClientPath)`; V2 → `hc.ResolvePaths(hc.EnvFrom(environ), os.Getuid(), runtime.GOOS, isDir)` (`ErrFixedSocket` → "herdr-select does not support HC_SOCKET or a fixed socket in herdr-core's config."; other error → its text), `os.Getwd`, then `hc.New(paths, env, environ, HcPath, ClientPath, dir)`. A constructor or working-directory error becomes a warning with its text.

4. **`cmd/herdr-select/main.go`**: `os.Exit(app.Run(os.Args[1:], version))`; the `--version` special case moves into `Run`.

## Tests

- `TestParseArgs` (`args_test.go`): none; `--version`; `--app=rodeo` and `--app rodeo`; a value with `=` or a path; `--app=`, `--app ""`, bare `--app`; repeated `--app` (both forms); `--version --app=x`, `--app=x --version`, `--version --version`; unknown option (`-h`, `--help`, `--ap=x`); extra positional argument.
- `TestChoose` (`choose_test.go`, fake `lookPath` over a set of names): herdr + herdr-app → V1 herdr; herdr only → V1; herdr-app only (+ hc) → V2 herdr-app; neither → install message; hc alone → install message; `--app=herdr` with herdr-app installed → V1; `--app=/x/bin/herdr` → V1 with that path; `--app=rodeo` → V2 rodeo with HcPath; `--app=/opt/rodeo` → display name `rodeo`; missing app (v1 text for herdr, "was not found on PATH." for others); v2 without hc names the client.
- `TestNewBackend` (`run_backend_test.go`, temp environment): v1 → `*herdr.Client`; v2 → `*hc.Backend` with `ClientName` `rodeo` and the temp state root; missing program text; `HERDR_PANE_ID` and `HC_TERMINAL_ID` each → nesting text in both protocols, empty values ignored; `HC_SOCKET` and a config `socket` → fixed-socket text; empty `HOME` → its error text; the missing-program check comes before nesting and nesting before the fixed-socket check.
- PTY tests (`run_test.go`, Linux): the helper environment drops inherited `HC_*` variables and points `HOME` and the XDG folders at temp folders; the existing v1 handoff, start-failure, warning and non-terminal tests keep passing (the nested warning text is the new one).
  - `TestPTYV2Handoff`: `--app=rodeo` with fake `rodeo` and `hc` on PATH and a session folder `work` under a temp state root; Enter on it launches the fake client, which reports a terminal, no arguments and `HC_SESSION=work` (an inherited `HC_SESSION=other` is replaced); its exit status is herdr-select's; `hc` was never run.
  - `TestPTYWarningsAndQuit` gains `hc-nested` (`HC_TERMINAL_ID` set, v1) and `v2-no-hc` (`--app=rodeo` without `hc`): each shows its warning and exits 1 on `q`.
  - `TestArgsWithoutTerminal`: an unknown option prints the usage line and exits 2; `--version` prints the version and exits 0, both without a terminal.
