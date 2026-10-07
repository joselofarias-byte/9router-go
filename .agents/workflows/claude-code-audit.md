---
name: claude-code-audit
description: Deep evidence-driven 9router-go audit using Claude in Antigravity before weekly included quota resets
---

# Workflow: Claude deep code audit

Audit only. Do not change runtime code, merge PRs, publish releases, or expose credentials.

## 0. Read project rules first

Read:

- `AGENTS.md`
- `CLAUDE.md`
- `docs/agent-briefs/CODE_AUDIT_SWARM_2026-10-04.es.md`
- `docs/FABRIC_PORT_MANIFEST.md` when present on the integration branch

When `graphify-out/graph.json` exists, query Graphify before broad raw searches.

For shell commands, follow the repository rule and prefix them with `rtk`.

## 1. Use the strong-model quota deliberately

This workflow is intended for **Claude via Antigravity**.

Use the highest-capability Claude model available in the Antigravity account and high reasoning when selectable. Do not downgrade to a free/local model merely to conserve weekly included quota.

Free/local models are for later support or cross-checking, not a prerequisite for this deep pass.

## 2. Refresh the GitHub state before judging code

The kickoff snapshot was:

- main: `3caa2a55cf7458a4182859c79cbc363b4007bf7c`
- PR #54: `f49dc983c5abd1e24147f39107af86a69802f34c`
- PR #57: `3218580b1ce9b5ae3f1c59906fb3b6317b16c02a`
- PR #62: `7d28a66a72b157148cc0a5939b57de3d543cfc34`
- PR #63: `5f83c39757907de7bd92319be181fbdbdc78c932`

Refresh all of them. Record the actual SHA used in the report.

Suggested commands:

```bash
rtk git status
rtk git fetch --all --prune
rtk gh pr view 54
rtk gh pr view 57
rtk gh pr view 62
rtk gh pr view 63
```

Treat branch relationships correctly:

- #54 is the upstream/Fabric integration lane.
- #57 is account-selection work layered after the capacity/integration baseline.
- #62 is a child of #57, not an independent main-targeted feature.
- #63 is the authenticated OpenCode Zen fail-closed correction.

Do not rewrite or switch a dirty working tree destructively. Use read-only refs, diffs, or worktrees as appropriate.

## 3. Architecture map before findings

Build a concise execution map from incoming model name to upstream request:

1. request/model resolution;
2. Fabric candidate discovery/eligibility;
3. scoring/ranking;
4. provider/model resolution;
5. concrete account selection;
6. quota/cooldown/model-lock checks;
7. fallback/retry;
8. route outcome/usage feedback;
9. trust/health/capacity update.

For each step identify the authoritative state and package. Flag duplicated authorities.

## 4. Required proof targets

Prove or refute each target. A hypothesis is not a finding until evidence supports it.

### A. Control Plane vs runtime capacity

Inspect at minimum:

- `internal/controlplane/routing/policy.go`
- `internal/controlplane/scoring/engine.go`
- `internal/controlplane/sync/accounts.go`
- `internal/handlers/chat/combo_bridge.go`
- `internal/handlers/chat/resolution.go`
- `internal/handlers/chat/connections.go`
- Codex and Antigravity quota/cooldown code
- relevant #54/#57/#62 changes

Determine whether Control Plane can rank/select a provider/model candidate while every concrete account for that route is currently unusable because of quota exhaustion, cooldown or model locks.

If yes, demonstrate the smallest deterministic test case and identify whether the consequence is:
- a harmless extra fallback hop,
- incorrect explainability,
- avoidable latency,
- repeated upstream failure,
- or wrong routing.

### B. Trust feedback actually wired

Find every production call to `RecordObservation` and every mutation of trust state.

Determine whether real request outcomes feed the same `globalTrustManager` used by routing. If not, explain which scoring dimensions remain effectively static and what #54 must wire.

Do not infer absence from a narrow grep; search repo-wide.

### C. Preserve explainability in #57/#62

Inspect `internal/controlplane/accountstrategy/selector.go` at the current #62 head.

Test whether a `Preserve=true` candidate is reported `Eligible=true` even when `preferNonPreserved` removes it from the actual selection set.

If confirmed, propose a minimal explanation contract such as:
- `Eligible` vs `Selectable`, or
- reason `preserved_held_back`.

Do not change code in this audit.

### D. Random model tie-breaking vs stable account routing

Inspect the pre-shuffle in `routing.Engine.SelectCandidates`.

Determine whether nondeterministic tie order conflicts with:
- explain-route reproducibility,
- sticky/session behavior,
- deterministic weighted rendezvous in #62,
- or tests/diagnostics.

Classify this as a bug only if an observable contract is violated.

### E. #54 integration completeness

Using the exact current head of #54, verify every item in `docs/FABRIC_PORT_MANIFEST.md`.

Explicitly state which `internal/controlplane/*` packages and runtime hooks are:
- present and wired,
- present but unwired,
- still absent/deferred.

Do not evaluate only `main` and call the integration ready.

### F. Auth/fail-closed boundaries

Audit #63 and adjacent provider/executor code for:
- empty/public pseudo credentials,
- cross-provider credential leakage,
- header injection,
- anonymous route accidentally reaching authenticated upstream,
- fallback from a restricted/free route into paid or unauthorized routes.

Preserve legitimate NoAuth providers such as local endpoints; do not generalize OpenCode's fix to every provider.

## 5. Broader audit lanes

After the required proof targets, inspect:

- concurrency and copy-on-write snapshots;
- DB generation sync and stale-state behavior;
- nil/error handling and goroutine/connection leaks;
- provider alias/canonical-id confusion;
- model capability/format translation;
- streaming/SSE completion behavior;
- entitlement expiry and Community/Pro fail-safe boundaries;
- updater and release identity;
- Termux/Android arm64 portability;
- tests that pass while exercising mocks but not the intended real contract.

Prioritize correctness and security over style.

## 6. Validation

Run only read-only tests/builds needed to validate findings. Never use real credentials or paid inference unless the operator explicitly authorizes that separate action.

Prefer focused tests first, then the relevant repository gate if practical.

Use `rtk` wrappers.

## 7. Finding format

For every finding:

```text
ID:
Severity: P0 | P1 | P2
State: confirmed | probable-needs-test | design | false-positive
Branch/SHA:
File/symbol:
What can happen:
Why:
Evidence:
How to reproduce or prove:
Missing test:
Minimal fix:
Confidence:
```

Rules:

- No generic style findings.
- No severity inflation.
- Distinguish existing-main bugs from integration-only issues.
- Distinguish #57 parent behavior from #62 child changes.
- If a hypothesis is false, say so and show why.

## 8. Final report

End with:

1. exact refs audited;
2. architecture map;
3. P0 findings;
4. P1 findings;
5. P2 findings;
6. hypotheses disproved;
7. merge blockers for #54, #57/#62 and #63 separately;
8. tests that should be added;
9. recommended order of fixes;
10. what should receive a second independent model review.

Do not implement fixes until explicitly instructed.

## 9. Independent Codex follow-up

After the Claude deep pass, use `.agents/workflows/codex-plugin-security-review.md` for the independent Codex native + adversarial reviews. Keep the native Codex pass uncontaminated by Claude conclusions; compare evidence only after Codex finishes.
