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

A publication trigger must enqueue an independent job without making a monitored service depend on the remote receiver. A timer provides eventual delivery after temporary failures.

## NixOS installation

Use the linked client and server modules on the corresponding machines. Each host can evaluate its own flake. The client timer and service restart hooks enqueue the same oneshot. A separate active activation unit detects changes to the Nix-generated manifest. A failed network call retries independently. Token and hash files are supplied to systemd as runtime credentials. Restart the receiver after rotating hash files.

[Complete consumer example](../examples/nixos.nix)

For a dedicated MagicKuma reconciler enable services.magickuma.server.reconciler and supply its environmentFile. The receiver and reader run as distinct users. The data and runtime directories are created by systemd. With a custom generatedFile, create the dedicated parent directory with receiver write and reader traverse permissions. To run an unrelated AutoKuma instance import nixosModules.autokuma separately and use services.autokuma.environment and services.autokuma.environmentFile.

For offline administration with the NixOS service stopped, use a copy of the non-secret native configuration whose tokenHashFiles refer to the original readable runtime hash paths. The systemd credential directory used by the generated service configuration exists only while the unit is running. Run administrative commands as the receiver user with access to those hashes or use a transient unit with the corresponding LoadCredential entries. Preserve state/export ownership.

The package, app and development shell support x86_64-linux, aarch64-linux and aarch64-darwin. Linux checks include the two-machine VM scenario. Consumers can pin a reviewed revision and follow their own nixpkgs input.
