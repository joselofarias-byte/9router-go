# Fabric port manifest — frozen upstream integration

Target branch: `integration/upstream-20cb41cc-fabric`

Frozen upstream parent for this cycle:

`bfa0e9544f5f6cbfd8943ef397e43acd2322de13`

This document exists to keep the integration finite. Do not advance the upstream baseline again during this cycle.

## Fork-owned Fabric packages to reapply

Current fork main contains these control-plane packages and the integration branch does not:

- `internal/controlplane/discovery/`
- `internal/controlplane/registry/`
- `internal/controlplane/routing/`
- `internal/controlplane/scoring/`
- `internal/controlplane/sync/`
- `internal/controlplane/trust/`
- `internal/controlplane/verification/`

Port them upstream-first: preserve upstream provider/data-plane behavior and adapt Fabric to the current upstream contracts rather than restoring old handler implementations verbatim.

## Runtime integration points

After the pure packages compile, wire only the minimum required hooks for:

- discovery/orchestration startup;
- shared registry/trust/routing instances;
- admin/explainability endpoints;
- existing request resolution/fallback path;
- route outcome feedback required by scoring/trust.

Do not create a parallel gateway, IDE or agent runtime.

## Explicitly deferred

Do **not** port these into this baseline step:

- PR #50 capacity/availability bridge;
- PR #57 generic account strategies;
- prompt-profile PR #51;
- new pricing/evidence runtime wiring.

Those are layered after this branch is stable.

## Fork invariants that must survive reconciliation

- fork-owned updater defaults and regression tests;
- fail-closed `version.json`;
- `-fabric` build identity;
- encrypted local backups and Google Drive backup flow;
- dynamic Termux artifact naming;
- Android/Termux arm64 CGO-free build;
- UnoRouter remains opt-in; no secrets in logs/snapshots;
- exact `:free` classification rules where applicable.

## Conflict rules

- Prefer frozen upstream `bfa0e954` fetchgate/live-test semantics over older fork test assertions.
- Preserve upstream DB lifecycle/SQLite/server-limit/relay-auth fixes.
- Do not cherry-pick #50 wholesale because it overlaps chat/combo/resolution.
- Discovery/probes must remain off the request hot path.

## Required exact-head gate

Before merge to main:

1. `go vet ./...`
2. `go test ./...`
3. relevant Fabric/routing/DB/app race tests
4. normal CGO-free build
5. Android/Termux arm64 build
6. GitHub CI green on the exact head
7. correctly named Termux artifact
8. updater/version/build-identity/UnoRouter/OpenCode invariant audit

Authenticated-provider inference and physical-device validation are separate evidence and must not be claimed unless actually performed.
