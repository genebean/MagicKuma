{ pkgs, self }:
pkgs.testers.runNixOSTest {
  name = "magickuma-publication";
  nodes.server = { pkgs, ... }: {
    imports = [ self.nixosModules.server ];
    services.magickuma.server = {
      enable = true;
      clients.web = {
        tokenHashFiles = [ "/run/mk-test/hash" ];
        allowedMonitorTypes = [ "http" ]; allowedDomains = [ "web.example.org" ]; allowedPorts = [ 443 ];
      };
    };
    networking.firewall.allowedTCPPorts = [ 443 ];
    services.nginx.enable = true;
    services.nginx.virtualHosts.server = {
      onlySSL = true; sslCertificate = "/run/mk-test/cert.pem"; sslCertificateKey = "/run/mk-test/key.pem";
      locations."/api/v1/".proxyPass = "http://127.0.0.1:8085";
    };
    systemd.services.magickuma-fixture = {
      wantedBy = [ "multi-user.target" ];
      before = [ "nginx.service" "magickuma-server.service" "nginx-config-reload.service" ];
      serviceConfig = { Type = "oneshot"; RemainAfterExit = true; };
      script = ''
        install -d -m 0750 -g nginx /run/mk-test
        umask 077
        printf '%s' 'vm-only-test-token' | ${pkgs.coreutils}/bin/sha256sum | cut -d' ' -f1 > /run/mk-test/hash
        ${pkgs.openssl}/bin/openssl req -x509 -newkey rsa:2048 -nodes -days 1 \
          -subj /CN=server -addext subjectAltName=DNS:server \
          -keyout /run/mk-test/key.pem -out /run/mk-test/cert.pem
        chgrp nginx /run/mk-test/{cert,key}.pem
        chmod 0640 /run/mk-test/{cert,key}.pem
      '';
    };
  };
  nodes.client = { pkgs, lib, ... }: {
    imports = [ self.nixosModules.client ];
    services.nginx.enable = true;
    services.nginx.virtualHosts.localhost.locations."/".return = "200 ok";
    services.magickuma.client = {
      enable = true; url = "https://server"; tokenFile = "/run/web.token";
      triggerServices = [ "nginx.service" ]; interval = "4s"; jitter = "0"; retryDelay = "2s";
    };
    monitoring.magickuma.monitors.health = {
      type = "http"; name = "Original"; url = "https://web.example.org/health";
    };
    systemd.services.magickuma-publish.environment.SSL_CERT_FILE = "/run/test-ca.pem";
    systemd.timers.magickuma-publish.timerConfig.AccuracySec = "1s";
    specialisation.changed.configuration.monitoring.magickuma.monitors.health.name = lib.mkForce "Changed";
    virtualisation.memorySize = 1536;
  };
  testScript = ''
    import base64
    import shlex
    server.start()
    server.wait_for_unit("magickuma-server.service")
    server.wait_for_unit("nginx.service")
    client.start()
    client.wait_for_unit("multi-user.target")
    cert = base64.b64encode(server.succeed("cat /run/mk-test/cert.pem").encode()).decode()
    client.succeed(f"echo {cert} | base64 -d > /run/test-ca.pem; printf vm-only-test-token > /run/web.token")
    record = "/var/lib/magickuma/manifests/web.json"
    stamp = "/var/lib/magickuma/metadata/web.json"
    # Boot activation queued the oneshot, whose retries now see the credentials.
    server.wait_until_succeeds(f"test -s {record}")
    server.wait_until_succeeds(f"test -s {stamp}")
    client.succeed("systemctl stop magickuma-publish.timer")
    def next_stamp(command):
        old = server.succeed(f"cat {stamp}").strip()
        client.succeed(command)
        server.wait_until_succeeds(f"test \"$(cat {stamp})\" != {shlex.quote(old)}")
    next_stamp("systemctl restart nginx")
    client.succeed("systemctl stop nginx")
    next_stamp("systemctl start nginx")
    client.succeed("/run/current-system/specialisation/changed/bin/switch-to-configuration test")
    server.wait_until_succeeds(f"grep -q Changed {record}")
    server.succeed("systemctl stop magickuma-server")
    client.succeed("systemctl restart nginx; systemctl is-active nginx; curl -fsS http://localhost/ >/dev/null")
    server.succeed("systemctl start magickuma-server")
    client.shutdown()
    server.succeed("systemctl restart magickuma-server")
    server.succeed("grep -q Changed /var/lib/magickuma-export/snapshot.json")
  '';
}
