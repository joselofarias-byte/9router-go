#!/usr/bin/env bash
set -euo pipefail

REPO="joselofarias-byte/9router-go"
REF="main"
CODEX_HOME_DIR="${CODEX_HOME:-$HOME/.codex}"
SKILL_DIR="$CODEX_HOME_DIR/skills/9router-code-audit"
BIN_DIR="${PREFIX:-$HOME/.local}/bin"
MCP_DIR="$HOME/.local/9router-codex-audit-mcp"

if ! command -v gh >/dev/null 2>&1; then
  echo "ERROR: falta gh (GitHub CLI)." >&2
  exit 1
fi
if ! command -v npm >/dev/null 2>&1 || ! command -v node >/dev/null 2>&1; then
  echo "ERROR: faltan Node.js/npm. En Termux: pkg install nodejs-lts" >&2
  exit 1
fi

mkdir -p "$SKILL_DIR/agents" "$BIN_DIR" "$MCP_DIR"

raw() {
  gh api "repos/$REPO/contents/$1?ref=$REF" -H "Accept: application/vnd.github.raw+json"
}

raw "codex-skills/9router-code-audit/SKILL.md" > "$SKILL_DIR/SKILL.md"
raw "codex-skills/9router-code-audit/agents/openai.yaml" > "$SKILL_DIR/agents/openai.yaml"
raw "scripts/codex-readonly-mcp.mjs" > "$MCP_DIR/server.mjs"
raw "scripts/codex-audit.sh" > "$BIN_DIR/codex-audit"
chmod +x "$BIN_DIR/codex-audit"

if [ ! -d "$MCP_DIR/node_modules/@modelcontextprotocol/sdk" ] || [ ! -d "$MCP_DIR/node_modules/zod" ]; then
  echo "Instalando lector MCP read-only..."
  npm install --prefix "$MCP_DIR" --ignore-scripts --no-audit --no-fund @modelcontextprotocol/sdk@1.26.0 zod@3.25.76
fi

node --check "$MCP_DIR/server.mjs" >/dev/null

echo "OK: skill instalada en $SKILL_DIR"
echo "OK: lector MCP read-only en $MCP_DIR"
echo "OK: comando instalado en $BIN_DIR/codex-audit"

if [ -x "$HOME/.local/codex-termux/node_modules/.bin/codex" ]; then
  echo "Codex Termux: $("$HOME/.local/codex-termux/node_modules/.bin/codex" --version 2>/dev/null || echo instalado)"
elif command -v codex >/dev/null 2>&1; then
  echo "Codex: $(codex --version 2>/dev/null || echo instalado)"
else
  echo "AVISO: Codex CLI no esta instalado."
fi
