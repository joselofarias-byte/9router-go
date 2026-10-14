# Capimux: Vercel frontend + persistent Go backend

## Verified
- Vercel deploys only `web/` (Svelte/Vite static bundle).
- The Go server in `cmd/9router-go` serves dashboard APIs, routing, OAuth and SQLite.
- Development Vite proxies `/api`, `/v1`, `/usage`, `/translator`, `/debug`, `/admin` to localhost:20130. These development proxies do not operate in Vercel production.
- SQLite stores provider credentials in plaintext JSON: protect filesystem, backups and logs.

## Deployment sequence (no production changes until tested)
1. Inspect existing Oracle VM inventory, SSH connectivity, CPU architecture, free memory, firewall and currently running services. Do not assume Oracle capacity or modify the VM without confirming it.
2. Build the Go binary for the actual host architecture and deploy as a systemd service with a persistent restricted DATA_DIR and DB_PATH, JWT_SECRET and INITIAL_PASSWORD set privately. Preserve a backup of the existing DB before any migration.
3. Expose the Go server only through an HTTPS reverse proxy or authenticated tunnel. Do not expose port 20130 directly. Restrict admin/debug endpoints and implement access controls on both proxy and Go backend.
4. Choose the canonical dashboard origin. Preferred simplest architecture: proxy the entire site through the Go server, which already embeds the SPA. If keeping Vercel as frontend, implement and test same-origin rewrites for /api, /v1, /usage, /translator, and other needed paths. Do not proxy /debug or /admin publicly without explicit authorization.
5. Confirm cookies, CORS, OAuth callback origins, CSRF protection, SSE streaming, websocket/long-running requests, model discovery, provider fallback, and account management.
6. Migrate only from a verified backup of the current Termux SQLite DB; never commit the DB, tokens, env secrets or backups.
7. Test unauthenticated access is rejected before any public DNS change. Add rollback and monitoring.
8. Once ready, update Vercel and DNS, and validate external use from mobile.

## Acceptance gates
- Public site renders; unauthenticated API/admin access blocked.
- Authenticated /api and /v1 work through TLS; streaming survives proxy.
- Provider connections survive restart; fallback tested without exposing secrets.
- SQLite backups/restores verified; rollback documented.
- No public production changes before review.

## Decision pending
Determine which Oracle VM is actually available, its architecture, and whether the Go server can be hosted there reliably. Vercel alone cannot host this persistent SQLite-backed Go process unchanged.
