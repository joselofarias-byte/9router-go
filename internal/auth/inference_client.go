package auth

// InferenceOnlyKeyPrefix is used for client credentials that can call the LLM
// API but must not access dashboard management endpoints. The prefix is a
// backward-compatible scope discriminator for persisted API keys; legacy
// sk- keys retain their current behavior without a database migration.
const InferenceOnlyKeyPrefix = "sk-infer-"
