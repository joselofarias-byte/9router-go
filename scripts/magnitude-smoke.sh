#!/usr/bin/env bash
set -euo pipefail
# Read-only smoke test; does not install, download or start a server.
# Magnitude's upstream server release documents port 10100.
BASE="${MAGNITUDE_BASE_URL:-http://127.0.0.1:10100}"
BASE="${BASE%/}"
KEY="${MAGNITUDE_API_KEY:-}"
AUTH=()
if [[ -n "$KEY" ]]; then AUTH=(-H "Authorization: Bearer $KEY"); fi
echo "Checking Magnitude-compatible endpoint at $BASE"
curl --fail-with-body --silent --show-error --max-time 10 "${AUTH[@]}" "$BASE/v1/models" | python3 -m json.tool
if [[ -z "${MAGNITUDE_MODEL:-}" ]]; then
  echo "Set MAGNITUDE_MODEL to test chat completions; no model inferred." >&2
  exit 0
fi
python3 -c 'import json,os;print(json.dumps({"model":os.environ["MAGNITUDE_MODEL"],"messages":[{"role":"user","content":"Respond with OK"}],"stream":False,"max_tokens":32}))' |
  curl --fail-with-body --silent --show-error --max-time 120 "${AUTH[@]}" -H 'Content-Type: application/json' -X POST "$BASE/v1/chat/completions" --data-binary @- | python3 -m json.tool
