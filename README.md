# 9router-go

[![CI](https://github.com/luqman-v1/9router-go/actions/workflows/ci.yml/badge.svg)](https://github.com/luqman-v1/9router-go/actions/workflows/ci.yml)
[![Release](https://github.com/luqman-v1/9router-go/actions/workflows/release.yml/badge.svg)](https://github.com/luqman-v1/9router-go/actions/workflows/release.yml)

9router-go is a single-binary AI gateway and dashboard. The Go process serves the OpenAI-, Claude-, Gemini-, and Ollama-compatible proxy APIs on the same port as a Svelte 5 dashboard. The dashboard is built with Vite, embedded in the binary, and needs no Node.js or separate web server at runtime.

```text
CLI / SDK ──► 9router-go :20130 ──► provider gateways
                    │  ├─ /v1/* and compatibility API
Browser ───────────┘  ├─ /dashboard + Svelte SPA
                       └─ /api/* management API
```

Open `http://localhost:20130` after starting an existing, initialized 9router data directory.
> **Shared database with upstream 9router (Next.js):** 9router-go has no separate database — it opens the same `DATA_DIR/db/data.sqlite` file (`~/.9router/db/data.sqlite` by default) as the upstream Next.js app, reading/writing the same tables and JSON shapes. Point Go at an existing initialized 9router data directory and providers, connections, combos, settings, and usage carry over. Schema bootstrap and migrations are still owned by upstream (Go only creates the `upstream_leases` table); see [Database compatibility](#database-compatibility-and-bootstrap-limit) and [`DATABASE.md`](DATABASE.md). Avoid running the Go and Next.js processes as concurrent writers in production — WAL helps readers, but daily usage aggregates can overwrite each other.

> **Version and compatibility baseline:** the Go release is **v1.9.2** (`VERSION`, `version.json`, and `internal/updater.CurrentVersion`). The local upstream checkout and Go manifest declare [`decolua/9router` v0.5.85](https://github.com/decolua/9router) as the synchronization baseline, while published upstream npm/Docker `latest` is v0.5.86. `CHANGELOG.md` records selected v0.5.86 parity work under Go v1.9.0 and explicitly deferred work; this is not a claim of complete endpoint, provider, or feature parity.

## Features

- Native Svelte 5 dashboard for providers, OAuth connections, provider nodes, combos, proxy pools, API keys, models, usage, quota, and settings
- OpenAI Chat Completions, Claude Messages, Gemini, Ollama-compatible, Responses, embeddings, media, search, and web-tool paths
- Combos with fallback, round-robin, sticky routing, fusion, capability-aware reordering, and account fallback
- Per-provider executors plus OpenAI-compatible and Gemini-native defaults; OAuth refresh and reactive 401 retry
- Bidirectional request/response translation, streamed SSE handling, usage capture, live usage/console streams, and stall detection
- SQLite WAL persistence, proxy pools, outbound proxy support, token-saver options, self-update, MITM commands, Docker, and cross-compilation

Detailed routing and provider behavior is documented in [`ARCHITECTURE.md`](ARCHITECTURE.md). Schema details and compatibility notes are in [`DATABASE.md`](DATABASE.md).

## Install

### Release binary

Download the archive for your platform from [GitHub Releases](https://github.com/luqman-v1/9router-go/releases/latest), then verify it against `SHA256SUMS.txt` from that release.

Release artifacts (click to download directly):

| Platform | Architecture | Binary |
| --- | --- | --- |
| Linux | `amd64` | [`9router-go-linux-amd64`](https://github.com/luqman-v1/9router-go/releases/latest/download/9router-go-linux-amd64) |
| Linux | `arm64` | [`9router-go-linux-arm64`](https://github.com/luqman-v1/9router-go/releases/latest/download/9router-go-linux-arm64) |
| macOS | `amd64` (Intel) | [`9router-go-darwin-amd64`](https://github.com/luqman-v1/9router-go/releases/latest/download/9router-go-darwin-amd64) |
| macOS | `arm64` (Apple Silicon M1/M2/M3/…) | [`9router-go-darwin-arm64`](https://github.com/luqman-v1/9router-go/releases/latest/download/9router-go-darwin-arm64) |
| Windows | `amd64` | [`9router-go-windows-amd64.exe`](https://github.com/luqman-v1/9router-go/releases/latest/download/9router-go-windows-amd64.exe) |

> **Orang awam pilih yang mana?** Windows → file `.exe`; Mac keluaran 2020 ke atas (M1/M2/M3/M4) → `darwin-arm64`, Mac Intel lama → `darwin-amd64`; VPS/server Linux → `linux-amd64`, Raspberry Pi → `linux-arm64`. Lengkapnya tetap di [GitHub Releases](https://github.com/luqman-v1/9router-go/releases/latest) (ada juga `SHA256SUMS.txt` buat verifikasi).

### Docker (image jadi, paling gampang)

Tidak mau build apa-apa? Pakai image yang sudah jadi dari Docker Hub:

```bash
docker run -d --name 9router-go --restart unless-stopped \
  -p 20130:20130 \
  -v 9router-data:/data \
  -e PORT=20130 -e DATA_DIR=/data \
  luqmenul/9router-go:latest
curl http://localhost:20130/health
```

Update ke rilis baru:

```bash
docker pull luqmenul/9router-go:latest
docker stop 9router-go && docker rm 9router-go
# jalankan lagi perintah docker run di atas
```

Pakai data 9router yang sudah ada (provider, koneksi, combo ikut terbawa karena DB-nya sharing, lihat catatan di atas):

```bash
docker run -d --name 9router-go --restart unless-stopped \
  -p 20130:20130 \
  -v "$HOME/.9router:/data" \
  -e PORT=20130 -e DATA_DIR=/data \
  luqmenul/9router-go:latest
```

### Docker Compose (build dari source)

```bash
docker compose up -d --build
curl http://localhost:20130/health
```

Perintah harian:

```bash
docker compose logs -f          # lihat log
docker compose up -d --build    # rebuild setelah update source
docker compose down             # matikan (data di volume 9router-data tetap aman)
```

File `docker-compose.yml` bawaan menyimpan `/data` di volume `9router-data` dan dashboard sudah tertanam di image — JavaScript hanya dibutuhkan saat build. Variabel env (`INITIAL_PASSWORD`, `RTK_ENABLED`, dll, lihat tabel [Environment](#environment)) bisa ditambah di blok `environment:`.

> Volume baru = file SQLite kosong, bukan skema lengkap. Lihat [Database compatibility](#database-compatibility-and-bootstrap-limit) sebelum pemakaian pertama — idealnya mount direktori 9router yang sudah terinisialisasi seperti contoh di atas.

### Build from source

Prerequisites: Go 1.27 and Bun 1.x. The Go package embeds `web/dist`, so build the dashboard before compiling the binary.

```bash
git clone https://github.com/luqman-v1/9router-go.git
cd 9router-go

make web-build       # bun install --frozen-lockfile && bun run build
make build           # embeds VERSION into the Go binary
```

Rebuild dashboard assets after frontend changes:

```bash
FORCE=1 make web-build
make build
```

`go build` alone is sufficient only when a current `web/dist/index.html` already exists.

## Run

The server reads `.env` and environment variables. There are no `--port` or `--db-path` flags; use `PORT` and `DB_PATH`.

```bash
PORT=20130 DATA_DIR="$HOME/.9router" ./9router-go
```

Common alternatives:

```bash
# Keep the default database path but move the data directory.
DATA_DIR=/srv/9router PORT=20130 ./9router-go

# Select a SQLite file explicitly.
DB_PATH=/srv/9router/data.sqlite PORT=20130 ./9router-go

# Restrict the listener to a local reverse proxy.
HOST=127.0.0.1 PORT=20130 ./9router-go

# Check liveness and release metadata.
curl http://localhost:20130/health
./9router-go version
```

The supported global flags are `--rtk`, `--caveman`, `--ponytail`, `--auto-update`, and `--no-injection-guard`. `make run` and `make dev` pass the corresponding Make variables. Commands are also available for `version`, `update`, and `mitm enable|disable|status`.

### Client setup

Use the gateway base URL and an active key created in **Settings → API Keys**:

```bash
curl http://localhost:20130/v1/chat/completions \
  -H 'Content-Type: application/json' \
  -H 'Authorization: Bearer sk-your-api-key' \
  -d '{"model":"ag/gemini-3.8-flash-high","messages":[{"role":"user","content":"Hello"}],"stream":true}'
```

For Claude Messages clients, use `ANTHROPIC_BASE_URL=http://localhost:20130/v1`. Engine routes always require an active client API key; `Authorization: Bearer` and `X-API-Key` are supported. Query-string keys are accepted only on paths ending in `/stream` for browser `EventSource` clients.

## Environment

| Variable | Default | Purpose |
| --- | --- | --- |
| `PORT` | `20130` | HTTP port |
| `HOST` / `BIND_ADDR` | all interfaces | Listener address |
| `DATA_DIR` | `~/.9router`; `%APPDATA%/9router` on Windows | Data root |
| `DB_PATH` | `$DATA_DIR/db/data.sqlite` | SQLite file; a directory is resolved when it contains a known database |
| `JWT_SECRET` | generated into `$DATA_DIR/jwt-secret` | Dashboard session signing secret |
| `INITIAL_PASSWORD` | unset; compatibility fallback is `123456` | First dashboard password before a saved hash exists |
| `API_KEY_SECRET` | compatibility default | API-key hashing compatibility setting |
| `MACHINE_ID_SALT` | compatibility default | Machine identity compatibility setting |
| `RTK_ENABLED` | `true` | RTK input compression |
| `CAVEMAN_ENABLED` | `false` | Caveman terse style |
| `PONYTAIL_ENABLED` | `false` | Ponytail code style |
| `AUTO_UPDATE` | `false` | Background self-update |
| `INJECTION_GUARD_DISABLED` | `false` | Disable prompt-injection detection |
| `LOG_FILE` | standard error | Append server logs to a file |
| `HTTP_PROXY` / `HTTPS_PROXY` | Go proxy defaults | Optional upstream egress proxy |
| `FX_LOGGING` | `false` | Emit Fx lifecycle events |
| `PPROF_ENABLED` | `false` | Expose `/debug/pprof/*`; keep disabled on untrusted networks |
| `TRUST_PROXY` / `TRUST_CLOUDFLARE` | unset | Trust forwarded client-IP headers for login limiting |

`.env.example` documents the security-sensitive subset and optional OAuth client overrides.

## Authentication boundaries

- `GET /health`, the dashboard HTML/assets, `/login`, auth status/login/logout, and provider OAuth callback landing pages are public.
- Proxy and compatibility routes are protected by `RequireApiKey`; this is not controlled by the dashboard's `requireLogin` setting.
- Dashboard management APIs use `RequireDashboardAuth`. Access is allowed when dashboard login is disabled, or when the request has a valid 24-hour `auth_token` cookie, the local `x-9r-cli-token`, or an active client API key.
- Destructive/admin operations require a valid dashboard session or local CLI token. Standard client API keys are rejected for shutdown, update, health reset, and database export/import.
- A remote fresh-install login using the compatibility default password is refused until the password is changed or `INITIAL_PASSWORD` is set. Local access still accepts the compatibility password until a dashboard password hash is stored.

## API surface

The most commonly used routes are:

```text
POST /v1/chat/completions       OpenAI Chat Completions
POST /v1/messages               Claude Messages
POST /v1/messages/count_tokens  Claude token counting
POST /v1/responses              Responses API
POST /v1/responses/compact      Compact Responses API
POST /v1/embeddings             Embeddings
POST /api/chat                  Ollama-compatible chat
GET  /v1/models                 Model catalog
GET  /v1/models/info            Model capabilities and limits
GET  /v1/models/{kind}          Models filtered by kind

POST /v1/images/generations     Image generation
POST /v1/videos/generations     Video generation
POST /v1/audio/speech           Text to speech
POST /v1/audio/transcriptions   Speech to text
POST /v1/search                 Provider-selected web search
POST /v1/scrape                 Web scrape

GET  /api/usage/stream          Live usage SSE
GET  /api/usage/stats           Current usage statistics
GET  /translator/console-logs/stream
                               Live console log SSE
GET  /health                    Liveness
GET  /api/version               Version and update metadata
```

The request middleware repeatedly removes leading `/v1/` segments before routing, so `/v1/chat/completions` and `/chat/completions` reach the same canonical handler. Some model and dashboard compatibility aliases are also registered explicitly.

## Database compatibility and bootstrap limit

The Go runtime opens the configured SQLite database in WAL mode with a five-second busy timeout and enforces private file permissions. It reads and writes the upstream 9router table/JSON shapes and adds the Go-only `upstream_leases` table for cross-process Freebuff session coordination.

**Current limitation:** `internal/db.OpenDatabase` creates directories and the SQLite file, but it does not create the upstream schema, seed API keys/settings, import legacy JSON, or run migrations. Only `upstream_leases` is created idempotently. Therefore, a truly fresh DB is not a supported standalone bootstrap path. Start with an existing schema-compatible 9router database; opening an empty file is not equivalent to a successful migration.

The `DB_PATH` resolver recognizes a directory containing `db/data.sqlite`, `data.sqlite`, or `9router.db`, which is useful for common upstream layouts. Back up the database before sharing it between processes or deployments. Provider secrets are stored in the database and files/directories are chmodded private where supported.

## Verify

The CI workflow is the release contract for pushes and pull requests:

```bash
make web-build   # Bun 1.4.2, frozen lockfile
go vet ./...
go test ./... -v
go build -ldflags='-s -w' -o /tmp/9router-go ./cmd/9router-go/
```

For local development of the dashboard:

```bash
cd web
bun install --frozen-lockfile
bun run dev       # Vite dev server; run the Go server on :20130 separately
bun run lint
```

The web package currently has no frontend unit/component test script. Validate dashboard changes against a running Go server and the affected browser route. `make test-short` runs `go test ./...`; `make vet` runs `go vet ./...`; `make cross` builds release binaries and checksums.

## Operational caveats

- Compatibility with upstream means selected data shapes, routes, and behaviors are ported; it is not a blanket 100% parity guarantee.
- A clean database requires bootstrap by a schema-capable upstream/runtime path; Go currently does not perform that bootstrap.
- The production dashboard is fully embedded, but building it still requires Bun and the committed lockfile.
- The API server defaults to all interfaces. Bind to localhost or protect the port when exposing it outside a trusted machine.
- `PPROF_ENABLED` exposes sensitive profiling endpoints and is disabled by default.
- Release binaries and the self-updater are unsigned. Verify release checksums; Windows may require an explicit allow action.
- Provider availability, account limits, vendor anti-abuse controls, and account-sharing policy remain upstream concerns; client cloaking does not guarantee account safety.

## Roadmap

See [`ROADMAP.md`](ROADMAP.md) for proposals only. Items there are not current behavior.

## Credits

- [9Router](https://github.com/decolua/9router) — original Next.js/React gateway and dashboard from which this Go implementation and Svelte port preserve selected compatibility contracts
