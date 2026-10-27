{
  lib,
  buildGoModule,
  git,
  makeWrapper,
}:
buildGoModule rec {
  pname = "atlas";
  version = "0.1.3";
  src = ../.;
  vendorHash = "sha256-gEMzw1zfgXLlfwkSydNbZ8b4A/6FanI8tsdhSu3vhmE=";

  buildPhase = ''
    runHook preBuild
    make build VERSION=${version}
    runHook postBuild
  '';

  installPhase = ''
    runHook preInstall

    install -Dm755 bin/atlas $out/bin/atlas

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
  checkPhase = "make test VERSION=${version}";
  nativeCheckInputs = [ git ];

  env.CGO_ENABLED = 0;

  meta = {
    description = "Atlas CLI that fetches Confluence/Bitbucket content and prints markdown-wrapped output";
    homepage = "https://github.com/kabilan108/atlas";
    license = lib.licenses.asl20;
    platforms = [ "x86_64-linux" ];
    mainProgram = "atlas";
  };

  nativeBuildInputs = [ makeWrapper ];
}
