#!/usr/bin/env bash
set -euo pipefail

MODE="check"
case "${1:-}" in
  ""|"--check") MODE="check" ;;
  "--apply") MODE="apply" ;;
  *)
    echo "Usage: $0 [--check|--apply]" >&2
    exit 2
    ;;
esac

ROOT="$(git rev-parse --show-toplevel)"
cd "$ROOT"

UPSTREAM_URL="${UPSTREAM_PROVIDER_REPO:-https://github.com/luqman-v1/9router-go.git}"
UPSTREAM_BRANCH="${UPSTREAM_PROVIDER_BRANCH:-main}"
CATALOG="web/src/lib/providers.ts"
STATE_FILE=".github/upstream-provider-sync-base"
REPORT_FILE="${PROVIDER_SYNC_REPORT:-/tmp/9router-provider-sync-report.md}"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

catalog_ids_stream() {
  awk '
    /^export const PROVIDER_CATALOG: ProviderCatalogItem\[\] = \[/ { inside=1; next }
    inside && /^\]/ { exit }
    inside && /^    "id": "[^"]+"/ {
      line=$0
      sub(/^    "id": "/, "", line)
      sub(/".*$/, "", line)
      print line
    }
  ' | sort -u
}

catalog_ids_file() {
  catalog_ids_stream < "$1"
}

catalog_ids_ref() {
  git show "$1:$CATALOG" | catalog_ids_stream
}

git fetch --quiet --no-tags "$UPSTREAM_URL" "$UPSTREAM_BRANCH"
UPSTREAM_SHA="$(git rev-parse FETCH_HEAD)"

if [[ -s "$STATE_FILE" ]]; then
  STATE_SHA="$(tr -d '[:space:]' < "$STATE_FILE")"
else
  STATE_SHA="$(git merge-base HEAD "$UPSTREAM_SHA")"
fi

if ! git cat-file -e "$STATE_SHA^{commit}" 2>/dev/null; then
  STATE_SHA="$(git merge-base HEAD "$UPSTREAM_SHA")"
fi

catalog_ids_file "$CATALOG" > "$tmp/current.ids"
catalog_ids_ref "$UPSTREAM_SHA" > "$tmp/upstream.ids"
comm -13 "$tmp/current.ids" "$tmp/upstream.ids" > "$tmp/missing.ids"

{
  echo "# 9router-go upstream provider sync"
  echo
  echo "- Mode: `$MODE`"
  echo "- Upstream: `$UPSTREAM_URL@$UPSTREAM_BRANCH`"
  echo "- Watermark: `$STATE_SHA`"
  echo "- Upstream HEAD: `$UPSTREAM_SHA`"
  echo
} > "$REPORT_FILE"

if [[ ! -s "$tmp/missing.ids" ]]; then
  echo "No providers are missing from the local catalog." | tee -a "$REPORT_FILE"
  exit 0
fi

echo "Missing provider IDs:" | tee -a "$REPORT_FILE"
while IFS= read -r id; do
  echo "- `$id`" | tee -a "$REPORT_FILE"
done < "$tmp/missing.ids"
echo >> "$REPORT_FILE"

mapfile -t commits < <(git rev-list --reverse "$STATE_SHA..$UPSTREAM_SHA" -- "$CATALOG")
relevant_count=0

for sha in "${commits[@]}"; do
  parent="$(git rev-parse "$sha^")"
  catalog_ids_ref "$parent" > "$tmp/before.ids"
  catalog_ids_ref "$sha" > "$tmp/after.ids"
  comm -13 "$tmp/before.ids" "$tmp/after.ids" > "$tmp/added.ids"

  : > "$tmp/relevant.ids"
  while IFS= read -r id; do
    [[ -z "$id" ]] && continue
    if grep -Fxq "$id" "$tmp/missing.ids"; then
      echo "$id" >> "$tmp/relevant.ids"
    fi
  done < "$tmp/added.ids"

  [[ -s "$tmp/relevant.ids" ]] || continue
  relevant_count=$((relevant_count + 1))

  subject="$(git show -s --format=%s "$sha")"
  ids="$(paste -sd, "$tmp/relevant.ids")"
  {
    echo "## Candidate commit"
    echo
    echo "- Commit: `$sha`"
    echo "- Subject: $subject"
    echo "- New providers: `$ids`"
    echo
  } >> "$REPORT_FILE"

  if [[ "$MODE" == "apply" ]]; then
    echo "Applying provider commit $sha ($ids)"
    if ! git cherry-pick -x "$sha"; then
      git cherry-pick --abort >/dev/null 2>&1 || true
      {
        echo "### Manual reconciliation required"
        echo
        echo "The upstream provider commit did not cherry-pick cleanly."
        echo "No partial cherry-pick was kept. Reconcile this commit manually and keep CI as the gate."
      } >> "$REPORT_FILE"
      cat "$REPORT_FILE"
      exit 42
    fi

    catalog_ids_file "$CATALOG" > "$tmp/current.ids"
    comm -13 "$tmp/current.ids" "$tmp/upstream.ids" > "$tmp/missing.ids"
  fi
done

if [[ "$relevant_count" -eq 0 ]]; then
  {
    echo "No provider-adding commit was found after the stored watermark."
    echo "The catalog differs, so manual reconciliation is required."
  } | tee -a "$REPORT_FILE"
  exit 42
fi

if [[ "$MODE" == "apply" ]]; then
  if [[ -s "$tmp/missing.ids" ]]; then
    {
      echo
      echo "Providers still missing after applying candidate commits:"
      while IFS= read -r id; do echo "- `$id`"; done < "$tmp/missing.ids"
    } | tee -a "$REPORT_FILE"
    exit 42
  fi

  printf '%s\n' "$UPSTREAM_SHA" > "$STATE_FILE"
  git add "$STATE_FILE"
  if ! git diff --cached --quiet; then
    git commit -m "chore: advance upstream provider sync watermark"
  fi
fi

cat "$REPORT_FILE"
