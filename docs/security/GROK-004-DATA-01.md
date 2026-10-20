# GROK-004 / DATA-01 — Privacy safeguards (draft)

Status: **review-only**. No deployment or migration has been run.

## Scope

* GET `/api/usage/request-details` is now mounted only inside `RequireAdminAuth()`; an ordinary active inference API key no longer grants access. Valid admin cookie or local CLI token is required, **including when `requireLogin=false`**.
* Diagnostic prompt and response *content* is **not persisted by default**. Metrics used for cost/tokens continue to be computed.
* `CAPIMUX_CAPTURE_REQUEST_CONTENT=true` explicitly re-enables the legacy detailed capture. Use only with informed consent. Current capture is bounded but is **not a guaranteed secrets redactor**; avoid enabling it for shared/multi-tenant workloads.
* `CAPIMUX_REQUEST_DETAILS_RETENTION_DAYS`: 1–365, default 7. Missing, malformed or out-of-range values fall back to 7 days.
* Expired `requestDetails` are pruned after roughly every 128 inserts (including first insert), and when viewing details.
* `DELETE /api/usage/request-details` (admin only) purges all diagnostic records. It requires `X-Capimux-Confirm: purge-request-details`; aggregate usage metrics are retained. No automatic purge is triggered by opening this PR.

## Important limitations

* **Existing history remains until pruning or an explicit purge.** The default content-capture change is not retroactive.
* SQLite `DELETE` is a *logical removal*, not secure wiping. WAL, old snapshots, filesystem copies, and backup media may still contain data; follow a separate retention/compaction and recovery policy.
* Admin cookies and CLI tokens still reveal permitted diagnostic content; protect them like administrative secrets.
* Endpoint protection only covers `requestDetails`. Additional API-key/dashboard routes still need separate privacy review.
* Runtime Go tests and GitHub CI must pass before this can merge. This PR must remain draft until validated.

## Validation checklist

- [ ] `go test ./internal/handlers ./internal/handlers/chat ./internal/db`
- [ ] `go test -race ./internal/handlers ./internal/handlers/chat ./internal/db`
- [ ] Confirm active inference key gets 401 on GET and DELETE.
- [ ] Confirm authenticated admin session gets 200 on GET.
- [ ] Confirm DELETE without confirmation header gets 428 and with header succeeds.
- [ ] Confirm default diagnostic records contain no prompt/response canaries.
- [ ] Confirm retention purges rows older than configured cutoff.
- [ ] Verify frontend details UI behavior and Android/Termux packaging.
