#!/usr/bin/env bash
# Build a Termux-native Android ARM64 binary that uses Bionic getaddrinfo.
# CGO-free android/arm64 binaries can listen on localhost and still fail DNS.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

VERSION="$(cat VERSION 2>/dev/null || echo 0.0.0)"
LDFLAGS="-s -w -X '9router/proxy/internal/updater.CurrentVersion=${VERSION}'"

if [[ -z "${ANDROID_NDK_HOME:-}" && -z "${ANDROID_NDK_ROOT:-}" ]]; then
  echo "ANDROID_NDK_HOME or ANDROID_NDK_ROOT is required for CGO Termux builds." >&2
  echo "Falling back to CGO-free android/arm64 (may fail DNS on device)." >&2
  CGO_ENABLED=0 GOOS=android GOARCH=arm64 go build -ldflags="${LDFLAGS}" -o 9router-go-termux-arm64 ./cmd/9router-go/
  CGO_ENABLED=0 GOOS=android GOARCH=arm64 go build -ldflags="${LDFLAGS}" -o 9router-netcheck-android-arm64 ./cmd/9router-netcheck/
  ls -lh 9router-go-termux-arm64 9router-netcheck-android-arm64
  exit 0
fi

NDK="${ANDROID_NDK_HOME:-$ANDROID_NDK_ROOT}"
API="${ANDROID_API:-24}"
PREBUILT="$(ls -d "${NDK}"/toolchains/llvm/prebuilt/* 2>/dev/null | head -n1)"
if [[ -z "${PREBUILT}" ]]; then
  echo "Could not find NDK llvm prebuilt toolchain under ${NDK}" >&2
  exit 1
fi

export CC="${PREBUILT}/bin/aarch64-linux-android${API}-clang"
export CGO_ENABLED=1
export GOOS=android
export GOARCH=arm64
export CGO_CFLAGS="-O2"
export GOFLAGS="-tags=netcgo"

go build -ldflags="${LDFLAGS}" -o 9router-go-android-arm64 ./cmd/9router-go/
go build -ldflags="${LDFLAGS}" -o 9router-netcheck-android-arm64 ./cmd/9router-netcheck/
ls -lh 9router-go-android-arm64 9router-netcheck-android-arm64
