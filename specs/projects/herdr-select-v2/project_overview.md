---
status: complete
---

# Herdr Select v2

Herdr Select today is a session picker for herdr v1: it lists, opens, stops and deletes herdr v1 sessions through the `herdr` CLI. Add support for herdr-core (`hc`, "v2") sessions while keeping full herdr v1 support. The work happens on a new branch, `next`.

## Choosing v1 or v2

If `--app` isn't passed, it depends on whether `herdr` or `herdr-app` is available on the system:

- Both available: use `herdr` (v1), to stay backward compatible.
- Only `herdr-app`: use `herdr-app` (v2).
- `rodeo` is reserved: it must be passed with `--app`.

A preference can be given with `--app`, for example:

```sh
herdr-select --app=herdr
herdr-select --app=rodeo
herdr-select --app=herdr-app
```

- `--app=herdr` is always herdr v1 and its socket and session format.
- `--app=<anything else>` is the name of a client CLI, and that client is always a herdr-core (v2) client.

The app choice decides whether herdr-select uses the v1 or the v2 session format.

## Clean adapter

Organize the code cleanly around an adapter, so herdr-select supports both paths, the v1 and the v2 socket and session protocols, clearly and separately.

## Launching

When a client launches, be it `herdr`, `rodeo`, `herdr-app` or anything else, it must use the correct session for its protocol. herdr-select's TUI works through the matching protocol, speaking to either the `herdr` CLI or the `hc` CLI.
