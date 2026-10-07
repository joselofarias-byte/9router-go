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

NATIVE_CODEX="$HOME/.local/codex-termux/node_modules/.bin/codex"
if [ -n "${CODEX_AUDIT_BIN:-}" ]; then
  CODEX_BIN="$CODEX_AUDIT_BIN"
elif [ -x "$NATIVE_CODEX" ]; then
  CODEX_BIN="$NATIVE_CODEX"
elif command -v codex >/dev/null 2>&1; then
  CODEX_BIN="$(command -v codex)"
else
  echo "ERROR: no encontre Codex CLI." >&2
  exit 1
fi

if ! command -v node >/dev/null 2>&1; then
  echo "ERROR: falta Node.js para el lector MCP." >&2
  exit 1
fi

MCP_DIR="$HOME/.local/9router-codex-audit-mcp"
MCP_SERVER="$MCP_DIR/server.mjs"
if [ ! -f "$MCP_SERVER" ] || [ ! -d "$MCP_DIR/node_modules/@modelcontextprotocol/sdk" ]; then
  echo "ERROR: falta el lector MCP read-only. Reejecuta el bootstrap de codex-audit." >&2
  exit 2
fi
node --check "$MCP_SERVER" >/dev/null

OUTDIR="$HOME/storage/downloads"
if [ ! -d "$OUTDIR" ]; then
  OUTDIR="$ROOT"
fi
OUT="$OUTDIR/CODEX_REPO_AUDIT.md"
ERR="$OUTDIR/CODEX_REPO_AUDIT.stderr.txt"

SKILL_FILE="${CODEX_HOME:-$HOME/.codex}/skills/9router-code-audit/SKILL.md"
if [ -f "$SKILL_FILE" ]; then
  PROMPT="$(cat "$SKILL_FILE")"
else
  PROMPT='Realiza una auditoria evidence-first y defect-first. No modifiques archivos. Prioriza seguridad, autenticacion, licencias/entitlements, fail-open, routing/fallback, cuota/cooldown, concurrencia, persistencia y aislamiento de credenciales.'
fi

NODE_BIN="$(command -v node)"
MCP_SERVER_TOML="${MCP_SERVER//\\/\\\\}"
MCP_SERVER_TOML="${MCP_SERVER_TOML//\"/\\\"}"
ROOT_TOML="${ROOT//\\/\\\\}"
ROOT_TOML="${ROOT_TOML//\"/\\\"}"
NODE_TOML="${NODE_BIN//\\/\\\\}"
NODE_TOML="${NODE_TOML//\"/\\\"}"

echo "Repo: $ROOT"
echo "Salida: $OUT"
echo "Codex: $("$CODEX_BIN" --version 2>/dev/null || echo "$CODEX_BIN")"
echo "Acceso al codigo: MCP LOCAL SOLO LECTURA"
echo "Shell de Codex: DESACTIVADO"
echo

EXTRA='
REGLAS DE EJECUCION PARA ESTA AUDITORIA:
- Usa EXCLUSIVAMENTE las herramientas del servidor MCP audit_repo para inspeccionar codigo y Git.
- El shell de Codex esta desactivado deliberadamente por incompatibilidad de sandbox en Android/Termux.
- No uses apply_patch ni ninguna herramienta de escritura, aunque aparezca disponible.
- No modifiques archivos, configuracion, Git, ramas ni credenciales.
- Empieza con repo_info y list_files; usa read_file/search_text/git_diff/git_log para obtener evidencia.
- No declares que el repositorio es inaccesible salvo que repo_info falle.
- Cada hallazgo material debe citar path:line y distinguir confirmado, probable-necesita-test, diseno o falso-positivo.
'

rm -f "$OUT" "$ERR"
set +e
printf '%s\n%s\n' "$PROMPT" "$EXTRA" |   "$CODEX_BIN" exec     --skip-git-repo-check     -C "$ROOT"     --sandbox read-only     -c 'features.shell_tool=false'     -c "mcp_servers.audit_repo.command=\"$NODE_TOML\""     -c "mcp_servers.audit_repo.args=[\"$MCP_SERVER_TOML\",\"$ROOT_TOML\"]"     -c 'mcp_servers.audit_repo.required=true'     -c 'mcp_servers.audit_repo.default_tools_approval_mode="approve"'     --output-last-message "$OUT"     - 2> >(tee "$ERR" >&2)
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

if grep -Eqi 'no pude acceder al repositorio|could not access the repository|MCP.*(failed|unavailable)|required MCP server.*failed' "$OUT"; then
  echo "ERROR: Codex no logro leer el repo por MCP; no cuenta como auditoria." >&2
  exit 4
fi

echo
echo "Auditoria terminada: $OUT"
