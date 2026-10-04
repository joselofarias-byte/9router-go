# Agent Brief — GoModel Mining

Reference: https://github.com/ENTERPILOT/GoModel

## Goal

Find mature, reusable patterns that improve Fabric without turning this fork into another generic gateway.

## Deliverable

Create a gap matrix with four columns:

1. GoModel capability
2. Current 9router-go/Fabric equivalent
3. Gap or overlap
4. Recommendation: adopt / adapt / skip

Then propose only the smallest implementation slices that close verified gaps.

## Priority topics

### 1. Circuit breaker

Inspect:
- open/half-open/closed transitions;
- failure thresholds;
- cooldown/reset behavior;
- per-provider vs per-model vs per-target scope;
- interaction with retries/failover;
- observability.

Map this against our trust/quarantine/cooldown model. Avoid a second competing health state machine if one can be extended.

### 2. Budgets

Inspect:
- global/user/key/model budget scope;
- accounting source;
- hard vs soft limits;
- time windows;
- fail-closed/fail-open behavior.

Map this to future Pro/Business controls. Do not wire billing logic until cost accounting is auditable.

### 3. Virtual models

Inspect:
- alias/redirect model structure;
- balanced vs failover behavior;
- how targets are exposed in UI;
- validation rules.

Compare with Fabric virtual profiles such as free-best, coding-best-free, fast-free, reasoning-free, long-context-free and local.

### 4. Cost tracking

Inspect:
- pricing sources;
- token accounting;
- unknown price behavior;
- aggregation dimensions;
- dashboard/API presentation.

Output a recommendation for separating:
- known actual API cost;
- configured baseline;
- estimated avoided cost.

Unknown price must never silently become zero.

### 5. Sticky sessions

Inspect the problem it solves and the key used for affinity. Recommend only if it preserves tool/agent coherence without undermining health/quota routing.

### 6. Failover and observability

Inspect:
- retry boundaries;
- streaming-commit safety;
- explainability;
- logs/metrics.

Compare with our existing combo/fallback path before proposing changes.

## Constraints

- MIT ideas/code may be adapted with preserved attribution where required.
- Do not replace the current 9router-go data plane.
- Do not add a second provider catalog.
- Do not touch active #54/#50 runtime lanes.
- First contribution is documentation/design only.
- State exact source commit(s) reviewed.

## Acceptance

The brief is complete when it identifies:
- at least three capabilities we should **not** rebuild;
- at least two verified gaps worth implementing;
- the exact Fabric packages those gaps would extend;
- tests/invariants required before code starts.
