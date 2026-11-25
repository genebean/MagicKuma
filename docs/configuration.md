# Configuration interfaces

Native server JSON and NixOS expose the same client policy. A client name is chosen by the server. Each token hash belongs to one client. Independent NixOS modules can contribute entries to monitoring.magickuma.monitors. Publication settings belong to services.magickuma.client. A non-Nix client can submit the same JSON with curl.

```json
{"listen":"127.0.0.1:8085","stateDir":"/var/lib/magickuma","clients":{"web":{"tokenHashFiles":["/run/secrets/web.sha256"],"allowedDomains":["web.example.org","*.apps.example.org"],"allowedPorts":[443],"allowedMonitorTypes":["http","keyword"],"maxMonitors":40,"minInterval":60}}}
```

Exact names match only themselves. A wildcard grants exactly one label beneath the suffix. Disjoint literal-IP grants use allowedNetworks. Unrestricted destination access is explicit and retains property validation and resource ceilings. Secret files belong outside the Nix store. Omit hashes or set an empty list to revoke publication while retaining existing monitors.

```nix
{
  inputs.magickuma.url = "github:genebean/magickuma";
  inputs.magickuma.inputs.nixpkgs.follows = "nixpkgs";
}
```

See the client, server and reconciliation examples as their implementation lands. A publication trigger must enqueue an independent job without making a monitored service depend on the remote receiver. A timer provides eventual delivery after temporary failures.
