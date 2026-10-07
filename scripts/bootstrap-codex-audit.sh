#!/usr/bin/env bash
set -euo pipefail

REPO="joselofarias-byte/9router-go"
REF="main"
CODEX_HOME_DIR="${CODEX_HOME:-$HOME/.codex}"
SKILL_DIR="$CODEX_HOME_DIR/skills/9router-code-audit"
BIN_DIR="${PREFIX:-$HOME/.local}/bin"

if ! command -v gh >/dev/null 2>&1; then
  echo "ERROR: falta gh (GitHub CLI)." >&2
  exit 1
fi

mkdir -p "$SKILL_DIR/agents" "$BIN_DIR"

raw() {
  gh api "repos/$REPO/contents/$1?ref=$REF" -H "Accept: application/vnd.github.raw+json"
}

raw "codex-skills/9router-code-audit/SKILL.md" > "$SKILL_DIR/SKILL.md"
raw "codex-skills/9router-code-audit/agents/openai.yaml" > "$SKILL_DIR/agents/openai.yaml"
raw "scripts/codex-audit.sh" > "$BIN_DIR/codex-audit"
chmod +x "$BIN_DIR/codex-audit"

echo "OK: skill instalada en $SKILL_DIR"
echo "OK: comando instalado en $BIN_DIR/codex-audit"

if command -v codex >/dev/null 2>&1; then
  echo "Codex: $(codex --version 2>/dev/null || echo instalado)"
else
  echo "AVISO: Codex CLI no esta instalado. Instalar con: npm install -g @openai/codex"
fi
