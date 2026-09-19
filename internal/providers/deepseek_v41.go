package providers

// DeepSeek V4.1 Flash is natively multimodal and reasoning-capable. Keep the
// canonical release name plus the short DeepSeek API alias so dynamically
// discovered providers inherit the right routing capabilities without waiting
// for the daily external catalog sync.
func init() {
	caps := Capabilities{Vision: true, Reasoning: true, Tools: true}

	modelCapabilities["deepseek-v4.1-flash"] = caps
	modelCapabilities["deepseek-flash"] = caps

	if providerCapabilities["deepseek"] == nil {
		providerCapabilities["deepseek"] = map[string]Capabilities{}
	}
	providerCapabilities["deepseek"]["deepseek-v4.1-flash"] = caps
	providerCapabilities["deepseek"]["deepseek-flash"] = caps

	// Cline currently advertises the canonical provider/model ID in its live
	// recommended catalog. This does not mark it free; pricing comes only from
	// the catalog's explicit `free` set in the discovery adapter.
	if providerCapabilities["cline"] == nil {
		providerCapabilities["cline"] = map[string]Capabilities{}
	}
	providerCapabilities["cline"]["deepseek/deepseek-v4.1-flash"] = caps

	// CodeBuddy can surface the canonical model name dynamically. Capability
	// registration is harmless if the account catalog has not enabled it yet.
	if providerCapabilities["codebuddy-cn"] == nil {
		providerCapabilities["codebuddy-cn"] = map[string]Capabilities{}
	}
	providerCapabilities["codebuddy-cn"]["deepseek-v4.1-flash"] = caps
}
