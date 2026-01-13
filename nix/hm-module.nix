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
      type = lib.types.package;
      default = pkgs.atlas;
      description = "The atlas package to use.";
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
    home.packages = [ cfg.package ];

    xdg.configFile."atlas/config.toml" = lib.mkIf (cfg.settings != { }) {
      source = tomlFormat.generate "atlas-config" cfg.settings;
    };
  };
}
