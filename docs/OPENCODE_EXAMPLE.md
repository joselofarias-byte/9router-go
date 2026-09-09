# Using 9router-go with OpenCode

9router-go is the recommended data plane and proxy gateway for connecting **OpenCode** to any of your configured providers safely. OpenCode acts strictly as the human/agent UI cockpit, while 9router-go handles routing, locks, quota, and failovers.

## Configuration

In your OpenCode settings (usually `opencode.json` or equivalent configuration interface), add a custom OpenAI-compatible provider pointing to your local 9router-go instance.

Here is a copy-paste valid example:

```json
{
  "$schema": "https://opencode.dev/schema.json",
  "provider.9router": {
    "npm": "@ai-sdk/openai-compatible",
    "options": {
      "baseURL": "http://127.0.0.1:20128/v1",
      "apiKey": "your_9router_api_key_or_empty_if_unsecured"
    },
    "models": {
      "gpt-4o": {},
      "gpt-4o:free": {},
      "claude-3-5-sonnet-20240620": {},
      "deepseek-chat": {}
    }
  }
}
```

*Note: You do not need to build a custom IDE extension or parallel catalog for OpenCode; its built-in OpenAI compat mode works seamlessly with 9router's translation layer.*

By explicitly listing `models` under the `provider.9router` block, OpenCode understands exactly what is available and avoids sending traffic to unrouted endpoints or colliding with internal default `models.dev` definitions.
