package providers

import (
	"math"
	"strings"
)

// ErrorRule classifies an upstream error by text match or status code.
// Checked top-to-bottom: text rules first, then status rules.
type ErrorRule struct {
	Text       string // substring match (case-insensitive); empty means not text-based
	Status     int    // HTTP status code match; 0 means not status-based
	CooldownMs int    // fixed cooldown duration; 0 means use exponential backoff
	Backoff    bool   // true = use exponential backoff (rate limit)
}

type ErrorCategory string

const (
	ErrTransient     ErrorCategory = "transient"
	ErrRateLimit     ErrorCategory = "rate_limit"
	ErrQuota         ErrorCategory = "quota_exhausted"
	ErrAuth          ErrorCategory = "auth_failed"
	ErrPermanent     ErrorCategory = "permanent"
	ErrModelNotFound ErrorCategory = "model_not_found"
	ErrNetwork       ErrorCategory = "network"
	ErrTimeout       ErrorCategory = "timeout"
	ErrSession       ErrorCategory = "session_expired"
)

// BackoffConfig controls exponential backoff scaling.
var BackoffConfig = struct {
	BaseMs   int
	MaxMs    int
	MaxLevel int
}{
	BaseMs:   2000,          // 2 seconds base
	MaxMs:    5 * 60 * 1000, // 5 minutes cap
	MaxLevel: 15,
}

// TransientCooldownMs is the default cooldown for unmatched/unknown errors.
const TransientCooldownMs = 30 * 1000 // 30 seconds

// cooldown durations (ms) used by ERROR_RULES
const (
	cooldownLong  = 2 * 60 * 1000 // 2 minutes
	cooldownShort = 5 * 1000      // 5 seconds
)

// ErrorRules is the ordered list of error classification rules, matching Next.js ERROR_RULES.
// Checked top-to-bottom: text rules first (by order), then status rules.
var ErrorRules = []ErrorRule{
	// --- Text-based rules (checked first, order = priority) ---
	{Text: "no credentials", CooldownMs: cooldownLong},
	{Text: "request not allowed", CooldownMs: cooldownShort},
	{Text: "improperly formed request", CooldownMs: cooldownLong},
	{Text: "rate limit", Backoff: true},
	{Text: "too many requests", Backoff: true},
	{Text: "quota exceeded", Backoff: true},
	{Text: "capacity", Backoff: true},
	{Text: "overloaded", Backoff: true},
	{Text: "resource_exhausted", Backoff: true},
	{Text: "resource has been exhausted", Backoff: true},
	{Text: "model_capacity_exhausted", Backoff: true},
	{Text: "server is temporarily unavailable", Backoff: true},
	{Text: "model not found", CooldownMs: cooldownLong},
	{Text: "does not exist", CooldownMs: cooldownLong},
	{Text: "unknown model", CooldownMs: cooldownLong},
	{Text: "context deadline exceeded", CooldownMs: cooldownShort},
	{Text: "client.timeout exceeded", CooldownMs: cooldownShort},
	{Text: "i/o timeout", CooldownMs: cooldownShort},
	{Text: "no such host", CooldownMs: cooldownLong},
	{Text: "connection refused", CooldownMs: cooldownShort},
	{Text: "network is unreachable", CooldownMs: cooldownLong},
	{Text: "connection reset by peer", CooldownMs: cooldownShort},
	{Text: "invalid session", CooldownMs: cooldownLong},
	{Text: "session expired", CooldownMs: cooldownLong},
	{Text: "token expired", CooldownMs: cooldownLong},
	{Text: "refresh token", CooldownMs: cooldownLong},

	// --- Status-based rules (fallback when text doesn't match) ---
	{Status: 401, CooldownMs: cooldownLong},
	{Status: 402, CooldownMs: cooldownLong},
	{Status: 403, CooldownMs: cooldownLong},
	{Status: 404, CooldownMs: cooldownLong},
	{Status: 429, Backoff: true},
	{Status: 502, Backoff: true},
	{Status: 503, Backoff: true},
	{Status: 504, Backoff: true},
}

// GetQuotaCooldown calculates exponential backoff cooldown for rate limits.
// Level 0 → 2s, Level 1 → 2s, Level 2 → 4s, Level 3 → 8s, ... capped at MaxMs.
func GetQuotaCooldown(backoffLevel int) int {
	level := max(backoffLevel-1, 0)
	cooldown := int(float64(BackoffConfig.BaseMs) * math.Pow(2, float64(level)))
	return min(cooldown, BackoffConfig.MaxMs)
}

// ErrorClassification holds the result of ClassifyError.
type ErrorClassification struct {
	ShouldFallback  bool
	CooldownMs      int
	NewBackoffLevel int // only meaningful when the matched rule has Backoff=true
	Category        ErrorCategory
}

// ClassifyError classifies an upstream error by matching text and status against ErrorRules.
// Returns the cooldown duration and new backoff level.
// Matches Next.js checkFallbackError() in open-sse/services/accountFallback.js.
func ClassifyError(statusCode int, errorText string, backoffLevel int) ErrorClassification {
	lowerError := ""
	if errorText != "" {
		lowerError = strings.ToLower(errorText)
	}

	for _, rule := range ErrorRules {
		// Text-based match (substring, case-insensitive)
		if rule.Text != "" && lowerError != "" && strings.Contains(lowerError, rule.Text) {
			if rule.Backoff {
				newLevel := min(backoffLevel+1, BackoffConfig.MaxLevel)
				return ErrorClassification{
					ShouldFallback:  true,
					CooldownMs:      GetQuotaCooldown(newLevel),
					NewBackoffLevel: newLevel,
					Category:        classifyCategory(rule),
				}
			}
			return ErrorClassification{
				ShouldFallback:  true,
				CooldownMs:      rule.CooldownMs,
				NewBackoffLevel: backoffLevel,
				Category:        classifyCategory(rule),
			}
		}

		// Status-based match
		if rule.Status != 0 && rule.Status == statusCode {
			if rule.Backoff {
				newLevel := min(backoffLevel+1, BackoffConfig.MaxLevel)
				return ErrorClassification{
					ShouldFallback:  true,
					CooldownMs:      GetQuotaCooldown(newLevel),
					NewBackoffLevel: newLevel,
					Category:        classifyCategory(rule),
				}
			}
			return ErrorClassification{
				ShouldFallback:  true,
				CooldownMs:      rule.CooldownMs,
				NewBackoffLevel: backoffLevel,
				Category:        classifyCategory(rule),
			}
		}
	}
	// Upstream parity (open-sse/services/accountFallback.js checkFallbackError):
	// request-scoped 4xx that match no rule say nothing about the credential,
	// so the account must not be cooled down. Account-scoped statuses keep
	// their rules above (401/402/403/404/429 + quota/capacity text rules).
	if statusCode >= 400 && statusCode < 500 && statusCode != 401 && statusCode != 402 && statusCode != 403 && statusCode != 429 {
		return ErrorClassification{ShouldFallback: false, Category: ErrPermanent}
	}

	// Default: transient cooldown for any unmatched error
	return ErrorClassification{
		ShouldFallback:  true,
		CooldownMs:      TransientCooldownMs,
		NewBackoffLevel: backoffLevel,
		Category:        ErrTransient,
	}
}

func classifyCategory(rule ErrorRule) ErrorCategory {
	switch {
	case rule.Status == 401 || rule.Status == 403 || rule.Text == "no credentials" || rule.Text == "request not allowed":
		return ErrAuth
	case rule.Status == 402 || rule.Text == "quota exceeded" || rule.Text == "capacity" || rule.Text == "resource_exhausted" || rule.Text == "resource has been exhausted" || rule.Text == "model_capacity_exhausted":
		return ErrQuota
	case rule.Status == 429 || rule.Text == "rate limit" || rule.Text == "too many requests" || rule.Text == "overloaded":
		return ErrRateLimit
	case rule.Text == "invalid session" || rule.Text == "session expired" || rule.Text == "token expired" || rule.Text == "refresh token":
		return ErrSession
	case rule.Text == "model not found" || rule.Text == "does not exist" || rule.Text == "unknown model":
		return ErrModelNotFound
	case rule.Text == "context deadline exceeded" || rule.Text == "client.timeout exceeded" || rule.Text == "i/o timeout":
		return ErrTimeout
	case rule.Text == "no such host" || rule.Text == "connection refused" || rule.Text == "network is unreachable" || rule.Text == "connection reset by peer":
		return ErrNetwork
	case rule.Status == 404 || rule.Text == "improperly formed request":
		return ErrPermanent
	default:
		return ErrTransient
	}
}
