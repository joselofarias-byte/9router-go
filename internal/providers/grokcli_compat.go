package providers

import (
	"fmt"
	"os"
	"runtime"
	"strings"
)

// GrokCLIDefaultClientVersion tracks the official Grok Build client identity
// accepted by cli-chat-proxy. GROK_CLI_CLIENT_VERSION is an emergency runtime
// override for xAI version gates so a server-side minimum bump does not require
// waiting for a new 9router binary.
const GrokCLIDefaultClientVersion = "1.0.45"

func GrokCLIClientVersion() string {
	if v := strings.TrimSpace(os.Getenv("GROK_CLI_CLIENT_VERSION")); v != "" {
		return v
	}
	return GrokCLIDefaultClientVersion
}

func GrokCLIUserAgent() string {
	return fmt.Sprintf("grok-shell/%s (%s; %s)", GrokCLIClientVersion(), runtime.GOOS, runtime.GOARCH)
}

// GrokCLIProxyHeaders is the common authenticated identity for xAI's
// cli-chat-proxy endpoints. Callers may add endpoint-specific headers such as
// x-grok-model-override.
func GrokCLIProxyHeaders(accessToken string) map[string]string {
	headers := map[string]string{
		"User-Agent":               GrokCLIUserAgent(),
		"x-grok-client-identifier": "grok-shell",
		"x-grok-client-version":    GrokCLIClientVersion(),
		"X-XAI-Token-Auth":         "xai-grok-cli",
		"x-authenticateresponse":   "authenticate-response",
		"x-grok-client-mode":       "headless",
	}
	if strings.TrimSpace(accessToken) != "" {
		headers["Authorization"] = "Bearer " + accessToken
	}
	return headers
}
