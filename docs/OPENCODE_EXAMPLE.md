# Using 9router-go with OpenCode

9router-go is the recommended data plane and proxy gateway for connecting **OpenCode** to any of your configured providers safely.

## Step 1: Configure OpenCode

In your OpenCode settings, add a new Custom Provider configured exactly as an OpenAI-compatible endpoint. This maps directly to your 9router-go host:

```json
{
  "name": "9router",
  "endpoint": "http://127.0.0.1:20128/v1",
  "apiKey": "your_9router_api_key_or_empty_if_unsecured"
}
```

*Note: You do not need to build a custom IDE extension or parallel catalog for OpenCode; its built-in OpenAI compat mode works seamlessly with 9router's translation layer.*

## Step 2: Configure Models

If your connection uses specific `models.dev` or `UnoRouter` models directly (for example, `gpt-4o:free`), add them exactly as named in the configuration array so OpenCode displays them locally, preventing name collisions:

```json
{
  "models": [
    "gpt-4o",
    "gpt-4o:free",
    "claude-3-5-sonnet-20240620",
    "deepseek-chat"
  ]
}
```

When you request one of these models from OpenCode, 9router-go handles the provider translation, tool call matching, and routing fallback safely via the Control Plane automatically.
