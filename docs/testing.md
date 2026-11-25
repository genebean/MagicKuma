# Verification

Run go vet ./... and go test -race ./.... The live fixture creates fresh loopback services, certificates, accounts and state. It uses only the Kuma Socket.IO interface and runs the released AutoKuma executable. It verifies the real numeric monitor ID, zero redirects, recoverable state, retained checks and coexistence with a manual monitor.

Set KUMA_SOURCE to a checkout of louislam/uptime-kuma at 2.5.12 with production npm dependencies and the matching release dist installed. Set AUTOKUMA to BigBoot/AutoKuma v2.0.0's Linux executable and MAGICKUMA to the built CLI. Run node tests/live.cjs. Node, openssl and a loopback-capable Linux environment are required. The fixture prints the location of its disposable logs on failure. Treat those logs and its files as private.

The flake pins the nixpkgs revision for packaging and VM evaluation. The live fixture pins the upstream pair independently so consumer nixpkgs overrides cannot silently redefine the compatibility claim.
