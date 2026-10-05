# Generic account selection strategies

This package is the provider-agnostic execution slice extracted from the codex-lb study.

It does **not** know about Codex, ChatGPT, Gemini, Antigravity, or any vendor-specific subscription semantics. A caller maps provider/account observations into `accountstrategy.Candidate`, calls `Select`, and receives an explainable decision.

Implemented strategies:

- `capacity_weighted`: deterministic highest-capacity selection when no hash key is supplied; weighted rendezvous selection when a request/sticky key is supplied.
- `relative_availability`: ranks fresh known capacity relative to the best candidate, applies configurable power/top-K, then uses the same stable weighted selection.
- `sequential_drain`: drains the smallest fresh positive capacity first, preserving larger pools.
- `reset_drain`: prefers the nearest **future** reset among fresh positive-capacity accounts. An elapsed reset never proves replenishment.
- `expiry_pressure`: prefers the largest amount of known capacity at risk of expiring, using `remaining / hours_until_reset`. This avoids wasting a nearly-full window just because another smaller window resets a few minutes earlier.
- `fill_first`: stable operator priority, then account ID.
- `single_account`: explicit account only.

Eligibility is evaluated before strategy:

- disabled => ineligible;
- exhausted => ineligible;
- active cooldown => ineligible;
- unknown/stale => eligible fallback, never treated as unlimited;
- preserved accounts are avoided while any non-preserved eligible account exists;
- automatic switching can be disabled per provider/account type.

## Integration with Phase 1 (#50)

After the capacity/availability bridge is ported onto the final #54 integration baseline:

- `capacity.Available` + fresh percentage -> `StatusAvailable`, `HasRemaining=true`;
- `capacity.Unknown` -> `StatusUnknown`;
- `capacity.Stale` -> `StatusStale`;
- `capacity.Exhausted` -> `StatusExhausted`;
- `BlockedUntil` -> `CooldownUntil`;
- `ResetAt` is copied as evidence only; selection never turns elapsed reset into fresh capacity.
- For `expiry_pressure`, callers should map the most urgent applicable provider window into `Remaining` + `ResetAt` (for example, the maximum pressure across model/session/weekly windows) and mark stale observations as `StatusStale`.

The first runtime wiring should live behind one account-selector interface and expose the chosen strategy plus skip reasons through explain-route.

## Why weighted rendezvous

Weighted rendezvous hashing gives two useful properties:

1. requests can distribute according to known remaining capacity;
2. the same request/session key maps stably to the same account while the eligible set is unchanged.

This gives us sticky behavior without a separate sticky-session subsystem.

## Policy boundary

Automatic account switching must be configurable. If `AutoSwitch=false`, only the explicitly preferred account may be selected. If that account is exhausted or cooling down, selection returns no account rather than silently rotating.

That policy boundary lets Fabric support different provider terms without baking one vendor's behavior into the router.
