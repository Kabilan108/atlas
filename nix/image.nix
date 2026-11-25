{
  atlas,
  dockerTools,
}:
let
  uid = "65532";
  port = "8303";
in
dockerTools.streamLayeredImage {
  name = "ghcr.io/kabilan108/atlas";
  tag = atlas.version;
  contents = [
    atlas
    dockerTools.caCertificates
  ];
  extraCommands = ''
    mkdir -m 1777 tmp
  '';
  config = {
    User = "${uid}:${uid}";
    Entrypoint = [
      "${atlas}/bin/atlas"
      "mcp"
      "serve"
      "--addr"
      ":${port}"
    ];
    ExposedPorts."${port}/tcp" = { };
    Env = [ "SSL_CERT_FILE=/etc/ssl/certs/ca-bundle.crt" ];
    Labels = {
      "org.opencontainers.image.source" = "https://github.com/Kabilan108/atlas";
      "org.opencontainers.image.description" = atlas.meta.description;
      "org.opencontainers.image.licenses" = "Apache-2.0";
      "org.opencontainers.image.version" = atlas.version;
    };
  };
}
