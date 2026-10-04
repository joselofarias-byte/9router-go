#!/data/data/com.termux/files/usr/bin/bash
set -u

REPO="joselofarias-byte/9router-go"
RUN_ID="37220390469"
ARTIFACT="9router-go-termux-arm64-run37220390469"
EXPECTED_HEAD="c6dc0c0692401b9be1e39c2d6ee5a63338fcde85"
EXPECTED_SHA256="0ba3357c1ad91ab91d9557064b575aa2a7b4101aa079f8b1e596d53abc2df237"
PORT="20139"

DL="$HOME/storage/downloads"
TS="$(date +%Y%m%d-%H%M%S)"
REPORT="$DL/9ROUTER-PHYSICAL-MAIN-C6DC0C0-$TS.txt"
WORK="$HOME/.9router-physical-c6dc0c0"
ARTDIR="$WORK/artifact"
DATA="$WORK/data"
BIN="$ARTDIR/9router-go-termux-arm64"
SERVER_LOG="$WORK/server.log"

mkdir -p "$DL" "$WORK" "$ARTDIR" "$DATA"
exec > >(tee "$REPORT") 2>&1

echo "===== 9ROUTER-GO PHYSICAL SMOKE TEST ====="
echo "DATE=$(date)"
echo "REPO=$REPO"
echo "RUN_ID=$RUN_ID"
echo "EXPECTED_HEAD=$EXPECTED_HEAD"
echo "ARTIFACT=$ARTIFACT"
echo "DEVICE=$(getprop ro.product.manufacturer 2>/dev/null) $(getprop ro.product.model 2>/dev/null)"
echo "ANDROID=$(getprop ro.build.version.release 2>/dev/null)"
echo "ABI=$(getprop ro.product.cpu.abi 2>/dev/null)"
echo

if [ ! -d "$DL" ]; then
  echo "ERROR: Termux storage not available. Run termux-setup-storage first."
  echo "RESULTADO=FAIL_STORAGE"
  exit 1
fi

if ! command -v gh >/dev/null 2>&1; then
  echo "ERROR: gh CLI not found."
  echo "RESULTADO=FAIL_NO_GH"
  exit 1
fi

if ! gh auth status >/dev/null 2>&1; then
  echo "ERROR: gh is not authenticated."
  echo "RESULTADO=FAIL_GH_AUTH"
  exit 1
fi

rm -rf "$ARTDIR"
mkdir -p "$ARTDIR"

echo "=== DOWNLOAD FROM GITHUB ACTIONS ==="
if ! gh run download "$RUN_ID" -R "$REPO" -n "$ARTIFACT" -D "$ARTDIR"; then
  echo "ERROR: gh run download failed."
  echo "RESULTADO=FAIL_DOWNLOAD"
  exit 1
fi

if [ ! -f "$BIN" ]; then
  echo "ERROR: artifact downloaded but binary not found: $BIN"
  find "$ARTDIR" -maxdepth 2 -type f -print 2>/dev/null || true
  echo "RESULTADO=FAIL_NO_BINARY"
  exit 1
fi

chmod 700 "$BIN"

echo
echo "=== SHA256 ==="
ACTUAL_SHA256="$(sha256sum "$BIN" | awk '{print $1}')"
echo "EXPECTED=$EXPECTED_SHA256"
echo "ACTUAL=$ACTUAL_SHA256"
if [ "$ACTUAL_SHA256" != "$EXPECTED_SHA256" ]; then
  echo "ERROR: binary SHA256 mismatch."
  echo "RESULTADO=FAIL_SHA256"
  exit 1
fi
echo "SHA256=OK"

echo
echo "=== EXECUTION ==="
if ! "$BIN" --help >/tmp/9router-help.$$ 2>&1; then
  cat /tmp/9router-help.$$ 2>/dev/null || true
  rm -f /tmp/9router-help.$$
  echo "RESULTADO=FAIL_EXEC"
  exit 1
fi
head -n 18 /tmp/9router-help.$$ 2>/dev/null || true
rm -f /tmp/9router-help.$$

echo
echo "=== START ISOLATED INSTANCE ==="
rm -rf "$DATA"
mkdir -p "$DATA"
: > "$SERVER_LOG"

AUTO_UPDATE=false \
HOST=127.0.0.1 \
PORT="$PORT" \
DATA_DIR="$DATA" \
"$BIN" >"$SERVER_LOG" 2>&1 &
PID=$!

cleanup() {
  kill "$PID" 2>/dev/null || true
  wait "$PID" 2>/dev/null || true
}
trap cleanup EXIT INT TERM

echo "PID=$PID"
echo "PORT=$PORT"
echo "DATA_DIR=$DATA"

HEALTH_OK=0
for i in $(seq 1 30); do
  if ! kill -0 "$PID" 2>/dev/null; then
    echo "ERROR: process exited during startup."
    break
  fi
  if curl -fsS "http://127.0.0.1:$PORT/health" >"$WORK/health.txt" 2>/dev/null; then
    HEALTH_OK=1
    break
  fi
  sleep 1
done

if [ "$HEALTH_OK" -ne 1 ]; then
  echo
  echo "=== SERVER LOG ==="
  tail -n 160 "$SERVER_LOG" 2>/dev/null || true
  echo "RESULTADO=FAIL_HEALTH"
  exit 1
fi

echo
echo "=== HEALTH ==="
cat "$WORK/health.txt" 2>/dev/null || true

echo
echo "=== DASHBOARD ==="
HTTP_CODE="$(curl -sS -o "$WORK/dashboard.html" -w '%{http_code}' "http://127.0.0.1:$PORT/" 2>/dev/null || true)"
echo "HTTP=$HTTP_CODE"

echo
echo "=== API VERSION ==="
curl -sS "http://127.0.0.1:$PORT/api/version" 2>/dev/null || true
echo

echo
echo "=== SERVER LOG TAIL ==="
tail -n 100 "$SERVER_LOG" 2>/dev/null || true

if [ "$HTTP_CODE" = "200" ]; then
  echo
  echo "RESULTADO=PASS_SMOKE_ANDROID_TERMUX"
else
  echo
  echo "RESULTADO=PASS_HEALTH_DASHBOARD_HTTP_$HTTP_CODE"
fi

echo "REPORT=$REPORT"
