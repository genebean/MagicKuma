#!/usr/bin/env bash
set -euo pipefail
version=${1:?supply a release version}
out=${2:?supply an output directory}
[[ $version =~ ^v[0-9]+\.[0-9]+\.[0-9]+([.-][a-zA-Z0-9.-]+)?$ ]]
mkdir -p "$out"
out=$(cd "$out" && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
for platform in linux/amd64 linux/arm64 darwin/arm64; do
  os=${platform%/*}
  arch=${platform#*/}
  CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" go build -trimpath \
    -ldflags="-s -w -X github.com/genebean/magickuma/internal/magic.Version=$version" \
    -o "$work/magickuma" ./cmd/magickuma
  cp LICENSE "$work/LICENSE"
  tar -czf "$out/magickuma-${version}-${os}-${arch}.tar.gz" -C "$work" magickuma LICENSE
done
(cd "$out" && sha256sum magickuma-"$version"-*.tar.gz > SHA256SUMS)
