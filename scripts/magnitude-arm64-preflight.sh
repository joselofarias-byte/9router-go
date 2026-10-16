#!/usr/bin/env bash
set -euo pipefail
# Read-only ARM64 preflight for Magnitude. No installs, downloads or services.
BASE="${MAGNITUDE_BASE_URL:-http://127.0.0.1:10100}"
BASE="${BASE%/}"
echo "=== Magnitude / Capimux ARM64 preflight ==="
echo "Kernel: $(uname -s)"
echo "Architecture: $(uname -m)"
if [[ "$(uname -s)" != "Linux" ]]; then
  echo "UNSUPPORTED_FOR_THIS_CHECK: expected Linux"; exit 2
fi
case "$(uname -m)" in
  aarch64|arm64) echo "ARM64_HOST=YES" ;;
  *) echo "ARM64_HOST=NO (this is a diagnostic, not an ARM64 validation)" ;;
esac
if command -v magnitude >/dev/null 2>&1; then
  echo "MAGNITUDE_CLI=FOUND: $(command -v magnitude)"
else
  echo "MAGNITUDE_CLI=NOT_FOUND"
fi
if command -v free >/dev/null 2>&1; then free -h; fi
if [[ -r /proc/meminfo ]]; then
  grep -E '^(MemTotal|MemAvailable|SwapTotal):' /proc/meminfo || true
fi
if command -v nvidia-smi >/dev/null 2>&1; then
  echo "NVIDIA=DETECTED"
else
  echo "NVIDIA=NOT_DETECTED"
fi
if [[ -e /dev/dri/renderD128 ]]; then
  echo "DRM_RENDER_NODE=PRESENT"
else
  echo "DRM_RENDER_NODE=ABSENT"
fi
echo "BASE_URL=$BASE"
# Never contact an arbitrary remote address without the operator opting in.
if [[ "$BASE" != "http://127.0.0.1:10100" && "$BASE" != "http://localhost:10100" && "${MAGNITUDE_ALLOW_CUSTOM_URL:-0}" != "1" ]]; then
  echo "CUSTOM_URL_REQUIRES_MAGNITUDE_ALLOW_CUSTOM_URL=1"; exit 2
fi
if ! command -v curl >/dev/null 2>&1; then
  echo "CURL=NOT_FOUND"; exit 2
fi
tmp="$(mktemp)"
trap 'rm -f "$tmp"' EXIT
if curl --noproxy '*' --silent --show-error --max-time 5 --output "$tmp" --write-out 'HTTP_STATUS=%{http_code}\n' "$BASE/v1/models"; then
  echo "HTTP_CONNECT=OK"
  if command -v python3 >/dev/null 2>&1; then
    python3 -m json.tool < "$tmp" || { echo "MODELS_JSON=INVALID"; exit 1; }
  else
    echo "PYTHON3=NOT_FOUND (JSON validation skipped)"
  fi
else
  echo "HTTP_CONNECT=FAILED_OR_SERVER_NOT_RUNNING"
  echo "No installation or startup attempted."
  exit 3
fi
