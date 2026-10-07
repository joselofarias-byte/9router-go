#!/usr/bin/env bash
set -euo pipefail

PREFIX_DIR="$HOME/.local/codex-termux"
PKG="@mmmbuto/codex-cli-termux@0.160.0-termux.3"
BIN="$PREFIX_DIR/node_modules/.bin/codex"

if ! command -v npm >/dev/null 2>&1; then
  echo "ERROR: npm no esta instalado. En Termux: pkg install nodejs-lts" >&2
  exit 1
fi

echo "Instalando Codex Termux en prefijo aislado: $PREFIX_DIR"
echo "Este runtime NO aporta un sandbox de filesystem en Android."
echo "codex-audit lo usa solo con shell desactivado y un MCP lector read-only."

npm install --prefix "$PREFIX_DIR" --no-audit --no-fund --allow-scripts=@mmmbuto/codex-cli-termux "$PKG" ||   npm install --prefix "$PREFIX_DIR" --no-audit --no-fund "$PKG"

if [ ! -x "$BIN" ]; then
  echo "ERROR: no aparecio el binario esperado: $BIN" >&2
  exit 1
fi

echo "OK: $("$BIN" --version 2>/dev/null || echo instalado)"

if [ -n "${PREFIX:-}" ] && [ -d "$PREFIX/bin" ]; then
  cat > "$PREFIX/bin/codex-termux" <<EOF
#!/usr/bin/env bash
exec "$BIN" "\$@"
EOF
  chmod +x "$PREFIX/bin/codex-termux"
  echo "OK: comando codex-termux instalado en $PREFIX/bin"
fi

echo
echo "No se ejecuta 'codex sandbox' en Android: ese subcomando no esta soportado."
echo "Para auditar usa el flujo MCP read-only: codex-audit ."
