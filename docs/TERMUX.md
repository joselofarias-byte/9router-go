# Termux ARM64

This fork publishes a CI-validated Android/ARM64 binary. It is a Termux executable, not an APK.

## Two build modes

| Target | Command | DNS behavior |
|--------|---------|----------------|
| CI default | `make build-termux` | `CGO_ENABLED=0 GOOS=android GOARCH=arm64`. Validated in GitHub Actions. Can listen on localhost and still fail provider DNS on some devices. |
| Bionic / recommended on device | `make build-termux-cgo` or `scripts/build-termux.sh` | Uses Android NDK clang + `netcgo` so Go calls Bionic `getaddrinfo`. Requires `ANDROID_NDK_HOME` or `ANDROID_NDK_ROOT`. |

Do not force a public DNS server or change the user's network configuration.

## Network check

`9router-netcheck` resolves and TLS-dials a few provider hosts without sending credentials, HTTP bodies, or prompts:

```sh
make build-netcheck
# on the phone:
./9router-netcheck-android-arm64
```

A successful compile does not prove on-device connectivity. Run the check on the phone before replacing a working gateway.

## Preserve the account database

Keep `DATA_DIR` (default `~/.9router`) and the existing SQLite file. This package does not migrate a PRoot install automatically. Stop the old process and back up the database before replacing the binary.

## Grok Responses

The grok-cli executor normalizes empty, root, and `/v1` bases to `/v1/responses` and sends `X-XAI-Token-Auth` plus `x-grok-model-override`. Custom full URLs are left unchanged.

## WorkBuddy on Termux

See `docs/WORKBUDDY.md` and `scripts/workbuddy-termux-bootstrap.sh` for the international API-key path. The authenticated CodeBuddy CLI session path (`workbuddy-session` / `wbf`) still requires a real browser login on the device.
