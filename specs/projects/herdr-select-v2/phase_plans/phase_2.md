---
status: complete
---

# Phase 2: herdr-core backend

## Overview

Add `internal/hc`, the v2 adapter that implements `session.Backend` for herdr-core. It discovers sessions from herdr-core's state and runtime folders without running `hc` (every `hc` command starts a core on Linux), probes sockets to tell Running, Stopped and Unavailable apart, applies hc's name rules, stops a session through `hc server stop --json`, deletes a stopped session's state folder and leftover sockets, builds the client launch command with `HC_SESSION`, and refuses to launch `herdr-app` while its single window is open. Nothing is wired into startup yet (Phase 3), so v1 behavior is untouched.

## Steps

1. **Dependency**: `go get github.com/pelletier/go-toml/v2@v2.4.3` (direct requirement).

2. **`internal/hc/paths.go`**:

   ```go
   type Env map[string]string
   func EnvFrom(environ []string) Env // "K=V" entries; the first of a repeated key wins, like getenv
   type Paths struct {
       Home, StateRoot string
       RuntimeDir      string // "" means each session's own state folder
       MaxSocket       int    // 107 on Linux, 103 on macOS
   }
   var ErrFixedSocket = errors.New("fixed socket")
   func ResolvePaths(env Env, uid int, goos string, isDir func(string) bool) (Paths, error)
   func (p Paths) SessionDir(name string) string
   func (p Paths) SocketFolder(name string) string
   func (p Paths) Socket(name string) string
   ```

   Rules as in the architecture: empty `HOME` is an error; `XDG_STATE_HOME`/`XDG_RUNTIME_DIR`/`XDG_CONFIG_HOME` count only when non-empty; `/run/user/<uid>` counts only when `isDir` says so; `HC_SOCKET` non-empty or a config that decodes into `struct{ Socket *string \`toml:"socket"\` }` with `Socket != nil` returns `ErrFixedSocket`. Unreadable or unparsable config is no fixed socket.

3. **`internal/hc/probe.go`**:

   ```go
   type State int
   const (Stopped State = iota; Running; Unavailable)
   func Probe(ctx context.Context, path string) (State, string)
   ```

   `os.Lstat` (not exist → Stopped; other error → Unavailable; not `ModeSocket` → Unavailable "not a socket"), then a 1-second `net.Dialer` Unix dial: success → close, Running; `ECONNREFUSED`/`ENOENT` → Stopped; anything else → Unavailable with the error text.

4. **`internal/hc/names.go`**: `NameHint` constant, `validExisting(name) bool` (non-empty, valid UTF-8, not `.`/`..`, no control rune, none of `/ \ : * ? " < > |`), and `validateNewName(paths, name, existing) error` in the architecture's check order with its messages (v1's texts for `.`/`..`, `default` and duplicates). The socket-length check is hc's: `max(len(Socket(name)), len(SocketFolder(name)/name.holder.sock)+1+8) <= MaxSocket`.

5. **`internal/hc/backend.go`**:

   ```go
   type Backend struct { paths Paths; env Env; environ []string; hc session.Runner; clientPath, dir string; listTimeout, stopTimeout time.Duration }
   func New(paths Paths, env Env, environ []string, hcPath, clientPath, dir string) *Backend
   var _ session.Backend = (*Backend)(nil)
   ```

   - `New` makes relative `hcPath`/`clientPath` absolute against `dir`; list deadline 5 s, stop 20 s, runner `WaitDelay` 1 s, `Program` "hc".
   - `List`: `ReadDir(StateRoot)` (not exist → empty; other error → `CommandError{list, state_root}`); keep real directories with valid names; per session, concurrently, `Probe` the socket and take the newest `ModTime` among regular files directly inside the folder; fill `Session` (`Default: name == "default"`, `Running`, `ConnectionError` for Unavailable); a list deadline that expired → `CommandError{list, timeout}`; `session.SortSessions`.
   - `withSession(environ, name)`: drop every `HC_SESSION=` entry and append `HC_SESSION=<name>`. Used by Stop and launch.
   - `Stop`: name must pass `validExisting` (non-uncertain `invalid_name`); `Runner.Run(ctx, "stop", 20s, true, diagnose, "server", "stop", "--json")`; decode `{"stopped": true}` with `DecodeOne`, else uncertain `invalid_response`. `diagnose` reads `{"error":{"kind","message","hint"}}` (message plus " " plus the hint when non-empty).
   - `Delete`: the architecture's checks in order (valid name, not `default`, direct child of the state root, real directory via `Lstat`, `Probe` Stopped), each refusal non-uncertain and removing nothing; then `os.RemoveAll`; then, when `RuntimeDir` is set, remove `<name>.sock` and every `ModeSocket` entry named `<name>.holder.*.sock` (string comparison, no globbing). A holder-pattern entry that is another existing session's socket or holder socket (a session named `<name>.holder.…`) is skipped. Not-exist errors are ignored; any other removal error is an uncertain `remove` error saying the session may be partly deleted. Comment the start race as v1 does for create.
   - `ValidateNewName` → `validateNewName(b.paths, ...)`; `NameHint` → constant; `ClientName` → `filepath.Base(clientPath)`.
   - `LaunchCommand(name)`: `exec.Command(clientPath)`, `Dir = dir`, `Env = withSession(environ, name)`.
   - `CheckLaunch`: only when `ClientName() == "herdr-app"`, `Probe` `<appHome>/herdr-app.sock` (`HERDR_APP_HOME` non-empty, else `Home/.config/herdr-app`); Running → "herdr-app is already open. Close it, then open ‘name’ again."; otherwise nil.

## Tests

All in `internal/hc`, in temp folders only; sockets live under `os.MkdirTemp("", "hs")` (symlinks resolved) to stay below 107 bytes. The fake `hc` is the test binary itself in helper mode (as in `internal/herdr`), recording argv, dir and `HC_SESSION`.

- `TestEnvFrom`: entries split at the first `=`, first repeat wins, malformed entries skipped.
- `TestResolvePaths`: XDG set, empty and unset; `/run/user/<uid>` present vs absent (per-session fallback and `SocketFolder`/`Socket`); `HC_SOCKET` → `ErrFixedSocket`; config `socket` (under `XDG_CONFIG_HOME` and under `HOME/.config`) → `ErrFixedSocket`; config without `socket`, unparsable config, and a non-string `socket` → no error; empty `HOME` → error; `MaxSocket` 107 for linux and 103 for darwin.
- `TestProbe`: no file → Stopped; live listener → Running; stale socket (`SetUnlinkOnClose(false)`) → Stopped; regular file → Unavailable "not a socket"; socket in an unreadable folder → Unavailable.
- `TestValidExisting`: accepted names (spaces, unicode, `[`), rejected names (empty, `.`, `..`, each forbidden character, control, invalid UTF-8).
- `TestValidateNewName`: inner space accepted; leading and trailing space; each forbidden character; control character; `.`/`..`; `default`; duplicate; check order; exact length boundary for Linux (107) and macOS (103) with a runtime folder, and with the per-session socket folder.
- `TestList`: missing root → empty; empty root → empty; regular files, symlinked folders and invalid names skipped; recency is the newest regular file (subfolders and symlinks ignored); empty folder → no age; `default` flagged and not invented; Running/Stopped/Unavailable rows from live, stale and non-socket socket paths; sorted; unreadable root → `state_root` error; listing creates nothing.
- `TestStop`: argv `server stop --json`, dir, `HC_SESSION` replaced (inherited `HC_SESSION=other` removed); `{"stopped":true}` → nil; `false`, missing and malformed → uncertain `invalid_response`; hc error JSON with and without hint; plain stderr fallback; timeout → uncertain `timeout`; invalid name → no run.
- `TestDelete`: folder, `<name>.sock` and holder sockets removed, unrelated files and another session's `<name>.holder.…` socket kept; a name containing `[`; per-session socket folder (no runtime folder); `default`, invalid name, symlinked folder, missing folder, running and unavailable refused with nothing removed; read-only state root → uncertain partial failure.
- `TestLaunchCommand`: argv is the client alone, dir, `HC_SESSION` replaced, relative paths made absolute; `ClientName` is the base name; `NameHint`; `ValidateNewName` uses the paths.
- `TestCheckLaunch`: herdr-app with a live `herdr-app.sock` under `HERDR_APP_HOME` → error text; under `HOME/.config/herdr-app` when unset; stale socket → nil; no socket → nil; rodeo with a live socket → nil.
