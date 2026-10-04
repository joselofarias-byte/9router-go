# Agent Brief — codex-lb Strategy Mining

Reference: https://github.com/Soju06/codex-lb

## Goal

Extract general account-capacity selection algorithms that can work for any provider/account in Fabric.

Do **not** design around multiplying one vendor's subscription quota. The output must be provider-agnostic and policy-aware.

## Deliverable

Produce a design document and acceptance-test matrix for a generic account selector.

Inputs should use the shared capacity/availability model, not Codex-specific fields.

## Strategies to study

### capacity_weighted

Define:
- normalized remaining capacity;
- behavior when capacity is unknown;
- whether weighting is deterministic or probabilistic;
- interaction with cooldown/exhausted states.

### relative_availability

Study the power/top-k tuning model.

Answer:
- what problem it solves compared with simple highest-remaining;
- whether it avoids draining one account too early;
- stable deterministic alternative if randomness is undesirable.

### sequential_drain / fill-first

Study:
- ordering;
- when an account becomes ineligible;
- benefits for preserving other accounts;
- reset-window interaction.

Generalize as a policy, not a hard-coded provider behavior.

### reset_drain / reset-aware preference

Define:
- reset_at semantics;
- stale timestamps;
- no-reset/unknown behavior;
- avoiding false assumptions that elapsed reset means replenished.

### cooldown and circuit gates

Selection must first apply eligibility:
- active;
- provider/model compatible;
- not quarantined/disabled;
- not exhausted;
- not in cooldown when another eligible option exists;
- within budget/policy.

Only then apply strategy.

## Required generic interface proposal

Design around something equivalent to:

- account ID
- provider ID
- status: unknown / stale / available / exhausted
- remaining fraction when known
- reset_at when known
- cooldown_until
- scope/shared-quota identity
- recent success/latency/trust
- operator preference/preserve flag

Do not invent a remaining balance from a 429/cooldown.

## Explainability

Every selection must be able to report:
- selected account;
- strategy;
- eligible candidates;
- skipped candidates with reason;
- capacity evidence timestamp/source;
- tie-break rule.

## Constraints

- First contribution is documentation/tests design only.
- Do not modify #54 or #50.
- #50 remains the semantic reference for unknown/stale/exhausted/cooldown behavior.
- State exact codex-lb source commit(s) reviewed.
- Avoid account rotation behavior where provider policy forbids it; routing policy must be configurable by provider/account type.

## Acceptance tests to specify

At minimum:
- two available accounts with different remaining capacity;
- available vs unknown;
- stale vs available;
- exhausted with future reset;
- elapsed reset becomes stale, not replenished;
- active cooldown with alternative account;
- all accounts cooling down;
- shared quota scope;
- deterministic tie;
- provider policy disables automatic account switching;
- explain-route output matches the actual decision.

## Acceptance

Complete when the proposal can be implemented behind one generic selector interface without importing Codex-specific product assumptions.
