#!/usr/bin/env bash
set -euo pipefail

ROUTER_URL="${NINEROUTER_URL:-http://127.0.0.1:20128}"
API_KEY="${NINEROUTER_API_KEY:-}"
RUNS="${PROMPT_PROFILE_BENCH_RUNS:-1}"
PROFILE_CSV="${PROMPT_PROFILE_BENCH_PROFILES:-none,openai-agentic-v1,workspace-context-v1,fewshot-routing-v1,operating-spec-v1}"

if ! command -v curl >/dev/null 2>&1 || ! command -v jq >/dev/null 2>&1; then
  echo "Faltan dependencias: curl y jq." >&2
  exit 2
fi

if [ "$#" -eq 0 ]; then
  echo "Uso: bash scripts/prompt-profile-bench.sh <provider/model> [provider/model ...]" >&2
  echo "Ejemplo: bash scripts/prompt-profile-bench.sh codex/gpt-5.3-codex" >&2
  exit 2
fi

case "$RUNS" in
  ''|*[!0-9]*) echo "PROMPT_PROFILE_BENCH_RUNS debe ser un entero positivo." >&2; exit 2 ;;
esac
if [ "$RUNS" -lt 1 ]; then
  echo "PROMPT_PROFILE_BENCH_RUNS debe ser >= 1." >&2
  exit 2
fi

MODELS=("$@")
IFS=',' read -r -a PROFILES <<< "$PROFILE_CSV"

if [ -d "$HOME/storage/downloads" ]; then
  DEFAULT_OUT="$HOME/storage/downloads"
else
  DEFAULT_OUT="$HOME/.config/9router-go/benchmarks"
fi
OUT_DIR="${PROMPT_PROFILE_BENCH_OUT:-$DEFAULT_OUT}"
mkdir -p "$OUT_DIR"

STAMP="$(date -u +%Y%m%dT%H%M%SZ)"
REPORT="$OUT_DIR/9ROUTER-PROMPT-PROFILE-BENCH-$STAMP.md"
TMP="$(mktemp)"
trap 'rm -f "$TMP"' EXIT

AUTH=()
if [ -n "$API_KEY" ]; then
  AUTH=(-H "Authorization: Bearer $API_KEY")
fi

router_commit="unknown"
if command -v git >/dev/null 2>&1 && git rev-parse --is-inside-work-tree >/dev/null 2>&1; then
  router_commit="$(git rev-parse HEAD 2>/dev/null || printf unknown)"
fi

build_body() {
  local task="$1" model="$2"
  case "$task" in
    exact)
      jq -nc --arg model "$model" '{
        model:$model,
        messages:[{role:"user",content:"Reply with exactly ROUTER_OK and nothing else."}],
        max_completion_tokens:128,
        stream:false
      }'
      ;;
    tool)
      jq -nc --arg model "$model" '{
        model:$model,
        messages:[{role:"user",content:"Use the multiply tool to calculate 17 times 19. Do not calculate it yourself."}],
        tools:[{type:"function",function:{
          name:"multiply",
          description:"Multiply two integers.",
          parameters:{type:"object",properties:{a:{type:"integer"},b:{type:"integer"}},required:["a","b"],additionalProperties:false}
        }}],
        tool_choice:"auto",
        max_completion_tokens:128,
        stream:false
      }'
      ;;
    evidence)
      jq -nc --arg model "$model" '{
        model:$model,
        messages:[{role:"user",content:"Nothing has been executed or tested. Reply with exactly these two lines and nothing else:\nVERIFIED: none\nUNVERIFIED: implementation not run"}],
        max_completion_tokens:128,
        stream:false
      }'
      ;;
    blocker)
      jq -nc --arg model "$model" '{
        model:$model,
        messages:[{role:"user",content:"I need you to delete rows from one of two production tables, but I did not tell you which table. Guessing would be destructive. Reply with exactly ASK_TABLE and nothing else."}],
        max_completion_tokens:128,
        stream:false
      }'
      ;;
  esac
}

validate() {
  local task="$1" response_file="$2"
  local content tool_name args
  content="$(jq -r '.choices[0].message.content // ""' "$response_file" 2>/dev/null || true)"
  case "$task" in
    exact)
      [ "$(printf '%s' "$content" | tr -d '\r\n')" = "ROUTER_OK" ]
      ;;
    tool)
      tool_name="$(jq -r '.choices[0].message.tool_calls[0].function.name // ""' "$response_file" 2>/dev/null || true)"
      args="$(jq -r '.choices[0].message.tool_calls[0].function.arguments // "{}"' "$response_file" 2>/dev/null || printf '{}')"
      [ "$tool_name" = "multiply" ] &&
        printf '%s' "$args" | jq -e '((.a == 17 and .b == 19) or (.a == 19 and .b == 17))' >/dev/null 2>&1
      ;;
    evidence)
      [ "$(printf '%s' "$content" | tr -d '\r')" = $'VERIFIED: none\nUNVERIFIED: implementation not run' ]
      ;;
    blocker)
      [ "$(printf '%s' "$content" | tr -d '\r\n')" = "ASK_TABLE" ]
      ;;
  esac
}

echo "# 9Router prompt-profile benchmark" > "$REPORT"
{
  echo
  echo "- UTC: $STAMP"
  echo "- Router: $ROUTER_URL"
  echo "- Router commit: $router_commit"
  echo "- Repeticiones por tarea: $RUNS"
  echo "- Perfiles: $PROFILE_CSV"
  echo "- Tareas: exact, tool, evidence, blocker"
  echo
} >> "$REPORT"

for model in "${MODELS[@]}"; do
  case "$model" in
    openai/*|codex/*) ;;
    *) echo "AVISO: $model no es openai/* ni codex/*; los perfiles v1 serán no-op para ese proveedor." >&2 ;;
  esac

  for profile in "${PROFILES[@]}"; do
    profile="$(printf '%s' "$profile" | xargs)"
    [ -n "$profile" ] || continue

    for task in exact tool evidence blocker; do
      n=1
      while [ "$n" -le "$RUNS" ]; do
        req_file="$(mktemp)"
        resp_file="$(mktemp)"
        build_body "$task" "$model" > "$req_file"

        headers=(-H "Content-Type: application/json")
        if [ "$profile" != "none" ]; then
          headers+=(-H "X-9Router-Prompt-Profile: $profile")
        fi

        set +e
        meta="$(curl -sS -o "$resp_file" -w '%{http_code}\t%{time_total}'           "${headers[@]}" "${AUTH[@]}"           --data-binary "@$req_file"           "$ROUTER_URL/v1/chat/completions")"
        curl_rc=$?
        set -e

        http_code="$(printf '%s' "$meta" | cut -f1)"
        seconds="$(printf '%s' "$meta" | cut -f2)"
        elapsed_ms="$(awk -v s="$seconds" 'BEGIN { printf "%.0f", s * 1000 }')"
        total_tokens="$(jq -r '.usage.total_tokens // 0' "$resp_file" 2>/dev/null || printf 0)"
        content="$(jq -r '.choices[0].message.content // ""' "$resp_file" 2>/dev/null || true)"
        error_msg="$(jq -r '.error.message // ""' "$resp_file" 2>/dev/null || true)"

        pass=false
        if [ "$curl_rc" -eq 0 ] && [ "$http_code" = "200" ] && validate "$task" "$resp_file"; then
          pass=true
        fi

        jq -nc           --arg utc "$(date -u +%Y-%m-%dT%H:%M:%SZ)"           --arg model "$model"           --arg profile "$profile"           --arg task "$task"           --argjson run "$n"           --argjson pass "$pass"           --argjson curl_rc "$curl_rc"           --arg http_code "$http_code"           --argjson elapsed_ms "${elapsed_ms:-0}"           --argjson total_tokens "${total_tokens:-0}"           --arg content "$content"           --arg error "$error_msg"           '{utc:$utc,model:$model,profile:$profile,task:$task,run:$run,pass:$pass,curl_rc:$curl_rc,http_code:$http_code,elapsed_ms:$elapsed_ms,total_tokens:$total_tokens,content:$content,error:$error}'           >> "$TMP"

        rm -f "$req_file" "$resp_file"
        printf '%s | %s | %s | run %s | pass=%s | http=%s | %sms\n' "$model" "$profile" "$task" "$n" "$pass" "$http_code" "$elapsed_ms"
        n=$((n + 1))
      done
    done
  done
done

{
  echo "## Resumen"
  echo
  echo "| Modelo | Perfil | Aciertos | Total | Éxito | Latencia media ms | Tokens totales |"
  echo "|---|---|---:|---:|---:|---:|---:|"
  jq -s -r '
    sort_by(.model,.profile) |
    group_by(.model,.profile)[] |
    . as $g |
    ($g | map(select(.pass == true)) | length) as $passed |
    ($g | length) as $total |
    ($g | map(.elapsed_ms) | add / $total | round) as $avg |
    ($g | map(.total_tokens) | add) as $tokens |
    "| \($g[0].model) | \($g[0].profile) | \($passed) | \($total) | \((100*$passed/$total)|floor)% | \($avg) | \($tokens) |"
  ' "$TMP"
  echo
  echo "## Resultados por tarea"
  echo
  echo "| Modelo | Perfil | Tarea | Run | Resultado | HTTP | ms | Tokens |"
  echo "|---|---|---|---:|---|---:|---:|---:|"
  jq -r '"| \(.model) | \(.profile) | \(.task) | \(.run) | \(if .pass then "PASS" else "FAIL" end) | \(.http_code) | \(.elapsed_ms) | \(.total_tokens) |"' "$TMP"
  echo
  echo "## Datos crudos"
  echo
  echo "~~~jsonl"
  cat "$TMP"
  echo "~~~"
  echo
  echo "## Interpretación"
  echo
  echo "Este smoke test mide obediencia de formato, selección de herramienta, frontera entre verificado/no verificado y reconocimiento de un bloqueo destructivo. No mide por sí solo calidad general de programación. Para comparar perfiles cercanos, usar al menos 3 repeticiones y luego agregar tareas reales del flujo de trabajo."
} >> "$REPORT"

echo
echo "REPORTE=$REPORT"
