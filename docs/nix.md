# Nix setup

Herdr Select provides a working package and app for `x86_64-linux` and `aarch64-linux`, plus a development shell, formatter, and package check. The package and shell use the same `pkgs.go` from the committed Nixpkgs pin. Package builds run tests, use the committed vendor hash, and set `GOTOOLCHAIN=local`; they do not download a compiler or override Go's source.

Install [Herdr](https://github.com/herdrdev/herdr) separately. The selector finds `herdr` on PATH and inherits its environment and configuration. Run it in an interactive terminal, outside any existing Herdr pane.

## Run or install

From a checkout:

```sh
nix build
nix run
nix profile add .#herdr-select
```

The build places the executable at `result/bin/herdr-select`. For new files not yet tracked by Git, use `path:.` in place of `.` so Nix includes them.

Once the repository commits are published:

```sh
nix run github:pkieltyka/herdr-select
nix profile add github:pkieltyka/herdr-select#herdr-select
```

The flake exposes `packages.<system>.default`, `packages.<system>.herdr-select`, `apps.<system>.default`, `devShells.<system>.default`, `formatter.<system>`, and `checks.<system>.package`.

## Declarative installation

Add the input to your configuration's `flake.nix`, then lock it to a published revision:

```nix
inputs.herdr-select.url = "github:pkieltyka/herdr-select";
```

Keep its independent Nixpkgs pin so its tested Go toolchain is used. An optional `inputs.herdr-select.inputs.nixpkgs.follows = "nixpkgs"` is suitable only when your host pin provides the Go version required by this project's `go.mod`.

If your Home Manager modules receive `inputs` through `extraSpecialArgs` or `specialArgs`:

```nix
{ inputs, pkgs, ... }:
{
  home.packages = [
    inputs.herdr-select.packages.${pkgs.stdenv.hostPlatform.system}.herdr-select
  ];
}
```

## Prepared integration for Peter's Nix configuration

[nix-config.patch](nix-config.patch) contains the exact consumer changes:

- `flake.nix`: add the GitHub input with its independent Nixpkgs pin.
- `overlays/herdr-select.nix`: expose the input package for the host system.
- `home/base/tui/dev-tools.nix`: install `herdr-select` alongside `herdr`.
- `home/linux/gui/hyprland/conf/keybindings.lua`: change only the existing Super+Shift+Return command's executable to `herdr-select`.
- `scripts/cooldown.py`: register the input-backed overlay alongside pi and opencode; its source age is checked through the flake lock.

The overlay follows that repository's input argument convention:

```nix
{ herdr-select, ... }:
final: prev: {
  herdr-select = herdr-select.packages.${prev.stdenv.hostPlatform.system}.herdr-select;
}
```

The shortcut launches:

```text
uwsm-app -- ghostty --gtk-single-instance=false -e herdr-select
```

This preserves UWSM, Ghostty's separate-window behavior, and Herdr's Super-key bindings. The selector hands the same terminal to Herdr and exits after the client detaches or quits.

The patch targets the current configuration baseline, including the existing Herdr shortcut. It is kept here so the unpublished input does not break the live configuration. Publication, applying the patch, final GitHub locking, and activation have not been performed.

## Validate before publication

Use an isolated copy of the configuration containing its current working files, including any staged and unstaged edits. Do not replace the live configuration or its index. In that copy, with the selector checkout path set explicitly:

```sh
selector_checkout=/absolute/path/to/herdr-select

git apply --check "$selector_checkout/docs/nix-config.patch"
git apply "$selector_checkout/docs/nix-config.patch"
# Nix's Git source filter needs the new overlay to be tracked in this copy.
git add overlays/herdr-select.nix
nix flake lock --override-input herdr-select "path:$selector_checkout"
make test
make check
make cooldown
```

Only this disposable copy gets the temporary local lock. The cooldown check treats a fresh local source as younger than 72 hours; that does not establish the eligibility of a future published revision. Do not disable the cooldown or copy this lock into the live repository.

For a single evaluation without writing a local lock:

```sh
nix flake check --no-write-lock-file \
  --override-input herdr-select "path:$selector_checkout"
```

## Finalize after publication

1. Publish the reviewed selector commits. Follow the configuration's 72-hour cooldown before consuming the published revision, and check the release ages of any changed dependencies. No cooldown exception is assumed.
2. Apply the patch to the configuration with `git apply --check` followed by `git apply`, preserving unrelated work and the existing index. Review any baseline drift before applying; do not force the patch.
3. Ensure the new overlay is included in the configuration's Git source, then create the remote lock with `nix flake lock --override-input herdr-select github:pkieltyka/herdr-select/COMMIT_SHA`, replacing `COMMIT_SHA` with the eligible full published commit hash. This pins the selected commit while leaving the declared GitHub URL intact. It adds the selector's independent Nixpkgs node; it must not update existing inputs. Review the lock diff for a GitHub `locked.rev`, no local `path:` entry, and no unrelated revisions changed.
4. Run `make cooldown`, `make test`, and `make check`. Review the five configuration changes and the new lock entry separately from any existing user edits, then commit only the intended integration changes.
5. Build the affected host using the configuration's normal process. Activate it only when explicitly requested. Super+Shift+Return starts the selector after that activation.

A successful local package build does not publish the repository or activate the shortcut.

## Maintaining the package

- Commit `flake.lock`, `go.mod`, and `go.sum` with relevant dependency changes.
- Recompute the fixed-output `vendorHash` in `flake.nix` when the Go dependency graph changes.
- Use the latest stable major versions of directly selected dependencies and pin compatible exact releases.
- Update Nixpkgs deliberately; `pkgs.go` follows the selected pin. There is no source override.
- Run `go test ./...`, `go test -race ./...`, `go vet ./...`, `nix build`, and `nix flake check` in the pinned development environment before publishing.
