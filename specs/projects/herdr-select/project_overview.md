---
status: complete
---

# Herdr Select

Herdr Select will be a Go TUI on top of the [`herdr` CLI from herdrdev/herdr](https://github.com/herdrdev/herdr), using Bubble Tea v2 and the related Charm TUI libraries. Herdr is already installed on this development machine (version `0.9.5` observed during planning). If the Herdr CLI is not installed on a user's machine, show a nicely presented TUI warning explaining that the user must install Herdr first.

This is a thin controller of the installed CLI's standard session commands. Show the sessions Herdr lists and delegate session actions to Herdr, following its capabilities and restrictions. Herdr owns the session behavior, including any remote-machine connections available once the session is open.

Currently, Super+Shift+Enter in my Nix configuration launches Herdr and always opens the default session. Instead, I want that shortcut to launch `herdr-select`, so I can choose among different named persistent sessions.

The interface should be a very simple selection screen: a dark-gray menu panel centered in the terminal window with a colored selector bar, using a teal/cyan color like the active tmux tab in my Nix configuration. Above the menu, render the supplied Herdr ram logo as character art made from the letters `herdr` and special characters. This replaces the earlier large text wordmark. Use the supplied OpenCode selection-menu screenshot as the reference for the panel's spacing, background, and contrasting selected row. Omit the “Select session” heading, Esc hint, and up/down navigation hint.

The first option should be **New Session**. On startup, highlight `default` wherever it appears in the recency-sorted list and scroll it into view. If no default session is returned, select New Session. Home jumps to New Session. Pressing Enter on New Session opens a session-name input with OK and Cancel; Esc returns to the picker. Validate the name against Herdr's rules and reject names already in use before launching the new session. Session creation asks only for the name.

Below New Session, show all existing session names, including the default session, sorted by their `session.json` filesystem modification time from most recent to oldest. The default session follows the same sorting rule as other sessions. Use the up and down arrows to select a session and Enter to open or resume it. Show Running/Stopped and a compact age on the right, such as `10s ago`, `5min ago`, `1h ago`, or `6d ago`, without an “Updated” prefix. Sessions with unavailable timestamps appear last and show `—` for the age.

Show context-sensitive actions in a footer at the very bottom of the terminal, inspired by the supplied Git TUI footer, with the action badges centered across the screen. New Session shows New on Enter. Running sessions show Open on Enter, Stop on Shift+S, and Delete on Shift+D. Stopped sessions show Resume on Enter and Delete on Shift+D, with no Stop action. Stopping requires confirmation. Deleting a running session confirms stopping and deleting together. Herdr does not support deleting the default session, so omit Delete for that row.

Use the MIT license. Provide a flake package and development shell using `pkgs.go` from locked Nixpkgs, without a custom Go source override, for clean installation through my Nix configuration. Use the latest stable major versions of dependencies. Include a README with the character-art ram PNG centered at the top; a finished-app screenshot will be supplied later as `docs/screenshot.png`.
