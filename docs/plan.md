# Implementation and review plan

Epic: Distributed declarative monitoring with a secure standalone receiver.

| Work item | Depends on | Acceptance criteria |
| --- | --- | --- |
| 1. Establish contracts and compatibility | Released-source review | Record name search, releases, ID derivation, templating, scanner consistency and deletion hazards. |
| 2. Authenticated snapshots | 1 | Strict bounded parsing, rotation, per-type validation, namespace isolation, TLS publisher and negative tests. |
| 3. Durable desired state | 2 | Serialize concurrent writes, retain complete last-known-good data, regenerate after failure, refresh unchanged submissions, expose freshness without publishing secrets. |
| 4. Coordinated AutoKuma reader | 1, 3 | Immutable per-process input, stable data path, retries after outage, pinned live tests for identity, update, retention and coexistence. |
| 5. NixOS deployment | 2, 3 | Independent client/server/AutoKuma modules, Linux packages, Darwin CLI, runtime credentials. VM tests for HTTPS, timer, new configuration and non-blocking service restart hooks. |
| 6. History-safe retirement and revocation | Operator decision, 4 | Decommission explicitly, report outstanding retirement, verify paused or removed checks, test policy tightening and type changes across empty snapshots. |
| 7. Distribution and operations | 4, 5, 6 | Release CI, checksums, OCI, Prometheus, tested recovery and installation instructions. |
| 8. Further delegation | Security review per type | DNS, MQTT, database and notification references have policy and integration fixtures. Puppet and Ansible clients use the same protocol. Discovery remains opt-in. |

Block production rollout until live reconciliation, revocation and NixOS deployment tests pass. PRs can be reviewed independently of any deployment. Each new adapter must name all fields that influence network activity.
