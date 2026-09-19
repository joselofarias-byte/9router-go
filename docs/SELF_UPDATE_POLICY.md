# Fork self-update policy

This fork carries a validated Fabric control-plane delta on top of upstream `luqman-v1/9router-go`. A fork binary must therefore never silently replace itself with an upstream-only release.

## Defaults

- `DefaultUpdateURL` points to this fork's `version.json`.
- `DefaultGitHubRepo` points to `joselofarias-byte/9router-go`.
- The fork manifest intentionally has no default binary download URL until this fork publishes its own release assets.
- `AUTO_UPDATE` remains off by default.

This is fail-closed behavior: an update can be detected without allowing an untrusted or wrong-channel binary to replace the running Fabric build.

## Explicit overrides

Operators may deliberately choose another trusted channel with:

- `UPDATE_URL`
- `UPDATE_REPO`

Those overrides are explicit operator decisions and are not applied automatically.

## Termux deployment

For Termux-native Android arm64, use the CI-validated `9router-go-termux-arm64-*` artifact produced from successful pushes to this fork's `main` branch until first-party fork release assets are available.
