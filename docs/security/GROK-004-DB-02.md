# GROK-004 / DB-02 — SQLite credential encryption design

Status: **architecture draft + isolated encryption primitive**, not an active migration.
Do **not** merge or enable until key custody and compatible DB access paths are reviewed.

## Observed current state

GROK-003-R2 reproduces plaintext credential canaries in the live SQLite DB (even though encrypted `.9rbak` exports are implemented). Filesystem permissions are restrictive but do not encrypt file bytes. See `internal/db/client.go`, provider connection and API key repository methods.

## Threat model and scope

Protect tokens from passive copies of `9router.db`, WAL and snapshots. Does not protect against code execution in the running process, an unlocked user account with memory access, logging secrets, compromised provider tokens, or theft of the external master key together with the database.

**Inventory before coding write-path integration**:
- `providerConnections.data` JSON, including `apiKey`, `accessToken`, `refreshToken`, provider cookies and credentials.
- `apiKeys.key`, issued engine keys and any caches or metadata that retain full tokens.
- OAuth tokens, provider nodes, settings, legacy import paths, analytics/logs, backup restore, tests and exported formats. Locate exact secret-bearing columns with schema and repository read/write audit; do not assume the two named columns are exhaustive.

## Staged primitive (NOT integrated)

`internal/db/field_crypto.go` prototypes authenticated **AES-256-GCM** with random per-value nonces, a versioned envelope and associated data binding table / record ID / column. Wrong keys, wrong row identities, modified ciphertext and plaintext inputs all fail closed. It intentionally accepts **only a supplied, high-entropy 32-byte key**; no UI password, passphrase KDF, key file or cloud dependency is implicitly selected.

## Key custody decision (requires owner sign-off)

Preferred production contract: external key provider interface, with Android Keystore on the autonomous APK, a protected OS secret store or a file owned by the service (0600 in a 0700 directory) in Termux/OCI. Do not keep the master key in SQLite, committed files, downloadable artifacts, logs or exported plaintext backups.

Recovery must specify escrow, rotation, backup restore and how the operator gets access on a *new* device. Losing the sole master key permanently loses access to encrypted credentials. Key rotation should decrypt/re-encrypt transactionally and retain the old key until validation succeeds.

## Migration plan (explicitly NOT executed)

1. Preflight inventory of all secret-bearing fields, working dir ownership/modes, read/write/query sites, active tokens and plaintext exports. Take a verified, separately encrypted backup before any schema change. Do not expose real secret values in the audit report.
2. Generate or provision master key via selected owner-approved backend. Validate access and recovery on a second device/offline process.
3. Add schema version, encryption marker and necessary lookup indexes. `apiKeys.key` is currently looked up by equality; random-nonce ciphertext **cannot** be matched by plaintext SQL equality. Add a keyed HMAC-SHA256 lookup column with a *separate subkey*, retain encrypted full key only if necessary, and use constant-time comparison as appropriate.
4. Add encryption/decryption strictly at repository boundary for each secret-bearing column; update key lookup, matching, OAuth refresh, provider export, import, clones and backups. Reject ciphertext when key is unavailable. Never silently insert plaintext on an encryption failure.
5. Disable writers during migration. Transactionally copy/transform all rows and verify encrypted field decrypts with its associated context before committing. Use an idempotent recorded migration stage and durable journal; recover from a failure without mixing encrypted and plaintext rows.
6. Start with dry-run on synthetic DB and restored encrypted backup. Then confirm with explicit operator consent for real migration. Do not write a key or plaintext report into `~/storage/downloads`.
7. Review SQLite WAL/pages/snapshots: a SQL UPDATE alone **does not securely erase** older plaintext bytes. After verified migration and recovery planning, consider checkpoint and offline compaction, disposal of old backups, and key-safe storage encryption where possible.
8. Test fresh installs, encrypted/legacy restore, lost/wrong keys, rotation, app restart, multi-worker access, Termux / Android Keystore lifecycle and rollback. Roll back using a deliberately retained *encrypted* backup, not silent plaintext writes.

## Release gates

- [ ] Approved threat model and complete secrets inventory
- [ ] Approved key provider and tested recovery
- [ ] Encrypted write and read paths for every secret-bearing column
- [ ] HMAC lookup of active API keys and backward-compatibility transition
- [ ] Full backup / restore / migration tests with canary bytes absent from raw SQLite and WAL
- [ ] No automatic migration before explicit operator approval
- [ ] Go tests and race checks green in CI; Android/Termux and OCI validation
- [ ] User security review and approved production rollout

**Do not mistake the standalone crypto primitive for fixing DB-02.** Actual SQLite rows remain plaintext on `main` and in this draft branch until integration and migration are completed.
