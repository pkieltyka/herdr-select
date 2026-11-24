---
status: complete
---

# Architecture: Herdr Select v2

Inputs: [functional spec](functional_spec.md), [project overview](project_overview.md), and the v1 [architecture](../herdr-select/architecture.md), [functional spec](../herdr-select/functional_spec.md) and [UI design](../herdr-select/ui_design.md). Everything the v1 architecture specifies stays in force unless this document changes it: dependencies, Bubble Tea model, async operation IDs, stop/delete sequencing, terminal handoff, signals, rendering, Nix packaging.

## Approach

The UI already depends only on a small `Backend` interface. v2 keeps that shape:

- Move the protocol-neutral domain (session record, sorting, ages, errors, the interface) into its own package.
- Keep the v1 `herdr` CLI adapter as it is, behind the interface.
- Add a v2 adapter for herdr-core beside it.
- Add a small startup layer that parses `--app`, chooses the protocol and client, checks requirements, and builds the backend.

The UI does not learn which protocol is in use. It asks the backend for the few things that differ: name validation, the naming hint, a launch preflight, the client's display name and the launch command.

One document is enough; no component designs.

## Repository layout

```text
cmd/herdr-select/main.go        entry point: os.Exit(app.Run(os.Args[1:], version))
internal/app/args.go            argument parsing (--app, --version, usage errors)
internal/app/choose.go          client and protocol selection, requirement checks
internal/app/run.go             startup order, warnings, UI loop, interactive launch
internal/app/child.go           unchanged
internal/session/session.go     Session, SortSessions, RelativeAge (moved from internal/herdr)
internal/session/backend.go     Backend interface, CommandError (moved, extended)
internal/session/exec.go        shared bounded subprocess runner (moved from herdr/client.go)
internal/herdr/client.go        v1 adapter (herdr CLI), behavior unchanged
internal/herdr/names.go         v1 name rules (moved from session.go)
internal/hc/paths.go            herdr-core path rules
internal/hc/probe.go            socket probe
internal/hc/backend.go          v2 adapter: List, Stop, Delete, launch
internal/hc/names.go            v2 name rules and hint
internal/ui/...                 unchanged except the points listed below
```

Tests move with the code they cover. The move is mechanical: no v1 behavior changes.

## Domain and interface

`internal/session`:

```go
type Session struct {
    Name, Dir, SocketPath string
    Default, Running      bool
    ConnectionError       string // non-empty means Unavailable
    ModifiedAt            time.Time
    HasModifiedAt         bool
}

type Backend interface {
    List(ctx context.Context) ([]Session, error)
    Stop(ctx context.Context, name string) error
    Delete(ctx context.Context, name string) error

    // ValidateNewName applies the protocol's rules, "default" and duplicates.
    ValidateNewName(name string, existing []Session) error
    // NameHint is the one-line hint under the New Session field.
    NameHint() string
    // CheckLaunch runs right before a launch request is emitted. nil means go ahead.
    CheckLaunch(ctx context.Context, name string) error
    // ClientName is used in messages such as "Could not start rodeo: ...".
    ClientName() string
    // LaunchCommand builds, but does not start, the interactive client.
    LaunchCommand(name string) *exec.Cmd
    // Consequence is the confirmation's explanation of stopping or deleting.
    Consequence(delete, running bool) string
}
```

`CommandError`, `SortSessions` and `RelativeAge` move unchanged. `Session.Default` is set by each adapter (v1 from herdr's `default` field, v2 from `name == "default"`).

`session.Runner` is the existing bounded subprocess code from `herdr/client.go` (deadlines, `WaitDelay`, 4 MiB / 64 KiB output limits, `limitedBuffer`, `decodeOne`), made reusable:

```go
type Runner struct {
    Executable, Dir string
    Env             []string
    WaitDelay       time.Duration
}
func (r Runner) Run(ctx context.Context, op string, timeout time.Duration, mutation bool,
    diagnose func(stderr []byte, err error) (code, message string), args ...string) ([]byte, error)
func DecodeOne(data []byte, target any) error
```

Each adapter passes its own `diagnose`: v1 reads herdr's `{"error":{"code","message"}}`; v2 reads hc's `{"error":{"kind","message","hint"}}` (code = kind; message = message, plus a space and the hint when present). Both fall back to trimmed stderr, then the Go error.

## UI changes

Only these, all through the interface:

- `model.go`: `herdr.ValidateNewName(...)` becomes `m.backend.ValidateNewName(...)`. "Could not start Herdr: " becomes `"Could not start " + m.backend.ClientName() + ": "`.
- `view.go`: both uses of the hard-coded hint string use `m.backend.NameHint()`. The form height calculation already wraps it, so a longer hint stays correct.
- `view.go`: the confirmation's explanation comes from `m.backend.Consequence(delete, running)`.
- `commands.go`: for `create` and `open`, after the fresh list and the existing checks pass, call `backend.CheckLaunch(ctx, name)`. A non-nil error goes into `r.err` with no launch request, so it shows as the form error (create) or a persistent red picker notice (open), exactly like a validation or missing-session error today.
- `ui.Warning` keeps a nil backend; warning views never call the interface.
- Type references change from `herdr.Session` to `session.Session`.

The ram, title, palette, footer, keys and layout do not change.

## v1 adapter (`internal/herdr`)

Unchanged behavior and command vectors. Changes are structural only:

- `NewClient(path string)` takes the resolved executable path from the startup layer instead of calling `exec.LookPath("herdr")` itself.
- It implements the new methods: `ValidateNewName` is today's function; `NameHint` returns `"1-64 characters, no spaces: A-Z a-z 0-9 . _ -"`; `CheckLaunch` returns nil; `ClientName` returns `"Herdr"` (today's message text); `LaunchCommand` is today's; `Consequence` returns today's v1 texts.

## v2 adapter (`internal/hc`)

### Paths

```go
type Env map[string]string

type Paths struct {
    Home       string
    StateRoot  string // $XDG_STATE_HOME/herdr-core or ~/.local/state/herdr-core
    RuntimeDir string // "" means: each session's own state folder
    MaxSocket  int    // 107 on Linux, 103 on macOS
}
var ErrFixedSocket = errors.New("fixed socket")
func ResolvePaths(env Env, uid int, goos string, isDir func(string) bool) (Paths, error)
func (p Paths) SessionDir(name string) string    // StateRoot/name
func (p Paths) SocketFolder(name string) string  // RuntimeDir, or SessionDir(name)
func (p Paths) Socket(name string) string        // SocketFolder(name)/name.sock
```

Rules, mirroring herdr-core 0.1.20 (`rodeo/crates/hc/src/paths.rs`):

- `Home` is `HOME`; an empty `HOME` is an error.
- State root: `XDG_STATE_HOME` when non-empty, else `Home/.local/state`; then `/herdr-core`.
- Runtime: `XDG_RUNTIME_DIR` non-empty → `$XDG_RUNTIME_DIR/herdr-core`; else `/run/user/<uid>` is a folder → `/run/user/<uid>/herdr-core`; else "" (per-session state folder).
- Fixed socket: `HC_SOCKET` non-empty, or the herdr-core config (`XDG_CONFIG_HOME` when non-empty, else `Home/.config`; then `/herdr-core/config.toml`) parses as TOML and has a string `socket`. Either returns `ErrFixedSocket`. A missing or unparsable config counts as no fixed socket, as in hc.
- TOML library: `github.com/pelletier/go-toml/v2` v2.4.3 (latest major and release, checked 2026-11-24), decoding into a struct with one field, `Socket *string`, tagged `toml:"socket"`.
- `Env` is built from `os.Environ()` at startup, so tests pass explicit environments.

### Socket probe

```go
type State int // Stopped, Running, Unavailable
func Probe(ctx context.Context, path string) (State, string)
```

1. `os.Lstat(path)`: not exist → Stopped. Other error → Unavailable with the error text. Not `ModeSocket` → Unavailable "not a socket".
2. `(&net.Dialer{Timeout: time.Second}).DialContext(ctx, "unix", path)`: success → close at once, Running.
3. `errors.Is(err, syscall.ECONNREFUSED)` or `ENOENT` (removed between the steps) → Stopped. Anything else (EACCES, timeout) → Unavailable with the error text.

The probe never writes and never speaks the hc protocol.

### List

1. `os.ReadDir(StateRoot)`: not exist → empty list. Other error → `CommandError{Operation: "list", Code: "state_root"}`.
2. Keep entries whose `DirEntry.Type()` is a directory (symlinks are skipped) and whose name passes `validExisting`.
3. For each, concurrently (one goroutine each, joined before returning, all under the 5-second list deadline): probe `Socket(name)`; read the session folder and take the newest `ModTime` among regular files directly inside it (`HasModifiedAt=false` when there are none or the folder can't be read).
4. Fill `Session{Name, Dir: SessionDir, SocketPath: Socket, Default: name == "default", Running, ConnectionError}`, then `session.SortSessions`.

### Names (`names.go`)

`validExisting` (listing filter): non-empty; valid UTF-8; no leading or trailing whitespace (`strings.TrimSpace(name) == name`, because hc trims `HC_SESSION`); not `.` or `..`; no `unicode.IsControl` rune; none of `/ \ : * ? " < > |`.

`ValidateNewName` checks in this order, each with its own message:

| Check | Message |
| --- | --- |
| empty | "Enter a session name." |
| first or last rune is `unicode.IsSpace` | "Remove spaces at the start or end." |
| control character | "Control characters are not allowed." |
| forbidden character | "These characters are not allowed: / \ : * ? " < > |" |
| `.` or `..` | v1's message |
| socket path too long | "This name is too long for herdr-core's socket path." |
| `default` | v1's message |
| duplicate (exact name) | v1's message |

Socket length is hc's own check: `len(filepath.Join(SocketFolder(name), name+".holder.sock")) + 1 + 8` and `len(Socket(name))` must both be at most `MaxSocket` (bytes).

`NameHint`: `No / \ : * ? " < > |, and no leading or trailing spaces`.

### Stop

`Runner.Run(ctx, "stop", 20s, true, diagnoseHC, "server", "stop", "--json")`, with `Env` = the inherited environment with every `HC_SESSION=` entry removed and `HC_SESSION=<name>` appended. Decode with `DecodeOne` into `struct{ Stopped *bool }` and require `true`; anything else is an uncertain `invalid_response`. The executable is the `hc` path resolved at startup.

The existing UI sequencing only calls `Stop` for a session it saw Running, so hc never starts a core here.

### Delete

1. `validExisting(name)` and `name != "default"`, else a non-uncertain `CommandError`.
2. `dir := SessionDir(name)`; require `filepath.Dir(dir) == StateRoot`, and `os.Lstat(dir)` must be a real directory (not a symlink).
3. `Probe(Socket(name))` must be Stopped. Running → "Session ‘name’ is running again; it was not deleted." Unavailable → "Session ‘name’ is unavailable; it was not deleted."
4. `os.RemoveAll(dir)`, which removes symlinks inside without following them. An error → uncertain `CommandError`: "The session may be partly deleted."
5. If `RuntimeDir` is set: remove `<name>.sock`, and every entry of `os.ReadDir(RuntimeDir)` whose name starts with `<name>.holder.`, ends with `.sock`, and whose type is `ModeSocket`. Compare names as strings, never with `filepath.Glob`, because hc names may contain glob characters. Ignore not-exist errors; report others as in step 4.

Probing again right before removal narrows, but cannot close, the race with a core that starts at that instant. Document it the way v1 documents its create race.

### Launch and herdr-app preflight

- `LaunchCommand(name)`: `exec.Command(clientPath)` with no arguments, `Dir` = startup working directory, `Env` with `HC_SESSION=<name>` as in Stop.
- `ClientName()`: base name of the client (`rodeo`, `herdr-app`).
- `CheckLaunch`: when the client base name is `herdr-app`, probe `filepath.Join(appHome, "herdr-app.sock")`, where `appHome` is `HERDR_APP_HOME` when non-empty, else `Home/.config/herdr-app`. Running → error "herdr-app is already open. Close it, then open ‘name’ again." Stopped or Unavailable → nil (herdr-app reports its own problems). Other clients → nil.

```go
func New(paths Paths, env Env, environ []string, hcPath, clientPath, dir string) *Backend
```

## Startup (`internal/app`)

### Arguments

```go
type Options struct {
    App     string
    Version bool
}
func ParseArgs(args []string) (Options, error) // error → usage on stderr, exit 2
```

Accept exactly: no arguments; `--version` alone; `--app=<v>`; `--app <v>`. Reject an empty value, a repeat, `--version` with anything else, unknown options and extra arguments. Usage: `usage: herdr-select [--app=<client>] | --version`.

### Choosing the client

```go
type Protocol int // V1, V2
type Choice struct {
    Protocol           Protocol
    Client, ClientPath string // display name and resolved path
    HcPath             string // V2 only
}
func Choose(app string, lookPath func(string) (string, error)) (Choice, string) // string: missing-program message
```

- `app != ""`: V1 if `filepath.Base(app) == "herdr"`, else V2. `lookPath(app)` (`exec.LookPath` also accepts paths). Not found → V1: v1's "Herdr is required…" text; V2: "<app> was not found on PATH."
- `app == ""`: `lookPath("herdr")` found → V1 with herdr; else `lookPath("herdr-app")` found → V2 with herdr-app; else "Install herdr or herdr-app, or pass --app=<client>."
- V2 then needs `lookPath("hc")`; missing → "hc (herdr-core) is required for <client>. Install herdr-core, then run herdr-select again."

### Run order

`func Run(args []string, version string) int`:

1. `ParseArgs`. Usage error → stderr, return 2. `--version` → print `version`, return 0. (`main.go` only passes `os.Args[1:]` and its link-time `version`.)
2. Interactive terminal check (unchanged): return 1.
3. `Choose`. A missing program → `ui.Warning(message)`.
4. Nesting: `HERDR_PANE_ID` or `HC_TERMINAL_ID` non-empty → `ui.Warning("Detach from your current herdr or hc session before opening another session.")`.
5. V2: `hc.ResolvePaths`. `ErrFixedSocket` → warning "herdr-select does not support HC_SOCKET or a fixed socket in herdr-core's config."; another error → a warning with its text.
6. Build `herdr.NewClient(path)` or `hc.New(...)`, then run the existing UI loop, which now calls `backend.LaunchCommand`.

Warning screens keep exit status 1, as in v1.

## Error handling

- Startup problems are warning views (exit 1) or usage errors (exit 2). No logging.
- Adapter errors are `session.CommandError`. `Uncertain` is true for Stop (always, once the child ran) and for Delete once removal started. The UI's existing handling is unchanged: refresh after every mutation, never retry, report stopped-but-not-deleted.
- Socket and per-session folder problems become Unavailable rows or a missing age, never a failed list. Only an unreadable state root fails the list (with Retry).

## Testing

Go's standard `testing`, as today. Tests never touch the real state, runtime or config folders and never start a real core: every test sets `HOME`, `XDG_STATE_HOME`, `XDG_RUNTIME_DIR`, `XDG_CONFIG_HOME` and `HERDR_APP_HOME` to temp folders, and `hc` is a fake script. Create socket folders with `os.MkdirTemp("", "hs")` so paths stay under the 107-byte limit; keep the symlinked-TMPDIR handling the v1 tests already have.

| Area | Cases |
| --- | --- |
| `ParseArgs` | none; `--version`; both `--app` forms; empty, repeated, unknown, extra; `--version --app=x` |
| `Choose` | herdr + herdr-app → v1 herdr; herdr-app only → v2; none → missing; `--app=herdr` and `--app=/x/herdr` → v1; `--app=rodeo` → v2; missing app; v2 without hc |
| `ResolvePaths` | XDG set, empty and unset; `/run/user/<uid>` present or absent (per-session fallback); `HC_SOCKET`; config `socket`; unparsable config ignored; empty `HOME` |
| `Probe` | no file; live listener; stale socket (`SetUnlinkOnClose(false)`, then close); regular file → Unavailable; unreachable folder → Unavailable |
| List | missing or empty root; files and symlinked folders skipped; invalid names skipped; recency is the newest file; empty folder → no age; default flag; sorting; unreadable root → error |
| Names | inner spaces accepted; leading/trailing space; each forbidden character; control character; `.` and `..`; `default`; duplicate; length at the boundary on Linux and macOS; per-session socket folder |
| Stop | fake `hc` records argv and `HC_SESSION` (an inherited `HC_SESSION=other` is replaced); `{"stopped":true}`; false or malformed → uncertain; hc error JSON with hint; timeout |
| Delete | folder and leftover sockets removed; `default`, invalid name, symlinked folder, running and unavailable refused with nothing removed; a name containing `[`; partial failure (read-only parent) → uncertain |
| Launch | `LaunchCommand` argv, dir and `HC_SESSION`; herdr-app preflight with a live `herdr-app.sock` → error, stale → nil; rodeo → nil |
| UI | fake backend: hint shown; validation from the backend; `CheckLaunch` error in the form and in the picker; `ClientName` in a start failure |
| App (PTY) | existing v1 tests unchanged; v2 handoff with a helper client asserting `HC_SESSION`; `HC_TERMINAL_ID` nesting warning; v2 without hc warning; usage error exit 2 |

Run `go test ./...`, `go vet ./...`, `nix build` and `nix flake check`. The vendor hash changes with go-toml.

## Docs and packaging

- README: both protocols, `--app`, the defaults, the herdr-app single-instance limit, and that v2 delete removes the session's state folder.
- `flake.nix`: new `vendorHash`; descriptions become "A terminal session picker for herdr and herdr-core".
- `VERSION`: unchanged here; bumped in the release step.
