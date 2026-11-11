---
status: complete
---

# Architecture: Herdr Select

## Approach

Implement one Go executable with a Bubble Tea UI and a small Herdr subprocess adapter. Herdr owns sessions; the selector owns presentation, confirmation, and command sequencing. There is no daemon, database, socket client, SSH layer, or selector configuration file. One architecture document is sufficient; separate component designs would add little.

Inputs: [approved functional spec](functional_spec.md), [approved UI design](ui_design.md), and its character-art asset. The user approved this architecture and the [implementation plan](implementation_plan.md) for implementation. Dependency/API verification was performed directly while drafting, as requested; no separate research workflow is needed.

## Runtime and dependencies

Target Linux first, including x86_64-linux and aarch64-linux Nix package outputs. Use `pkgs.go` from the project's locked Nixpkgs input, as requested, with no source override. The current pin provides Go 1.27.3, matching `go.mod`. Updating the input is the supported way to adopt newer packaged Go releases; do not bypass Nixpkgs just to get a newer upstream patch.

Use the latest stable **major** of every directly selected library, including any later test dependency. Pin compatible exact releases in `go.mod` and commit `go.sum`; do not use prereleases or floating dependencies. Recheck major releases when dependencies are added during implementation. Inspect transitive dependencies through Go's module graph rather than rewriting upstream major-version imports.

| Dependency | Selected release | Responsibility |
| --- | --- | --- |
| `charm.land/bubbletea/v2` | v2.1.1 | Event loop, terminal input, rendering, terminal restoration |
| `charm.land/bubbles/v2` | v2.2.1 | Focused single-line text input and cursor |
| `charm.land/lipgloss/v2` | v2.1.0 | Colors, panel/row/button styling, cell-aware layout |
| `github.com/charmbracelet/x/ansi` | v0.11.9 | Cell-aware truncation and stripping terminal escapes |
| `github.com/charmbracelet/x/term` | v0.2.2 | Interactive-terminal checks |

Versions were checked against official release metadata and the Go module proxy on 2026-11-11. Sources: [Bubble Tea](https://github.com/charmbracelet/bubbletea/releases/tag/v2.1.1), [Bubbles](https://github.com/charmbracelet/bubbles/releases/tag/v2.2.1), [Lip Gloss](https://github.com/charmbracelet/lipgloss/releases/tag/v2.1.0), [ANSI](https://github.com/charmbracelet/x/tree/ansi/v0.11.9/ansi), [terminal utilities](https://pkg.go.dev/github.com/charmbracelet/x/term@v0.2.2). These are current major versions; v0 remains the published major for the two utilities.

Use the standard library for subprocesses, JSON, filesystem metadata, sorting, time, and testing. A custom list renderer avoids the filtering, titles, and pagination assumptions of a general list widget. No image library is needed at runtime.

## Repository layout

```text
cmd/herdr-select/main.go       entry point and exit code
internal/app/run.go           startup, UI run/recovery loop, interactive launch
internal/herdr/client.go      command execution, JSON decoding, typed errors
internal/herdr/session.go     session record, validation, ordering, ages
internal/ui/model.go          state and event transitions
internal/ui/commands.go       asynchronous operations and result messages
internal/ui/view.go           layout and views
internal/ui/styles.go         palette and reusable styles
internal/ui/ram.txt            approved art, embedded with go:embed
internal/.../*_test.go         focused behavioral tests
flake.nix / flake.lock         package, app, dev shell, formatter, checks
README.md / docs/              user documentation and branding image
LICENSE                       MIT, copyright Peter Kieltyka
```

Copy the approved ram text into `internal/ui/ram.txt` at implementation time. Do not read the specs directory or PNGs at runtime. Keep the PNG under `docs/` for the README only.

## Domain model and interfaces

```go
type Session struct {
    Name, Dir, SocketPath string
    Default, Running bool
    ConnectionError string
    ModifiedAt time.Time
    HasModifiedAt bool
}

type Backend interface {
    List(context.Context) ([]Session, error)
    Stop(context.Context, string) error
    Delete(context.Context, string) error
}

func ValidateNewName(name string, existing []Session) error
func SortSessions(sessions []Session)
func RelativeAge(modified, now time.Time) string
```

These types/functions live in `internal/herdr`. A concrete `Client` holds the resolved executable path, inherited environment, and working directory. Tests use the small Backend interface for UI transitions and a helper executable for process-boundary coverage. The app layer owns interactive launch; it does not go through the noninteractive Backend.

`List` decodes the CLI response, reads each `<session_dir>/session.json` with `os.Stat`, and sorts. A stat error or non-regular target sets `HasModifiedAt=false`; it does not fail discovery. Selection identity is the exact session name. New Session is a separate row kind, never a fake session name.

Sort known modification times descending, break ties by exact name, then place unknown timestamps last by name. Preserve raw future timestamps for ordering and clamp negative age to zero. Use whole elapsed seconds, minutes, hours, and 24-hour days as specified. Inject `now` into formatting and UI construction for deterministic tests.

## Herdr subprocess adapter

Resolve `herdr` once through PATH with `exec.LookPath`. Pass argument arrays directly; never invoke a shell. Inherit the environment and working directory. Explicit session arguments choose the intended target even when session/socket environment variables are present.

| Operation | Exact argument vector after executable |
| --- | --- |
| List | `session`, `list`, `--json` |
| Create or attach | one argument: `--session=` followed by the exact name |
| Stop | `session`, `stop`, `--json`, `--`, exact name |
| Delete | `session`, `delete`, `--json`, `--`, exact name |

The create/attach spelling is an implementation refinement of the functional spec's equivalent attach command: Herdr's `session attach help` takes a help path despite `help` being a valid session name. `--session=<name>` avoids that collision. The option terminator for lifecycle commands also handles valid names such as `--json`. Do not invent extra name restrictions. Sources: [session argument handling](https://github.com/herdrdev/herdr/blob/v0.9.5/src/session.rs), [CLI parsing](https://github.com/herdrdev/herdr/blob/v0.9.5/src/cli.rs).

List response: an object with a required `sessions` array. Each record requires `name`, `default`, `running`, `session_dir`, and `socket_path`; `connection_error` is optional. Decode required booleans as pointers in private wire types so missing values cannot become false. Reject missing/null required fields, malformed JSON, duplicate identities, invalid names, and inconsistent default identity; allow unknown fields. Reject trailing non-whitespace after the one JSON object.

Stop success contains `stopped: true` and `session`; delete success contains `deleted: true` and `session`. Require the matching acknowledgement and target name. Nonzero exit status is always failure even if stdout resembles success. Parse Herdr's stderr `error.code` / `error.message` when possible, otherwise retain a bounded plain diagnostic. A malformed acknowledgement after a mutation is an uncertain outcome, not proof nothing happened.

**Exceptional discovery state:** Herdr may return `connection_error` with `running: false` when a socket is inaccessible. Preserve the row and show **Unavailable**, plus its diagnostic when selected; do not mislabel it Stopped. Offer Open and Refresh, but disable Stop/Delete until discovery can determine state. This is an error-state clarification for architecture review, not a new session type.

Run discovery and mutations in `tea.Cmd` functions using context deadlines: 5 seconds per list, 20 seconds per stop (Herdr itself waits up to 15), and 10 seconds per delete. Set `exec.Cmd.WaitDelay` to one second to bound inherited-pipe waits. Keep at most 4 MiB of stdout and 64 KiB of stderr; drain/discard overflow and return a clear output-limit error. Do not block the event loop or leave goroutines reading pipes after cancellation.

A timeout kills only the CLI child; the server may already have accepted the operation. Report an uncertain result, refresh the list, and never retry a mutation automatically. If refresh fails, display the last list as stale and require a successful refresh before further actions.

## UI state and asynchronous work

Use a single `Model` implementing Bubble Tea v2's `Init`, `Update`, and `View() tea.View`. Construct views with `tea.NewView` and `AltScreen=true`. Handle `tea.KeyPressMsg` and `tea.WindowSizeMsg`; do not use v1 method signatures. [Bubble Tea model/view API](https://github.com/charmbracelet/bubbletea/blob/v2.1.1/tea.go).

Model fields: view kind, sessions, selected row identity/index, scroll offset, terminal size, input model/focus, confirmation target/action, busy operation, latest operation ID, notice/error, last known time, and optional launch request. View kinds are Loading, Picker, NewSession, Confirm, StartupError, and DiscoveryError; busy state belongs to the current view.

Every asynchronous result includes a monotonically increasing operation ID. Ignore results that no longer match the current operation. Capture immutable request values in command closures; update UI state only through messages. Allow only one session operation at a time. Input to a busy mutation is ignored except terminal resizing; normal quit waits for its bounded result. Process termination still exits cleanly and never promises rollback.

On initial discovery, select the default record at its sorted position and ensure visibility; otherwise select New Session. Later refreshes preserve identity. If it disappears, clamp the old index into the new row list. Cancelling creation returns to New Session. Home selects row zero. Age ticks run once per second without I/O or resorting; maintain at most one scheduled tick.

Match uppercase `S` and `D` as well as explicit Shift+s/d events to accommodate terminal key reporting. Only Picker handles these action shortcuts. Text entry receives ordinary q/r/S/D characters. Disable mouse reporting; keyboard focus drives the footer.

## Creation and validation

Use `textinput.New`, its focus/blur and width APIs, a virtual cursor, no suggestions, and validation errors displayed outside the field. Keep input values longer than 64 characters editable so the user sees an overlength error rather than silently truncating them. The domain validator enforces the approved ASCII rules and duplicate check. [Bubbles input API](https://github.com/charmbracelet/bubbles/blob/v2.2.1/textinput/textinput.go).

Intercept bracketed-paste messages before the widget: reject control characters rather than allowing widget sanitization to transform them into a different valid name. Disable the widget's clipboard-read shortcut; terminal paste remains supported without an additional external clipboard command. Preserve spaces as typed so whitespace validation is explicit. Reject paste events larger than 4 KiB without partial insertion; ordinary invalid input remains editable.

Focus order is input → enabled OK → Cancel, wrapping in both directions. Enter in the input validates/submits; Enter on a button activates it. Esc cancels. The form has no bottom footer, including during validation or launch preparation.

Submission captures the name, runs a fresh List, then validates against that result. On failure restore input focus and preserve text. On success emit a launch request. Herdr does not offer exclusive creation: document the small check/launch race rather than writing session directories or locks behind its back.

## Stop/delete sequencing

1. Refresh before constructing confirmation; capture exact target identity and known state. Missing targets return to the refreshed picker with a notice.
2. Cancellation issues no mutation. Confirmation defaults to Cancel.
3. On acceptance, refresh again. Default deletion is blocked locally. Unavailable state blocks mutation. A formerly stopped target that is now running requires a new stop-and-delete confirmation.
4. For Stop: stop only if still running; an already stopped target yields an informational result. For Delete: if the confirmed target is running, stop it first.
5. After a successful stop-for-delete, list again. If running again or unavailable, abort deletion; if already absent, report it no longer exists. Otherwise invoke Delete once.
6. Refresh after success or failure. Carry an explicit `StoppedButNotDeleted` outcome so the user sees partial success accurately. Stop errors prevent deletion, including ambiguous timeouts.

Always act on the captured name, never the row's current numeric index. A list refresh cannot redirect an operation to another session. Concurrent external commands still create unavoidable races; the CLI remains the final authority.

## Interactive handoff and terminal ownership

Do not run an interactive Herdr client inside an asynchronous background command. After preflight succeeds, set a `LaunchRequest` and quit Bubble Tea normally. `Program.Run` returns only after its renderer/input cleanup; then the app layer starts Herdr with the real `os.Stdin`, `os.Stdout`, and `os.Stderr` and no timeout.

Use `Start` and `Wait` separately. If Start fails, return to the saved UI model with an error and no startup-focus reset. Once Start succeeds, wait for the client to exit/detach and exit the selector with the same status (signal exits map to 128+signal). Do not reconstruct the picker after a launched client fails. Its stderr is inherited directly, so diagnostics are not overwritten by a redraw.

During the child wait, terminal Ctrl+C belongs to the interactive client. Keep the parent waiting rather than allowing it to exit before the child; both are in the foreground process group, so do not forward a second terminal SIGINT. Forward a directly received SIGTERM to the child and wait, with a five-second termination grace period before killing only that child. Do not kill a process group or Herdr's persistent server. Test terminal restoration and handoff with a disposable helper executable under a PTY.

Reject nonterminal stdin/stdout before entering the TUI. Missing Herdr and nested launch use approved warning views. Ensure all program exits pass through the app's cleanup before main calls `os.Exit`. No log file is needed; UI errors and final stderr diagnostics are sufficient.

## Rendering and small terminals

Compute layout in terminal cells. Panel width is `min(76, width-4)` when margins fit. Metadata widths come from the longest visible status and age; reserve them before truncating names. Escape terminal control sequences in names/diagnostics before styling. Use `ansi`/Lip Gloss width operations rather than byte counts.

Measure and wrap footer badges first, center each row, and reserve their height. Form mode hides the footer but retains the previous footer-height reservation when space permits to avoid a jump. Use the approved full ram when it leaves room for the active panel; otherwise replace it with compact `herdr`. Render once to the terminal dimensions, with a background fill and clamped nonnegative padding.

Fit the remaining list viewport and keep selection visible. For confirmations and errors, wrap content and use a scroll offset if it exceeds available space; keep the buttons visible. If even one row/input and essential actions cannot fit, show the resize-or-quit view without destroying state. Keep glyph fallback simple: non-UTF-8 locale uses plain `herdr`; terminal font coverage cannot reliably be auto-detected.

Colors and spacing follow the approved design. Let the renderer handle color capability conversion and use bold/reverse styling where color is unavailable. No image assets or Nerd Font glyphs are required by the app.

## Nix packaging and desktop installation

Provide a standalone flake with a locked Nixpkgs input, `packages.<system>.default` and `.herdr-select`, `apps.<system>.default`, `devShells.<system>.default`, `formatter`, and a package check. Use `buildGoModule`, `subPackages = [ "cmd/herdr-select" ]`, a real vendor hash once dependencies exist, tests enabled, `CGO_ENABLED=0`, and `GOTOOLCHAIN=local` so sandboxed builds do not download compilers. Both package and shell use the same pinned Go derivation. The package metadata declares MIT and `mainProgram = "herdr-select"`.

The initial flake is development scaffolding: it can evaluate and supply Go before application source exists. `vendorHash = null` is correct only for the current dependency-free module; phase 1 must replace it with the computed fixed-output hash when adding Charm dependencies. A package build is not considered validated until the real executable exists and `nix build` passes.

Consume it in `~/.nix-config` with `inputs.herdr-select.url = "github:pkieltyka/herdr-select"`, an overlay exposing the input's package for `prev.stdenv.hostPlatform.system`, and `herdr-select` in the existing `home.packages`. Keep this flake's Nixpkgs pin independent by default so its tested Go toolchain is reproducible. A consumer may opt into `follows` only when the host pin provides the Go version required by `go.mod`. Use a local flake override for integration tests until the repository contents have been published; adding a Git remote does not publish them.

After building/installing the package, replace only the existing Super+Shift+Return command's final `herdr` with `herdr-select`, preserving UWSM, Ghostty, and `--gtk-single-instance=false`. Read and follow the Nix-config repository instructions; preserve unrelated dirty files. Use its eval/check commands without system activation. Upstream dependency pin changes in that repository must respect its cooldown policy. This planning turn does not alter the user's Nix configuration or publish the repository.

## Verification

- Pure domain cases: all name boundaries, preserved case/whitespace, duplicates, future/missing/tied mtimes, age-unit boundaries, default selection, and stable selection after sorting.
- Adapter fixtures: exact argv including `help`, `-h`, `--json`, and `--`; valid/invalid/null/missing JSON; connection errors; process failure; bounded outputs; timeout/uncertain mutation results; sequential stop/delete failures. Use temporary directories and a fake executable, never personal Herdr state.
- UI transitions: New/Running/Stopped/Default/Unavailable footers, hidden creation footer, form focus/disabled OK, late duplicate discovery, cancellation, stale async replies, resize and scroll, and destructive-action identity preservation.
- Render assertions at representative wide/narrow/small sizes: no overflow, centered footer, intact metadata, logo fallback, and no lost input. Prefer semantic assertions over brittle whole-screen snapshots.
- Process integration: child starts with a terminal, detaches cleanly, exposes diagnostics, propagates exit status, and restores the selector only on start failure. Check signal handling with disposable child processes.
- Run `go test ./...`, `go test -race ./...`, `go vet ./...`, `gofmt`, `nix build`, and `nix flake check` in the pinned development environment. Nix builds run normal tests; race tests run in the development shell with a compiler available.
- Manually exercise the completed UI in Ghostty against disposable sessions/config state and compare it with the approved previews. Capture the final application screenshot as `docs/screenshot.png` when the user provides it.
