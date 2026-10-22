# Request statistics

The Usage > Details table now shows the serving account, cached-input percentage,
end-to-end output tokens per second and estimated USD cost with six decimal places.
The inspector includes those metrics and the originating API endpoint. The overview
also shows the share of input tokens reused from cache.

- Cache percentage is cached input / total input, never a correctness score. Missing
  or inconsistent counts show an em dash.
- Output throughput is completion tokens * 1000 / total milliseconds. This includes
  waiting time and is not a measurement of decoding-only speed. Estimated token
  counts mark the rate with `~`.
- Cost is calculated at request time using Capimux's existing pricing table. It is
  an estimate, not vendor billing or subscription spend. A known zero tariff can
  show zero; an unknown tariff and old records without cost show an em dash.
- Account names come from the dashboard's existing connection list. Credentials
  are not added to request statistics.
- Collision-safe UUIDs preserve simultaneous same-model request records.

This is an additive fork feature inspired by Magpie. The upstream usage stats
endpoint and dashboard were inspected; no routing, fallback, provider selection,
production configuration or database schema changes are required. Existing detail
records keep working and their absent fields stay unavailable.

Still separate work: collecting explicit routing-decision reasons, attributing
requests to agent applications, and a context-composition inspector. These cannot
be inferred reliably from the existing request history. This change does not
claim parity with all Magpie analytics.

## Validation

```sh
cd web
bun install --frozen-lockfile
bun test
bun run build
cd ..
go test ./...
go vet ./...
go test -tags=integration ./internal/integration/...
go test -race ./internal/handlers/chat -run 'TestRequestStatistics|TestLogFailure'
go build -o /tmp/capimux-statistics ./cmd/9router-go
```

The integration test uses the production router, temporary database and fake
upstream; it checks the dashboard API receives the persisted token counts,
connection, endpoint and cost metadata without using real provider credits.
