#!/usr/bin/env bash
set -euo pipefail

if ! command -v codex >/dev/null 2>&1; then
  echo "ERROR: Codex CLI no esta instalado." >&2
  echo "Instalacion: npm install -g @openai/codex" >&2
  exit 1
fi

ROOT="${1:-$HOME/9router-license-test}"
ROOT="$(cd "$ROOT" && pwd)"

if [ ! -d "$ROOT/.git" ]; then
  echo "ERROR: $ROOT no parece ser un repositorio Git." >&2
  exit 1
fi

OUTDIR="$HOME/storage/downloads"
if [ ! -d "$OUTDIR" ]; then
  OUTDIR="$ROOT"
fi
OUT="$OUTDIR/CODEX_REPO_AUDIT.md"

cd "$ROOT"

echo "Repo: $ROOT"
echo "Salida: $OUT"
echo "Modo: SOLO LECTURA"
echo

codex exec --sandbox read-only --output-last-message "$OUT" '$9router-code-audit realiza una auditoria completa de este repositorio. Prioriza seguridad, autenticacion, licencias/entitlements, fail-open, routing/fallback, cuota/cooldown, concurrencia, persistencia y aislamiento de credenciales. No modifiques nada.'

echo
echo "Auditoria terminada: $OUT"
