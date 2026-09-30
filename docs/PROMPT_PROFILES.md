# Prompt profiles

9router-go supports an opt-in instruction profile for OpenAI/ChatGPT-oriented engineering work.

## Why this exists

A prompt profile is a reproducible working-style layer, not a replacement for provider or model policy. It lets experiments vary the instruction layer independently from provider, model, tools, reasoning settings, and budget.

The first built-in profile is `openai-agentic-v1`. It is intentionally focused on execution quality: inspect before editing, avoid duplicated work, verify mutations, prefer minimal diffs, and record enough metadata to reproduce LLM routing experiments.

## ChatGPT Project version

For the ChatGPT product, copy the contents of:

`docs/prompt-profiles/chatgpt-project-agentic-v1.md`

into the Project Instructions field. This keeps the ChatGPT experiment separate from API/router behavior.

## 9router request version

The router version is embedded from:

`internal/promptprofile/profiles/openai-agentic-v1.txt`

Select it per request with:

`X-9Router-Prompt-Profile: openai-agentic-v1`

Accepted values are:

- `openai-agentic-v1` - balanced engineering execution profile
- `workspace-context-v1` - workspace/repository state first
- `fewshot-routing-v1` - compact examples for action/tool routing
- `operating-spec-v1` - explicit scope/acquire/act/verify/report loop
- `none` - control / explicit opt-out

Unknown names return HTTP 400 instead of silently falling back.

The v1 profile is provider-scoped. It currently applies only when the resolved provider is `openai` or `codex`. If a combo begins on another provider and later falls back to OpenAI/Codex, the profile is injected only on that OpenAI/Codex attempt.

### Injection semantics

- OpenAI Chat Completions: add or extend a `developer` message. Existing caller system/developer content is preserved.
- Responses-format bodies: add or extend top-level `instructions`.
- Codex: preserve the caller's first system/developer instruction, append the profile to it, then let the existing Chat-Completions-to-Responses adapter promote the combined text into Responses `instructions`.

No arbitrary instruction text is accepted from the header; the header selects only a fixed built-in profile.

## Experiment matrix

Use the same task set and model settings for each treatment:

1. control: no profile header;
2. `openai-agentic-v1`;
3. later profiles, one variable at a time.

Record exact provider/model, date, request format, tools, reasoning level, latency, token or credit usage, repetitions, correctness, tool-call success, and refusal/format behavior. This makes the comparison useful even when provider behavior changes.


## Local comparison harness

Run `scripts/prompt-profile-bench.sh` against one or more OpenAI/Codex routes. The script compares `none` plus every built-in profile on the same objective smoke tasks, records HTTP status, latency and reported token usage, and emits a single Markdown report.

~~~bash
PROMPT_PROFILE_BENCH_RUNS=3 \
NINEROUTER_API_KEY='YOUR_LOCAL_9ROUTER_CLIENT_KEY' \
bash scripts/prompt-profile-bench.sh codex/gpt-5.3-codex openai/gpt-5.6-sol
~~~

On Termux the report defaults to `~/storage/downloads` when that directory exists. Else it uses `~/.config/9router-go/benchmarks`.

The smoke set measures reproducible instruction-following and routing mechanics, not general intelligence. Use at least three repetitions before comparing close results, and do not generalize a profile win from one model to another.

### Live OpenCode E2E tests

The Muse Spark tests contact the real OpenCode free-tier service. They are opt-in so ordinary CI is not made red by external entitlement changes. Run them explicitly with:

~~~bash
OPENCODE_LIVE_E2E=1 go test ./internal/handlers/chat -run 'TestIntegration_OpenCode_MuseSpark' -v
~~~
