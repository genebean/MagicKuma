# Pre-release security audit

Reviewed 2026-11-25. Scope includes the Go receiver, publisher, local administrative commands, generated AutoKuma input, reader supervisor, NixOS units, OCI image recipe, GitHub Actions, and release scripts. The reference deployment uses a loopback receiver behind a TLS proxy. Testing used disposable systems.

## Findings addressed in this change

| Boundary | Finding | Resolution and evidence |
| --- | --- | --- |
| Receiver admission | A per-body limit alone allowed many valid credentials or requests to queue large decoded snapshots. | Bound concurrent authenticated PUT bodies to 16 per handler. Excess work gets 429 and Retry-After before body parsing. The busy-path test uses a saturated handler. |
| Authoritative state | An administrator could accidentally point the generated aggregate into the manifest or metadata directories. Group-writable working directories also allowed unrelated local writers to change the reader input. | Reject overlapping paths, including directory aliases, before generation. Require service ownership and exclusive owner write access. Retain group read for the exported snapshot. Tests cover group-writable directories and symlinked source/export overlap. |
| Native values | A JSON number could exceed the range accepted by AutoKuma's TOML conversion. Percent-encoding could expand a URL past the limit used on restart. | Validate integer and float ranges and the final URL representation before committing. Exercise normalization twice in a fuzz test. |
| Check scheduling | The upstream retry interval has its own default. | Generate retry_interval from the admitted interval. The live test reads the resulting value through Kuma's API. |
| Secret files and processes | Failures while removing partial token files or retired process inputs could be hidden. | Preserve cleanup errors for token generation and log reader cleanup failures. Use absolute executable and input paths. |
| Release supply chain | The builder tag and frontend archive could change independently of review. A release rerun could overwrite assets of an already published release. | Pin the multi-platform Go builder index by digest, verify upstream binary and frontend checksums, serialize releases by tag, and reserve a draft before publishing. Published releases fail that check. CI checkouts discard credentials. |

## Manual review

- Traced bearer generation, file permissions, hash loading, overlapping credentials, constant-time comparisons and namespace selection. The server derives identity from the credential. Request fields cannot select a different client. The HTTPS publisher rejects untrusted certificates and redirects. Tests cover both cases.
- Traced every admitted property for HTTP, keyword, JSON-query, TCP and ping through normalization, storage, Tera/TOML conversion and generated JSON. Rejected forms include arbitrary headers, proxies, credential fields, redirects, raw quoted strings, template expressions and unsupported types. The receiver writes its own stable identity description. Simple dotted JSONata paths are the only delegated query expressions.
- Reviewed CIDR matching, mapped IPv6, DNS label boundaries, fixed and explicit ports, and the separation of DNS and literal-IP grants. Reviewed quotas across removed IDs, reserved types, current-policy revalidation and explicit retirement. The server never removes a monitor because a publisher is late.
- Reviewed same-directory temp files, restrictive modes, fsync/rename order, writer locking, source corruption and recovery. A directory-sync failure after the authoritative rename fences the writer. The reader uses one complete private directory per child. It stops and reaps the old child before starting the next. AutoKuma's ownership database remains persistent.
- Reviewed proxy exposure, systemd credentials and privileges, separation of receiver/reader users, public probe output, logs and metric labels. Rotation takes effect when the receiver restarts. The privileged service hook queues a fixed, non-blocking systemd command with an absolute executable path.
- Reviewed the PR-to-release workflow. PR jobs have read-only repository permission. Pinned actions execute without persisted checkout credentials. Release writes require a maintainer-created release tag and successful CI. The final image uses scratch, the CA trust bundle, the binary, its license, and UID/GID 65532.

## Verification

| Check | Result or interpretation |
| --- | --- |
| Go 1.27.4, go vet, race-enabled tests | Passed. Tests include TLS trust/redirect handling, admission limits, atomic rejection, rotation, namespace separation, restart recovery, staleness and retirement. |
| govulncheck 1.10.0 | No known vulnerabilities reported in the Go source or locally built Linux amd64, Linux arm64 and Darwin arm64 distribution executables. The OCI CI job scans the binary extracted from the built image. MagicKuma's Go module has no third-party runtime dependencies. |
| gosec 2.30.0 | Reviewed every remaining report: eight G304 variable-path notices and two G703 taint notices. File/config paths originate in administrator configuration or local CLI arguments. Recursive cleanup receives a private directory created by MkdirTemp. A submission cannot supply any of these paths. No scanner rules were suppressed. |
| Go fuzzing | Ran the publication normalization round-trip target for 25 seconds without a failure. Seeds run during ordinary Go tests. |
| actionlint 1.7.12 | Both workflow files passed. |
| Betterleaks 1.10.1 | ci/secrets.sh checks Git history and the working tree with output redaction and live credential validation disabled. GitHub publishes a pass/fail JUnit result without raw findings. |
| Integration | The live released pair and two-host NixOS VM are CI gates. See the PR's check run for the exact revision. The live test uses a synthetic HTTPS publisher, upstream APIs, a real target service, and the real AutoKuma binary. |

The audited action releases are checkout 7.0.1, setup-go 7.1.0, setup-node 7.1.0, upload-artifact 7.0.2 and install-nix-action 32.1.0. Each workflow references full commit SHAs. The compatibility pair is AutoKuma 2.0.0 and Uptime Kuma 2.5.12. Review new upstream versions together with the fixture before updating a compatibility claim.

## Follow-up review

Two independent AI reviewers examined implementation complexity and first-time operator documentation. The follow-up changes give offline inspection a shared, read-only lock and restrict it to the requested client's record and timestamp. Startup counts distinct pending and declared checks when enforcing a tightened quota. Regression tests cover both cases and the committed timestamp returned to concurrent publishers. Strict parsing still validates duplicate keys and nesting before decoding monitor values.

## Remaining operating assumptions

- A domain grant delegates requests to that name and its permitted ports, including any path. DNS may change between publication and execution. Enforce network isolation at the executor and grant only origins safe for the permitted request types.
- A steady-state check interval does not cap initial checks after edits. Apply ingress rate limits and service limits appropriate to the fleet. The application bounds admitted concurrent bodies; the proxy remains responsible for connection-level flood control.
- The receiver can verify durable acceptance and local generation. Only AutoKuma and Uptime Kuma can confirm downstream state. History-preserving retirement requires a human to pause the correct check, then acknowledge it. An administrative acknowledgement is an assertion by that operator.
- Protect the filesystem ancestors, TLS terminator, systemd units and AutoKuma environment from untrusted writers. Back up desired state, the AutoKuma mapping database and Kuma's state together. Losing ownership mappings can create duplicates.
- Release archives were built and scanned locally. Publishing a real tag and exercising registry/release credentials is a maintainer operation. No production deployment or network penetration testing was part of this audit.

[Go vulnerability scanning](https://go.dev/doc/security/vuln/) · [GitHub Actions secure use](https://docs.github.com/en/actions/reference/security/secure-use) · [OWASP SSRF guidance](https://cheatsheetseries.owasp.org/cheatsheets/Server_Side_Request_Forgery_Prevention_Cheat_Sheet.html) · [Betterleaks release](https://github.com/betterleaks/betterleaks/releases/tag/v1.10.1)
