{
  config,
  lib,
  pkgs,
  ...
}:

let
  cfg = config.programs.atlas;
  tomlFormat = pkgs.formats.toml { };
in
{
  options.programs.atlas = {
    enable = lib.mkEnableOption "atlas CLI for Bitbucket";

    package = lib.mkOption {
      type = lib.types.nullOr lib.types.package;
      default = null;
      description = "The atlas package to install. If null, the package must be installed separately.";
    };

    settings = lib.mkOption {
      type = tomlFormat.type;
      default = { };
      example = lib.literalExpression ''
        {
          workspace = "mycompany";
          username = "user@example.com";
          app_password = "\''${env:ATLAS_APP_PASSWORD}";
        }
      '';
      description = "Configuration written to ~/.config/atlas/config.toml";
    };
  };

  config = lib.mkIf cfg.enable {
    home.packages = lib.mkIf (cfg.package != null) [ cfg.package ];

    xdg.configFile."atlas/config.toml" = lib.mkIf (cfg.settings != { }) {
      source = tomlFormat.generate "atlas-config" cfg.settings;
    };
  };
}
