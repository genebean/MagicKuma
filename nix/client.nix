{ self }:
{ config, lib, pkgs, ... }:
let
  inherit (lib) mkOption types;
  cfg = config.services.magickuma.client;
  manifest = pkgs.writeText "magickuma-manifest.json" (builtins.toJSON {
    schemaVersion = 1;
    inherit (config.monitoring.magickuma) monitors;
  });
  enqueue = "${pkgs.systemd}/bin/systemctl --no-block start magickuma-publish.service";
in {
  options.monitoring.magickuma.monitors = mkOption {
    type = types.attrsOf (types.attrsOf (pkgs.formats.json {}).type);
    default = {};
    description = "Native monitor declarations. These values enter the Nix store.";
  };
  options.services.magickuma.client = {
    enable = lib.mkEnableOption "MagicKuma snapshot publication";
    package = mkOption { type = types.package; default = self.packages.${pkgs.stdenv.hostPlatform.system}.default; };
    url = mkOption { type = types.strMatching "https://[^/]+/?"; description = "Receiver HTTPS origin."; };
    tokenFile = mkOption { type = types.str; description = "Absolute path to the runtime token."; };
    interval = mkOption { type = types.str; default = "15min"; };
    jitter = mkOption { type = types.str; default = "90s"; };
    retryDelay = mkOption { type = types.str; default = "1min"; };
    triggerServices = mkOption { type = types.listOf (types.strMatching "[a-zA-Z0-9_@.-]+[.]service"); default = []; };
  };
  config = lib.mkIf cfg.enable {
    assertions = [{ assertion = lib.hasPrefix "/" cfg.tokenFile && !(lib.hasPrefix "/nix/store/" cfg.tokenFile);
      message = "MagicKuma tokenFile must name an absolute runtime file outside the Nix store."; }];
    systemd.services = lib.mkMerge [
      {
        magickuma-publish = {
          after = [ "network-online.target" ];
          wants = [ "network-online.target" ];
          startLimitIntervalSec = 0;
          serviceConfig = {
            Type = "oneshot";
            DynamicUser = true;
            NoNewPrivileges = true;
            ProtectSystem = "strict";
            ProtectHome = true;
            PrivateTmp = true;
            LoadCredential = "token:${cfg.tokenFile}";
            ExecStart = lib.escapeShellArgs [ (lib.getExe cfg.package) "publish" "-url" cfg.url "-manifest" "${manifest}" "-token-file" "%d/token" ];
            Restart = "on-failure";
            RestartSec = cfg.retryDelay;
          };
        };
        # The activation unit remains active, giving switch-to-configuration
        # something to restart even after the network oneshot has exited.
        magickuma-manifest = {
          wantedBy = [ "multi-user.target" ];
          restartTriggers = [ manifest ];
          serviceConfig = { Type = "oneshot"; RemainAfterExit = true; ExecStart = enqueue; };
        };
      }
      (lib.genAttrs (map (lib.removeSuffix ".service") cfg.triggerServices) (_: {
        serviceConfig.ExecStartPost = lib.mkAfter [ "-+${enqueue}" ];
      }))
    ];
    systemd.timers.magickuma-publish = {
      wantedBy = [ "timers.target" ];
      timerConfig = { OnBootSec = "1min"; OnUnitInactiveSec = cfg.interval; RandomizedDelaySec = cfg.jitter; Unit = "magickuma-publish.service"; };
    };
  };
}
