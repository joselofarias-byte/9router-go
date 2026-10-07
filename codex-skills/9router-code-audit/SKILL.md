---
name: 9router-code-audit
description: Perform a read-only, evidence-first code and security audit of 9router-go or 9router-license-test. Use for repository reviews, security audits, licensing/entitlement review, auth boundaries, routing/fallback, quota/cooldown state, concurrency, persistence, provider isolation, Termux/ARM64 portability, and review of a branch, commit, working tree, or the full repository.
---

# 9Router Code Audit

Work read-only. Do not modify files, create commits, push branches, rotate credentials, publish releases, or apply fixes.

## Establish the target

1. Read `AGENTS.md` and `CLAUDE.md` when present.
2. Record the current branch and SHA.
3. Determine whether the request targets the full repository, uncommitted changes, a branch against a base, a specific commit, or a focused subsystem.
4. If reviewing a branch, resolve the merge base and inspect the diff that would actually merge.
5. If auditing the full repository, inspect architecture and call paths before reporting findings.

## Audit priorities

Prioritize concrete defects over style.

Inspect especially:

- authentication and authorization boundaries;
- license and entitlement enforcement;
- client-controlled versus server-controlled state;
- fail-open paths and unsafe fallback;
- provider/model/account selection;
- quota, cooldown, reset, stale-state and shared-scope handling;
- concurrency, TOCTOU and copy-on-write snapshots;
- persistence, SQLite, migrations and generation/sync state;
- cross-provider secret/header leakage;
- downgrade, rollback and update paths;
- streaming/SSE lifecycle and error propagation;
- trust/health/capacity feedback wiring;
- deterministic routing and explainability;
- Termux/Android ARM64 and multiplatform behavior;
- tests that appear to pass without exercising the real contract.

For `9router-license-test`, additionally pressure-test whether a locally controlled client can bypass licensing, device binding, expiry, server-side policy, signature verification, revocation, downgrade protection or fail-closed behavior.

## Evidence standard

Do not accept a concern merely because it is plausible.

For each material finding:

1. Trace the affected call path.
2. Read enough surrounding code to establish preconditions.
3. Check relevant tests and call sites.
4. Distinguish confirmed defect, probable-needs-test, design concern, or false positive.
5. Cite the smallest useful `path:line` range.
6. State how to reproduce or prove the issue.
7. Suggest the smallest safe fix, but do not implement it.

Do not expose tokens, cookies, databases, API keys, device secrets or real credentials in the report.

## 9router proof targets

When applicable, explicitly prove or refute:

1. Whether Control Plane can rank a route while every concrete account is unusable due to quota, cooldown or model locks.
2. Whether real request outcomes feed the trust/scoring state used by routing.
3. Whether preserved accounts are explained consistently as eligible versus selectable.
4. Whether random tie-breaking conflicts with sticky/deterministic routing or reproducible explainability.
5. Whether Fabric/integration manifests match the code actually wired at the audited SHA.
6. Whether authenticated routes can accidentally fall through to anonymous, paid, wrong-provider or wrong-entitlement paths.
7. Whether license/entitlement checks rely on mutable client-local state that should be enforced server-side.

## Output

Report findings first, ordered by severity.

Use:

```text
ID:
Severity: P0 | P1 | P2 | P3
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

After findings, include exact refs audited, a short architecture/call-path map, hypotheses disproved, material test gaps, recommended remediation order, and residual risks.

If no qualifying finding exists, say so explicitly. Do not invent findings to fill the report.
