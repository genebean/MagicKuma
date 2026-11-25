# Verification

Run these commands from the repository root.

```sh
go vet ./...
go test -race ./...
bash ci/live.sh
nix build -L --no-link .#checks.x86_64-linux.publication
```

The live wrapper fetches the pinned upstream sources and verifies the release artifact checksums. Run it on Linux amd64 with Go, Node, npm, curl, tar and OpenSSL. The NixOS command needs a Linux runner with KVM.

 The live fixture creates fresh loopback services, certificates, accounts and state. It uses only the Kuma Socket.IO interface and runs the released AutoKuma executable. It verifies the real numeric monitor ID, zero redirects, recoverable state, retained checks and coexistence with a manual monitor.

To use locally provisioned upstream builds instead, set KUMA_SOURCE to a checkout of louislam/uptime-kuma at 2.5.12 with production npm dependencies and the matching release dist installed. Set AUTOKUMA to BigBoot/AutoKuma v2.0.0's Linux executable and MAGICKUMA to the built CLI. Run node tests/live.cjs. Node, openssl and a loopback-capable Linux environment are required. The fixture prints the location of its disposable logs on failure. Treat those logs and its files as private.

The flake pins the nixpkgs revision for packaging and VM evaluation. The live fixture pins the upstream pair independently so consumer nixpkgs overrides cannot silently redefine the compatibility claim.

GitHub Actions runs formatting, module consistency, go vet and cross-compilation first. Race tests run next. The live upstream and two-node NixOS jobs depend on the unit job. Each test job publishes JUnit XML and a job summary. Go results name individual cases. The live and VM reports each wrap a suite invocation. Read the corresponding job log for the failing integration step. Go coverage is available beside the unit report. Fork PRs need only read permission. The flake VM requires a Linux runner with KVM.

The static job also scans the complete checkout with Betterleaks v1.10.1 and runs govulncheck against the Go source. Betterleaks uses redacted output with live credential validation disabled. The secret-scan JUnit artifact contains only the result and duration. The image job extracts the shipped executable and runs binary-mode govulncheck against it.
