# Ultramarine provider integration spike

Status: design only; no API calls or paid usage.

## Goals
- Add Paper Instruments Ultramarine as an opt-in paid provider.
- Preserve existing OpenAI Chat Completions and Anthropic Messages routing.
- Do not mark this provider as free or eligible for free-only automatic fallback.
- Keep API credentials in existing encrypted/secret configuration; never commit keys.

## Candidate API configuration (verify against live vendor documentation)
- Base URL: https://api.paperinstruments.com/v1
- Model: ultramarine-1-flash
- Auth: Bearer token
- Chat endpoint: /chat/completions

## Implementation discovery
1. Locate existing OpenAI-compatible provider adapter and registry conventions.
2. Confirm model identifier, context window, pricing and tool/stream support with official documentation.
3. Add provider metadata and capabilities using existing conventions, not a new routing path.
4. Ensure unsupported parameters are filtered or rejected explicitly.
5. Propagate 400/401/429 errors and honor retry/backoff.
6. Add unit tests with a local mock server, including streaming and secret redaction.
7. Run go test ./internal/handlers/chat/... and relevant provider tests.
8. Validate with a real API only after explicit spending authorization.

## Acceptance criteria
- No changes to default free routing.
- Existing API endpoints remain compatible.
- Zero secrets in logs.
- Provider disabled until configured.
- No deployment or merge without review.

## External code
Feather and Paper Office libraries are separate research items. Verify repository existence and licenses before reuse.
