# Agent Execution Board

Updated: 2026-10-05

This file is the coordination source of truth for the active 9router-go fork work. Human intervention is reserved for physical device/credential tests or real blockers.

## Current order

1. **Finish upstream-first Fabric integration (#54).**
2. **Port/revalidate account capacity semantics from #50 onto the completed integration head.**
3. **Add generic account selection strategies.**
4. **Add auditable cost/value telemetry.**
5. **Build Easy Mode UI over the same routing engine.**

## Lane A — Integration / upstream-first

**Status:** ACTIVE — highest priority  
**PR:** #54  
**Role:** integration implementer / Jules lane

**Checkpoint (2026-10-04):** exact head `f49dc983c5abd1e24147f39107af86a69802f34c` remains draft, the PR diff still contains no `internal/controlplane/` tree, and no exact-head GitHub Actions run is evidenced. Fabric reapplication is not complete.

**Physical baseline evidence (current main only):** `c6dc0c0692401b9be1e39c2d6ee5a63338fcde85` passed an on-device Termux smoke test on HONOR ELI-NX9 / Android 16 / arm64-v8a: artifact SHA matched, native execution succeeded, `/health` returned OK, dashboard returned HTTP 200, and `/api/version` reported `1.9.6-fabric` on `android/arm64`. This does **not** validate #54 until repeated on #54's exact integration SHA, and does not claim authenticated-provider inference.

Freeze this cycle on upstream:

`bfa0e9544f5f6cbfd8943ef397e43acd2322de13`

Do not keep fast-forwarding upstream while Fabric is still absent.

Required:

- reapply only the minimal Fabric delta;
- preserve current-main encrypted + Google Drive backups;
- preserve dynamic Termux artifact naming from #52;
- preserve fork-owned updater defaults and fail-closed version manifest;
- preserve `-fabric` build identity;
- preserve Termux/Android arm64 and CGO-free builds;
- preserve upstream routing/health/quota locks;
- keep UnoRouter opt-in and secrets out of logs/snapshots;
- prefer upstream bfa0 live/fetchgate test semantics if they conflict with current-main #55;
- keep the POSIX-safe live-test recipe merged through #56.

Exact-head gate before merge:

- `go vet ./...`
- `go test ./...`
- relevant race tests
- normal build
- Android/Termux arm64 build
- CI green on the exact integration SHA

## Lane B — Capacity / account availability

**Status:** READY — hold for port  
**Reference PR:** #50 (draft/HOLD)  
**Role:** Cursor capacity lane

#50 is the validated reference implementation on its older base. Do not expand it and do not cherry-pick it wholesale into #54. Its health-check path has one known reference defect: the probe can double-record outcomes through inner forwarding plus outer validation; when porting after #54, emit exactly one final observation and add healthy/invalid-200 regressions.

After Lane A is green, port/reimplement its semantics onto the completed integration head.

Required semantics:

- unknown is not unlimited;
- stale is not replenished;
- exhausted/cooldown accounts are skipped when another eligible account exists;
- cooldown observation never invents remaining quota;
- reset timestamps that have elapsed become stale until fresh evidence arrives;
- explain-route must show why an account was preferred, degraded or skipped.

## Lane C — Generic account strategy design

**Status:** IMPLEMENTATION VALIDATED IN #57; HOLD FOR RUNTIME WIRING UNTIL Lane B  
**Reference project:** Soju06/codex-lb  
**Agent brief:** `docs/agent-briefs/CODEX_LB_STRATEGIES.md`

Mine algorithms, not product-specific behavior:

- capacity-weighted selection;
- relative availability;
- sequential/fill drain;
- reset-aware preference;
- stable selection;
- cooldown/circuit eligibility gates.

First output must be a design note and acceptance-test matrix mapped to our generic provider/account model. Do not add Codex-only account pooling to Fabric.

## Lane D — Gateway feature mining

**Status:** DESIGN IN PARALLEL  
**Reference project:** ENTERPILOT/GoModel  
**Agent brief:** `docs/agent-briefs/GOMODEL_MINING.md`

Produce a gap matrix against current Fabric for:

- per-target circuit breakers;
- budgets;
- virtual-model UX;
- cost tracking;
- sticky sessions;
- configurable failover;
- observability.

Rule: if the capability already exists upstream or in Fabric, integrate/adapt rather than rebuild.

## Lane E — Product evidence

**Status:** ACTIVE — #58 auditable cost evidence landed  
**Landed foundation:** #49 measurement harness

Define auditable metrics:

- requests by route/profile;
- free/local share;
- known actual cost;
- configurable comparison baseline;
- estimated avoided cost;
- fallbacks absorbed;
- provider/account cooldown events.

No savings figure is valid without an explicit baseline and a traceable price source.

## Lane F — Prompt profiles

**Status:** PARKED  
**PR:** #51

Prompt rewriting is not core differentiation. Keep #51 draft/stacked until Lane A+B are complete, then reassess against measured value.

## Landed coordination/support work

- #52 dynamic Termux artifact identity
- #55 current-main fetchgate test hardening
- #53 LICENSE + product strategy
- #49 measurement harness
- #56 POSIX-safe live-test opt-in into #54 integration branch
- #58 auditable cost evidence (known-free vs unknown-price + explicit baseline comparison)
- #59 beta/product strategy documentation
- #60 isolated entitlement/license foundation (no runtime wiring)
- #61 entitlement expiry enforcement + immutable verified-license snapshot
- #64 Claude/Antigravity deep audit workflow and expanded multi-model audit brief
- Coordination cleanup (2026-10-04): superseded/reference-only PRs #12, #13, #14, #15, #16, #19, #21, #24, #28, #31, #32, #33 and #34 were closed; their branches/history remain available for selective reference.

## Deferred / held side lanes

These lanes may continue only as isolated design/test references. They must not become alternate integration paths before Lane A + Lane B complete.

- **#57 / #62 / #68 — account strategies:** draft/HOLD. #62 fixes weighted hash mixing; #68 adds expiry-pressure strategy logic. No runtime wiring until #54 is complete and #50 semantics are ported/revalidated.
- **#65 / #67 / #69 — Antigravity account behavior:** draft/HOLD behind #54 + #50. #65 currently mixes Antigravity auto-rotation with unrelated Gemini/tool-call-ID changes; split or independently justify that delta before future integration. #69 still needs direct deterministic tests for its new expiry-pressure helpers.
- **#63 — OpenCode Zen auth hardening:** draft. Exact-head CI is green; merge remains gated on one authorized authenticated-provider end-to-end inference with sanitized evidence. A prior phone-side 502 is not a successful provider validation.
- **#70 — entitlement lease v2:** draft/BLOCKED. CI is green, but LeaseProvider can reactivate after a clock rollback once a terminal grace/build boundary has been crossed. Require a same-provider rollback regression plus a monotonic/irreversible runtime policy before reconsideration.



Before starting code, every agent must state:

1. exact base SHA;
2. intended packages/files;
3. which active lanes overlap;
4. whether an existing implementation can be adapted instead.

Do not modify another active lane without explicit coordination in the relevant PR.

## Product rule

We are not building another generic LLM gateway.

The differentiated product is the layer that decides **which configured AI resource is best to use now** based on availability, quality, cost, trust and operator policy.
