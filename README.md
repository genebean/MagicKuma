# MagicKuma

Distributed, declarative monitoring for Uptime Kuma.

A host can publish the checks for its own services, even when another team runs monitoring and the two hosts use different configuration repositories. An administrator assigns the host a token and limits which destinations and monitor types it may declare. The last accepted configuration survives a publishing host going offline.

MagicKuma accepts HTTPS snapshots. AutoKuma synchronizes the definitions into Uptime Kuma. Uptime Kuma runs the checks and holds their history. Only the reconciler needs Kuma credentials.

## Project documents

- [Upstream compatibility and architecture](docs/architecture.md)
- [Security and operating assumptions](docs/security.md)
- [Publication protocol](docs/api.md)
- [Native and NixOS configuration](docs/configuration.md)
- [Implementation dependencies and acceptance tests](docs/plan.md)

Apache-2.0. This repository is under development.
