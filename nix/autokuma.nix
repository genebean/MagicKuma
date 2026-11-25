{ config, lib, pkgs, ... }:
let
  cfg = config.services.autokuma;
  inherit (lib) mkOption types;
in {
  options.services.autokuma = {
    enable = lib.mkEnableOption "AutoKuma synchronization";
    package = lib.mkPackageOption pkgs "autokuma" {};
    environment = mkOption { type = types.attrsOf types.str; default = {}; description = "Non-secret upstream AUTOKUMA__ settings."; };
    environmentFile = mkOption { type = types.nullOr types.str; default = null; description = "Runtime systemd EnvironmentFile for Kuma credentials."; };
  };
  config = lib.mkIf cfg.enable {
    users.groups.autokuma = {};
    users.users.autokuma = { isSystemUser = true; group = "autokuma"; };
    systemd.services.autokuma = {
      wantedBy = [ "multi-user.target" ]; after = [ "network-online.target" ]; wants = [ "network-online.target" ];
      environment = { AUTOKUMA__DATA_PATH = "/var/lib/autokuma"; AUTOKUMA__ON_DELETE = "keep"; AUTOKUMA__DOCKER__ENABLED = "false"; } // cfg.environment;
      serviceConfig = {
        User = "autokuma"; Group = "autokuma";
        StateDirectory = "autokuma"; WorkingDirectory = "/var/lib/autokuma"; StateDirectoryMode = "0700";
        ExecStart = lib.getExe cfg.package;
        Restart = "always"; RestartSec = "5s";
        ProtectSystem = "strict"; ProtectHome = true; NoNewPrivileges = true; PrivateTmp = true; UMask = "0077";
      } // lib.optionalAttrs (cfg.environmentFile != null) { EnvironmentFile = cfg.environmentFile; };
    };
  };
}
