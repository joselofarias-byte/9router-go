# Ecosystem Strategy — Integrate Before Building

## Decision

`9router-go` is the canonical runtime/data plane for this ecosystem. The project should not become another IDE, another coding agent, or a generic reimplementation of mature LLM gateways.

The governing rule is:

1. If a mature component already solves the problem and exposes a clean interface, integrate it.
2. If it almost fits, adapt it at the boundary.
3. Build new code only for a verified gap that is specific to this ecosystem.
4. If two internal components overlap, consolidate around the one with the clearest operational advantage.

## Canonical architecture

```text
OpenCode (primary human/agent cockpit)
        |
        | OpenAI-compatible endpoint
        v
9router-go Data Plane
        |
        +-- existing provider executors / translators
        +-- account rotation / health / cooldowns / quota handling
        +-- streaming / multimodal compatibility
        |
        v
Fabric Control Plane (narrow, differentiated layer)
        |
        +-- discovery of usable/free/free-tier capacity
        +-- verification and probes
        +-- capability and model identity/fingerprinting
        +-- trust / quarantine / scoring
        +-- snapshots, rollback and explainability
        +-- synchronization into the existing 9router runtime

External workers (not embedded): Jules, OpenHands, other coding agents
Coordination/source of truth: GitHub
Recovery/ops: TBM
Optional upstream gateways/providers: UnoRouter, OrcaRouter, OpenRouter, etc.
```

## What we keep building

The custom Fabric layer is justified where existing gateways do not solve the actual objective:

- continuously discover *currently usable* capacity, especially free/free-tier sources;
- verify that advertised models really answer and expose the expected capabilities;
- distinguish model/provider/account health from catalog metadata;
- score availability, quota, trust, latency and cost with a free-first policy;
- quarantine bad or misleading endpoints;
- persist safe snapshots and roll back automatically;
- feed those decisions into the proven 9router-go routing path without regressing its existing health/model locks.

## What we explicitly do not build

### IDE / coding UI
Use OpenCode. It already supports OpenAI-compatible custom endpoints, provider configuration and models.dev. 9router-go should expose a clean endpoint and optional generated config/docs for OpenCode rather than creating an IDE.

### Generic gateway from scratch
Do not replace the existing Go data plane with LiteLLM, Bifrost or another gateway merely to gain features 9router-go already has. Treat those projects as references and optional interoperability targets. Adopt a component only when it removes more complexity than it adds.

### Coding-agent runtime
Do not implement a Jules/OpenHands clone inside 9router-go. Workers remain external and communicate through GitHub/API boundaries.

### Duplicate model catalog
Use models.dev and provider-native `/models`/catalog endpoints where reliable. Persist only the normalized state needed for verification/routing. Do not hand-maintain a second broad model catalog.

### Speculative discovery sources
Do not ship runtime adapters for sources without a verified, stable and documented API. Unsupported or exploratory sources belong in documentation/tests until they become real integrations.

## Provider/gateway priority

A provider adapter is intentionally thin. It should normalize an existing service rather than recreate its routing logic.

Priority order:

1. Sources already available and actually used by this installation.
2. Sources with verified free/free-tier models and documented APIs.
3. Aggregators/gateways that add useful capacity (for example UnoRouter).
4. Optional paid/adaptive routers (for example OrcaRouter/OpenRouter), only as additional providers rather than architectural dependencies.

UnoRouter deserves first-class treatment because it exposes an OpenAI-compatible API, publishes free `:free` models and has an OpenCode integration path. Its own router/failover remains upstream; Fabric only verifies and scores the resulting capacity.

## Repository roles

- `9router-go`: **canonical active runtime** and home of the narrow Fabric control plane.
- `9router`: upstream/reference/dashboard source; do not duplicate its runtime where Go parity exists.
- `9router-v2`: reference/mining source for UI/skills/features only; do not run a second overlapping gateway in the canonical architecture.
- `9routemanage`: isolated auxiliary account-management experiment. Do not merge stealth-browser/account-automation code into the canonical runtime. Prefer official OAuth/API flows and preserve authentication boundaries.
- `TBM-Recovery-Master`: independent recovery/backup/verification layer; integrate through commands/artifacts, not by coupling its internals to the router.

## Pull-request gate

Before adding a subsystem, every PR must answer:

1. What verified gap does this solve?
2. Which mature existing components were evaluated?
3. Why is an adapter insufficient?
4. What existing code can be removed because of this change?
5. Does it preserve Termux-native operation?
6. Does it preserve existing 9router health locks, account rotation, streaming and provider behavior?
7. Are secrets kept out of snapshots/logs?
8. Are discovery/probes off the request hot path?

If those questions do not justify new code, integration wins.
