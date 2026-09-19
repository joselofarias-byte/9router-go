# 9router-go Fabric Integration Report

**Fork:** [joselofarias-byte/9router-go](https://github.com/joselofarias-byte/9router-go)  
**Integration branch:** `cursor/fabric-integration-0049`  
**Base:** `origin/sync/upstream-2026-09-15` ≡ `upstream/main` @ `5aee802` (v1.8.17)  
**PR:** _filled after open_  
**Date:** 2026-09-19

This report is the single source of truth for the integration. It records what was inspected, what was ported, what was discarded, and what still needs a real phone or live account.

## 1. Branch topology

```
588273f  common ancestor (v1.8.9-era SSE flush)
│
├── origin/main (42ad2b8)  35 fork commits  [v1.8.9 + Fabric + WorkBuddy]
│     └── origin/feat/free-best-dynamic-routing (183ea5e)  +3 / 0 behind
│
├── origin/feat/fabric-free-pool (a124fc4)  branched at 96bef62, +3 / 4 behind main
│
├── origin/sync/upstream-2026-09-15 (5aee802)  ≡ upstream/main v1.8.17
│
├── origin/sync/upstream-2026-09-11 (c9044b6)  stale sync + optional Svelte dashboard
│
├── origin/fabric/termux-native (3b64ea1)  pre-control-plane architecture
│
└── already-on-main (ancestors or squash-merged):
      chore/fabric-build-identity, chore/fork-safe-release,
      ci/termux-artifact, fix/fork-self-update-safety,
      sync/upstream-2026-09-08,
      fabric-cline-deepseek-v41,
      feature/workbuddy-{intl-bootstrap,credit-telemetry,session-free}
```

**Stacking order used (non-binding prior guesses discarded after inspection):**

1. Base = current upstream (`5aee802`).
2. Port origin/main Fabric/WorkBuddy/fork-safety files; 3-way merge overlap.
3. Port `feat/fabric-free-pool` capabilities as patches, not the 3 commits.
4. Fold `feat/free-best-dynamic-routing` into the same pool (aliases only).
5. Cherry-pick useful Termux/Grok bits from `fabric/termux-native` after validating against current grok-cli `/v1/responses` code.

## 2. Commit disposition

| Branch / commits | Disposition | Why |
|------------------|-------------|-----|
| `chore/*`, `ci/termux-artifact`, `fix/fork-self-update-safety`, `sync/upstream-2026-09-08` | already-in-main | Ancestors of `origin/main` |
| `fabric-cline-deepseek-v41`, `feature/workbuddy-*` | already-in-main | Squash-merged; trees match or are subsets |
| `e4c30d7` noop, `9a2da83` temp file | do-not-merge | Noise |
| `sync/upstream-2026-09-15` | **integration base** | Identical to `upstream/main` |
| `sync/upstream-2026-09-11` | obsolete for parity | Missing v1.8.13–1.8.17; dashboard not required |
| `fabric/termux-native` | do-not-merge as a branch | Old `db/fabric.go` API; 44 commits behind. Ported only Bionic/netcheck docs + Grok header/path extras that still apply |
| `feat/fabric-free-pool` | port-patch | Highest-value unmerged Fabric work |
| `feat/free-best-dynamic-routing` | port-patch (partial) | Aliases + empty-model policy; not a second resolver |

## 3. Upstream comparison

| Pair | Ahead / behind | Notes |
|------|----------------|-------|
| `origin/main` … `upstream/main` | 35 / 33 | Matches the stated ~35 / ~33 |
| `origin/main` … `sync/upstream-2026-09-15` | 35 / 33 | Same 33 commits |
| `sync/upstream-2026-09-15` … `upstream/main` | 0 / 0 | Exact match @ `5aee802` |

**Why not use `origin/main` as the base:** it is v1.8.9 and would drop Claude OAuth, CommandCode, `/v1/models` population, grok-cli `/v1/responses`, stream dedup, Freebuff, and live E2E work.

**Overlap files (3-way merged):**

- `internal/handlers/chat/connections.go` — clean (Fabric intercept + upstream rotation/locks)
- `internal/providers/aliases.go` — clean
- `internal/providers/errorclassify.go` — clean, then Fabric categories added
- `internal/providers/providers.go` — conflict on Kiro URL; **kept upstream** `https://q.us-east-1.amazonaws.com/generateAssistantResponse`
- `internal/proxy/executor/init.go` — clean
- `version.json` — kept `1.8.17`, empty `downloadUrl` (fork fail-closed)

## 4. Architectural decisions

1. **`fabric-free` and `free-best` are unified.** One pool, three names. Documented in `docs/FABRIC.md`.
2. **Selection is live snapshot + trust, not a static list.** Paid/`unknown` pricing never enters the pool.
3. **Models.dev is free-filtered.** Zero-cost rows only; unknown/paid catalog rows stay out of Fabric snapshots.
4. **Kira is always-on discovery** (public catalog, free chat only). Dispatch still needs a key.
5. **Kiro is a static free-set adapter** using `providers.GetProviderModels("kiro")`. It becomes a candidate only when a Kiro account exists.
6. **UnoRouter / OrcaRouter stay opt-in.** Both now have `KnownProviders` entries so discovered models can actually forward.
7. **Probes are on-demand** through `tryForwardWithConnection`. No background account hammer.
8. **Trust now carries quota cooldown, session expiry, and stale latency** (`StaleLatencyAfter = 15m`).
9. **Termux:** keep CI `CGO_ENABLED=0` artifact; document/add NDK `netcgo` path + `9router-netcheck`.
10. **Old `internal/db/fabric.go` / `handlers/fabric.go` from termux-native were not ported.** They conflict with `internal/controlplane/registry`.

## 5. Commands run (raw outcomes)

Go toolchain: `go1.27.0 linux/amd64` installed from go.dev (image had only 1.22).

```
go test ./...
# all packages ok (see §6)

go vet ./...
# exit 0

go test -race ./internal/controlplane/... ./internal/handlers/chat/...
# all ok

GOOS=linux GOARCH=arm64 go build -o /tmp/9router-go-linux-arm64 ./cmd/9router-go/
# exit 0

make build-termux
# CGO_ENABLED=0 GOOS=android GOARCH=arm64 → 9router-go-termux-arm64 17M

CGO_ENABLED=0 GOOS=android GOARCH=arm64 go build -o /tmp/9router-netcheck-android-arm64 ./cmd/9router-netcheck/
# exit 0

gofmt -w <changed Go files>
# exit 0
```

No in-repo `Fuzz*` tests exist. None were invented as stubs.

## 6. Raw test results

```
?   	9router/proxy/cmd/9router-go	[no test files]
?   	9router/proxy/cmd/9router-netcheck	[no test files]
ok  	9router/proxy/internal/auth
ok  	9router/proxy/internal/config
ok  	9router/proxy/internal/controlplane	[no tests to run]
ok  	9router/proxy/internal/controlplane/discovery
ok  	9router/proxy/internal/controlplane/pools
ok  	9router/proxy/internal/controlplane/registry
ok  	9router/proxy/internal/controlplane/routing
ok  	9router/proxy/internal/controlplane/scoring
ok  	9router/proxy/internal/controlplane/sync
ok  	9router/proxy/internal/controlplane/trust
ok  	9router/proxy/internal/controlplane/verification
ok  	9router/proxy/internal/db
ok  	9router/proxy/internal/handlers
ok  	9router/proxy/internal/handlers/chat
ok  	9router/proxy/internal/handlers/media
ok  	9router/proxy/internal/handlers/oauth
ok  	9router/proxy/internal/handlers/shared
ok  	9router/proxy/internal/handlerutil
ok  	9router/proxy/internal/headroom
ok  	9router/proxy/internal/log
ok  	9router/proxy/internal/middleware
ok  	9router/proxy/internal/mitm
ok  	9router/proxy/internal/mitm/handlers
ok  	9router/proxy/internal/pricing
ok  	9router/proxy/internal/providers
ok  	9router/proxy/internal/proxy
ok  	9router/proxy/internal/proxy/executor
ok  	9router/proxy/internal/proxy/oauth
ok  	9router/proxy/internal/shutdown
ok  	9router/proxy/internal/tokensaver
ok  	9router/proxy/internal/tracing
ok  	9router/proxy/internal/translator
ok  	9router/proxy/internal/updater
ok  	9router/proxy/internal/usagetracker
```

Race (control plane + chat): all `ok`.

## 7. Confirmed defects (found and fixed)

| Defect | Fix |
|--------|-----|
| `free-best` / `fabric-free` missing on main | Unified pool + `resolveFabricPool` |
| Production routing always `PolicyBalanced` | Pools force `PolicyFreeOnly`; exact models still default balanced |
| Prober was interface-only | `FabricProber` via real dispatch |
| Trust never saw live traffic | `recordRouteOutcome` on fallback/combo success and failure |
| Admin handlers unwired; explain-route used a fresh empty trust manager | Routes mounted; uses `globalRoutingEngine` |
| Models.dev polluted snapshots with `unknown` pricing | Ingest zero-cost rows only |
| OrcaRouter `defer resp.Body.Close()` in a loop | Close each body immediately |
| UnoRouter / Kira not in `KnownProviders` | Added |
| Kiro discovery absent | Static adapter from current registry models |
| Error classify missing session/timeout/model-not-found | Categories added without dropping upstream 502/503/504 rules |
| Grok custom `/v1` bases not normalized | `resolveGrokCLIURL` + CLI headers from Termux/FFP |
| WorkBuddy CLI login failures unclassified | `session expired` phrasing when stderr looks like logout |

## 8. Changes made (this branch)

New:

- `internal/controlplane/pools/`
- discovery `kira.go`, `kiro.go` + tests
- `internal/handlers/chat/fabric_bridge.go`, `fabric_probe.go`, `fabric_status.go`
- adversarial tests, pool tests, trust quota/session/stale tests, grok URL test
- `internal/controlplane/fabric_bench_test.go`
- `cmd/9router-netcheck/`, `scripts/build-termux.sh`, `docs/FABRIC.md`, `docs/TERMUX.md`
- this report

Preserved from origin/main (ported, not re-invented):

- full `internal/controlplane/{discovery,registry,scoring,sync,verification}`
- fork-safe updater (`internal/updater/fork_defaults.go`)
- WorkBuddy session + credit telemetry + Termux bootstrap
- Cline free adapter, DeepSeek V4.1 caps
- CI Termux artifact + fork-safe release workflows
- OpenCode example, account DB migrations, existing credential formats

## 9. Benchmarks

Host: cloud agent VM, `go test -bench -benchmem -count=1`.

| Benchmark | Result |
|-----------|--------|
| `BenchmarkScoringCalculate-4` | 7978800 iter, **155.3 ns/op**, 256 B/op, 2 allocs/op |
| `BenchmarkSelectCandidatesFreeBest-4` | earlier run ~343k iter, **~3294 ns/op**, 2018 B/op, 20 allocs/op (log lines interleave) |
| `BenchmarkSnapshotRead-4` | in-memory pointer read; no functional I/O |
| `BenchmarkDiscoveryKiraParse-4` | in-process HTTP stub, 1 free model |

These are CPU/allocation numbers, not live provider RTT.

## 10. Adversarial coverage

| Scenario | Where |
|----------|-------|
| Concurrent routing + snapshot account swaps | `TestConcurrentRoutingAndSnapshotUpdates` |
| Corrupt snapshots | existing `registry/snapshot_test.go` + `TestCorruptSnapshotLKGStillWorks` |
| Provider outage / auth quarantine | `TestProviderOutageQuarantine` |
| Invalid discovery payloads | `TestInvalidDiscoveryPayloads`, adapter tests |
| Exhausted quotas | `TestSelectCandidates_QuotaSkip`, `TestFallbackLoopDoesNotRetrySameAccountForever` |
| Expired sessions | `TestSelectCandidates_ExpiredSession`, WorkBuddy helper test |
| Account-risk penalties | `TestSelectCandidates_AccountRiskOrders`, `TestAccountRiskPenaltyApplied` |
| Retry / fallback loops | combo/fallback record + quota block |
| Malformed SSE | `TestMalformedSSEScanner` via `proxy.ScanStream` |
| Cancellation | `TestCancellationDoesNotMarkSuccess` |
| Stale latency | `TestTrustStaleLatency` |
| All free providers unavailable | `TestSelectCandidates_AllFreeUnavailable`, `TestResolveFabricPool_AllUnavailable` |

## 11. Git / PR

- Branch: `cursor/fabric-integration-0049`
- Remote: `origin` = joselofarias-byte/9router-go only. `upstream` used for fetch/compare; never pushed.
- PR URL: _see top of this file after open_

## 12. Remaining risks (need real Termux / accounts)

- On-device Bionic DNS: NDK CGO build was not produced here (no Android NDK). CI still ships the CGO-free artifact.
- Live Kira / UnoRouter / OrcaRouter catalogs and keys.
- WorkBuddy/CodeBuddy interactive login, credit burn, and streaming on a phone.
- Kiro social OAuth usability of the static free set.
- Background probe policy for `ProbeConservative` surfaces is defined but not scheduled.
- `origin/sync/upstream-2026-09-11` Svelte dashboard was not imported.
- Live E2E tests that need real upstream keys still SKIP as upstream designed them.

## 13. WorkBuddy audit (code-level)

| Area | Status |
|------|--------|
| API-key path `codebuddy-intl` | Preserved; low account-risk |
| Session path `workbuddy-session` / `wbf` / `wbs` | Separate provider; virtual `DefaultAPIKey=session`; no tools/images; semaphore=1; `--max-turns 1` |
| Credit telemetry | `usage.credit` kept in SSE/JSON |
| Session reuse | CLI saved login; not an HTTP cookie store in this repo |
| Expired session | stderr heuristics now classify as `session expired` |
| Termux bootstrap | `scripts/workbuddy-termux-bootstrap.sh` preserved |
| Live safety | Still needs a real International Site login on device |
