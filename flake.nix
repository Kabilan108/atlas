{
  description = "atlas CLI and MCP server (Bitbucket/Jira)";

  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";

  outputs =
    { nixpkgs, ... }:
    let
      systems = [
        "x86_64-linux"
        "aarch64-linux"
      ];
      forAllSystems = f: nixpkgs.lib.genAttrs systems (system: f system);
    in
    {
      overlays.default = final: prev: {
        atlas = final.callPackage ./nix/package.nix { };
      };

      packages = forAllSystems (
        system:
        let
          pkgs = import nixpkgs { inherit system; };
          atlas = pkgs.callPackage ./nix/package.nix { };
        in
        {
          default = atlas;
          image = pkgs.callPackage ./nix/image.nix { inherit atlas; };
        }
      );

      homeManagerModules.default = import ./nix/hm-module.nix;

      devShells = forAllSystems (
        system:
        let
          pkgs = import nixpkgs { inherit system; };
        in
        {
          default = pkgs.mkShell {
            buildInputs = with pkgs; [
              go
              gopls
              git
              gnumake
            ];
          };
        }
      );
    };
}
