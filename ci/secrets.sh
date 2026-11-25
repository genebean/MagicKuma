#!/usr/bin/env bash
set -euo pipefail
version=1.10.1
digest=c174b42d7aa60caf58c0c3e555c0e4bed01fe6b0d99a412bce777b5af4ce913e
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
asset="betterleaks_${version}_linux_x64.tar.gz"
curl --fail --silent --show-error --location --retry 3 \
  "https://github.com/betterleaks/betterleaks/releases/download/v$version/$asset" -o "$work/$asset"
(cd "$work" && printf '%s  %s\n' "$digest" "$asset" | sha256sum -c -)
tar -xzf "$work/$asset" -C "$work" betterleaks
"$work/betterleaks" git --redact=100 --validation=false --log-opts=--all .
"$work/betterleaks" dir --redact=100 --validation=false .
