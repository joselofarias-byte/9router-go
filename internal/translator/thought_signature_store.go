package translator

import (
	"strings"
	"sync"
	"time"
)

const (
	maxMemorySignatures = 2000
	memoryTTL           = time.Hour
)

type signatureEntry struct {
	signature string
	expiresAt time.Time
}

type thoughtSignatureStore struct {
	mu      sync.RWMutex
	entries map[string]signatureEntry
	order   []string // FIFO / LRU approximation
}

var globalThoughtSigStore = &thoughtSignatureStore{
	entries: make(map[string]signatureEntry),
	order:   make([]string, 0, maxMemorySignatures),
}

func (s *thoughtSignatureStore) pruneExpiredLocked(now time.Time) {
	for k, v := range s.entries {
		if now.After(v.expiresAt) {
			delete(s.entries, k)
		}
	}

	// Bound max capacity
	if len(s.entries) > maxMemorySignatures {
		newOrder := make([]string, 0, len(s.entries))
		for _, k := range s.order {
			if _, ok := s.entries[k]; ok {
				newOrder = append(newOrder, k)
			}
		}
		for len(newOrder) > maxMemorySignatures {
			oldest := newOrder[0]
			newOrder = newOrder[1:]
			delete(s.entries, oldest)
		}
		s.order = newOrder
	}
}

// StoreGeminiThoughtSignature stores a thought signature for a tool_call_id with optional session namespace.
func StoreGeminiThoughtSignature(toolCallID, signature, sessionID string) {
	if toolCallID == "" || signature == "" {
		return
	}

	now := time.Now()
	exp := now.Add(memoryTTL)

	// Clean toolCallID if it contains __ts__ suffix
	cleanID := toolCallID
	if idx := strings.LastIndex(toolCallID, "__ts__"); idx != -1 {
		cleanID = toolCallID[:idx]
	}

	globalThoughtSigStore.mu.Lock()
	defer globalThoughtSigStore.mu.Unlock()

	globalThoughtSigStore.pruneExpiredLocked(now)

	keys := []string{toolCallID}
	if cleanID != toolCallID {
		keys = append(keys, cleanID)
	}

	if sessionID != "" {
		keys = append(keys, sessionID+":"+toolCallID)
		if cleanID != toolCallID {
			keys = append(keys, sessionID+":"+cleanID)
		}
	}

	for _, k := range keys {
		if _, exists := globalThoughtSigStore.entries[k]; !exists {
			globalThoughtSigStore.order = append(globalThoughtSigStore.order, k)
		}
		globalThoughtSigStore.entries[k] = signatureEntry{
			signature: signature,
			expiresAt: exp,
		}
	}
}

// GetGeminiThoughtSignature retrieves a thought signature by tool_call_id (checking session namespace first).
func GetGeminiThoughtSignature(toolCallID, sessionID string) string {
	if toolCallID == "" {
		return ""
	}

	cleanID := toolCallID
	if idx := strings.LastIndex(toolCallID, "__ts__"); idx != -1 {
		cleanID = toolCallID[:idx]
	}

	now := time.Now()

	globalThoughtSigStore.mu.RLock()
	defer globalThoughtSigStore.mu.RUnlock()

	// 1. Session-scoped check
	if sessionID != "" {
		if entry, ok := globalThoughtSigStore.entries[sessionID+":"+toolCallID]; ok && now.Before(entry.expiresAt) {
			return entry.signature
		}
		if cleanID != toolCallID {
			if entry, ok := globalThoughtSigStore.entries[sessionID+":"+cleanID]; ok && now.Before(entry.expiresAt) {
				return entry.signature
			}
		}
	}

	// 2. Global toolCallID check
	if entry, ok := globalThoughtSigStore.entries[toolCallID]; ok && now.Before(entry.expiresAt) {
		return entry.signature
	}
	if cleanID != toolCallID {
		if entry, ok := globalThoughtSigStore.entries[cleanID]; ok && now.Before(entry.expiresAt) {
			return entry.signature
		}
	}

	return ""
}

// ClearGeminiThoughtSignatures resets the store (useful for tests).
func ClearGeminiThoughtSignatures() {
	globalThoughtSigStore.mu.Lock()
	defer globalThoughtSigStore.mu.Unlock()
	globalThoughtSigStore.entries = make(map[string]signatureEntry)
	globalThoughtSigStore.order = make([]string, 0, maxMemorySignatures)
}
