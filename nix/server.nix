{ self }:
{ config, lib, pkgs, ... }:
let
  inherit (lib) mkOption types;
  cfg = config.services.magickuma.server;
  list = t: mkOption { type = types.listOf t; default = []; };
  indexed = name: p: lib.imap0 (i: path: { inherit path; key = "hash-${name}-${toString i}"; }) p.tokenHashFiles;
  credentials = lib.concatLists (lib.mapAttrsToList indexed cfg.clients);
  serverConfig = pkgs.writeText "magickuma-server.json" (builtins.toJSON {
    listen = "${if lib.hasInfix ":" cfg.listenAddress then "[${cfg.listenAddress}]" else cfg.listenAddress}:${toString cfg.port}";
    stateDir = "/var/lib/magickuma";
    inherit (cfg) generatedFile staleAfter;
    clients = lib.mapAttrs (name: p: p // {
      tokenHashFiles = map (x: "credential:${x.key}") (indexed name p);
    }) cfg.clients;
  });
in {
  options.services.magickuma.server = {
    enable = lib.mkEnableOption "MagicKuma receiver";
    package = mkOption { type = types.package; default = self.packages.${pkgs.stdenv.hostPlatform.system}.default; };
    listenAddress = mkOption { type = types.str; default = "127.0.0.1"; };
    port = mkOption { type = types.port; default = 8085; };
    generatedFile = mkOption { type = types.str; default = "/var/lib/magickuma-export/snapshot.json"; description = "Aggregate in a dedicated writable directory."; };
    staleAfter = mkOption { type = types.str; default = "1h"; };
    reconciler = {
      enable = lib.mkEnableOption "a dedicated AutoKuma reader for MagicKuma";
      package = lib.mkPackageOption pkgs "autokuma" {};
      environmentFile = mkOption { type = types.str; description = "Runtime systemd EnvironmentFile containing the upstream Kuma connection settings."; };
    };
    clients = mkOption { default = {}; type = types.attrsOf (types.submodule {
      options = {
        tokenHashFiles = list types.str;
        allowedDomains = list types.str;
        allowedNetworks = list types.str;
        allowedPorts = list types.port;
        allowedMonitorTypes = list types.str;
        unrestricted = mkOption { type = types.bool; default = false; };
        maxMonitors = mkOption { type = types.ints.positive; default = 100; };
        minInterval = mkOption { type = types.ints.positive; default = 60; };
      };
    }); };
  };
  config = lib.mkIf cfg.enable {
    assertions = [
      { assertion = lib.all (n: builtins.match "[a-z0-9][a-z0-9-]{0,62}" n != null) (builtins.attrNames cfg.clients); message = "MagicKuma client names must be lowercase IDs."; }
      { assertion = lib.all (x: lib.hasPrefix "/" x.path && !(lib.hasPrefix "/nix/store/" x.path)) credentials; message = "MagicKuma hash files must be runtime paths."; }
    ];
    users.groups.magickuma-reader = lib.mkIf cfg.reconciler.enable {};
    users.users.magickuma-reader = lib.mkIf cfg.reconciler.enable {
      isSystemUser = true; group = "magickuma-reader"; extraGroups = [ "magickuma" ];
    };
    systemd.services.magickuma-reader = lib.mkIf cfg.reconciler.enable {
      description = "AutoKuma reader for immutable MagicKuma snapshots";
      wantedBy = [ "multi-user.target" ]; after = [ "network-online.target" "magickuma-server.service" ];
      environment.AUTOKUMA__DATA_PATH = "/var/lib/magickuma-autokuma";
      serviceConfig = {
        User = "magickuma-reader"; Group = "magickuma-reader";
        StateDirectory = "magickuma-autokuma"; StateDirectoryMode = "0700";
        RuntimeDirectory = "magickuma-reader"; RuntimeDirectoryMode = "0700";
        WorkingDirectory = "/var/lib/magickuma-autokuma";
        EnvironmentFile = cfg.reconciler.environmentFile;
        ExecStart = lib.escapeShellArgs [
          (lib.getExe cfg.package) "run-autokuma" "-snapshot" cfg.generatedFile
          "-program" (lib.getExe cfg.reconciler.package) "-work-dir" "/run/magickuma-reader"
        ];
        Restart = "always"; RestartSec = "5s"; KillMode = "mixed"; TimeoutStopSec = "25s";
        ProtectSystem = "strict"; ProtectHome = true; PrivateTmp = true; NoNewPrivileges = true; UMask = "0077";
      };
    };
    users.groups.magickuma = {};
    users.users.magickuma = { isSystemUser = true; group = "magickuma"; };
    systemd.services.magickuma-server = {
      description = "MagicKuma publication receiver";
      wantedBy = [ "multi-user.target" ];
      serviceConfig = {
        User = "magickuma"; Group = "magickuma";
        StateDirectory = [ "magickuma" "magickuma-export" ]; StateDirectoryMode = "0750";
        LoadCredential = map (x: "${x.key}:${x.path}") credentials;
        ExecStart = "${lib.getExe cfg.package} serve -config ${serverConfig}";
        Restart = "on-failure"; RestartSec = "5s";
        ProtectSystem = "strict"; ProtectHome = true; PrivateTmp = true; PrivateDevices = true;
        NoNewPrivileges = true; CapabilityBoundingSet = ""; ProtectKernelTunables = true;
        ProtectKernelModules = true; ProtectControlGroups = true; RestrictSUIDSGID = true;
        ReadWritePaths = [ (builtins.dirOf cfg.generatedFile) ];
        RestrictAddressFamilies = [ "AF_UNIX" "AF_INET" "AF_INET6" ];
        UMask = "0027";
      };
    };
  };
}
