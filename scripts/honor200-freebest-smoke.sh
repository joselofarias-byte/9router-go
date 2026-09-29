#!/data/data/com.termux/files/usr/bin/bash
set -Eeuo pipefail
IFS=$'\n\t'

REPO="joselofarias-byte/9router-go"
PR23_RUN_ID="36482861141"
PR23_ARTIFACT="9router-go-linux-arm64-pr23"
ROUTER_PORT="${ROUTER_PORT:-20130}"
LLAMA_PORT="${LLAMA_PORT:-8080}"
BASE="$HOME/.9router-honor200"
BIN_DIR="$BASE/bin"
DATA_DIR="$BASE/data"
RUN_DIR="$BASE/run"
LOG_DIR="$BASE/log"
SECRETS="$BASE/secrets.env"
ROUTER_BIN="$BIN_DIR/9router-go-pr23"
ROUTER_LOG="$LOG_DIR/9router.log"
LLAMA_LOG="$LOG_DIR/llama-server.log"
ROUTER_PID="$RUN_DIR/9router.pid"
LLAMA_PID="$RUN_DIR/llama.pid"
REPORT_DIR="$HOME/storage/downloads"
STAMP="$(date +%Y%m%d-%H%M%S)"
REPORT="$REPORT_DIR/9ROUTER-HONOR200-SMOKE-$STAMP.txt"

mkdir -p "$BIN_DIR" "$DATA_DIR" "$RUN_DIR" "$LOG_DIR" "$REPORT_DIR"
chmod 700 "$BASE"
: > "$REPORT"
exec > >(tee -a "$REPORT") 2>&1

log() { printf '[%s] %s\n' "$(date '+%Y-%m-%d %H:%M:%S')" "$*"; }
section() { printf '\n===== %s =====\n' "$*"; }
have() { command -v "$1" >/dev/null 2>&1; }

pid_alive() {
  local f="$1" p
  [ -s "$f" ] || return 1
  p="$(cat "$f" 2>/dev/null || true)"
  [ -n "$p" ] && kill -0 "$p" 2>/dev/null
}

wait_http() {
  local url="$1" tries="${2:-40}" delay="${3:-1}" i
  for ((i=1; i<=tries; i++)); do
    if curl -fsS --max-time 3 "$url" >/dev/null 2>&1; then
      return 0
    fi
    sleep "$delay"
  done
  return 1
}

ensure_tools() {
  section "PRECONDICIONES"
  local c missing=0
  for c in gh curl jq sha256sum llama-server; do
    if have "$c"; then
      echo "$c=OK"
    else
      echo "$c=MISSING"
      missing=1
    fi
  done
  [ "$missing" -eq 0 ] || {
    echo "FATAL: faltan herramientas; no se modifica nada."
    exit 4
  }
}

find_model() {
  if [ -n "${MODEL_GGUF:-}" ] && [ -f "$MODEL_GGUF" ]; then
    printf '%s\n' "$MODEL_GGUF"
    return 0
  fi

  local list="$RUN_DIR/gguf-candidates.txt"
  : > "$list"
  local d
  for d in     "$HOME/.llm"     "$HOME/.qwen"     "$HOME/models"     "$HOME/llama.cpp"     "$HOME/.cache"     "$HOME/storage/downloads"
  do
    [ -d "$d" ] || continue
    find "$d" -maxdepth 6 -type f -iname '*.gguf' -print 2>/dev/null >> "$list" || true
  done
  sort -u -o "$list" "$list"

  local chosen
  chosen="$(grep -Ei '/[^/]*qwen[^/]*(0[._-]?6|0[._-]?5|0[._-]?8|0\.6b|0\.5b|0\.8b)[^/]*q4[^/]*\.gguf$' "$list" | head -n1 || true)"
  [ -n "$chosen" ] || chosen="$(grep -Ei '/[^/]*qwen[^/]*q4[^/]*\.gguf$' "$list" | head -n1 || true)"
  [ -n "$chosen" ] || chosen="$(head -n1 "$list" || true)"

  [ -n "$chosen" ] && [ -f "$chosen" ] || return 1
  printf '%s\n' "$chosen"
}

start_llama() {
  section "LLAMA.CPP LOCAL"

  if curl -fsS --max-time 3 "http://127.0.0.1:$LLAMA_PORT/v1/models" >/dev/null 2>&1; then
    echo "LLAMA_SERVER=YA_ACTIVO"
    return 0
  fi

  local model
  model="$(find_model || true)"
  if [ -z "$model" ]; then
    echo "FATAL: no encontré un .gguf en las ubicaciones conocidas."
    echo "Ejecutá luego con MODEL_GGUF=/ruta/modelo.gguf"
    exit 4
  fi
  echo "MODEL_GGUF=$model"

  rm -f "$LLAMA_PID"
  : > "$LLAMA_LOG"
  log "Iniciando llama-server en 127.0.0.1:$LLAMA_PORT, CPU (-ngl 0)."
  nohup llama-server     --host 127.0.0.1     --port "$LLAMA_PORT"     -m "$model"     -c 2048     -ngl 0     >"$LLAMA_LOG" 2>&1 &
  echo $! > "$LLAMA_PID"

  if ! wait_http "http://127.0.0.1:$LLAMA_PORT/v1/models" 90 1; then
    echo "LLAMA_SERVER=FALLO"
    tail -n 80 "$LLAMA_LOG" || true
    exit 4
  fi
  echo "LLAMA_SERVER=OK"
  curl -fsS "http://127.0.0.1:$LLAMA_PORT/v1/models" | jq -c . || true
}

install_router_artifact() {
  section "9ROUTER PR23"

  if [ -x "$ROUTER_BIN" ]; then
    echo "9ROUTER_BIN=YA_INSTALADO"
    return 0
  fi

  local dl="$RUN_DIR/artifact-$STAMP"
  mkdir -p "$dl"
  log "Descargando artefacto CI validado del PR23 (run $PR23_RUN_ID)."
  gh run download "$PR23_RUN_ID"     -R "$REPO"     -n "$PR23_ARTIFACT"     -D "$dl"

  [ -f "$dl/9router-go-linux-arm64" ] || {
    echo "FATAL: el artefacto no contiene 9router-go-linux-arm64"
    exit 4
  }
  [ -f "$dl/9router-go-linux-arm64.sha256" ] || {
    echo "FATAL: falta checksum del artefacto"
    exit 4
  }

  (
    cd "$dl"
    sha256sum -c 9router-go-linux-arm64.sha256
  )
  install -m 700 "$dl/9router-go-linux-arm64" "$ROUTER_BIN"
  echo "9ROUTER_BIN=OK"
  "$ROUTER_BIN" version 2>&1 | sed -n '1,5p' || true
}

ensure_secrets() {
  if [ ! -f "$SECRETS" ]; then
    local pass
    pass="honor200-local-$STAMP-$$"
    umask 077
    {
      printf 'DASHBOARD_PASSWORD=%q\n' "$pass"
      printf 'CLIENT_API_KEY=%q\n' ""
    } > "$SECRETS"
  fi
  # shellcheck disable=SC1090
  source "$SECRETS"
}

save_client_key() {
  local key="$1"
  # shellcheck disable=SC1090
  source "$SECRETS"
  umask 077
  {
    printf 'DASHBOARD_PASSWORD=%q\n' "$DASHBOARD_PASSWORD"
    printf 'CLIENT_API_KEY=%q\n' "$key"
  } > "$SECRETS"
}

start_router() {
  section "ARRANQUE 9ROUTER"

  ensure_secrets

  if curl -fsS --max-time 3 "http://127.0.0.1:$ROUTER_PORT/health" >/dev/null 2>&1; then
    echo "9ROUTER=YA_ACTIVO"
    return 0
  fi

  rm -f "$ROUTER_PID"
  : > "$ROUTER_LOG"
  log "Iniciando 9router PR23 en localhost:$ROUTER_PORT."
  nohup env     HOST=127.0.0.1     PORT="$ROUTER_PORT"     DATA_DIR="$DATA_DIR"     INITIAL_PASSWORD="$DASHBOARD_PASSWORD"     LLAMACPP_BASE_URL="http://127.0.0.1:$LLAMA_PORT"     AUTO_UPDATE=false     "$ROUTER_BIN"     >"$ROUTER_LOG" 2>&1 &
  echo $! > "$ROUTER_PID"

  if ! wait_http "http://127.0.0.1:$ROUTER_PORT/health" 60 1; then
    echo "9ROUTER=FALLO"
    tail -n 100 "$ROUTER_LOG" || true
    exit 4
  fi
  echo "9ROUTER=OK"
}

ensure_client_key() {
  section "API KEY LOCAL"
  ensure_secrets

  if [ -n "${CLIENT_API_KEY:-}" ]; then
    local code
    code="$(curl -sS -o "$RUN_DIR/models-check.json" -w '%{http_code}'       -H "Authorization: Bearer $CLIENT_API_KEY"       "http://127.0.0.1:$ROUTER_PORT/v1/models" || true)"
    if [ "$code" = "200" ]; then
      echo "CLIENT_API_KEY=YA_FUNCIONAL"
      return 0
    fi
  fi

  local cookie="$RUN_DIR/cookies.txt"
  local login_code key_code body key
  rm -f "$cookie"

  login_code="$(curl -sS -o "$RUN_DIR/login.json" -w '%{http_code}'     -c "$cookie"     -H 'Content-Type: application/json'     -d "$(jq -cn --arg p "$DASHBOARD_PASSWORD" '{password:$p}')"     "http://127.0.0.1:$ROUTER_PORT/api/auth/login" || true)"
  if [ "$login_code" != "200" ]; then
    echo "LOGIN_LOCAL=FALLO_HTTP_$login_code"
    cat "$RUN_DIR/login.json" || true
    exit 4
  fi
  echo "LOGIN_LOCAL=OK"

  key_code="$(curl -sS -o "$RUN_DIR/key-create.json" -w '%{http_code}'     -b "$cookie"     -H 'Content-Type: application/json'     -d '{"name":"HONOR 200 local Fabric smoke"}'     "http://127.0.0.1:$ROUTER_PORT/api/keys" || true)"
  if [ "$key_code" != "200" ]; then
    echo "CREAR_API_KEY=FALLO_HTTP_$key_code"
    cat "$RUN_DIR/key-create.json" || true
    exit 4
  fi

  body="$(cat "$RUN_DIR/key-create.json")"
  key="$(printf '%s' "$body" | jq -r '.key // empty')"
  [ -n "$key" ] || {
    echo "CREAR_API_KEY=RESPUESTA_SIN_KEY"
    printf '%s\n' "$body"
    exit 4
  }
  save_client_key "$key"
  CLIENT_API_KEY="$key"
  echo "CLIENT_API_KEY=OK_GUARDADA_EN_$SECRETS"
}

test_free_best() {
  section "PRUEBA FREE-BEST"

  ensure_secrets
  [ -n "${CLIENT_API_KEY:-}" ] || {
    echo "FATAL: no hay CLIENT_API_KEY guardada"
    exit 4
  }

  echo "Esperando el primer ciclo de discovery de Fabric (arranca a los ~30 s)."
  local i code body="$RUN_DIR/free-best.json"
  for ((i=1; i<=8; i++)); do
    code="$(curl -sS --max-time 120 -o "$body" -w '%{http_code}'       -H "Authorization: Bearer $CLIENT_API_KEY"       -H 'Content-Type: application/json'       -d '{"model":"free-best","stream":false,"messages":[{"role":"user","content":"Respondé brevemente: 9router local operativo."}]}'       "http://127.0.0.1:$ROUTER_PORT/v1/chat/completions" || true)"

    if [ "$code" = "200" ]; then
      local answer
      answer="$(jq -r '.choices[0].message.content // .choices[0].text // empty' "$body" 2>/dev/null || true)"
      if [ -n "$answer" ]; then
        echo "FREE_BEST=OK"
        echo "RESPUESTA:"
        printf '%s\n' "$answer" | head -c 1200
        echo
        return 0
      fi
    fi

    if jq -e '.error.code=="free_route_unavailable"' "$body" >/dev/null 2>&1; then
      echo "Intento $i: Fabric todavía no publicó la ruta; reintento."
      sleep 8
      continue
    fi

    echo "FREE_BEST=FALLO_HTTP_$code"
    cat "$body" || true
    echo
    tail -n 80 "$ROUTER_LOG" || true
    return 4
  done

  echo "FREE_BEST=PENDIENTE_DISCOVERY"
  cat "$body" || true
  tail -n 100 "$ROUTER_LOG" || true
  return 4
}

status() {
  section "ESTADO"
  if curl -fsS --max-time 3 "http://127.0.0.1:$LLAMA_PORT/v1/models" >/dev/null 2>&1; then
    echo "llama-server=UP"
  else
    echo "llama-server=DOWN"
  fi
  if curl -fsS --max-time 3 "http://127.0.0.1:$ROUTER_PORT/health" >/dev/null 2>&1; then
    echo "9router=UP"
  else
    echo "9router=DOWN"
  fi
  echo "endpoint=http://127.0.0.1:$ROUTER_PORT/v1"
  echo "secrets=$SECRETS"
  echo "router_log=$ROUTER_LOG"
  echo "llama_log=$LLAMA_LOG"
}

stop_all() {
  section "DETENCION"
  local f p
  for f in "$ROUTER_PID" "$LLAMA_PID"; do
    if pid_alive "$f"; then
      p="$(cat "$f")"
      kill "$p" 2>/dev/null || true
      echo "STOPPED_PID=$p"
    fi
    rm -f "$f"
  done
}

main() {
  local action="${1:-start-test}"
  case "$action" in
    start-test)
      ensure_tools
      start_llama
      install_router_artifact
      start_router
      ensure_client_key
      test_free_best
      status
      echo
      echo "9ROUTER_HONOR200_SMOKE=OK"
      echo "INFORME=$REPORT"
      ;;
    test)
      ensure_client_key
      test_free_best
      status
      ;;
    status)
      status
      ;;
    stop)
      stop_all
      ;;
    *)
      echo "Uso: $0 [start-test|test|status|stop]"
      exit 2
      ;;
  esac
}

main "$@"
