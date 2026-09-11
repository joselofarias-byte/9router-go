package translator

import (
	"encoding/json"
	"testing"
)

func TestThoughtSignatureStore_BasicAndSession(t *testing.T) {
	ClearGeminiThoughtSignatures()

	callID := "call_test_123"
	sig := "sig_abc_xyz"
	sessionID := "session_1"

	StoreGeminiThoughtSignature(callID, sig, sessionID)

	// Retrieve by session + callID
	if got := GetGeminiThoughtSignature(callID, sessionID); got != sig {
		t.Errorf("expected %q, got %q", sig, got)
	}

	// Retrieve by callID alone (global fallback)
	if got := GetGeminiThoughtSignature(callID, ""); got != sig {
		t.Errorf("expected %q, got %q", sig, got)
	}

	// Retrieve with __ts__ suffix attached
	if got := GetGeminiThoughtSignature(callID+"__ts__dummy", sessionID); got != sig {
		t.Errorf("expected %q for clean lookup, got %q", sig, got)
	}

	// Unknown callID
	if got := GetGeminiThoughtSignature("call_unknown", sessionID); got != "" {
		t.Errorf("expected empty for unknown, got %q", got)
	}
}

func TestThoughtSignatureStore_ParallelCallsFirstGetsSig(t *testing.T) {
	ClearGeminiThoughtSignatures()

	reqJSON := []byte(`{
		"model": "gemini-2.5-flash",
		"messages": [
			{
				"role": "assistant",
				"tool_calls": [
					{
						"id": "call_first_001",
						"type": "function",
						"function": {
							"name": "test_tool_1",
							"arguments": "{\"a\":1}"
						}
					},
					{
						"id": "call_second_002",
						"type": "function",
						"function": {
							"name": "test_tool_2",
							"arguments": "{\"b\":2}"
						}
					}
				]
			}
		]
	}`)

	geminiBytes, err := TranslateOpenAIToGemini(reqJSON)
	if err != nil {
		t.Fatalf("TranslateOpenAIToGemini failed: %v", err)
	}

	var gReq GeminiRequest
	if err := json.Unmarshal(geminiBytes, &gReq); err != nil {
		t.Fatalf("unmarshal geminiBytes failed: %v", err)
	}

	if len(gReq.Contents) != 1 {
		t.Fatalf("expected 1 content, got %d", len(gReq.Contents))
	}

	parts := gReq.Contents[0].Parts
	if len(parts) != 2 {
		t.Fatalf("expected 2 parts, got %d", len(parts))
	}

	// First call should get DefaultThinkingSignature
	if parts[0].ThoughtSignature != DefaultThinkingSignature {
		t.Errorf("first call should have default signature, got %q", parts[0].ThoughtSignature)
	}

	// Second sibling call without cached sig should be unsigned (empty)
	if parts[1].ThoughtSignature != "" {
		t.Errorf("second sibling call should be unsigned, got %q", parts[1].ThoughtSignature)
	}
}
