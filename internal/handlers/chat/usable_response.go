package chat

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
)

// Keep an OpenCode upstream's empty 200 (including SSE role/usage-only
// preludes) private so combo fallback can still try the next model. Once text
// or a tool call starts, preserve streaming and never splice a second answer.
type usableResponseWriter struct {
	w         http.ResponseWriter
	header    http.Header
	code      int
	buf       bytes.Buffer
	pending   []byte
	committed bool
	err       error
	mu        sync.Mutex
}

func newUsableResponseWriter(w http.ResponseWriter) *usableResponseWriter {
	return &usableResponseWriter{w: w, header: w.Header().Clone(), code: http.StatusOK}
}

func (w *usableResponseWriter) Header() http.Header  { return w.header }
func (w *usableResponseWriter) WriteHeader(code int) { w.code = code }

func usableMessage(m map[string]json.RawMessage) bool {
	for _, key := range []string{"content", "refusal"} {
		var text string
		if json.Unmarshal(m[key], &text) == nil && strings.TrimSpace(text) != "" {
			return true
		}
		var parts []struct {
			Text string `json:"text"`
		}
		if json.Unmarshal(m[key], &parts) == nil {
			for _, p := range parts {
				if strings.TrimSpace(p.Text) != "" {
					return true
				}
			}
		}
	}
	var calls []json.RawMessage
	if json.Unmarshal(m["tool_calls"], &calls) == nil && len(calls) > 0 {
		return true
	}
	var call map[string]json.RawMessage
	return json.Unmarshal(m["function_call"], &call) == nil && len(call) > 0
}

func usableCompletion(b []byte) bool {
	var response struct {
		Choices []struct {
			Message map[string]json.RawMessage `json:"message"`
			Delta   map[string]json.RawMessage `json:"delta"`
		} `json:"choices"`
	}
	if json.Unmarshal(b, &response) != nil {
		return false
	}
	for _, c := range response.Choices {
		if usableMessage(c.Message) || usableMessage(c.Delta) {
			return true
		}
	}
	return false
}

func (w *usableResponseWriter) commit() error {
	for k := range w.w.Header() {
		delete(w.w.Header(), k)
	}
	for k, v := range w.header {
		w.w.Header()[k] = append([]string(nil), v...)
	}
	w.w.WriteHeader(w.code)
	w.committed = true
	_, err := w.w.Write(w.buf.Bytes())
	w.buf.Reset()
	return err
}

func (w *usableResponseWriter) Write(b []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.err != nil {
		return 0, w.err
	}
	if w.committed {
		return w.w.Write(b)
	}
	if w.buf.Len()+len(b) > 1024*1024 {
		w.err = emptyCompletionError()
		return 0, w.err
	}
	w.buf.Write(b)
	if strings.HasPrefix(w.header.Get("Content-Type"), "text/event-stream") {
		w.pending = append(w.pending, b...)
		for {
			i := bytes.IndexByte(w.pending, '\n')
			if i < 0 {
				break
			}
			line := bytes.TrimSpace(w.pending[:i])
			w.pending = w.pending[i+1:]
			if bytes.HasPrefix(line, []byte("data:")) && usableCompletion(bytes.TrimSpace(line[5:])) {
				w.err = w.commit()
				break
			}
		}
	}
	return len(b), w.err
}

func (w *usableResponseWriter) Flush() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.committed {
		if f, ok := w.w.(http.Flusher); ok {
			f.Flush()
		}
	}
}

func emptyCompletionError() error {
	return &upstreamError{StatusCode: http.StatusBadGateway, Body: []byte(`{"error":{"message":"upstream returned no text or tool calls","type":"empty_completion","code":502}}`)}
}

func isEmptyCompletionError(ue *upstreamError) bool {
	if ue.StatusCode != http.StatusBadGateway {
		return false
	}
	var body struct {
		Error struct {
			Type string `json:"type"`
		} `json:"error"`
	}
	return json.Unmarshal(ue.Body, &body) == nil && body.Error.Type == "empty_completion"
}

func (w *usableResponseWriter) finish() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.err != nil {
		return w.err
	}
	if w.committed {
		return nil
	}
	if w.code == http.StatusOK && usableCompletion(w.buf.Bytes()) {
		return w.commit()
	}
	return emptyCompletionError()
}
