#!/data/data/com.termux/files/usr/bin/bash
set -euo pipefail

ROUTER_BASE_URL="${ROUTER_BASE_URL:-http://127.0.0.1:20128}"
WORKBUDDY_KEYS_URL="https://www.codebuddy.ai/profile/keys"
WORKBUDDY_PROFILE_URL="https://www.codebuddy.ai/profile"
WORKBUDDY_PROBE_MODEL="${WORKBUDDY_PROBE_MODEL:-gpt-5.6-luna}"
ROUTER_TOKEN="${NINEROUTER_API_KEY:-}"
CODEBUDDY_KEY=""
TMP_RESPONSE=""
AUDIT_FILE="$HOME/.config/9router-go/workbuddy-last-probe.json"

cleanup() {
  CODEBUDDY_KEY=""
  unset CODEBUDDY_API_KEY 2>/dev/null || true
  if [ -n "${TMP_RESPONSE:-}" ] && [ -f "$TMP_RESPONSE" ]; then
    rm -f "$TMP_RESPONSE"
  fi
}
trap cleanup EXIT INT TERM

say() {
  printf '%s\n' "$*"
}

need_cmd() {
  command -v "$1" >/dev/null 2>&1
}

new_tmp_response() {
  if [ -n "${TMP_RESPONSE:-}" ] && [ -f "$TMP_RESPONSE" ]; then
    rm -f "$TMP_RESPONSE"
  fi
  TMP_RESPONSE="$(mktemp)"
  chmod 600 "$TMP_RESPONSE"
}

install_termux_dependencies() {
  if ! need_cmd pkg; then
    return 0
  fi

  local packages=()
  need_cmd curl || packages+=(curl)
  need_cmd jq || packages+=(jq)
  need_cmd node || packages+=(nodejs)
  need_cmd npm || packages+=(nodejs)

  if [ "${#packages[@]}" -gt 0 ]; then
    say "==> Instalando dependencias Termux: ${packages[*]}"
    pkg install -y "${packages[@]}"
  fi
}

install_codebuddy_cli() {
  if need_cmd codebuddy; then
    say "==> CodeBuddy CLI ya está instalado: $(codebuddy --version 2>/dev/null | head -n 1 || true)"
    return 0
  fi

  if ! need_cmd npm; then
    say "ERROR: npm no está disponible y no se pudo instalar automáticamente." >&2
    exit 1
  fi

  say "==> Instalando CodeBuddy CLI oficial..."
  npm install -g @tencent-ai/codebuddy-code

  if ! need_cmd codebuddy; then
    say "ERROR: npm terminó, pero 'codebuddy' no quedó disponible en PATH." >&2
    exit 1
  fi

  say "==> CodeBuddy CLI instalado: $(codebuddy --version 2>/dev/null | head -n 1 || true)"
}

open_key_page() {
  say
  say "==> Se necesita una API key personal de WorkBuddy/CodeBuddy International."
  say "    Página oficial: $WORKBUDDY_KEYS_URL"
  if need_cmd termux-open-url; then
    termux-open-url "$WORKBUDDY_KEYS_URL" >/dev/null 2>&1 || true
    say "==> Abrí la página de claves en el navegador."
  fi
  say "    Creá/copiala y volvé a Termux. La clave se leerá sin mostrarla en pantalla."
}

read_codebuddy_key() {
  printf 'Pegá la API key de WorkBuddy/CodeBuddy y presioná Enter: '
  IFS= read -r -s CODEBUDDY_KEY
  printf '\n'

  CODEBUDDY_KEY="${CODEBUDDY_KEY//$'\r'/}"
  if [ -z "$CODEBUDDY_KEY" ]; then
    say "ERROR: no se ingresó ninguna API key." >&2
    exit 1
  fi

  export CODEBUDDY_API_KEY="$CODEBUDDY_KEY"
}

load_router_token() {
  if [ -n "$ROUTER_TOKEN" ]; then
    return 0
  fi

  if [ -r "$HOME/.config/9router-go/admin-token" ]; then
    ROUTER_TOKEN="$(tr -d '\r\n' < "$HOME/.config/9router-go/admin-token")"
  fi
}

ensure_router_running() {
  if curl -fsS --max-time 3 "$ROUTER_BASE_URL/health" >/dev/null 2>&1; then
    say "==> 9router-go responde en $ROUTER_BASE_URL"
    return 0
  fi

  if need_cmd 9router-go; then
    mkdir -p "$HOME/.config/9router-go"
    say "==> 9router-go no estaba activo; iniciándolo bajo demanda..."
    nohup 9router-go >"$HOME/.config/9router-go/workbuddy-bootstrap-router.log" 2>&1 &
    local i
    for i in 1 2 3 4 5 6 7 8 9 10; do
      sleep 1
      if curl -fsS --max-time 3 "$ROUTER_BASE_URL/health" >/dev/null 2>&1; then
        say "==> 9router-go iniciado correctamente."
        return 0
      fi
    done
  fi

  say "ERROR: no pude contactar 9router-go en $ROUTER_BASE_URL." >&2
  say "       Iniciá 9router-go y volvé a ejecutar este script." >&2
  exit 1
}

import_connection() {
  load_router_token
  if [ -z "$ROUTER_TOKEN" ]; then
    say "ERROR: falta la credencial de 9router-go." >&2
    say "       No existe ~/.config/9router-go/admin-token y NINEROUTER_API_KEY no está definido." >&2
    exit 1
  fi

  new_tmp_response

  say "==> Importando la clave como proveedor codebuddy-intl..."
  local status
  status="$({
    jq -n --arg key "$CODEBUDDY_KEY" \
      '{apiKey:$key,name:"WorkBuddy International"}' |
      curl -sS --max-time 30 \
        -o "$TMP_RESPONSE" \
        -w '%{http_code}' \
        -H "Authorization: Bearer $ROUTER_TOKEN" \
        -H 'Content-Type: application/json' \
        --data-binary @- \
        "$ROUTER_BASE_URL/api/oauth/codebuddy-intl/import"
  } || true)"

  if [ "$status" != "200" ]; then
    say "ERROR: 9router-go rechazó la importación (HTTP ${status:-sin-respuesta})." >&2
    if [ -s "$TMP_RESPONSE" ]; then
      jq -c 'del(.apiKey,.accessToken,.refreshToken)' "$TMP_RESPONSE" 2>/dev/null || cat "$TMP_RESPONSE" >&2
    fi
    if [ "$status" = "401" ]; then
      say "       El token local no autoriza esta ruta. Definí NINEROUTER_API_KEY con una API key activa de 9router-go." >&2
    fi
    exit 1
  fi

  local connection_id
  connection_id="$(jq -r '.id // .connection // empty' "$TMP_RESPONSE" 2>/dev/null || true)"
  if [ -z "$connection_id" ]; then
    say "ERROR: la respuesta de importación no incluyó un connection id." >&2
    exit 1
  fi

  say "==> Conexión creada: $connection_id"
}

save_probe_audit() {
  local status="$1"
  mkdir -p "$(dirname "$AUDIT_FILE")"
  local audit_tmp
  audit_tmp="${AUDIT_FILE}.tmp.$$"

  if jq -e . "$TMP_RESPONSE" >/dev/null 2>&1; then
    jq \
      --arg timestamp "$(date -u +'%Y-%m-%dT%H:%M:%SZ')" \
      --arg provider "codebuddy-intl" \
      --arg requestedModel "$WORKBUDDY_PROBE_MODEL" \
      --arg httpStatus "$status" \
      '{timestamp:$timestamp,provider:$provider,requestedModel:$requestedModel,httpStatus:($httpStatus|tonumber? // $httpStatus),responseModel:(.model // null),usage:(.usage // null),error:(.error // null)}' \
      "$TMP_RESPONSE" > "$audit_tmp"
  else
    jq -n \
      --arg timestamp "$(date -u +'%Y-%m-%dT%H:%M:%SZ')" \
      --arg provider "codebuddy-intl" \
      --arg requestedModel "$WORKBUDDY_PROBE_MODEL" \
      --arg httpStatus "$status" \
      '{timestamp:$timestamp,provider:$provider,requestedModel:$requestedModel,httpStatus:($httpStatus|tonumber? // $httpStatus),responseModel:null,usage:null,error:"non-json response"}' \
      > "$audit_tmp"
  fi

  chmod 600 "$audit_tmp"
  mv -f "$audit_tmp" "$AUDIT_FILE"
}

report_probe_usage() {
  local prompt_tokens completion_tokens total_tokens credit response_model
  prompt_tokens="$(jq -r '.usage.prompt_tokens // .usage.input_tokens // empty' "$TMP_RESPONSE" 2>/dev/null || true)"
  completion_tokens="$(jq -r '.usage.completion_tokens // .usage.output_tokens // empty' "$TMP_RESPONSE" 2>/dev/null || true)"
  total_tokens="$(jq -r '.usage.total_tokens // empty' "$TMP_RESPONSE" 2>/dev/null || true)"
  credit="$(jq -r '.usage.credit // .usage.credits // empty' "$TMP_RESPONSE" 2>/dev/null || true)"
  response_model="$(jq -r '.model // empty' "$TMP_RESPONSE" 2>/dev/null || true)"

  say
  say "=== TELEMETRÍA WORKBUDDY ==="
  if [ -n "$response_model" ]; then
    say "Modelo devuelto: $response_model"
  fi
  if [ -n "$prompt_tokens" ]; then
    say "Tokens entrada: $prompt_tokens"
  fi
  if [ -n "$completion_tokens" ]; then
    say "Tokens salida: $completion_tokens"
  fi
  if [ -n "$total_tokens" ]; then
    say "Tokens totales: $total_tokens"
  fi
  if [ -n "$credit" ]; then
    say "Créditos consumidos en el probe: $credit"
  else
    say "Crédito exacto no vino en la respuesta OpenAI de este probe."
    say "Saldo y detalle oficial: $WORKBUDDY_PROFILE_URL -> Usage"
  fi
  say "Auditoría local sin credenciales: $AUDIT_FILE"
}

probe_router() {
  new_tmp_response

  say "==> Probando $WORKBUDDY_PROBE_MODEL a través de 9router-go..."
  local status
  status="$({
    jq -n --arg model "codebuddy-intl/$WORKBUDDY_PROBE_MODEL" \
      '{model:$model,messages:[{role:"user",content:"Reply with exactly: WB_OK"}],stream:false,max_tokens:16}' |
      curl -sS --max-time 120 \
        -o "$TMP_RESPONSE" \
        -w '%{http_code}' \
        -H "Authorization: Bearer $ROUTER_TOKEN" \
        -H 'Content-Type: application/json' \
        --data-binary @- \
        "$ROUTER_BASE_URL/chat/completions"
  } || true)"

  save_probe_audit "${status:-0}"

  if [ "$status" != "200" ]; then
    say "ADVERTENCIA: la conexión fue importada, pero el probe devolvió HTTP ${status:-sin-respuesta}." >&2
    if [ -s "$TMP_RESPONSE" ]; then
      jq -c . "$TMP_RESPONSE" 2>/dev/null || head -c 1200 "$TMP_RESPONSE" >&2
      printf '\n' >&2
    fi
    say "Auditoría: $AUDIT_FILE" >&2
    return 0
  fi

  local reply
  reply="$(jq -r '.choices[0].message.content // .choices[0].text // empty' "$TMP_RESPONSE" 2>/dev/null | tr -d '\r' | head -n 1 || true)"
  if [ "$reply" = "WB_OK" ]; then
    say "==> Probe OK: WorkBuddy está respondiendo a través de 9router-go."
  else
    say "==> HTTP 200 recibido. Respuesta del probe: ${reply:-<sin texto>}"
  fi

  report_probe_usage
}

say "=== WorkBuddy International -> 9router-go / Termux ==="
install_termux_dependencies
install_codebuddy_cli
open_key_page
read_codebuddy_key
ensure_router_running
import_connection
probe_router

say
say "=== LISTO ==="
say "Proveedor canónico: codebuddy-intl"
say "Aliases del router: workbuddy, wb, cbai"
say "Modelo de probe: $WORKBUDDY_PROBE_MODEL"
say "La API key no se guardó en este script ni se imprimió en pantalla."
