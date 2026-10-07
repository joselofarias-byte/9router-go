#!/usr/bin/env bash
set -euo pipefail

PREFIX_DIR="$HOME/.local/codex-termux"
PKG="@mmmbuto/codex-cli-termux@latest"
BIN="$PREFIX_DIR/node_modules/.bin/codex"

if ! command -v npm >/dev/null 2>&1; then
  echo "ERROR: npm no esta instalado. En Termux: pkg install nodejs-lts" >&2
  exit 1
fi

echo "Instalando Codex Termux en prefijo aislado: $PREFIX_DIR"
echo "No reemplaza tu codex oficial."

npm install --prefix "$PREFIX_DIR" "$PKG"

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
echo "Probando sandbox Android..."
if "$BIN" sandbox linux -- /bin/true; then
  echo "OK: sandbox Android operativo."
else
  echo "ERROR: el sandbox Android tampoco pudo arrancar en este dispositivo." >&2
  exit 2
fi

echo
echo "Si pide autenticacion al ejecutar la auditoria, usa: codex-termux login"
echo "Luego ejecuta: codex-audit ."
