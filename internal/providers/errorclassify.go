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
	ErrTransient    ErrorCategory = "transient"
	ErrRateLimit    ErrorCategory = "rate_limit"
	ErrQuota        ErrorCategory = "quota_exhausted"
	ErrAuth         ErrorCategory = "auth_failed"
	ErrPermanent    ErrorCategory = "permanent"
)

// BackoffConfig controls exponential backoff scaling.
var BackoffConfig = struct {
	BaseMs   int
	MaxMs    int
	MaxLevel int
}{
	BaseMs:   2000,       // 2 seconds base
	MaxMs:    5 * 60 * 1000, // 5 minutes cap
	MaxLevel: 15,
}

// TransientCooldownMs is the default cooldown for unmatched/unknown errors.
const TransientCooldownMs = 30 * 1000 // 30 seconds

// cooldown durations (ms) used by ERROR_RULES
const (
	cooldownLong  = 2 * 60 * 1000  // 2 minutes
	cooldownShort = 5 * 1000       // 5 seconds
)

// ErrorRules is the ordered list of error classification rules, matching Next.js ERROR_RULES.
// Checked top-to-bottom: text rules first (by order), then status rules.
var ErrorRules = []ErrorRule{
	// --- Text-based rules (checked first, order = priority) ---
	{Text: "no credentials",              CooldownMs: cooldownLong},
	{Text: "request not allowed",         CooldownMs: cooldownShort},
	{Text: "improperly formed request",   CooldownMs: cooldownLong},
	{Text: "rate limit",                  Backoff: true},
	{Text: "too many requests",           Backoff: true},
	{Text: "quota exceeded",              Backoff: true},
	{Text: "capacity",                    Backoff: true},
	{Text: "overloaded",                  Backoff: true},

	// --- Status-based rules (fallback when text doesn't match) ---
	{Status: 401, CooldownMs: cooldownLong},
	{Status: 402, CooldownMs: cooldownLong},
	{Status: 403, CooldownMs: cooldownLong},
	{Status: 404, CooldownMs: cooldownLong},
	{Status: 429, Backoff: true},
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
			return buildClassification(rule, backoffLevel)
		}

		// Status-based match
		if rule.Status != 0 && rule.Status == statusCode {
			return buildClassification(rule, backoffLevel)
		}
	}

	// Default: transient cooldown for any unmatched error
	return ErrorClassification{
		ShouldFallback:  true,
		CooldownMs:      TransientCooldownMs,
		NewBackoffLevel: backoffLevel,
		Category:        ErrTransient,
	}
}

func buildClassification(rule ErrorRule, backoffLevel int) ErrorClassification {
	c := ErrorClassification{
		ShouldFallback: true,
	}

	if rule.Backoff {
		c.NewBackoffLevel = min(backoffLevel+1, BackoffConfig.MaxLevel)
		c.CooldownMs = GetQuotaCooldown(c.NewBackoffLevel)
	} else {
		c.NewBackoffLevel = backoffLevel
		c.CooldownMs = rule.CooldownMs
	}

	// Categorize the error
	c.Category = ErrTransient // default unless overridden
	if rule.Status == 402 || rule.Text == "quota exceeded" || rule.Text == "capacity" {
		c.Category = ErrQuota
	} else if rule.Status == 429 || rule.Text == "rate limit" || rule.Text == "too many requests" || rule.Text == "overloaded" {
		c.Category = ErrRateLimit
	} else if rule.Status == 401 || rule.Status == 403 || rule.Text == "no credentials" || rule.Text == "request not allowed" {
		c.Category = ErrAuth
	} else if rule.Status == 404 || rule.Text == "improperly formed request" {
		c.Category = ErrPermanent
	}

	return c
}
