# Prompt profiles

9router-go supports opt-in instruction profiles for OpenAI and Codex requests. No profile is active unless a request selects one.

## Selection

Send the profile name on the request header:

```
X-9Router-Prompt-Profile: openai-agentic-v1
```

Accepted values:

- `openai-agentic-v1` — balanced engineering execution
- `workspace-context-v1` — workspace and repository state first
- `fewshot-routing-v1` — compact examples for action and tool routing
- `operating-spec-v1` — explicit scope, acquire, act, verify, and report loop
- `none` — explicit opt-out

Unknown names return HTTP 400. The header selects only a fixed built-in profile; it does not accept arbitrary instruction text.

The v1 profiles apply only when the resolved provider is `openai` or `codex`. A combo that starts on another provider and later falls back to OpenAI or Codex receives the profile only on that OpenAI or Codex attempt.

## Injection

- OpenAI Chat Completions: add or extend a `developer` message. Existing caller system and developer content is preserved.
- Responses-format bodies: add or extend top-level `instructions`.
- Codex Chat Completions: preserve the caller's first system or developer instruction, append the profile to it, then let the existing Chat-to-Responses adapter promote the combined text into Responses `instructions`.

Injecting the same profile text again is a no-op.
