#!/usr/bin/env bash
set -euo pipefail
for entry in arm64:arm64-v8a amd64:x86_64; do
  architecture="${entry%%:*}"
  abi="${entry#*:}"
  destination="android/app/src/main/jniLibs/$abi"
  mkdir -p "$destination"
  CGO_ENABLED=0 GOOS=android GOARCH="$architecture" go build -buildmode=pie -ldflags='-s -w -R 16384' -o "$destination/libcapimux.so" ./cmd/9router-go/
done
