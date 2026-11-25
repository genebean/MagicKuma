# Standalone operation

Build with Go 1.24 or later using `go build -o magickuma ./cmd/magickuma`. Install the binary in the service PATH. Copy examples/server.json and adapt the namespace and destination grants to your deployment. One MagicKuma receiver owns one local POSIX filesystem state directory. Use a separate service account for AutoKuma.

1. Run `magickuma token generate -token-file web.token -hash-file web.sha256` in a private directory. The command creates new mode-0600 files and refuses to overwrite existing files. Deliver web.token to the publisher and web.sha256 to the receiver. Rotation accepts both hashes during rollout. Restart the receiver after changing policy or hash files.
2. Give the receiver ownership of /var/lib/magickuma (0700). Create /var/lib/magickuma-export owned by the receiver and its reader group (0750). Grant the reconciler group membership for that export only. Each exported snapshot is 0640. Where the receiver's primary group differs, set the directory's setgid bit. Never share the writable state directory between processes or hosts.
3. Start `magickuma serve -config /etc/magickuma/server.json`. Terminate HTTPS on your proxy and forward the publication route to 127.0.0.1:8085. Preserve the Authorization header. Client certificates and custom trust roots use the ordinary system trust store. See the curl command in the API document.
4. Run a separate supervised `magickuma run-autokuma -program /usr/local/bin/autokuma -snapshot /var/lib/magickuma-export/snapshot.json -work-dir /run/magickuma-reader` as the reader. Create a private runtime work directory and persistent AutoKuma data directory owned by that account. Use a clean working directory. Set AUTOKUMA__DATA_PATH, AUTOKUMA__KUMA__URL, AUTOKUMA__KUMA__USERNAME and AUTOKUMA__KUMA__PASSWORD from a service-manager file. Protect the upstream connection using TLS when it crosses hosts. Restart the supervisor on exit and stop its entire process group at shutdown. Keep all other static configuration in a separate AutoKuma instance with a distinct persistent database and disjoint ownership.
5. Run `magickuma validate -manifest examples/monitors.json`, then `magickuma publish -url https://monitoring.example.org -token-file /run/secrets/web.token -manifest examples/monitors.json`. Local validation checks supported syntax. The server applies the authenticated policy. For private per-client status expose the authenticated GET /api/v1/status route to the client and use `magickuma status` with the same URL and token flags.

## Monitor fields and authorization

| Type | Required native fields besides type/name | Behavior |
| --- | --- | --- |
| http | url | GET, zero redirects, normal TLS validation |
| keyword | url, keyword | Literal body match |
| json-query | url, json_path, expected_value | Simple dotted JSONata paths and optional json_path_operator |
| port | hostname, port | One authorized TCP destination |
| ping | hostname | ICMP to an authorized destination |

Interval defaults to 60 seconds. Explicit grants are required for types, addresses or domains, and ports. unrestricted grants any canonical destination, port and implemented type. Field safety, quotas, template guards and minimum intervals still apply. Arbitrary HTTP headers, methods, secrets, notification references, DNS servers and opaque connection strings are unavailable for delegation in this release. Restrict the executor's network egress to protect IP policy across DNS changes. Use an isolated Kuma instance when stronger separation is required.

## Retire checks and change policy

Omitting an ID (including publishing an empty map) withdraws its generated definition. The real check continues to execute under AutoKuma's keep policy. Ready status becomes degraded and status lists the pending IDs. The generated description carries the authenticated client/local ID to identify the correct check despite duplicate display names. Stop the old reader or confirm it has loaded the new snapshot before carrying out retirement. Pause the identified check in Kuma to preserve history. Deleting a check requires a separate operator decision and can destroy its history.

Stop the receiver and run the administrative command as its service identity with the same config and readable hash files. Run `magickuma inspect -config FILE -client web` for local inspection, then `magickuma ack-retired -config FILE -client web -monitor health` after confirming the pause in Kuma. Restart the receiver. The acknowledgement records an operator's action. If a paused ID is later reintroduced, verify its destination and explicitly resume in Kuma. Its original type stays reserved. Pending retirements count toward the active quota, and each namespace reserves at most 10,000 lifetime IDs.

To revoke only the publishing credential, remove its hash and keep the client policy. The last valid checks stay active. To decommission, revoke all hashes, stop the receiver, run `magickuma decommission -config FILE -client web`, then retire and acknowledge the outstanding checks. Missing policy and tighter destination, type or count grants quarantine previously accepted declarations on receiver startup. The receiver excludes them from future input, persists the reason, and keeps readiness degraded until the operator resolves each check. For immediate containment block executor egress or pause in Kuma first. Apply the policy and inspect pending IDs before restoring traffic.

## Status and recovery

Monitor GET /healthz for process liveness and GET /readyz for generated input and outstanding retirements. Scrape /metrics locally. Keep all three endpoints behind private access controls. Staleness only affects status and metrics. It never changes monitor configuration. Publication timestamps persist for changed and identical requests. Reconciliation success remains observable in AutoKuma logs and Kuma's UI.

Back up manifests, metadata, AutoKuma's data directory and Kuma using their supported backup procedures. Restore them consistently to preserve identity. On startup a corrupt source record aborts regeneration and preserves the previous aggregate. With the receiver stopped a missing generated snapshot can be regenerated from state. Materialization failures leave the old reader on its complete input. If restarting the reader fails after stopping the previous process, its service manager retries. Uptime Kuma continues executing previously configured checks independently.

## Monitor the receiver from Uptime Kuma

Create independent HTTP monitors in Kuma, or copy the two files in examples/central-monitors into the static directory of the **central** AutoKuma instance. Set their URLs to the receiver's actual HTTPS hostname. Keep them outside the MagicKuma-generated directory so they exist before the first publisher and survive a receiver outage. Set normal Kuma notifications on these central monitors.

| Probe from Kuma | A failure tells the operator |
| --- | --- |
| GET /healthz | The HTTPS route, TLS terminator or MagicKuma process is unreachable. |
| GET /readyz | Local generation or the last state write failed, or a formerly authorized check needs retirement. Inspect logs and per-client status. |
| GET /freshz (optional) | At least one credentialed publisher has never published or is past staleAfter. Existing checks continue. |

Forward these exact GET paths through the same HTTPS frontend that accepts publication. Restrict them to Kuma's source network when possible. Each returns one boolean and a 200 or 503 response. No publishing token is needed. Keep /metrics private for Prometheus. These HTTP monitors do not prove a PUT succeeds or that AutoKuma applied a change. The reader logs the generated snapshot digest and child PID when starting AutoKuma. Inspect AutoKuma logs and Kuma's own check results for downstream failures.

An nginx example for the private probes (replace the example monitoring address):

```nginx
location ~ ^/(healthz|readyz|freshz)$ {
    allow 192.0.2.10;
    deny all;
    limit_except GET { deny all; }
    proxy_pass http://127.0.0.1:8085;
}
```

## Binaries and containers

Reviewed v-prefixed release tags trigger the full CI workflow. After verification, the release job builds Linux amd64/arm64 and Darwin arm64 archives, publishes SHA256SUMS, and pushes a non-root two-platform Linux image to GHCR. Until a release is tagged, build locally from the reviewed checkout. Verify an archive with its SHA256SUMS before installing the executable.

```sh
docker build -t magickuma:local .
docker run --rm -v "$PWD/examples:/examples:ro" magickuma:local validate -manifest /examples/monitors.json
```

The image runs as UID/GID 65532. Mount the config and token/hash files read-only, and provide writable state and export mounts for serve. Use a TLS proxy sharing the receiver's network namespace to reach its loopback listener. Another option on Linux is host networking with the proxy on the same host. The image contains the Go executable and system trust bundle. Run the upstream AutoKuma executable separately under the coordinated reader.
