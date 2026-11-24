---
status: complete
---

# Functional Spec: Herdr Select v2

Herdr Select v2 adds herdr-core (`hc`, "v2") sessions beside herdr v1 sessions. It extends the [v1 functional spec](../herdr-select/functional_spec.md) and [UI design](../herdr-select/ui_design.md). Everything those documents specify (picker, footer, form, confirmations, notices, sizing, ages, sorting rules, terminal handoff, exit codes) applies to both protocols unless this document changes it. The UI does not show which protocol is in use.

Work happens on the `next` branch.

## Terms

- **Protocol:** how herdr-select finds, launches, stops and deletes sessions. **v1** goes through the `herdr` CLI. **v2** goes through herdr-core: its state folders, its sockets and the `hc` CLI.
- **Client:** the program herdr-select launches on the chosen session: `herdr` for v1, or a herdr-core client such as `herdr-app` or `rodeo` for v2.

## Command line

```
herdr-select [--app=<client>]
herdr-select --version
```

- `--app=<client>` and `--app <client>` are equivalent. `<client>` is a command name looked up on PATH, or a path to an executable.
- `--app=herdr` selects v1. The client is v1 when the base name of `<client>` is exactly `herdr` (so `--app=/nix/store/…/bin/herdr` is also v1).
- Any other `<client>` selects v2, with `<client>` as the herdr-core client.
- An empty `--app` value, a repeated `--app`, an unknown option or an extra argument prints a usage line to stderr and exits with status 2.
- `--version` keeps its current behavior and takes no other arguments.

## Choosing the protocol and client

Without `--app`, herdr-select picks the client by what is on PATH, in this order:

| On PATH | Client | Protocol |
| --- | --- | --- |
| `herdr` (with or without `herdr-app`) | `herdr` | v1 |
| `herdr-app` but no `herdr` | `herdr-app` | v2 |
| neither | none: error | none |

v1 stays the default when `herdr` is installed, for backward compatibility. `rodeo` and any other v2 client are used only through `--app`. Having only `hc` installed does not choose a client.

Requirements, checked before any session is listed:

| Protocol | Needs on PATH |
| --- | --- |
| v1 | `herdr` |
| v2 | the client, and `hc` (used to stop sessions) |

If a requirement is missing, herdr-select shows its existing centered warning screen, naming exactly what is missing, and exits with status 1 when dismissed. Example texts:

- No client found: "Install herdr or herdr-app, or pass --app=<client>."
- `--app=rodeo` with no rodeo: "rodeo was not found on PATH."
- v2 with no hc: "hc (herdr-core) is required for rodeo. Install herdr-core, then run herdr-select again."

## Nested sessions

herdr-select refuses to run inside any existing session, in both protocols. If `HERDR_PANE_ID` (a herdr v1 pane) or `HC_TERMINAL_ID` (an hc terminal) is set and not empty, it shows the existing warning screen: "Detach from your current herdr or hc session before opening another session." Nothing is listed or launched. It exits with status 1 when dismissed.

This also means herdr-select cannot run from a pane inside rodeo or herdr-app, since those panes are hc terminals.

## v1 protocol

Unchanged from the v1 spec: the same `herdr` commands, name rules, recency file (`session.json`), stop and delete behavior.

## v2 protocol

herdr-core has no command that lists or deletes sessions, and on Linux every `hc` command for a session starts that session's core (even `hc server status` and `hc ls`). So herdr-select discovers sessions and their state itself, without running `hc`, and uses `hc` only to stop a session.

### Paths

Follow herdr-core 0.1.20's path rules (`crates/hc/src/paths.rs` in rodeo mirrors them):

- **State root:** `$XDG_STATE_HOME/herdr-core`, or `~/.local/state/herdr-core` when `XDG_STATE_HOME` is unset or empty. Each session's state folder is `<state root>/<name>`.
- **Runtime folder:** `$XDG_RUNTIME_DIR/herdr-core`; when `XDG_RUNTIME_DIR` is unset or empty, `/run/user/<uid>/herdr-core` if `/run/user/<uid>` is a folder; otherwise the session's own state folder.
- **Socket:** `<runtime folder>/<name>.sock`. Holder sockets are `<runtime folder>/<name>.holder.<id>.sock`.
- If `HC_SOCKET` is set and not empty, or `~/.config/herdr-core/config.toml` (under `XDG_CONFIG_HOME` when set) sets `socket`, every session shares one fixed socket and sessions cannot be told apart. herdr-select shows the warning screen "herdr-select does not support HC_SOCKET or a fixed socket in herdr-core's config." and exits with status 1.
- A `HC_SESSION` already set in the environment is ignored for listing. Launch and stop set it explicitly.

### Listing

- A session is each folder directly under the state root whose name is a valid existing-session name (below). Other entries are ignored. A missing state root means no sessions.
- Do not invent a `default` entry when its folder is absent, as in v1.
- **Running** means the socket file exists and accepts a Unix-socket connection within 1 second. herdr-select connects and closes at once, without speaking the protocol.
- **Stopped** means there is no socket file, or the socket refuses connections (a stale socket left by a crashed core).
- **Unavailable** (v1's existing exceptional state) covers any other result: permission denied, a timeout, or a socket path that is not a socket. The row shows Unavailable with its diagnostic when selected, offers Open and Refresh, and disables Stop and Delete, exactly as in v1.
- **Recency:** the newest modification time among the regular files directly inside the session's state folder (for example `core.json`, `terminals.json`, `docs.json`). If none can be read, show `—` and sort it last, as in v1.
- Listing never starts, stops or changes a core, and never writes to the state or runtime folders.
- The listing has the same bounded wait, error and Retry behavior as v1.

### Names

Existing-session names are accepted for listing when they follow hc's rules: not empty, not `.` or `..`, no control characters, and none of `/ \ : * ? " < > |`. Names with leading or trailing whitespace are also skipped: hc trims `HC_SESSION` (and an all-whitespace value becomes `default`), so such a folder would open, stop or delete a different session. hc never creates one.

New-session names in v2 use these rules, checked as the user types, with the same form behavior as v1:

- At least 1 character, no leading or trailing whitespace (hc would trim it, so the name would change), and the hc rules above.
- Not `default` (reserved, as in v1), and not the name of an existing session.
- Short enough for hc's socket path: the length in bytes of `<runtime folder>/<name>.holder.sock` plus 9 must be at most 107 on Linux (103 on macOS). Error: "This name is too long for herdr-core's socket path."
- Spaces inside a name are allowed in v2. The v2 naming hint replaces v1's `1-64 characters, no spaces: A-Z a-z 0-9 . _ -` with `No / \ : * ? " < > |, and no leading or trailing spaces`. The form layout is unchanged.

### Launch (new, open, resume)

- Run the client with no arguments, in herdr-select's working directory, with the inherited environment plus `HC_SESSION=<name>`. The client starts the session's core when needed.
- New Session uses the same final duplicate check as v1 before launching.
- Terminal handoff, exit and error handling are as in v1. A GUI client such as `herdr-app` opens its own window; herdr-select waits for the client process and then exits with its status.

### herdr-app is single-instance

`herdr-app` runs one window per app home. Launching it while a window is open only brings that window forward, on whatever session it already shows; it does not switch to the chosen session.

- When the client's base name is `herdr-app`, herdr-select checks right before launch whether `herdr-app.sock` in the app home (`$HERDR_APP_HOME`, or `~/.config/herdr-app` when unset or empty) accepts a connection, using the same 1-second connect check as sessions.
- If it does, herdr-select does not launch. It stays in the picker (or the form) and shows the error "herdr-app is already open. Close it, then open ‘name’ again." Nothing else changes.
- Other v2 clients, such as `rodeo`, launch normally.

### Stop

- Run `hc server stop --json` with `HC_SESSION=<name>`, and expect `{"stopped": true}`. Any other result is an error with the same uncertain-outcome handling as v1.
- Stop applies to running sessions, including `default`, with the same confirmation as any other session.
- Confirmation: "Stop session ‘name’?" with the explanation "Its programs end. Terminals with keep on come back as shells next time."

### Delete

- hc has no delete. herdr-select deletes a v2 session by removing its state folder, and only when the session is stopped. This is a deliberate exception to v1's rule "do not bypass upstream restrictions by deleting files directly": for v2 there is no upstream command to defer to. v1 still never deletes files itself.
- `default` is never deletable: no Delete in its footer, Shift+D does nothing.
- Confirmation for a stopped session: "Delete session ‘name’?" with "Its saved layout and history are removed. This can't be undone."
- A running session gets "Stop and delete session ‘name’?" with "Its programs end. Its saved layout and history are removed. This can't be undone." It is stopped first, then deleted, following v1's stop-then-delete flow (never delete after a failed stop, recheck after stopping).
- Right before removing, recheck that the socket does not accept connections. If the session is running again, report it and do not delete.
- Remove `<state root>/<name>` recursively, and any leftover `<name>.sock` and `<name>.holder.*.sock` files in the runtime folder.
- Safety: delete only a real folder (not a symlink) that is a direct child of the state root, with a valid name that is not `default`. Never follow symlinks while removing. If any check fails, report an error and remove nothing.
- If removal fails part-way, report that the session may be partly deleted, and refresh.

## Out of scope

- Other machines (hc's `--on` / machine links).
- Showing the protocol or client in the UI.
- `HC_SOCKET` and fixed-socket configs (refused, as above).
- Changes to `~/.nix-config` (the Super+Shift+Enter binding can later pass `--app`).
- Windows. Linux is the tested platform; macOS follows the same path rules.

## Acceptance examples

- With `herdr` and `herdr-app` installed, `herdr-select` behaves exactly like today (v1).
- With only `herdr-app` and `hc`, `herdr-select` lists hc sessions and opens the chosen one in herdr-app with `HC_SESSION` set.
- `herdr-select --app=rodeo` lists hc sessions and opens rodeo on the chosen one; `herdr-select --app=herdr` is v1 even when herdr-app is installed.
- `--app=rodeo` without rodeo, or v2 without `hc`, shows a warning naming the missing program and exits with status 1.
- Run inside a herdr pane or an hc terminal, herdr-select refuses in both protocols.
- Listing v2 sessions never creates a socket or state folder, and never starts a core.
- A session with a stale socket file shows as Stopped.
- Stopping a v2 session runs `hc server stop` for that session only.
- With a herdr-app window already open, choosing a session with `herdr-select` (herdr-app as client) shows the "already open" error and launches nothing.
- Deleting a stopped v2 session removes its state folder and leftover sockets; `default` has no Delete; a session that started again before removal is not deleted.
- A v2 name with a space is accepted; a name with `/`, a leading space, or one too long for the socket path is rejected with a specific message.
