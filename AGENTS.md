# Working on MagicKuma

Use focused branches and pull requests. Keep the default branch deployable. Link a change to its tracking issue. Use ordinary Go and the standard library unless a dependency buys a concrete capability. Run gofmt, go vet ./..., go test -race ./..., and nix flake check for the platforms affected.

The token selects the client namespace. Treat every byte in a publication as untrusted. Review a new field through the entire chain from JSON through AutoKuma's template and TOML conversion to the check Uptime Kuma actually executes. Pair every delegated capability with policy tests. Keep network destinations, credentials, and payloads out of logs.

Preserve last-known-good state, keep manual monitors outside our ownership, and distinguish acceptance, local generation and downstream execution. Exercise restart, failed publication, concurrency and destructive edge cases. Changes to ownership, type replacement, removal semantics or policy revocation require a design review and an integration test against pinned releases.

Keep the standalone protocol usable with curl. NixOS modules translate the same native configuration. Use synthetic hosts and credentials in examples and tests. Run service integration only against disposable instances.

## Repository map and review workflow

The project starts at cmd/magickuma. The internal/magic package owns strict
JSON parsing, destination policy, the single-writer state store, the HTTP
interface and supervision of the static-file reader. The nix directory
contains independent modules. The tests directory holds the live Kuma
fixture and the two-machine NixOS test. CI and release helpers live in ci.

Read docs/architecture.md, docs/security.md and docs/operations.md before
changing delegated capabilities. Start with issue #1 to find the relevant
work item. The compatibility fixture uses AutoKuma 2.0.0 and Uptime Kuma
2.5.12. Verify any changed behavior against tagged source and a real test.

Use a short imperative commit subject of at most 50 characters. Leave one
blank line before the body and wrap prose at 72 characters. Every
agent-authored commit includes a Co-authored-by trailer identifying the
agent and the actual model used. For this session the trailer is:

    Co-authored-by: OpenAI Codex (GPT-6) <codex@openai.com>

Keep checks in dependency order: formatting and vet, race-tested Go tests,
then live upstream and NixOS VM integration. Publish JUnit results and a
GitHub job summary. Avoid leaking target URLs or tokens through public
metrics or test artifacts. Exercise operational probes through failures.
Central receiver health monitors must work even if publication is down.

Use the pinned flake or Go 1.24+ for development. Run:

    gofmt -l cmd internal
    go mod tidy -diff
    go vet ./...
    go test -race ./...
    nix flake check --all-systems --no-build
    nix build -L --no-link .#checks.x86_64-linux.publication
    bash ci/live.sh

The last two commands need a KVM-capable Linux host and a Linux amd64 host
with Go, Node, npm, openssl, tar and curl, respectively. Give each PR a
clear dependency/base and record which checks actually ran. Scope all
fixtures to disposable state. A release tag is an explicit maintainer
action after review. Changes to this repository do not authorize a
deployment to a consumer's infrastructure.
