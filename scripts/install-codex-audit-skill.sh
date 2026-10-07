#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SRC="$ROOT/codex-skills/9router-code-audit"
DEST="${CODEX_HOME:-$HOME/.codex}/skills/9router-code-audit"

if [ ! -f "$SRC/SKILL.md" ]; then
  echo "ERROR: no se encontro $SRC/SKILL.md" >&2
  exit 1
fi

mkdir -p "$(dirname "$DEST")"
rm -rf "$DEST"
cp -R "$SRC" "$DEST"

if [ -n "${PREFIX:-}" ] && [ -d "$PREFIX/bin" ]; then
  cat > "$PREFIX/bin/codex-audit" <<EOF
#!/usr/bin/env bash
exec bash "$ROOT/scripts/codex-audit.sh" "\$@"
EOF
  chmod +x "$PREFIX/bin/codex-audit"
  echo "Comando corto instalado: codex-audit"
fi

echo "Skill instalada en: $DEST"
echo
echo "Uso dentro de Codex:"
echo '  $9router-code-audit audita este repositorio completo'
echo
echo "Uso no interactivo:"
echo '  codex exec --sandbox read-only "$9router-code-audit audita este repositorio completo"'
