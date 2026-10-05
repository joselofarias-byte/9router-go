package chat

import (
	"context"
	json "encoding/json/v2"
	"math"
	"sort"
	"strings"
	"time"

	"9router/proxy/internal/log"
	"9router/proxy/internal/models"
	"9router/proxy/internal/translator"
)

// AntigravitySelectionQuotaTTL is the maximum quota age accepted by the
// expiry-pressure selector. Stale or missing data never blocks a request:
// selection falls back to the existing round-robin while a background refresh
// primes the next request.
var AntigravitySelectionQuotaTTL = 90 * time.Second

// AntigravityExpiryPressure describes how urgently one account's expiring
// allowance should be consumed. Score is percentage-points remaining per hour
// until reset. Larger means more usable quota is at risk of expiring.
type AntigravityExpiryPressure struct {
	Score               float64
	RemainingPercentage float64
	ResetAt             time.Time
	QuotaKey            string
}

func antigravityQuotaKeysForModel(model string) []string {
	keys := make([]string, 0, 4)
	add := func(key string) {
		if key == "" {
			return
		}
		for _, existing := range keys {
			if existing == key {
				return
			}
		}
		keys = append(keys, key)
	}

	add(model)
	if canonical, ok := translator.AntigravityModelSynonyms[model]; ok {
		add(canonical)
	}

	switch {
	case strings.HasPrefix(model, "gemini-"):
		add("gemini_session")
		add("gemini_weekly")
	case strings.HasPrefix(model, "claude-"), strings.HasPrefix(model, "gpt-"):
		add("claude_gpt_session")
		add("claude_gpt_weekly")
	}
	return keys
}

func antigravityQuotaSnapshot(connectionID string) (map[string]AntigravityModelQuota, time.Time) {
	agQuotaMu.RLock()
	defer agQuotaMu.RUnlock()

	src := agQuotaCache[connectionID]
	dst := make(map[string]AntigravityModelQuota, len(src))
	for key, value := range src {
		dst[key] = value
	}
	return dst, agLastRefreshAt[connectionID]
}

func antigravityQuotaNeedsRefresh(connectionID, model string, now time.Time) bool {
	if connectionID == "" || model == "" {
		return false
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}

	modelsMap, lastRef := antigravityQuotaSnapshot(connectionID)

	if lastRef.IsZero() || len(modelsMap) == 0 {
		return true
	}
	if AntigravitySelectionQuotaTTL > 0 && now.Sub(lastRef) >= AntigravitySelectionQuotaTTL {
		return true
	}

	hasUsableFutureWindow := false
	for _, key := range antigravityQuotaKeysForModel(model) {
		q, ok := modelsMap[key]
		if !ok {
			continue
		}
		if !q.ResetAt.IsZero() && !q.ResetAt.After(now) {
			return true
		}
		if q.RemainingPercentage > 0 && q.ResetAt.After(now) {
			hasUsableFutureWindow = true
		}
	}
	return !hasUsableFutureWindow
}

func getAntigravityExpiryPressure(connectionID, model string, now time.Time) (AntigravityExpiryPressure, bool) {
	var zero AntigravityExpiryPressure
	if antigravityQuotaNeedsRefresh(connectionID, model, now) {
		return zero, false
	}

	modelsMap, _ := antigravityQuotaSnapshot(connectionID)

	best := zero
	found := false
	const minHours = 1.0 / 60.0

	for _, key := range antigravityQuotaKeysForModel(model) {
		q, ok := modelsMap[key]
		if !ok || q.RemainingPercentage <= 0 || q.ResetAt.IsZero() || !q.ResetAt.After(now) {
			continue
		}
		hours := q.ResetAt.Sub(now).Hours()
		if hours < minHours {
			hours = minHours
		}
		score := q.RemainingPercentage / hours
		if !found || score > best.Score ||
			(score == best.Score && q.ResetAt.Before(best.ResetAt)) ||
			(score == best.Score && q.ResetAt.Equal(best.ResetAt) && q.RemainingPercentage > best.RemainingPercentage) {
			best = AntigravityExpiryPressure{
				Score:               score,
				RemainingPercentage: q.RemainingPercentage,
				ResetAt:             q.ResetAt,
				QuotaKey:            key,
			}
			found = true
		}
	}
	return best, found
}

func mergeAntigravityWeeklyForSelection(connectionID string, weekly map[string]AntigravityWeeklyQuota) {
	if connectionID == "" || len(weekly) == 0 {
		return
	}
	agQuotaMu.Lock()
	defer agQuotaMu.Unlock()
	if agQuotaCache[connectionID] == nil {
		agQuotaCache[connectionID] = make(map[string]AntigravityModelQuota)
	}
	for key, q := range weekly {
		agQuotaCache[connectionID][key] = AntigravityModelQuota{
			RemainingPercentage: q.RemainingPercentage,
			ResetAt:             q.ResetAt,
		}
	}
}

func invalidateExpiredAntigravityWeeklyCache(accessToken, projectID string, now time.Time) {
	if accessToken == "" {
		return
	}
	cacheKey := accessToken + "::" + projectID
	agWeeklyMu.Lock()
	defer agWeeklyMu.Unlock()
	cached, ok := agWeeklyCache[cacheKey]
	if !ok {
		return
	}
	for _, q := range cached {
		if !q.ResetAt.IsZero() && !q.ResetAt.After(now) {
			delete(agWeeklyCache, cacheKey)
			delete(agWeeklyAt, cacheKey)
			return
		}
	}
}

func projectIDFromConnectionJSON(raw string) string {
	if strings.TrimSpace(raw) == "" {
		return ""
	}
	var data map[string]any
	if err := json.Unmarshal([]byte(raw), &data); err != nil {
		return ""
	}
	for _, key := range []string{"cloudaicompanionProject", "projectId", "projectID"} {
		if id := extractProjectID(data[key]); id != "" {
			return id
		}
	}
	if psd, ok := data["providerSpecificData"].(map[string]any); ok {
		for _, key := range []string{"cloudaicompanionProject", "projectId", "projectID"} {
			if id := extractProjectID(psd[key]); id != "" {
				return id
			}
		}
	}
	return ""
}

// primeAntigravityExpiryPressure refreshes stale quota in the background so
// routing never waits on Google's quota endpoints. First cold request therefore
// uses round-robin; subsequent requests become expiry-aware as data arrives.
func (h *ChatHandler) primeAntigravityExpiryPressure(conns []*models.ProviderConnection, model string) {
	if h == nil || model == "" {
		return
	}
	now := time.Now().UTC()
	for _, conn := range conns {
		if conn == nil || conn.Provider != "antigravity" || conn.AuthType != "oauth" {
			continue
		}
		if !antigravityQuotaNeedsRefresh(conn.ID, model, now) {
			continue
		}

		var data ConnectionData
		if err := json.Unmarshal([]byte(conn.Data), &data); err != nil {
			continue
		}
		token := data.AccessToken
		if token == "" {
			token = data.APIKey
		}
		if token == "" {
			continue
		}
		projectID := projectIDFromConnectionJSON(conn.Data)
		client := h.getClientForConnection(&data)
		connID := conn.ID

		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			defer cancel()

			if _, err := RefreshAntigravityQuota(ctx, client, connID, token, projectID); err != nil {
				log.Warn("ag_expiry", "quota refresh failed", "conn", shortConnID(connID), "error", err)
				return
			}
			invalidateExpiredAntigravityWeeklyCache(token, projectID, time.Now().UTC())
			if weekly, err := FetchAntigravityWeeklyQuota(ctx, client, token, projectID); err == nil {
				mergeAntigravityWeeklyForSelection(connID, weekly)
			}
		}()
	}
}

// rankAntigravityByExpiryPressure applies the policy only when every eligible
// account has fresh evidence. Partial information falls back to round-robin;
// this avoids starving an account merely because its quota has not loaded yet.
func rankAntigravityByExpiryPressure(conns []*models.ProviderConnection, model string, now time.Time) ([]*models.ProviderConnection, bool) {
	if len(conns) <= 1 || model == "" {
		return conns, false
	}

	type scored struct {
		conn     *models.ProviderConnection
		pressure AntigravityExpiryPressure
		index    int
	}
	items := make([]scored, 0, len(conns))
	for i, conn := range conns {
		if conn == nil {
			return conns, false
		}
		p, ok := getAntigravityExpiryPressure(conn.ID, model, now)
		if !ok {
			return conns, false
		}
		items = append(items, scored{conn: conn, pressure: p, index: i})
	}

	sort.SliceStable(items, func(i, j int) bool {
		a, b := items[i], items[j]
		if math.Abs(a.pressure.Score-b.pressure.Score) > 1e-9 {
			return a.pressure.Score > b.pressure.Score
		}
		if !a.pressure.ResetAt.Equal(b.pressure.ResetAt) {
			return a.pressure.ResetAt.Before(b.pressure.ResetAt)
		}
		if a.pressure.RemainingPercentage != b.pressure.RemainingPercentage {
			return a.pressure.RemainingPercentage > b.pressure.RemainingPercentage
		}
		return a.index < b.index
	})

	ranked := make([]*models.ProviderConnection, 0, len(items))
	for _, item := range items {
		ranked = append(ranked, item.conn)
	}
	return ranked, true
}
