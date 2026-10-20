# GROK-004 / OPS-01 — Updater trust chain (review-only)

**Draft implementation. Do not merge, deploy or enable auto-update yet.**

## Included code
- A missing/invalid SHA-256 **fails closed** before any network request or binary replacement.
- A valid digest is compared to the **extracted executable**, not just the archive.
- New Ed25519 **signed manifest verifier** validates trusted key IDs, a domain-separated signature, schema, channel, product, version, timestamps, HTTPS asset, expected size and SHA-256.
- Tests exercise checksum validity, corrupted downloads and signed manifest trust/rollback/expiry.
- Ed25519 verification is **not yet wired into CheckUpdate** and no public/private signing key is configured.

## Critical risk

A checksum distributed over the same compromised channel as its binary provides *integrity*, **not publisher authenticity**. Until a trusted signing key is embedded and the update source is verified, OPS-01 is only partially mitigated.

The SHA-256 fail-closed change may also **disable existing self-updates** from unsigned/sha-less releases. This is deliberate as a review-stage safety gate, but is a behavior change to evaluate before merging.

## Release pipeline and owner-controlled key decisions (required)

1. Generate Ed25519 signing key offline; store private key outside GitHub and off developer devices when possible. Never commit or print it.
2. Pin a trusted public key and stable keyId in source. Document emergency rotation and revocation.
3. Define `version.signed.json`: base64 canonical signed payload plus Ed25519 signature with context `capimux-update-manifest-v1\\n`.
4. Build release binaries; calculate checksums for the *actual extracted executables*; sign manifest offline; publish signed manifest and matching assets atomically.
5. Wire `CheckUpdate` to consume only the verified manifest; do **not** fall back to unsigned release API for installation.
6. Consider monotonic anti-rollback state, restore-on-health-check, expiration refresh cadence and Android/Termux update strategy.
7. Validate with real release rehearsal and `go test -race ./internal/updater` before marking ready.

## Non-goals

This PR does **not** generate real keys, run GitHub Actions releases, update live binaries, or modify production deployment.
