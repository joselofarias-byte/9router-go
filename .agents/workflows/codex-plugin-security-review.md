---
name: codex-plugin-security-review
description: Independent read-only Codex review plus adversarial challenge review using the official OpenAI Codex plugin for Claude Code
---

# Workflow: Codex plugin security review

Use the official `openai/codex-plugin-cc` plugin. This workflow is review-only.

Do not modify runtime code, merge PRs, publish releases, rotate credentials, or enable the review gate automatically.

## 0. Read project rules

Read:

- `AGENTS.md`
- `CLAUDE.md`
- `docs/agent-briefs/CODE_AUDIT_SWARM_2026-10-04.es.md`
- `docs/agent-briefs/CODEX_PLUGIN_SECURITY_REVIEW.md`

When Graphify data is present, use it before broad raw searches. Follow the repository RTK rules for shell work.

## 1. One-time plugin setup

Inside Claude Code:

```text
/plugin marketplace add openai/codex-plugin-cc
/plugin install codex@openai-codex
/reload-plugins
/codex:setup
```

If setup reports that Codex is installed but unauthenticated, use `!codex login`.

Do not enable the review gate by default. It can create long Claude/Codex loops and consume quota rapidly.

## 2. Native Codex review

For a branch relative to main:

```text
/codex:review --background --base main
```

For uncommitted work, use:

```text
/codex:review --background
```

This pass is intentionally not steerable. Treat it as an independent native reviewer, not as confirmation of another model's claims.

Check progress/result with:

```text
/codex:status
/codex:result
```

## 3. Adversarial Codex review

Run a second, steerable challenge review:

```text
/codex:adversarial-review --background --base main challenge auth and license boundaries, fail-open behavior, account and quota state races, stale snapshots, provider credential isolation, fallback correctness, rollback/downgrade paths, concurrency, persistence consistency, and assumptions that could let a locally controlled client bypass server-side enforcement
```

The adversarial pass must question architecture and assumptions, not merely restate lint/style issues.

Then:

```text
/codex:status
/codex:result
```

## 4. Required proof targets

Cross-check the same proof targets defined in the swarm brief:

1. Control Plane ranking versus concrete account capacity/cooldown/model-lock state.
2. Real request outcome feedback into trust/scoring.
3. Preserve explainability in account selection.
4. Random tie-breaking versus deterministic/sticky routing.
5. Fabric integration completeness.
6. Auth and fail-closed boundaries.
7. License/entitlement enforcement and client-vs-server trust boundaries.
8. Concurrency, stale snapshots and TOCTOU around account/quota state.
9. Cross-provider secret/header isolation.
10. Fallback paths that could cross policy, paid/free or entitlement boundaries.

A finding is not accepted because Codex says so. Require code evidence, test/reproduction, or a precise technical argument.

## 5. Finding format

Normalize material findings to:

```text
ID:
Severity: P0 | P1 | P2
State: confirmed | probable-needs-test | design | false-positive
Reviewer: codex-native | codex-adversarial | both
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

## 6. Merge with the multi-model audit

Do not vote by majority.

Compare Codex findings against:

- Claude/Antigravity deep audit;
- Qwen baseline;
- Qwen abliterated red-team;
- focused tests and reproductions.

A disagreement is a trigger for evidence gathering, not a reason to average opinions.

Keep Codex as an independent reviewer. Do not feed another model's conclusions into the native `/codex:review` pass before it runs.

## 7. Review gate policy

The official plugin supports:

```text
/codex:setup --enable-review-gate
```

Do not enable this globally or by default for 9router-go.

Only enable it temporarily when the operator explicitly wants Codex to block Claude from finishing until a targeted review passes. Disable it afterward:

```text
/codex:setup --disable-review-gate
```

Reason: unattended stop-gate loops can burn both Claude and Codex usage without adding proportional evidence.
