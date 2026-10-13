package multiagent

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"
)

type Policy struct {
	Models []string `json:"models"`
	ExternalEnabled bool `json:"external_enabled"`
	MaxModels int `json:"max_models"`
	MaxOutputTokens int `json:"max_output_tokens"`
}

func currentPolicy() Policy {
	p := Policy{Models: []string{}, ExternalEnabled: os.Getenv("CAPIMUX_MULTIAGENT_ENABLE_EXTERNAL") == "1", MaxModels: 8, MaxOutputTokens: 4096}
	seen := make(map[string]bool)
	for _, raw := range strings.Split(os.Getenv("CAPIMUX_MULTIAGENT_ALLOWED_MODELS"), ",") {
		model := strings.TrimSpace(raw)
		if model != "" && !seen[model] { p.Models = append(p.Models, model); seen[model] = true }
	}
	return p
}

func (p Policy) allows(model string) bool {
	for _, allowed := range p.Models { if allowed == model { return true } }
	return false
}

// PolicyHandler exposes the operator's allowlist for the dashboard model picker.
func PolicyHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(currentPolicy())
}
