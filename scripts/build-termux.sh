#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
out="${1:-dist/termux}"
mkdir -p "$out"
version="${VERSION:-v1.8.6-fabric.2}"
CGO_ENABLED=0 GOOS=android GOARCH=arm64 go build -trimpath \
  -ldflags="-s -w -X 9router/proxy/internal/updater.CurrentVersion=$version" \
  -o "$out/9router-go-android-arm64" ./cmd/9router-go
cp docs/TERMUX.md "$out/README.md"
(cd "$out" && sha256sum 9router-go-android-arm64 > SHA256SUMS)
