# Codex plugin code/security review — 9router-go

## Purpose

Use OpenAI's official `openai/codex-plugin-cc` from inside Claude Code as an independent code reviewer for 9router-go.

This is complementary to the existing Claude/Antigravity and Qwen security passes. It is not a replacement for either one.

## What the official plugin adds

The plugin exposes:

- `/codex:review` — native, read-only Codex code review against the working tree or a base branch.
- `/codex:adversarial-review` — steerable challenge review for design assumptions, tradeoffs and failure modes.
- `/codex:rescue` — delegate an investigation or repair task to Codex after audit/triage.
- `/codex:status`, `/codex:result`, `/codex:cancel` — manage long-running background work.
- `/codex:transfer` — hand a Claude Code session into a persistent Codex thread.
- optional review gate — Codex can review when Claude tries to stop and block completion if it finds issues.

The plugin uses the local Codex CLI/app-server and the same local authentication/configuration. It does not create a separate Codex runtime.

The plugin also bundles internal reusable skills for Codex CLI runtime handling, result handling and GPT-5.4 prompting. Those are implementation support for the plugin; the main user-facing review surfaces are the slash commands above.

## One-time installation

Inside Claude Code:

```text
/plugin marketplace add openai/codex-plugin-cc
/plugin install codex@openai-codex
/reload-plugins
/codex:setup
```

Requirements from the official plugin:

- Node.js 18.18 or newer.
- local Codex CLI availability;
- authenticated ChatGPT/Codex account or API key.

If Codex is missing, `/codex:setup` can offer to install `@openai/codex` through npm.

## Recommended 9router-go passes

### Pass 1 — native review

```text
/codex:review --background --base main
```

This pass intentionally takes no custom focus text. Preserve that independence.

### Pass 2 — adversarial review

```text
/codex:adversarial-review --background --base main challenge auth and license boundaries, fail-open behavior, account and quota state races, stale snapshots, provider credential isolation, fallback correctness, rollback/downgrade paths, concurrency, persistence consistency, and assumptions that could let a locally controlled client bypass server-side enforcement
```

This is the pass for challenging architectural assumptions and hidden failure modes.

### Retrieve results

```text
/codex:status
/codex:result
```

## Do not enable the review gate by default

The plugin can enable a stop-time review gate, but its own documentation warns that it can create a long Claude/Codex loop and consume usage quickly.

Use it only for explicitly selected high-risk work, and disable it afterward.

## Evidence policy

Codex findings enter the same evidence pipeline as every other model:

- no finding accepted by authority;
- no majority voting;
- require file/symbol and concrete evidence;
- verify P0/P1 against current branch/SHA;
- reproduce or add a focused test before remediation when practical;
- distinguish main bugs from integration-branch-only behavior.

## Relationship to Qwen abliterated

These are different roles:

- Codex native review: code-centric independent reviewer.
- Codex adversarial review: challenges design decisions and assumptions.
- Qwen abliterated: separate red-team perspective with reduced refusal behavior.
- Qwen normal / Claude: architecture and independent verification.

The value comes from disagreement and different failure modes, not from asking four models the same prompt.
