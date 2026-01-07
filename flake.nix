{
  description = "atlas CLI (Confluence/Bitbucket) — x86_64-linux only";

  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";

  outputs =
    { self, nixpkgs }:
    let
      systems = [ "x86_64-linux" ];
      forAllSystems = f: nixpkgs.lib.genAttrs systems (system: f system);
    in
    {
      packages = forAllSystems (
        system:
        let
          pkgs = import nixpkgs { inherit system; };
          lib = pkgs.lib;
        in
        {
          default = pkgs.buildGoModule rec {
            pname = "atlas";
            version = "0.0.1";
            src = ./.;
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

            # ensure tests run under nix
            doCheck = true;
            checkPhase = "make test";

            # ensure fully static build as your makefile requests
            env.CGO_ENABLED = 0;

            meta = with lib; {
              description = "Atlas CLI that fetches Confluence/Bitbucket content and prints markdown-wrapped output";
              homepage = "https://github.com/kabilan108/atlas";
              license = licenses.asl20;
              platforms = [ system ];
              mainProgram = "atlas";
            };

            # toolchain for build/test (buildgomodule wires go itself, but make/useful tools live here)
            nativeBuildInputs = [ pkgs.makeWrapper ];
          };
        }
      );

      # optional: a minimal dev shell
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
