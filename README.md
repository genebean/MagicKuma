# MagicKuma

Distributed, declarative monitoring for Uptime Kuma.

A host can publish the checks for its own services, even when another team runs monitoring and the two hosts use different configuration repositories. An administrator assigns the host a token and limits which destinations and monitor types it may declare. The last accepted configuration survives a publishing host going offline.

MagicKuma accepts HTTPS snapshots. AutoKuma synchronizes the definitions into Uptime Kuma. Uptime Kuma runs the checks and holds their history. Only the reconciler needs Kuma credentials.

## Try it

```sh
go build -o magickuma ./cmd/magickuma
./magickuma validate -manifest examples/monitors.json
./magickuma token generate -token-file web.token -hash-file web.sha256
```

Configure a loopback receiver behind HTTPS, then publish using the token file. AutoKuma 2.0.0 and Uptime Kuma 2.5.12 have been tested together with a disposable Linux instance. An empty publication withdraws the declaration and flags the running check for explicit retirement, preserving its history.

- [Standalone setup, supported checks, rotation and retirement](docs/operations.md)
- [Example server configuration](examples/server.json)
- [Example JSON-query monitor](examples/monitors.json)
- [Live integration test](docs/testing.md)

## Project documents

- [Upstream compatibility and architecture](docs/architecture.md)
- [Security and operating assumptions](docs/security.md) and [pre-release security audit](docs/security-audit.md)
- [Publication protocol](docs/api.md)
- [Native and NixOS configuration](docs/configuration.md) and [client/server flake example](examples/nixos.nix)
- [Implementation dependencies and acceptance tests](docs/plan.md)

Apache-2.0. This repository is under development.

## Monitor MagicKuma itself

Keep the receiver's health and readiness checks in central AutoKuma configuration or create them directly in Uptime Kuma. They use the real HTTPS endpoint without a publishing credential. An optional freshness probe detects missed publications. [Central monitor examples](examples/central-monitors) · [Probe meanings and proxy configuration](docs/operations.md#monitor-the-receiver-from-uptime-kuma)
