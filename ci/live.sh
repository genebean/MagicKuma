#!/usr/bin/env bash
set -euo pipefail
project=$(pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
source_dir="$work/uptime-kuma"
mkdir "$source_dir"
curl --fail --silent --show-error --location --retry 3 \
  "https://codeload.github.com/louislam/uptime-kuma/tar.gz/5aa5dec425f8a955b54e150636979a6927745ec5" |
  tar -xz --strip-components=1 -C "$source_dir"
curl --fail --silent --show-error --location --retry 3 \
  "https://github.com/louislam/uptime-kuma/releases/download/2.5.12/dist.tar.gz" |
  tar -xz -C "$source_dir"
(cd "$source_dir" && npm ci --omit=dev --no-audit --no-fund)
curl --fail --silent --show-error --location --retry 3 \
  "https://github.com/BigBoot/AutoKuma/releases/download/v2.0.0/autokuma-linux" -o "$work/autokuma"
(cd "$work" && echo '844c3e7be680a7b4216e80148635316bf156dde706c9cea3b039c4b00edea286  autokuma' | sha256sum -c -)
chmod +x "$work/autokuma"
go build -o "$work/magickuma" ./cmd/magickuma
KUMA_SOURCE="$source_dir" AUTOKUMA="$work/autokuma" MAGICKUMA="$work/magickuma" node "$project/tests/live.cjs"
