package executor

import (
	json "encoding/json/v2"
	"testing"
)

func TestSSEToOpenAIJSON_PreservesCreditUsage(t *testing.T) {
	sse := "data: {\"id\":\"chatcmpl-credit\",\"created\":123,\"model\":\"gpt-5.6-luna\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"WB_OK\"}}]}\n\n" +
		"data: {\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":7,\"completion_tokens\":2,\"total_tokens\":9,\"credit\":0.14}}\n\n" +
		"data: [DONE]\n\n"

	out, ok := sseToOpenAIJSON([]byte(sse))
	if !ok {
		t.Fatal("expected SSE detected")
	}

	var resp struct {
		Usage struct {
			PromptTokens     int     `json:"prompt_tokens"`
			CompletionTokens int     `json:"completion_tokens"`
			TotalTokens      int     `json:"total_tokens"`
			Credit           float64 `json:"credit"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(out, &resp); err != nil {
		t.Fatalf("output not valid JSON: %v\n%s", err, out)
	}
	if resp.Usage.PromptTokens != 7 || resp.Usage.CompletionTokens != 2 || resp.Usage.TotalTokens != 9 {
		t.Fatalf("token usage not preserved: %+v", resp.Usage)
	}
	if resp.Usage.Credit != 0.14 {
		t.Fatalf("credit usage not preserved: got %v want 0.14", resp.Usage.Credit)
	}
}
