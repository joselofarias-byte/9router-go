# WorkBuddy / CodeBuddy International

9router-go supports two deliberately separate CodeBuddy/WorkBuddy paths:

- `codebuddy-intl`: direct international chat endpoint authenticated with a personal API key.
- `workbuddy-session`: local execution through the official CodeBuddy CLI using an already authenticated browser-login session. This path is useful when an account can use CodeBuddy but cannot create an API key, including the Free account used for validation on 2026-09-10.

Do not merge the credential semantics of these two providers. An API key is an ordinary API credential; a saved CLI login is a product/account session and receives more conservative routing treatment.

## API-key route (`codebuddy-intl`)

For accounts that can create a personal API key, use:

https://www.codebuddy.ai/profile/keys

The official CLI also accepts `CODEBUDDY_API_KEY`. Do not commit a key, paste it into issue/PR text, or store it in shell history.

The direct upstream chat provider is:

- provider: `codebuddy-intl`
- endpoint: `https://www.codebuddy.ai/v2/chat/completions`
- stream transport: required by CodeBuddy; 9router aggregates it automatically for non-stream clients
- auth: Bearer plus `X-API-Key` compatibility header
- aliases: `workbuddy`, `wb`, `cbai`

Because personal API-key authentication is an officially documented path for individual developers, Fabric classifies `codebuddy-intl` as a normal low-risk API-key route. Normal quota and health controls still apply.

### API-key Termux bootstrap

From a checkout of this repository:

```bash
bash scripts/workbuddy-termux-bootstrap.sh
```

The script installs missing dependencies, installs the official CodeBuddy CLI, opens the API-key page, imports the credential through 9router, runs a low-cost probe, reports usage telemetry, and writes a credential-free snapshot to `~/.config/9router-go/workbuddy-last-probe.json`.

The default local router URL is `http://127.0.0.1:20128`. The bootstrap first tries `NINEROUTER_API_KEY`; if it is unset, it uses `~/.config/9router-go/admin-token` when present.

## Free/session route (`workbuddy-session`)

The Free validation account could not create an API key: the CodeBuddy profile UI marked API Keys as a Pro-only feature. The same account could, however, log in through the official CodeBuddy CLI using `International Site` and use its Free/bonus credits. The CLI persists that authenticated session under its own account state, and 9router can invoke it without extracting or copying the credential.

Install and log in once:

```bash
pkg install -y nodejs
npm install -g @tencent-ai/codebuddy-code
codebuddy
```

Choose `International Site` when prompted, finish the browser login, and then exit the interactive CLI. No CodeBuddy token is entered into 9router.

Use the session-backed provider explicitly:

```text
workbuddy-session/gpt-5.6-luna
```

Equivalent provider aliases are `wbs`, `wbf`, and `workbuddy-free`, for example:

```text
wbs/gpt-5.6-luna
```

The existing `workbuddy` and `wb` aliases intentionally remain mapped to `codebuddy-intl` for backwards compatibility with API-key users.

### Session execution policy

The session executor is intentionally conservative:

- it shells out with `exec.CommandContext`; no user-controlled shell command is constructed;
- requests are serialized one at a time to avoid hammering a quota-limited personal session;
- Auto Memory and CodeBuddy background tasks are disabled for each routed request;
- each invocation uses one turn and disables session persistence;
- the working directory defaults to the user's home directory and can be overridden with `WORKBUDDY_SESSION_CWD`;
- the CLI executable can be overridden with `WORKBUDDY_CODEBUDDY_PATH`;
- no `-y` / permission-bypass flag is used;
- tools, tool-call history, and image input are rejected for now rather than silently degraded;
- streaming clients are supported with a buffered single-result SSE response. The underlying CodeBuddy invocation is currently one-shot headless rather than token-by-token streaming.

Fabric classifies this route as a medium-risk product surface with conservative probe policy. It remains usable as a fallback but should lose ties against equivalent ordinary API-key/no-auth providers.

## Credit telemetry

CodeBuddy current clients expose actual credit consumption inside assistant event telemetry as `providerData.rawUsage.credit`. The API-key CodeBuddy SSE path preserves the upstream `usage` object. The session executor parses the official CLI JSON event stream and returns an OpenAI-compatible `usage.credit` field alongside token/cache counts.

Do not estimate credits from visible user tokens alone. CodeBuddy injects substantial internal context and may charge according to model/session behavior that is not represented by the small user prompt.

### Observed Free-session baseline

On 2026-09-10, GPT-5.6 Luna was validated twice on the same Free account:

| Mode | Credit | Prompt tokens | Cache write | Output |
| --- | ---: | ---: | ---: | ---: |
| Default headless | 0.50 | 19,885 | 19,882 | 8 |
| Lean headless | 0.35 | 13,930 | 13,927 | 8 |

The lean invocation reduced both reported credit consumption and injected prompt/cache volume by about 30%. These are observations, not guaranteed pricing. Real tasks can consume more.

The lean settings validated on Termux are equivalent to:

```bash
CODEBUDDY_DISABLE_AUTO_MEMORY=1 \
CODEBUDDY_CODE_DISABLE_AUTO_MEMORY=1 \
CODEBUDDY_CODE_DISABLE_BACKGROUND_TASKS=1 \
codebuddy \
  -p \
  --model gpt-5.6-luna \
  --max-turns 1 \
  --no-session-persistence \
  --system-prompt 'You are a general-purpose assistant. Answer the user directly. Do not use tools.' \
  --output-format json \
  'Respond exactly: WB_OK'
```

The `workbuddy-session` executor applies those settings automatically.

## Official CLI and local service

The upstream CLI requires Node.js 18+ and can be installed with:

```bash
npm install -g @tencent-ai/codebuddy-code
```

CodeBuddy also exposes a beta local HTTP service:

```bash
codebuddy --serve --port 8080
```

Keep its default authentication enabled. The session adapter does not depend on that server; it invokes the official headless CLI on demand so there is no permanent extra daemon.

## Model availability

Do not hard-code the WorkBuddy web selector as a permanent catalog. Model availability and account entitlements can change. Treat a model as usable only after the current account exposes it and a probe succeeds.

On 2026-09-10 the validated account exposed GPT-5.6 Sol/Terra/Luna, GPT-5.5, GPT-5.4, GPT-5.3-Codex, Gemini-3.5-Flash, GLM-5.3, GLM-5.2, Kimi-K3, Kimi-K2.6, and MiniMax-M3 in the CLI selector. GPT-6 Astra was not exposed by that account at that time. This is an observed snapshot, not a provider guarantee.
