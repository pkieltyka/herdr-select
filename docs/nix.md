# Nix setup

Herdr Select exposes a package, app, development shell, and formatter through its flake. The package and development shell use the same `pkgs.go` from the committed Nixpkgs pin. There is no Go source override or compiler download during package builds.

The development shell works now. The package remains scaffolding until application implementation adds `cmd/herdr-select`, Go dependencies, and the corresponding vendor hash. The installation examples below describe the finished package; GitHub references require the repository changes to be published first.

## Add the flake input

In your Nix configuration's `flake.nix`:

```nix
inputs.herdr-select.url = "github:pkieltyka/herdr-select";
```

Keep the input's own Nixpkgs pin by default, so its tested Go toolchain is used. An optional `inputs.herdr-select.inputs.nixpkgs.follows = "nixpkgs"` is suitable only when your host pin provides the Go version required by this project's `go.mod`.

## Install with Home Manager

If your modules receive `inputs` through `extraSpecialArgs` or `specialArgs`:

```nix
{ inputs, pkgs, ... }:
{
  home.packages = [
    inputs.herdr-select.packages.${pkgs.stdenv.hostPlatform.system}.herdr-select
  ];
}
```

Install Herdr separately. The selector uses the `herdr` on PATH, so it works with the same CLI and configuration you already use.

## Integration with Peter's Nix configuration

The existing repository discovers files in `overlays/` automatically. An `overlays/herdr-select.nix` following its current convention can expose the package:

```nix
{ inputs, ... }:
final: prev: {
  herdr-select = inputs.herdr-select.packages.${prev.stdenv.hostPlatform.system}.herdr-select;
}
```

Then add `herdr-select` alongside `herdr` in `home/base/tui/dev-tools.nix`'s `home.packages` list.

Once the package is available, the existing Super+Shift+Return binding in `home/linux/gui/hyprland/conf/keybindings.lua` can launch:

```text
uwsm-app -- ghostty --gtk-single-instance=false -e herdr-select
```

This keeps Herdr directly inside Ghostty and preserves its Super-key bindings. The selector hands the same terminal to Herdr; it exits after the Herdr client detaches or quits.

Follow the configuration repository's checks and normal activation process. The setup described here has not yet been applied to that repository.

## Build locally before publication

Once application code exists:

```sh
nix build
nix run
```

For a working tree whose new flake files are not yet tracked by Git, explicitly select the directory source:

```sh
nix develop path:.
nix build path:.
```

When checking a consuming configuration before publication, override the input with the local checkout:

```sh
nix flake check --no-write-lock-file \
  --override-input herdr-select path:/path/to/herdr-select
```

Do not commit a development machine's absolute checkout path into the configuration's lock file.

## Maintaining the package

- Commit `flake.lock`, `go.mod`, and `go.sum` together with relevant dependency changes.
- The initial dependency-free module uses `vendorHash = null`. Replace this with the actual fixed-output vendor hash when adding dependencies; do not ship a fake hash.
- Recompute the vendor hash when the Go dependency graph changes.
- Use the latest stable major versions of dependencies and pin compatible exact releases.
- Update Nixpkgs deliberately; `pkgs.go` follows the selected pin rather than floating at evaluation time.
- Run Go tests, `nix build`, and `nix flake check` before publishing an application version.
