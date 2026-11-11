{
  description = "A terminal session picker for Herdr";

  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";

  outputs =
    { self, nixpkgs }:
    let
      systems = [
        "x86_64-linux"
        "aarch64-linux"
      ];
      forAllSystems = nixpkgs.lib.genAttrs systems;
      perSystem = forAllSystems (
        system:
        let
          pkgs = import nixpkgs { inherit system; };
          go = pkgs.go;
          buildGoModule = pkgs.buildGoModule.override { inherit go; };
          package = buildGoModule {
            pname = "herdr-select";
            version = "0.1.0-dev";
            src = pkgs.lib.cleanSourceWith {
              src = ./.;
              filter =
                path: type:
                pkgs.lib.cleanSourceFilter path type
                && !(builtins.elem (builtins.baseNameOf path) [
                  ".specs_skill_state"
                  "specs"
                  "docs"
                  "bin"
                ]);
            };
            vendorHash = "sha256-WVavIlbsXEtxuES4BtHjnVPza6KGmSkkV65Q0tsvwY4=";
            subPackages = [ "cmd/herdr-select" ];
            env = {
              CGO_ENABLED = "0";
              GOTOOLCHAIN = "local";
            };
            ldflags = [
              "-s"
              "-w"
            ];
            doCheck = true;
            checkPhase = ''
              runHook preCheck
              export GOFLAGS=''${GOFLAGS//-trimpath/}
              go test ./...
              runHook postCheck
            '';
            meta = {
              description = "A terminal session picker for Herdr";
              homepage = "https://github.com/pkieltyka/herdr-select";
              license = pkgs.lib.licenses.mit;
              platforms = systems;
              mainProgram = "herdr-select";
            };
          };
        in
        {
          inherit pkgs go package;
        }
      );
    in
    {
      packages = forAllSystems (system: {
        default = perSystem.${system}.package;
        herdr-select = perSystem.${system}.package;
      });
      apps = forAllSystems (system: {
        default = {
          type = "app";
          program = "${self.packages.${system}.herdr-select}/bin/herdr-select";
          meta.description = "Choose a Herdr session";
        };
      });
      devShells = forAllSystems (
        system:
        let
          inherit (perSystem.${system}) pkgs go;
        in
        {
          default = pkgs.mkShell {
            packages = [
              go
              pkgs.gopls
              pkgs.nixfmt
              pkgs.git
              pkgs.gnumake
              pkgs.stdenv.cc
            ];
            GOTOOLCHAIN = "local";
          };
        }
      );
      formatter = forAllSystems (system: perSystem.${system}.pkgs.nixfmt);
      checks = forAllSystems (system: {
        package = self.packages.${system}.herdr-select;
      });
    };
}
