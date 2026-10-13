#!/usr/bin/env bash
set -euo pipefail
go mod download -json github.com/mattn/go-sqlite3
for entry in ${CAPIMUX_BUILD_ABIS:-arm64:arm64-v8a amd64:x86_64}; do
  architecture="${entry%%:*}"
  abi="${entry#*:}"
  destination="android/app/src/main/jniLibs/$abi"
  mkdir -p "$destination"
  target=x86_64
  if [[ "$architecture" == arm64 ]]; then target=aarch64; fi
  compiler="${ANDROID_HOME:?}/ndk/28.0.13004108/toolchains/llvm/prebuilt/linux-x86_64/bin/${target}-linux-android26-clang"
  CGO_ENABLED=1 CC="$compiler" GOOS=android GOARCH="$architecture" go build -tags=capimux_apk -buildmode=pie -ldflags='-s -w -extldflags=-Wl,-z,max-page-size=16384' -o "$destination/libcapimux.so" ./cmd/9router-go/
done
