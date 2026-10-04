# Auditable cost evidence

The existing pricing layer intentionally preserves upstream behavior: an unpriced model produces an estimated cost of 0.

That behavior is useful for parity, but a product dashboard must not confuse:

- **known free** — pricing exists and the calculated cost is exactly 0;
- **unknown price** — no pricing evidence exists.

`EstimateCostEvidence` adds that distinction without changing the existing `EstimateCost` contract.

`CompareCostToBaseline` also requires both the actual route and the caller-selected baseline to have known pricing before it produces a monetary delta.

A positive `DeltaUSD` means the selected route was cheaper than the explicit baseline. A negative value means it was more expensive.

No baseline is inferred automatically. No unpriced model is silently treated as free.

This is the minimum accounting primitive needed before the product can honestly show metrics such as “estimated cost avoided”.
