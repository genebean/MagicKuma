# Import the client and server examples on the corresponding independent hosts.
{
  client = { inputs, ... }: {
    imports = [ inputs.magickuma.nixosModules.client ];
    services.magickuma.client = {
      enable = true;
      url = "https://monitoring.example.org";
      tokenFile = "/run/secrets/web.token";
      triggerServices = [ "nginx.service" ];
    };
    monitoring.magickuma.monitors.health = {
      type = "http"; name = "Web health"; url = "https://web.example.org/health";
    };
  };
  server = { inputs, ... }: {
    imports = [ inputs.magickuma.nixosModules.server ];
    services.magickuma.server = {
      enable = true;
      clients.web = {
        tokenHashFiles = [ "/run/secrets/web.sha256" ];
        allowedDomains = [ "web.example.org" ];
        allowedMonitorTypes = [ "http" ];
        allowedPorts = [ 443 ];
      };
      reconciler = { enable = true; environmentFile = "/run/secrets/autokuma.env"; };
    };
    # Point an existing TLS reverse proxy at the receiver. Example location:
    # services.nginx.virtualHosts."monitoring.example.org".locations."= /api/v1/monitors".proxyPass =
    #   "http://127.0.0.1:8085";
  };
}
