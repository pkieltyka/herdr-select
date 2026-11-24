---
status: complete
---

# Phase 1: Shared session package, no behavior change

## Overview

Move the protocol-neutral domain out of `internal/herdr` into a new `internal/session` package, widen the `Backend` interface to the eight methods the v2 work needs, and make the v1 `herdr` client implement them with today's behavior. The UI switches to the interface for name validation, the naming hint, a launch preflight and the client's display name. Nothing a user can see changes: the existing v1 tests keep passing, moved with the code they cover.

## Steps

1. **`internal/session/session.go`**: move `Session`, `SortSessions` and `RelativeAge` from `internal/herdr/session.go` unchanged. Package doc: the protocol-neutral session domain shared by the adapters and the UI.

2. **`internal/session/backend.go`**: move `CommandError` (unchanged) and define the interface:

   ```go
   type Backend interface {
       List(ctx context.Context) ([]Session, error)
       Stop(ctx context.Context, name string) error
       Delete(ctx context.Context, name string) error
       ValidateNewName(name string, existing []Session) error
       NameHint() string
       CheckLaunch(ctx context.Context, name string) error
       ClientName() string
       LaunchCommand(name string) *exec.Cmd
   }
   ```

3. **`internal/session/exec.go`**: the bounded subprocess runner, moved from `herdr/client.go`:

   ```go
   const StdoutLimit, StderrLimit = 4 << 20, 64 << 10
   type Runner struct {
       Executable, Dir string
       Env             []string
       WaitDelay       time.Duration
       Program         string // display name in the output-limit message ("Herdr")
   }
   func (r Runner) Command(ctx context.Context, args ...string) *exec.Cmd
   func (r Runner) Run(ctx context.Context, op string, timeout time.Duration, mutation bool,
       diagnose func(stderr []byte, err error) (code, message string), args ...string) ([]byte, error)
   func DecodeOne(data []byte, target any) error
   type limitedBuffer struct{...}
   ```

   `Run` keeps today's order: start failure → `start` (never uncertain); context done → `timeout`; overflow → `output_limit` ("<Program> output exceeded the allowed size."); non-zero exit → `diagnose`. When `diagnose` returns an empty message, `Run` falls back to code `process` with the trimmed stderr, then the Go error, so both adapters share the fallback. `Program` is one field beyond the architecture's `Runner`, needed to keep v1's exact "Herdr output exceeded…" text.

4. **`internal/herdr/names.go`**: move `ValidateNewName` and `validateName` from `session.go` (taking `[]session.Session`), add `const NameHint = "1-64 characters, no spaces: A-Z a-z 0-9 . _ -"`. Delete `internal/herdr/session.go`.

5. **`internal/herdr/client.go`**: 
   - `Client` holds a `session.Runner` plus the three timeouts.
   - `NewClient(path string) (*Client, error)` takes the resolved executable, makes it absolute, captures `os.Getwd()` and `os.Environ()`; no `exec.LookPath`.
   - `List`, `Stop`, `Delete`, `LaunchCommand` keep today's argv and checks, now through `Runner`; `diagnose` parses herdr's `{"error":{"code","message"}}`.
   - New methods: `ValidateNewName` (calls the package function), `NameHint` (the constant), `CheckLaunch` (nil), `ClientName` ("Herdr").
   - `var _ session.Backend = (*Client)(nil)`.

6. **`internal/ui`**: 
   - `herdr.Session`/`herdr.Backend` → `session.Session`/`session.Backend`; `herdr.RelativeAge` → `session.RelativeAge`. The local test helper named `session` is renamed so the package name is usable.
   - `model.go`: `validForm` and `validationMessage` call `m.backend.ValidateNewName`; `LaunchFailed` says `"Could not start " + m.backend.ClientName() + ": "`.
   - `view.go`: `formHeight` and `formBody` use `m.nameHint()`, which returns `""` for the nil-backend warning model. `formHeight` always budgets at least one hint row, so the warning layout is unchanged.
   - `commands.go`: create validates through `backend.ValidateNewName`, then calls `backend.CheckLaunch`; open calls `backend.CheckLaunch` after the existence check. A non-nil error goes into `r.err` with no launch request.

7. **`internal/app/run.go`**: resolve `herdr` with `exec.LookPath` and call `herdr.NewClient(path)`; the launch loop uses the `session.Backend` value. Same warnings and order as today (the `--app` startup layer is Phase 3).

## Tests

- `internal/session/session_test.go`: `TestSortSessions`, `TestRelativeAge` moved unchanged.
- `internal/session/backend_test.go`: `CommandError` text with and without `Uncertain`, and `Unwrap`.
- `internal/session/exec_test.go` (helper-process fixture, as in the herdr tests): stdout overflow, stderr overflow, timeout (keeps `context.DeadlineExceeded`), inherited pipe (keeps `exec.ErrWaitDelay`), missing executable and canceled context → non-uncertain `start`; argv, dir and env reach the child; `diagnose` result used; empty `diagnose` falls back to trimmed stderr then the Go error; `Program` in the overflow message; `DecodeOne` rejects trailing data; `limitedBuffer` drains past its limit.
- `internal/herdr/names_test.go`: `TestValidateNewName` moved unchanged.
- `internal/herdr/client_test.go`: existing list, invalid-list, mutation and failure tests, adapted to `Runner` fields; `TestNewClient` now passes the path (relative path made absolute, env and dir captured, production deadlines); list timeout and start failure through the client; new `TestClientInterface`: `NameHint`, `ClientName` "Herdr", `CheckLaunch` nil, `ValidateNewName` matches the package rules.
- `internal/ui/model_test.go`: `fakeBackend` implements the full interface, defaulting to v1 rules and hint so existing tests are unchanged. New tests: the backend's hint is shown in the form; validation comes from the backend (a fake rule rejecting a v1-valid name, accepting a v1-invalid one); `CheckLaunch` error shown as the form error on create and as a persistent red picker notice on open, with no launch; `ClientName` used in a start failure message in the form and the picker.
- `internal/app/run_test.go`: unchanged PTY tests pass.
