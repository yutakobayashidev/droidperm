{
  description = "Manage Android runtime permissions and AppOps as code";

  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixpkgs-unstable";

  outputs =
    { self, ... }@inputs:

    let
      goVersion = 25; # Change this to update the whole stack

      supportedSystems = [
        "x86_64-linux"
        "aarch64-linux"
        "aarch64-darwin"
      ];
      forEachSupportedSystem =
        f:
        inputs.nixpkgs.lib.genAttrs supportedSystems (
          system:
          f {
            inherit system;
            pkgs = import inputs.nixpkgs {
              inherit system;
              overlays = [ inputs.self.overlays.default ];
            };
          }
        );
    in
    {
      overlays.default = final: prev: {
        go = final."go_1_${toString goVersion}";
      };

      packages = forEachSupportedSystem (
        { pkgs, ... }:
        let
          droidperm = pkgs.buildGoModule rec {
            pname = "droidperm";
            version = self.shortRev or self.dirtyShortRev or "dev";
            src = self;

            vendorHash = "sha256-ni8JT3c+xSbWd9jjcwZ8lj8e4LkOn+q92+0sC8j7tkE=";
            subPackages = [ "cmd/droidperm" ];

            env.CGO_ENABLED = 0;

            ldflags = [
              "-s"
              "-w"
              "-X main.version=${version}"
            ];

            meta = {
              description = "Manage Android runtime permissions and AppOps as code";
              homepage = "https://github.com/yutakobayashidev/droidperm";
              license = pkgs.lib.licenses.asl20;
              mainProgram = "droidperm";
            };
          };
        in
        {
          inherit droidperm;
          default = droidperm;
        }
      );

      apps = forEachSupportedSystem (
        { pkgs, system }:
        let
          droidperm = {
            type = "app";
            program = pkgs.lib.getExe self.packages.${system}.droidperm;
            meta.description = "Manage Android runtime permissions and AppOps as code";
          };
        in
        {
          inherit droidperm;
          default = droidperm;
        }
      );

      devShells = forEachSupportedSystem (
        { pkgs, system }:
        {
          default = pkgs.mkShell {
            packages = with pkgs; [
              # go (version is specified by overlay)
              go

              # goimports, godoc, etc.
              gotools

              # https://github.com/golangci/golangci-lint
              golangci-lint

              # scripts/restrict-third-party-appops.nu
              nushell

              self.packages.${system}.droidperm
              self.formatter.${system}
            ];
          };
        }
      );

      formatter = forEachSupportedSystem ({ pkgs, ... }: pkgs.nixfmt);
    };
}
