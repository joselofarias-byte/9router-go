# Using 9router-go with OpenCode

9router-go is the canonical data plane and proxy gateway for OpenCode. OpenCode is the cockpit; 9router-go/Fabric handles provider routing, locks, quota, health, verification, and failover.

## Configuration

Create or update `opencode.json` with a custom OpenAI-compatible provider that points to the local 9router-go `/v1` endpoint.

```json
{
  "$schema": "https://opencode.ai/config.json",
  "provider": {
    "9router": {
      "npm": "@ai-sdk/openai-compatible",
      "name": "9router-go Fabric",
      "options": {
        "baseURL": "http://127.0.0.1:20128/v1",
        "apiKey": "{env:NINE_ROUTER_API_KEY}"
      },
      "models": {
        "gpt-4o": {
          "name": "gpt-4o via 9router"
        },
        "gpt-4o:free": {
          "name": "gpt-4o:free via 9router"
        },
        "claude-3-5-sonnet-20240620": {
          "name": "claude-3-5-sonnet via 9router"
        },
        "deepseek-chat": {
          "name": "deepseek-chat via 9router"
        }
      }
    }
  }
}
```

Set `NINE_ROUTER_API_KEY` only when the local 9router-go endpoint requires authentication. If the local endpoint is intentionally unsecured, omit `options.apiKey` instead of placing a literal secret in `opencode.json`.

OpenCode must point to 9router-go/Fabric, not directly to UnoRouter or another upstream provider. UnoRouter remains an optional Fabric discovery/provider source configured separately through `UNOROUTER_API_KEY`; its credentials are not part of the OpenCode configuration above.

The `models` map should contain the exact model IDs you want visible in OpenCode. Keep the IDs unchanged so 9router-go can apply its existing translation, routing, health, quota, and fallback behavior without a parallel catalog or IDE-specific routing layer.
