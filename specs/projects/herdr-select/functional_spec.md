---
status: complete
---

# Functional Spec: Herdr Select

## Purpose and scope

Herdr Select is a small keyboard-driven Go TUI controlling the installed [Herdr CLI](https://github.com/herdrdev/herdr)'s standard named-session commands. Running `herdr-select` opens a centered session picker. The intended desktop entry point is Super+Shift+Enter, launching the picker directly inside Ghostty.

Use Bubble Tea v2 and related Charm libraries, selecting the latest stable major versions of dependencies. License the project under MIT. Herdr remains responsible for session persistence, server processes, workspace restoration, and agent resumption. The selector does not maintain its own session registry.

V1 includes listing, creating, opening/resuming, stopping, and deleting the sessions exposed by the installed CLI. Session renaming, searching/filtering, agent prompts, directory selection, and management of workspaces or panes are outside scope. No user configuration file is required.

## Thin CLI controller

Herdr is the authority for session discovery, identity, name validity, status, lifecycle, and errors. Show every session returned by the standard session list, without introducing a separate local/remote classification or filtering sessions according to their contents. The TUI adds selection, input, confirmations, and readable feedback around these operations:

| UI action | Herdr command |
| --- | --- |
| List / refresh | `herdr session list --json` |
| Create named session | `herdr --session <name>` |
| Open / resume | `herdr session attach <name>` |
| Stop | `herdr session stop <name> --json` |
| Delete | `herdr session delete <name> --json` |
| Stop and delete | Stop, then delete, following the confirmed lifecycle flow below |

The installed CLI runs on the same host as the selector. Herdr's current session commands manage that host's sessions. This describes upstream behavior rather than an extra restriction imposed by the selector. Any saved remote-machine connections available inside the launched Herdr client remain Herdr's responsibility and are unaffected by the selector.

Direct SSH attachment and saved-machine management are separate Herdr interfaces (`--remote`, `machine`). The [upstream CLI reference](https://herdr.dev/tui/docs/cli-reference/#saved-ssh-machines) explicitly excludes session management from `--machine` forwarding. Therefore v1 adds no host picker, SSH orchestration, or remote session inventory. Those would require a separate UI and command flow, rather than happen automatically through the standard session list.

## Startup

- Find `herdr` through PATH before loading sessions.
- If unavailable, display a centered warning: “Herdr is required. Install Herdr first, then run herdr-select again.” Include the upstream project URL and a quit hint. Do not attempt installation. Exit unsuccessfully when dismissed.
- Require an interactive terminal. When one is unavailable, print a concise error to stderr and exit unsuccessfully.
- If launched inside a Herdr pane (`HERDR_PANE_ID` is nonempty), explain that the user must detach before opening another session, and allow quitting. Do not start a nested Herdr client.
- Load sessions through `herdr session list --json`. Show a loading state while this runs. Malformed output or command failure produces a readable error with Retry and Quit actions; it must not appear as an empty list.
- Use the same inherited Herdr environment and configuration as a direct CLI invocation. Do not hard-code the user's home directory or force the default config location.

## Session picker

- Center a character-art rendering of the supplied Herdr ram logo above a dark-gray menu panel. Build the logo from `herdr` letters and special characters, replacing the earlier text wordmark. Center the combined group in the area above the terminal footer. Follow the supplied references and [UI design](ui_design.md).
- Omit the “Select session” heading, Esc hint, and up/down navigation hint. These keys retain their normal behavior.
- Define the panel with its background and padding rather than a visible outline. Use a cyan/teal selection bar with dark foreground text across the row, inspired by the cyan active tmux tab in the user's Nix configuration. Other rows have light foreground text and muted status text.
- The first row is always **New Session**. It has no running/stopped status or age. On initial load, select the session marked `default: true` wherever it appears in the sorted list, and scroll it into view. If no default session is returned, select New Session. Selection does not change sort order.
- List the sessions returned by Herdr underneath it, including `default` if returned. Do not invent a default entry when absent.
- Sort existing sessions by the filesystem modification time of `<session_dir>/session.json`, newest first and oldest last. The default session has no special sort priority. Equal timestamps are ordered by session name using case-sensitive lexical order; unavailable timestamps sort last, also by name.
- Show **Running** or **Stopped**, based on Herdr's `running` field, followed by a right-aligned compact age such as `10s ago`, `5min ago`, `1h ago`, or `6d ago`. Do not prefix ages with “Updated” or “Last active”. Status is textual so it remains understandable without color.
- Up/Down moves the selection without wrapping. Home selects New Session and scrolls to the top. Enter activates the selected row. Esc, q, or Ctrl+C quits from the picker without changing sessions.
- Anchor an action footer to the very bottom of the terminal, outside the centered menu. Center the action badges as a group across the terminal width. Use readable action labels with bracketed keys, following the supplied Git TUI footer reference. New Session shows `New [enter]` and `Refresh [r]`. A running session shows `Open [enter]`, `Stop [shift+s]`, `Delete [shift+d]`, and `Refresh [r]`. A stopped session shows `Resume [enter]`, `Delete [shift+d]`, and `Refresh [r]`, with no Stop action. Update the footer immediately when the highlighted row changes; this does not require adding mouse-hover support.
- Shift+S acts only on running session rows. Shift+D acts only on existing non-default session rows; omit Delete for the default session because Herdr does not support deleting it. Unavailable shortcuts do nothing and their hints are omitted.
- Support lists longer than the terminal height by scrolling the session rows while keeping the footer visible. Resize without losing the selected session or entered name. Truncate long names for display without changing the underlying name.
- In a terminal too small to render a usable screen, show a resize-or-quit message and preserve the current view state.
- On smaller terminals, collapse the character-art logo to a compact single-line `herdr` title with normal letter spacing before reducing the list's usable area. Keep the menu and action footer usable before allocating space to decoration.
- `r` refreshes the list. Preserve selection by session identity where possible; if it disappeared, select the closest remaining row. After successful deletion, select the next remaining row, or the preceding row if there is no next row.
- Reload after stop/delete operations, including failures where the operation may have partly completed. Initial default-session focus applies only at startup, not every refresh. Returning from the new-session form restores selection to New Session.

## Create a session

1. Enter on New Session opens an initially empty, focused single-line **Session name** input, with **OK** and **Cancel** buttons below it. Separate the title from the field with a subtle horizontal divider across the form interior; keep the gap above the centered buttons compact while retaining readable naming hints and validation. Show `e.g. my-project` as placeholder text, not a prefilled name. Hide the bottom footer throughout this form, including error and submission states; keep OK and Cancel in the panel. Explicitly state “no spaces” in the naming hint. Restore the contextual footer on return to the picker.
2. Enter in the input submits through OK. Tab/Shift+Tab cycles through the input and enabled buttons. Enter activates the focused button. Cancel or Esc returns to the picker with New Session selected and creates nothing; Ctrl+C quits. Letters such as q, r, S, and D are ordinary text in the input.
3. Follow the verified [Herdr 0.9.5 validation and normalization rules](https://github.com/herdrdev/herdr/blob/v0.9.5/src/session.rs#L425): 1–64 ASCII characters drawn from `A–Z`, `a–z`, `0–9`, `.`, `_`, and `-`; reject `.` and `..`. Reject whitespace anywhere, including leading/trailing whitespace, rather than silently trimming or rewriting the name. These ASCII-only names have the same character and byte counts. Reject `default` in the new-session form because it denotes the built-in session, not a new named session. Preserve case.
4. Validate locally against these rules and the last successfully loaded session list as the user types. Empty input initially shows only the naming hint; show a specific inline error after invalid nonempty input or an attempted empty submission. OK is disabled while empty, invalid, or known to be in use. Editing clears or updates the relevant error; never discard the input.
5. Before launching, refresh the session list to check duplicates again using exact names on the target Linux filesystem. If the name is in use, show “A session named ‘name’ already exists.” and keep the form open. If discovery fails, show the error and allow another submission to retry; do not launch without a successful duplicate check.
6. While checking or launching, disable duplicate submissions and show progress. On validation/discovery failure, restore input focus and preserve the name. Enable OK when local validation succeeds and no known duplicate exists; clicking/activating OK still performs the fresh check.
7. Launch using `herdr --session <name>`. Ask only for the session name; there is no AI-prompt field or working-directory prompt. Inherit the selector's working directory.

Herdr's use-or-create command is not an exclusive-create transaction. Another process creating the same name between the final check and launch may cause attachment to that session. The selector must not claim it can prevent this race.

## Open or resume a session

- Enter on an existing row opens that exact session through Herdr, regardless of whether it is running or stopped.
- Refresh before launch; if the selected session disappeared, report this and return to the refreshed picker rather than intentionally recreating it.
- Hand control of the terminal to Herdr cleanly, with normal keyboard input, colors, and terminal sizing. Herdr opens in the same terminal window.
- After the Herdr client exits or detaches, the selector exits as well. Do not reopen the picker automatically.
- If Herdr cannot be started, restore the picker and show the error. If the launched Herdr process exits with an error, preserve its diagnostic output and propagate the failure on exit.
- Resuming a stopped session means invoking Herdr's restoration behavior; the selector does not promise to restore previously running operating-system processes.

## Stop and delete

Confirmation views identify the full session name. Focus **Cancel** initially; Enter activates the focused choice, Left/Right or Tab changes it, and Esc cancels. Ctrl+C exits the selector without starting the pending operation. Cancellation changes nothing.

| Action | Selected state | Behavior |
| --- | --- | --- |
| Shift+S | Running | Ask “Stop session ‘name’?” Explain that running work will be interrupted and saved session state will remain. On confirmation, stop through Herdr. |
| Shift+S | Stopped | Unavailable: no action and no Stop footer item. Enter resumes the session. |
| Shift+D | Running | Ask “Stop and delete session ‘name’?” Explain that running work will be interrupted and the saved session will be removed. On confirmation, stop first and then delete. |
| Shift+D | Stopped | Ask “Delete session ‘name’?” Explain that the saved session will be removed. On confirmation, delete. |

- The default session can be opened/resumed and stopped with the normal confirmation flow. Herdr 0.9.5 explicitly rejects deleting it ([source](https://github.com/herdrdev/herdr/blob/v0.9.5/src/session.rs#L284)). Hide its Delete footer action and make Shift+D a no-op; never stop it as part of an unsupported delete attempt. The delete rows in the action table apply only to non-default sessions. Do not bypass upstream restrictions by deleting files directly.
- Refresh the selected session's state before presenting confirmation. If a session confirmed for deletion while stopped becomes running before execution, require a new stop-and-delete confirmation before stopping it.
- Run lifecycle commands sequentially and disable repeated submissions while busy. Show the current operation.
- Never proceed to deletion when stopping fails. If stopping succeeds but deletion fails, explicitly report that the session is stopped but was not deleted. Do not restart it automatically.
- Recheck after stopping before deleting. If another process restarted it, report that it is running again and return to the refreshed picker; do not repeatedly stop it.
- Once an operation has been submitted, wait for its outcome before accepting another action or a normal quit request. A process crash or forced termination cannot roll back an already completed stop or delete.
- On success, return to the refreshed list with a short result message. On failure, keep the picker usable and show the failed action and relevant Herdr diagnostic.

## Session recency and relative age

Use the filesystem modification time of `<session_dir>/session.json` for both session ordering and the displayed relative age. Resolve `session_dir` from Herdr's list response, including for the default session; do not reconstruct paths from session names or assume a fixed config root. Read file metadata only, without parsing or modifying the saved session file.

This measures when the saved state file changed. It is a useful approximation of recency, not an assertion about the last keystroke, client attachment, or agent action. No selector-owned activity database or log parsing is required.

Format elapsed time using the largest applicable unit, rounded down:

| Elapsed time | Display |
| --- | --- |
| Under 60 seconds | `Ns ago`, including `0s ago` |
| 60 seconds to under 1 hour | `Nmin ago` |
| 1 hour to under 24 hours | `Nh ago` |
| 24 hours or more | `Nd ago`, where one day is 24 hours |

- Clamp negative elapsed time to zero if the file timestamp is in the future; retain the actual timestamp for sorting.
- If the file is missing, unreadable, or not a regular file, show `—` and sort that session after all sessions with known timestamps. Metadata failure must not hide the session, change its Running/Stopped status, or prevent session actions.
- Read timestamps on initial load and whenever the session list is refreshed, including refreshes after lifecycle actions. Preserve selection by session identity when ordering changes.
- Capture the relative-age reference time when a session-list load succeeds, and keep displayed ages fixed until another successful load. The `r` action renews this snapshot along with status and file timestamps; successful operation-triggered reloads also renew it. Failed or stale asynchronous results retain the previous snapshot. Do not schedule age ticks or update ages during idle navigation/resizing. Background writes do not cause rows to jump while the user navigates. This approved UI-feedback override replaces the earlier once-per-second ticking requirement.

## Errors and command contract

- Execute Herdr with separate argument values, never by interpolating a session name into a shell command.
- Treat names and diagnostics as plain terminal text; escape control characters so they cannot inject terminal commands or corrupt the UI.
- Keep list/lifecycle work off the UI event loop. Discovery must have a bounded wait and a retry path. Architecture will specify process deadlines and handling of uncertain mutation outcomes.
- Unknown extra JSON fields are allowed. Missing required fields or an invalid response are reported as compatibility errors rather than guessed.
- Preserve the distinction between no sessions, a stopped session, and failure to discover sessions.
- A normal user cancellation or quit exits successfully. Startup failures and a failed launched Herdr client exit unsuccessfully. Recoverable picker errors remain visible until dismissed or retried.
- Restore terminal state on normal exit and handled errors.

## Desktop integration

Provide a standalone `flake.nix` and lock file with package and development-shell outputs, using `pkgs.go` from Nixpkgs without a custom source override. Supply README installation instructions and a centered character-art logo PNG under `docs/`; add the user-provided `docs/screenshot.png` later.

The final setup must make `herdr-select` available to the desktop launcher and update the existing Super+Shift+Enter binding to launch it in Ghostty. Preserve the current `uwsm-app`, separate Ghostty process, and direct-terminal behavior:

`uwsm-app -- ghostty --gtk-single-instance=false -e herdr-select`

The current binding is in `~/.nix-config/home/linux/gui/hyprland/conf/keybindings.lua`. The architecture and implementation plan must describe how the binary is packaged or installed before the binding changes. Planning does not modify or activate the desktop configuration.

## Acceptance examples

- With Herdr absent, launching the selector shows the installation warning and never attempts session commands.
- With zero sessions, New Session remains usable. A list-command failure instead shows an error and retry action.
- With running and stopped sessions, each has the correct status; Enter opens the selected session and detaching exits the selector.
- The bottom footer follows selection: New on New Session, Open/Stop/Delete on running sessions, and Resume/Delete on stopped sessions. No Select session, Esc, or up/down hints appear in the picker.
- New Session stays first; default is initially selected and visible wherever it sorts, with New Session as the fallback when default is absent. Existing sessions, including default, are ordered newest to oldest by `session.json` modification time. Ties are deterministic, and unavailable timestamps appear last with `—`.
- Ages use seconds/minutes/hours/days at the stated boundaries, stay fixed between successful session-list loads, and never show negative values. Refreshing timestamps can reorder rows while preserving the selected session.
- An empty, invalid, reserved, overlength, or duplicate new name prevents creation and leaves the input editable. Accept names such as `my-project`, `Project_2`, and `work.v2`; reject whitespace, non-ASCII characters, `.`, `..`, and `default`. An existing name found during the final refresh stays in the form with a duplicate error. Cancel/Esc returns without invoking a creation command.
- Default never exposes Delete and Shift+D on it does not invoke stop or delete.
- Cancelling either confirmation sends no stop/delete command.
- Confirming deletion of a running session stops it before deleting it; a stop failure prevents deletion, and a delete failure reports the stopped-but-not-deleted result.
- A long list and long names remain navigable on resize; input and selection survive resizing.
- Super+Shift+Enter launches the installed selector directly in Ghostty, with default initially selected when present, regardless of its recency position.
