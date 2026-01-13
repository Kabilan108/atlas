{
  description = "atlas CLI (Confluence/Bitbucket) — x86_64-linux only";

  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";

  outputs =
    { self, nixpkgs }:
    let
      systems = [ "x86_64-linux" ];
      forAllSystems = f: nixpkgs.lib.genAttrs systems (system: f system);

      mkPackage =
        pkgs:
        pkgs.buildGoModule rec {
          pname = "atlas";
          version = "0.0.1";
          src = self;
          vendorHash = "sha256-wZFWVVXNPOjF6S9midTi/xB/+kN6svZnPUXdWSFomYE=";

          buildPhase = ''
            runHook preBuild
            make build VERSION=${version}
            runHook postBuild
          '';

          installPhase = ''
            runHook preInstall

            install -Dm755 build/atlas $out/bin/atlas

            # Shell completions (Cobra-generated)
            install -d $out/share/bash-completion/completions
            $out/bin/atlas completion bash > $out/share/bash-completion/completions/atlas

            install -d $out/share/zsh/site-functions
            $out/bin/atlas completion zsh > $out/share/zsh/site-functions/_atlas

            install -d $out/share/fish/vendor_completions.d
            $out/bin/atlas completion fish > $out/share/fish/vendor_completions.d/atlas.fish

            runHook postInstall
          '';

          doCheck = true;
          checkPhase = "make test";

          env.CGO_ENABLED = 0;

          meta = with pkgs.lib; {
            description = "Atlas CLI that fetches Confluence/Bitbucket content and prints markdown-wrapped output";
            homepage = "https://github.com/kabilan108/atlas";
            license = licenses.asl20;
            platforms = [ "x86_64-linux" ];
            mainProgram = "atlas";
          };

          nativeBuildInputs = [ pkgs.makeWrapper ];
        };
    in
    {
      packages = forAllSystems (
        system:
        let
          pkgs = import nixpkgs { inherit system; };
        in
        {
          default = mkPackage pkgs;
        }
      );

      overlays.default = final: prev: {
        atlas = mkPackage final;
      };

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
