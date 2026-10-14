# ARGON-001 — Integration review (2026-10-14)

Contributor: Gemini Argon (proposed design and code). Reviewer/coordinator: ChatGPT. Status: **proposal only; not validated or merged**.

## Verified repository facts
- Go module: `9router/proxy` (not `capimux`).
- Dashboard: Svelte 5 / Vite in `web/`, not Vue.
- Product strategy: Fabric already targets discovery, availability, quotas, trust, routing, and scoring.
- Preserve existing OpenAI-compatible API routes and current provider dispatch.

## Required corrections before implementation
1. Map real Go packages, provider execution APIs, dashboard components, and authentication middleware before adding routes.
2. Reuse Fabric model discovery and health checks. Model listing is not proof of inference availability.
3. Pricing status must be `free`, `paid`, or `unknown`, supported by provider metadata; never infer zero cost from a provider name or local flag alone.
4. Remove simulated successful inference from production UI; clearly label any demo fixture.
5. Fix catalog discovery deadlock: producers can exceed a buffered channel of 128 while consumer starts only after WaitGroup completion.
6. Protect catalog refresh against SSRF (arbitrary provider URLs), bound response sizes, propagate cancellation, and handle nil response safely.
7. Validate selected models against authorized configured providers and enforce quotas, concurrency limits, request size, timeout, and paid-call policy.
8. Reuse Svelte dashboard, not CDN Vue/Tailwind.
9. Include genuine tests and CI logs. Argon's pasted test transcript was not independently verified and its catalog test was not supplied.
10. Persist provenance and attribution, with separate proposal/implementation/review/validation roles.

## Acceptance gates
- Existing endpoints and UI remain functional.
- 2–10 real distinct configured models can be selected without entering technical IDs.
- No paid requests without explicit authorization.
- A failed backend shows a real error, not fabricated responses.
- Race tests, backend tests, frontend build and CI pass.
- Real integration is reviewed in a separate PR before merge.

## Attribution
Gemini Argon: proposal, Go concurrency skeleton, Vue selector mockup, sample tests. ChatGPT: architecture inspection, gap analysis and review record. No verified integration, build, or production deployment claimed.
