package translator

import (
	json "encoding/json/v2"
	"testing"
)

func TestTranslateOpenAIToGeminiPreservesToolCallIDs(t *testing.T) {
	body := []byte(`{
		"model":"claude-opus-4-6-thinking",
		"messages":[
			{"role":"user","content":"inspect the repo"},
			{"role":"assistant","content":"","tool_calls":[
				{
					"id":"call_read_42__ts__sig123",
					"type":"function",
					"function":{"name":"read_file","arguments":"{\"path\":\"README.md\"}"}
				}
			]},
			{"role":"tool","tool_call_id":"call_read_42__ts__sig123","content":"ok"}
		]
	}`)

	out, err := TranslateOpenAIToGemini(body)
	if err != nil {
		t.Fatalf("TranslateOpenAIToGemini: %v", err)
	}

	var req GeminiRequest
	if err := json.Unmarshal(out, &req); err != nil {
		t.Fatalf("unmarshal Gemini request: %v", err)
	}

	var callID, responseID, sig string
	for _, content := range req.Contents {
		for _, part := range content.Parts {
			if part.FunctionCall != nil {
				callID = part.FunctionCall.ID
				sig = part.ThoughtSignature
			}
			if part.FunctionResponse != nil {
				responseID = part.FunctionResponse.ID
			}
		}
	}

	if callID == "" {
		t.Fatal("functionCall.id must not be empty; Antigravity Claude maps it to Anthropic tool_use.id")
	}
	if responseID == "" {
		t.Fatal("functionResponse.id must not be empty")
	}
	if callID != "call_read_42" {
		t.Fatalf("functionCall.id = %q, want %q", callID, "call_read_42")
	}
	if responseID != callID {
		t.Fatalf("functionResponse.id = %q, want matching functionCall.id %q", responseID, callID)
	}
	if sig != "sig123" {
		t.Fatalf("thought signature = %q, want %q", sig, "sig123")
	}
}
