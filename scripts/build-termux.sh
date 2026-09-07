#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
out="${1:-dist/termux}"
mkdir -p "$out"
version="${VERSION:-v1.8.6-fabric.4}"
# Android needs Bionic getaddrinfo for system DNS (including private DNS/VPN).
# A CGO-free cross-build can start successfully but fail every hostname lookup.
if [ "$(go env GOHOSTOS)/$(go env GOHOSTARCH)" = "android/arm64" ]; then
  export CC="${CC:-clang}"
else
  ndk="${ANDROID_NDK_HOME:-${ANDROID_NDK_ROOT:-}}"
  : "${ndk:?Set ANDROID_NDK_HOME to the Android NDK directory}"
  export CC="$ndk/toolchains/llvm/prebuilt/linux-x86_64/bin/aarch64-linux-android24-clang"
fi
command -v "$CC" >/dev/null || { echo "Android C compiler not found: $CC" >&2; exit 1; }
export CGO_ENABLED=1 GOOS=android GOARCH=arm64
go build -tags netcgo -trimpath \
  -ldflags="-s -w -X 9router/proxy/internal/updater.CurrentVersion=$version" \
  -o "$out/9router-go-android-arm64" ./cmd/9router-go
go build -tags netcgo -trimpath -ldflags='-s -w' \
  -o "$out/9router-netcheck-android-arm64" ./cmd/9router-netcheck
go version -m "$out/9router-go-android-arm64" > "$out/BUILD-INFO.txt"
grep -q 'CGO_ENABLED=1' "$out/BUILD-INFO.txt"
cp docs/TERMUX.md "$out/README.md"
(cd "$out" && sha256sum 9router-go-android-arm64 9router-netcheck-android-arm64 > SHA256SUMS)
