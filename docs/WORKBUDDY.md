# WorkBuddy / CodeBuddy International

9router-go already contains a native CodeBuddy executor and an international provider named `codebuddy-intl`. WorkBuddy is therefore integrated through that provider rather than through browser scraping or a separate reverse proxy.

## Authentication

For an individual international account, create a personal API key at:

https://www.codebuddy.ai/profile/keys

The official CLI uses `CODEBUDDY_API_KEY`. Do not commit the key, paste it into issue/PR text, or store it in shell history.

The upstream chat provider is:

- provider: `codebuddy-intl`
- endpoint: `https://www.codebuddy.ai/v2/chat/completions`
- stream transport: required by CodeBuddy; 9router aggregates it automatically for non-stream clients
- auth: Bearer plus `X-API-Key` compatibility header
- aliases: `workbuddy`, `wb`, `cbai`

Because personal API-key authentication is an officially documented path for individual developers, Fabric classifies `codebuddy-intl` as a normal low-risk API-key route. It is not treated like browser/session automation. Normal quota and health controls still apply.

## Termux bootstrap

From a checkout of this repository:

```bash
bash scripts/workbuddy-termux-bootstrap.sh
```

The script:

1. installs missing Termux dependencies (`curl`, `jq`, `nodejs`) only when needed;
2. installs the official CodeBuddy CLI package if it is not already present;
3. opens the official international API-key page when `termux-open-url` is available;
4. reads the API key without echoing it;
5. starts `9router-go` on demand when it is installed but not already running;
6. imports the credential through the existing `/api/oauth/codebuddy-intl/import` endpoint;
7. probes `codebuddy-intl/gpt-5.6-luna` through the normal 9router chat path;
8. reports returned token/credit usage when exposed by the upstream response;
9. writes a credential-free probe snapshot to `~/.config/9router-go/workbuddy-last-probe.json` for later Fabric analysis.

The default local router URL is `http://127.0.0.1:20128`. The bootstrap first tries `NINEROUTER_API_KEY`; if it is unset, it uses `~/.config/9router-go/admin-token` when present.

To probe another model without editing the script:

```bash
WORKBUDDY_PROBE_MODEL=glm-5.3 bash scripts/workbuddy-termux-bootstrap.sh
```

## Credit telemetry

CodeBuddy publishes credit consumption as part of usage telemetry in current clients. The 9router CodeBuddy SSE aggregator preserves the complete upstream `usage` object, including a `credit` field when supplied. The bootstrap therefore prints exact per-probe credit consumption when the chat endpoint exposes it, alongside token counts.

When the chat response does not expose a credit value, the authoritative account balance and consumption history remains CodeBuddy Profile -> Usage. The bootstrap records only non-secret probe metadata locally; it never writes the entered personal API key into the audit file.

Current promotional WorkBuddy Free terms are quota-limited and can change. Treat plan limits and daily rewards as dynamic account metadata, not hard-coded router capacity.

## Official CLI

The upstream CLI requires Node.js 18+ and can be installed with:

```bash
npm install -g @tencent-ai/codebuddy-code
```

A simple headless request is:

```bash
CODEBUDDY_API_KEY='YOUR_KEY' codebuddy --model 'GPT-5.6-Luna' -p --output-format json 'Reply with exactly: WB_OK'
```

Do not add `-y` merely for a text-only probe. The flag is intended for non-interactive operations that need tool permissions such as file writes, shell commands, or network tools.

CodeBuddy also exposes a beta local HTTP service:

```bash
codebuddy --serve --port 8080
```

Keep its default authentication enabled. The native `codebuddy-intl` route in 9router is preferred for ordinary model traffic because it avoids an extra local proxy process.

## Model availability

Do not hard-code the WorkBuddy web selector as a permanent catalog. Model availability and account entitlements can change. Use the models actually authorized for the credential and treat newly advertised models (for example a staged rollout) as unavailable until the account exposes them and a probe succeeds.

As of the initial integration work on 2026-09-10, the account used for validation exposed GPT-5.6 Sol/Terra/Luna, GPT-5.5, GPT-5.4, GPT-5.3-Codex, Gemini-3.5-Flash, GLM-5.3, GLM-5.2, Kimi-K3, Kimi-K2.6, Hy3 and Hy4 Preview. GPT-6 Astra was not exposed by that account at that time. This is an observed snapshot, not a provider guarantee.
