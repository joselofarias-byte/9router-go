#!/usr/bin/env bash
set -euo pipefail
for entry in ${CAPIMUX_BUILD_ABIS:-arm64:arm64-v8a amd64:x86_64}; do
  architecture="${entry%%:*}"
  abi="${entry#*:}"
  destination="android/app/src/main/jniLibs/$abi"
  mkdir -p "$destination"
  if [[ "$architecture" == amd64 ]]; then
    compiler="${ANDROID_HOME:?}/ndk/28.0.13004108/toolchains/llvm/prebuilt/linux-x86_64/bin/x86_64-linux-android26-clang"
    CGO_ENABLED=1 CC="$compiler" GOOS=android GOARCH="$architecture" go build -buildmode=pie -ldflags='-s -w -extldflags=-Wl,-z,max-page-size=16384' -o "$destination/libcapimux.so" ./cmd/9router-go/
  else
    CGO_ENABLED=0 GOOS=android GOARCH="$architecture" go build -buildmode=pie -ldflags='-s -w -R 16384' -o "$destination/libcapimux.so" ./cmd/9router-go/
  fi
done
