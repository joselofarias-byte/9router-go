package proxy

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestScanStreamChunks(t *testing.T) {
	streamData := []byte("data: {\"choices\":[{\"delta\":{\"content\":\"hello\"}}]}\n\ndata: [DONE]\n\n")
	buf := bytes.NewBuffer(streamData)

	var chunks [][]byte
	err := ScanStream(buf, func(chunk []byte) {
		chunks = append(chunks, chunk)
	})

	if err != nil {
		t.Fatalf("ScanStream failed: %v", err)
	}

	if len(chunks) != 2 {
		t.Errorf("expected 2 chunks, got %d", len(chunks))
	}
	if !bytes.Equal(chunks[1], []byte("[DONE]")) {
		t.Errorf("expected last chunk to be [DONE], got %s", string(chunks[1]))
	}
}

func TestScanStreamAccumulatesEvents(t *testing.T) {
	// Multi-line data payload, keep-alive, comment, and CRLF endings.
	streamData := []byte(": keep-alive\r\n" +
		"data: {\"a\":1,\r\n" +
		"data: \"b\":2}\r\n" +
		"\r\n" +
		"data: \r\n" +
		"\r\n" +
		"data: [DONE]\r\n")
	buf := bytes.NewBuffer(streamData)

	var chunks [][]byte
	err := ScanStream(buf, func(chunk []byte) {
		chunks = append(chunks, chunk)
	})

	if err != nil {
		t.Fatalf("ScanStream failed: %v", err)
	}
	if len(chunks) != 2 {
		t.Fatalf("expected 2 chunks, got %d: %q", len(chunks), chunks)
	}
	// Multi-line payload is joined with a newline.
	want := "{\"a\":1,\n\"b\":2}"
	if string(chunks[0]) != want {
		t.Errorf("expected %q, got %q", want, string(chunks[0]))
	}
	if string(chunks[1]) != "[DONE]" {
		t.Errorf("expected last chunk [DONE], got %q", string(chunks[1]))
	}
}

type mockFlusher struct {
	bytes.Buffer
	flushed bool
}

func (f *mockFlusher) Flush() {
	f.flushed = true
}

func TestStreamWriterAndWriteChunk(t *testing.T) {
	// Test StreamWriter with a flusher
	flusher := &mockFlusher{}
	sw := NewStreamWriter(flusher)

	n, err := sw.WriteChunk([]byte("hello"))
	if err != nil {
		t.Fatalf("WriteChunk failed: %v", err)
	}
	expected := "data: hello\n\n"
	if flusher.String() != expected {
		t.Errorf("expected output %q, got %q", expected, flusher.String())
	}
	if n != len(expected) {
		t.Errorf("expected length %d, got %d", len(expected), n)
	}
	if !flusher.flushed {
		t.Errorf("expected flusher to be called")
	}

	// Test WriteChunk directly
	flusher2 := &mockFlusher{}
	n2, err2 := WriteChunk(flusher2, []byte("world"))
	if err2 != nil {
		t.Fatalf("WriteChunk failed: %v", err2)
	}
	expected2 := "data: world\n\n"
	if flusher2.String() != expected2 {
		t.Errorf("expected output %q, got %q", expected2, flusher2.String())
	}
	if n2 != len(expected2) {
		t.Errorf("expected length %d, got %d", len(expected2), n2)
	}
	if !flusher2.flushed {
		t.Errorf("expected flusher to be called")
	}
}

type errorWriter struct{}

func (ew *errorWriter) Write(p []byte) (n int, err error) {
	return 0, errors.New("write error")
}

func TestStreamWriterErrors(t *testing.T) {
	ew := &errorWriter{}
	sw := NewStreamWriter(ew)

	_, err := sw.WriteChunk([]byte("hello"))
	if err == nil {
		t.Error("expected error, got nil")
	}

	_, err2 := WriteChunk(ew, []byte("hello"))
	if err2 == nil {
		t.Error("expected error, got nil")
	}
}

type mockResponseWriter struct {
	bytes.Buffer
	header  http.Header
	code    int
	flushed bool
}

func (m *mockResponseWriter) Header() http.Header {
	if m.header == nil {
		m.header = make(http.Header)
	}
	return m.header
}

func (m *mockResponseWriter) WriteHeader(code int) {
	m.code = code
}

func (m *mockResponseWriter) Flush() {
	m.flushed = true
}

func TestHeartbeatWriter_EmitsKeepAliveWhenIdle(t *testing.T) {
	rec := &mockResponseWriter{}
	hw := NewHeartbeatWriter(context.Background(), rec, 25*time.Millisecond)
	defer hw.Close()

	// Wait for 2 heartbeat ticks (idle)
	time.Sleep(70 * time.Millisecond)

	out := rec.String()
	if !strings.Contains(out, ": keep-alive\n\n") {
		t.Errorf("expected keep-alive in output, got %q", out)
	}
	if !rec.flushed {
		t.Errorf("expected flusher to be called")
	}
}

func TestHeartbeatWriter_ActiveStreamDelaysKeepAlive(t *testing.T) {
	rec := &mockResponseWriter{}
	hw := NewHeartbeatWriter(context.Background(), rec, 50*time.Millisecond)
	defer hw.Close()
	for range 4 {
		time.Sleep(15 * time.Millisecond)
		hw.Write([]byte("data: chunk\n\n"))
	}

	out := rec.String()
	if strings.Contains(out, ": keep-alive\n\n") {
		t.Errorf("did not expect keep-alive while stream was active, got %q", out)
	}
	if !strings.Contains(out, "data: chunk\n\n") {
		t.Errorf("expected data chunks, got %q", out)
	}
}

func TestHeartbeatWriter_ClosesOnContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	rec := &mockResponseWriter{}
	hw := NewHeartbeatWriter(ctx, rec, 20*time.Millisecond)

	cancel()
	time.Sleep(50 * time.Millisecond)

	// Writing after close should return ErrClosedPipe
	_, err := hw.Write([]byte("after close"))
	if err == nil {
		t.Errorf("expected write error on closed writer, got nil")
	}
}
