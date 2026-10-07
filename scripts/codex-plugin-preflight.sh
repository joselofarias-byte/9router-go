#!/usr/bin/env bash
set -euo pipefail

fail=0

echo "== Codex plugin preflight =="

if command -v node >/dev/null 2>&1; then
  echo "Node: $(node --version)"
  node -e 'const v=process.versions.node.split(".").map(Number); if (v[0] < 18 || (v[0]===18 && v[1] < 18)) process.exit(1)' || {
    echo "ERROR: Node debe ser 18.18 o superior."
    fail=1
  }
else
  echo "ERROR: node no esta instalado."
  fail=1
fi

if command -v codex >/dev/null 2>&1; then
  echo "Codex: $(codex --version 2>/dev/null || echo instalado)"
else
  echo "Codex CLI: NO encontrado."
  echo "Instalacion manual: npm install -g @openai/codex"
fi

if command -v claude >/dev/null 2>&1; then
  echo "Claude Code: $(claude --version 2>/dev/null || echo instalado)"
else
  echo "Claude Code: no se encontro en este entorno."
fi

echo
cat <<'EOF'
Dentro de Claude Code, una sola vez:
/plugin marketplace add openai/codex-plugin-cc
/plugin install codex@openai-codex
/reload-plugins
/codex:setup

Revision normal:
/codex:review --background --base main

Revision adversarial:
/codex:adversarial-review --background --base main challenge auth and license boundaries, fail-open behavior, account and quota state races, stale snapshots, provider credential isolation, fallback correctness, rollback/downgrade paths, concurrency, persistence consistency, and assumptions that could let a locally controlled client bypass server-side enforcement

Resultados:
/codex:status
/codex:result
EOF

exit "$fail"
