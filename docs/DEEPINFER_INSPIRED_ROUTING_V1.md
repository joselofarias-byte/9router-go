# CAPIMUX — DeepInfer-inspired routing experiment V1

Status: **shadow-only prototype; not wired into production**.

## Scope
This is an original experimental Go telemetry collector inspired by the operational goals of high-performance inference infrastructure. **It is not a DeepInfer port, fork, or integration.** DeepInfer supports specific GPU/NPU hardware, while Capimux primarily routes external and local endpoints.

## Added
- `internal/adaptive/collector.go`: in-memory, concurrency-safe per-provider/model aggregates.
- `internal/adaptive/collector_test.go`: unit tests for success, invalid inputs, concurrent writes, cold start.
- No routes, middleware, dependency or DB schema changes.

## Scoring (diagnostic only)
- Unknown routes: neutral score 50.
- Observed routes: 70 × success rate + 20 / (1 + average latency ms / 2000) + 10 × min(1, tokens per second / 50).
- Scores are a **heuristic, not a quality guarantee**. Small sample sizes may mislead.
- Cost is collected only from explicitly provided nonnegative amounts; not estimated. It does not yet influence score.

## Privacy / safety
- No prompts, responses, API keys, connection IDs or account identifiers are stored.
- Data is ephemeral and process-local.
- No external calls and no changes to live model selection.
- Never record payloads or secrets when wiring call sites.
- The classifier is not yet concurrency-window-aware or streaming-aware; `Latency` is whole-request elapsed time, not TTFT.

## Verification required before integration
1. Run `go test ./internal/adaptive -race` with the Go toolchain declared in go.mod.
2. Inspect actual `usagetracker` and request finalization paths for nonduplicative, post-response observations.
3. Distinguish response TTFT from total duration, streaming completions, cancellation, retries and error categories.
4. Add a bounded-memory cardinality policy, optional retention, cost provenance and confidence floor.
5. Add an opt-in `shadow` flag disabled by default, report-only endpoint, redaction tests.
6. Run reproducible A/B simulation against existing fallback with no changes to production routing.
7. Only after review and separate approval evaluate any activation of weighted routing.

## Keep unchanged
- Existing fallback, account rotation, quota and cooldown mechanisms.
- Oracle resources, external API accounts and the running 20130 service.
- `main` branch and deployments.

Reference: https://github.com/deepseek-ai/DeepInfer

## V2 observational wiring (not deployed)
The opt-in hook has been added to `internal/handlers/chat/fallback.go` at the final per-connection upstream outcome, after the existing trace logic. It reports per-provider/model elapsed time and success. Canceled requests are excluded. The environment variable `CAPIMUX_ADAPTIVE_SHADOW=1` enables the collector; all other values disable it. The in-memory collector is capped at 512 distinct routes and never stores payloads or connection IDs.

**Limitations:** currently no authenticated metrics API and no persistence; observers can only consume `adaptive.ShadowSnapshot` within the Go process. Tokens and cost are not yet wired into the hook. A reactive OAuth retry remains part of one recorded connection attempt rather than one separate observation. This is intentional until attempt definitions and cost attribution are agreed. No shadow recommendation is applied to routing.

Validation on a Go 1.27-compatible environment is still required: `go test ./internal/adaptive -race`, then `go test ./internal/handlers/chat ./internal/adaptive` and inspect any compilation or behavioral failures. The authoring environment did not have repository-clone connectivity or the required toolchain, so passing tests must **not** be claimed.

## V3 JSON export for the offline viewer

`internal/adaptive/export.go` adds `(*Collector).ExportJSON()` and `ShadowExportJSON()`. JSON includes `schema_version: 1` and `routes` compatible with `docs/shadow-analytics.html`. Rows are sorted by provider and model, and export happens in-process. No external endpoint, file persistence or request interception was added. Even if the opt-in flag is enabled, an external dashboard cannot access the running process's metrics yet; future access must be authenticated and explicitly approved.

Token throughput and cost remain **not instrumented** (the viewer intentionally shows 'Sin datos'). This is a schema and diagnostic-export foundation, not an operational metrics dashboard. Test with `go test -race ./internal/adaptive` before using.
