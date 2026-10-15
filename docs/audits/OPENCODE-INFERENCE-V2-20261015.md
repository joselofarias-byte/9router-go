# Capimux — OpenCode inference v2 compatibility audit (2026-10-15)

## Evidence
External reference: https://github.com/WhoisNeon/9router/commit/01c491dce8af58837b9f37d6e78c5c0a3239631a

The external JS adapter reports OpenCode's free endpoint moving from `/zen/v1` to `/inference/v1` on 2026-10-14. It also reports a different client version format, session headers and exact tool definitions. These are claims from an external implementation, **not verified against Capimux's Go adapter**.

## Implementation checklist (do not blindly cherry-pick JS)
- [ ] Locate the Go OpenCode provider executor and all free-tier URL builders.
- [ ] Determine whether Capimux currently calls `/zen/v1`, `/inference/v1` or a different authorized endpoint.
- [ ] Check official provider documentation and supported client behavior before changing headers or tool schemas.
- [ ] Add a versioned endpoint selection strategy; preserve backward compatibility where supported.
- [ ] Classify HTTP 403 separately from 401, 429 and 5xx. Do not interpret every 403 as a quota failure.
- [ ] Respect provider access controls; do not forge client identity or spoof tool fingerprints to bypass restrictions.
- [ ] Ensure a failed provider is isolated and other independently authorized routes can still be selected.
- [ ] Add redacted structured logs with provider, model, endpoint family, status and failure class; never log API keys.
- [ ] Add Go table-driven tests for endpoint routing, error classification, fallback eligibility, retry boundaries and SSE streaming.
- [ ] Run go test ./... and integration checks with authorized credentials only.

## Acceptance criteria
1. Existing OpenCode routes and other providers remain compatible.
2. Unsupported/forbidden upstream access yields a clear actionable error rather than a false success.
3. Fallback only uses independently configured authorized providers.
4. No changes are merged without passing tests and an actual supported endpoint verification.

## Attribution
External research and patch: WhoisNeon/9router commit 01c491d.
Capimux analysis and audit checklist: ChatGPT, 2026-10-15.
Implementation and validation: pending.
