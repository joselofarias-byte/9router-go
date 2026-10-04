#!/usr/bin/env bash
# Free-coding measurement harness.
#
# Patterns follow scripts/prompt-profile-bench.sh (JSONL rows, Markdown
# report, pass/fail, latency, HTTP status) without modifying that script.
# Tiers:
#   offline    fixtures and local graders only (default; no network)
#   discovery  public OpenRouter and Cline catalogs, no Authorization header
#   inference  authenticated free inference, opt-in via FREE_CODING_INFERENCE=1
#
# A catalog or fixture result is not a coding ranking.
set +x
set -euo pipefail

ROOT="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
TIER="${FREE_CODING_TIER:-offline}"
OUT="${FREE_CODING_OUT:-}"
ALLOW="${FREE_CODING_ALLOW:-}"
RUN="${FREE_CODING_RUNS:-1}"
MODELS=()

usage() {
  cat <<'EOF'
Uso: bash scripts/free-coding-measure.sh [--tier offline|discovery|inference] [--out DIR] [--allow FILE] [--run N] [provider/model-id ...]

  offline     (default) fixtures locales. No red y no credenciales.
  discovery   GET público de catálogos OpenRouter y Cline. Sin Authorization.
  inference   Inferencia gratis autenticada. Exige FREE_CODING_INFERENCE=1
              y OPENROUTER_FREE_API_KEY, CLINE_FREE_API_KEY o FREE_CODING_API_KEY.
              OPENROUTER_API_KEY se ignora. Sin el flag, escribe SKIP y no llama a la red.

Ejemplos:
  bash scripts/free-coding-measure.sh
  bash scripts/free-coding-measure.sh --tier discovery
  FREE_CODING_INFERENCE=1 OPENROUTER_FREE_API_KEY=... \
    bash scripts/free-coding-measure.sh --tier inference --allow .free-coding-out/LATEST/runs.jsonl \
    openrouter/some-model:free

El reporte Markdown y runs.jsonl quedan en el directorio de salida.
No es un ranking de programación salvo que haya corridas autenticadas comparables.
EOF
}

while [ "$#" -gt 0 ]; do
  case "$1" in
    --tier)
      TIER="${2:-}"
      shift 2
      ;;
    --out)
      OUT="${2:-}"
      shift 2
      ;;
    --allow)
      ALLOW="${2:-}"
      shift 2
      ;;
    --run)
      RUN="${2:-}"
      shift 2
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    --)
      shift
      MODELS+=("$@")
      break
      ;;
    -*)
      echo "Flag desconocida: $1" >&2
      usage >&2
      exit 2
      ;;
    *)
      MODELS+=("$1")
      shift
      ;;
  esac
done

case "$RUN" in
  ''|*[!0-9]*) echo "FREE_CODING_RUNS / --run debe ser un entero >= 1." >&2; exit 2 ;;
esac
if [ "$RUN" -lt 1 ]; then
  echo "--run debe ser >= 1." >&2
  exit 2
fi

if ! command -v go >/dev/null 2>&1; then
  echo "Falta go en PATH." >&2
  exit 2
fi

if [ -z "$OUT" ]; then
  STAMP="$(date -u +%Y%m%dT%H%M%SZ)"
  OUT="$ROOT/.free-coding-out/$STAMP"
fi
mkdir -p "$OUT"

cd "$ROOT"

case "$TIER" in
  offline|fixture)
    go run ./cmd/free-coding-measure offline -out "$OUT"
    ;;
  discovery|discover)
    go run ./cmd/free-coding-measure discover -out "$OUT"
    ;;
  inference|infer)
    args=(infer -out "$OUT" -run "$RUN")
    if [ -n "$ALLOW" ]; then
      args+=(-allow "$ALLOW")
    fi
    if [ "${FREE_CODING_INFERENCE:-}" != "1" ]; then
      echo "SKIP: FREE_CODING_INFERENCE no es 1. No se llama a ningún modelo." >&2
    fi
    go run ./cmd/free-coding-measure "${args[@]}" ${MODELS[@]+"${MODELS[@]}"}
    ;;
  *)
    echo "Tier desconocido: $TIER (offline, discovery, inference)." >&2
    exit 2
    ;;
esac
