#!/usr/bin/env bash
set -euo pipefail

if [ "$#" -gt 0 ] && [ "${1#-}" = "$1" ]; then
  ROOT="$1"
  shift
elif [ -d "$PWD/.git" ]; then
  ROOT="$PWD"
else
  echo "ERROR: ejecuta codex-audit dentro de un repositorio Git o pasale una ruta." >&2
  exit 1
fi
ROOT="$(cd "$ROOT" && pwd)"

if [ ! -d "$ROOT/.git" ]; then
  echo "ERROR: $ROOT no parece ser un repositorio Git." >&2
  exit 1
fi

TERMUX_CODEX="$HOME/.local/codex-termux/node_modules/.bin/codex"
if [ -n "${CODEX_AUDIT_BIN:-}" ]; then
  CODEX_BIN="$CODEX_AUDIT_BIN"
elif [ -x "$TERMUX_CODEX" ]; then
  CODEX_BIN="$TERMUX_CODEX"
elif command -v codex >/dev/null 2>&1; then
  CODEX_BIN="$(command -v codex)"
else
  echo "ERROR: no encontre Codex CLI." >&2
  exit 1
fi

OUTDIR="$HOME/storage/downloads"
if [ ! -d "$OUTDIR" ]; then
  OUTDIR="$ROOT"
fi
OUT="$OUTDIR/CODEX_REPO_AUDIT.md"
ERR="$OUTDIR/CODEX_REPO_AUDIT.stderr.txt"

PROMPT_FILE="$ROOT/codex-skills/9router-code-audit/SKILL.md"
if [ -f "$PROMPT_FILE" ]; then
  PROMPT="$(cat "$PROMPT_FILE")"
else
  PROMPT='Realiza una auditoria read-only, evidence-first y defect-first de este repositorio. Prioriza seguridad, autenticacion, licencias/entitlements, client-vs-server trust, fail-open, routing/fallback, quota/cooldown/stale state, concurrencia/TOCTOU, persistencia, aislamiento de credenciales, downgrade/rollback y Termux/ARM64. No modifiques ningun archivo. Cada hallazgo material debe incluir severidad, estado, archivo:linea, evidencia, impacto, como probarlo y cambio minimo sugerido.'
fi

echo "Repo: $ROOT"
echo "Salida: $OUT"
echo "Codex: $("$CODEX_BIN" --version 2>/dev/null || echo "$CODEX_BIN")"
echo "Modo: SOLO LECTURA"
echo

# Fail fast on Android/Termux builds whose Linux bubblewrap sandbox cannot start.
# The community Termux build provides an Android seccomp+ptrace backend and should
# pass this probe. Do not silently downgrade to unsandboxed execution.
if ! "$CODEX_BIN" sandbox linux -- /bin/true >/dev/null 2>"$ERR"; then
  if grep -Eqi 'bwrap|bubblewrap|bind mount|oldroot|sandbox.*(fail|error)|overflowuid' "$ERR"; then
    echo "ERROR: este Codex no puede establecer el sandbox read-only en Android/Termux." >&2
    echo "No voy a continuar sin sandbox." >&2
    echo "Instala el runtime Termux aislado con:" >&2
    echo "  bash ~/9router-go/scripts/install-codex-termux-audit-runtime.sh" >&2
    exit 2
  fi
fi

rm -f "$ERR"
set +e
printf '%s\n\n%s\n' "$PROMPT" 'Audita ahora el repositorio indicado. No modifiques nada.' |   "$CODEX_BIN" exec     --skip-git-repo-check     -C "$ROOT"     --sandbox read-only     --output-last-message "$OUT"     - 2> >(tee "$ERR" >&2)
rc=$?
set -e

if [ "$rc" -ne 0 ]; then
  echo "ERROR: Codex termino con codigo $rc. No marco la auditoria como completada." >&2
  exit "$rc"
fi

if [ ! -s "$OUT" ]; then
  echo "ERROR: Codex no genero un informe util." >&2
  exit 3
fi

if grep -Eqi 'bloquead[oa] por un fallo|no pude acceder al repositorio|sandbox.*(fail|error)|bwrap:' "$OUT"; then
  echo "ERROR: Codex devolvio un informe de entorno bloqueado; no cuenta como auditoria." >&2
  exit 4
fi

echo
echo "Auditoria terminada: $OUT"
