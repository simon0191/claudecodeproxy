package converter

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestStreamResponse_CleansMessageStart(t *testing.T) {
	// Simulate CLI output with non-standard fields in message_start
	cliEvent := `{"type":"stream_event","event":{"type":"message_start","message":{"id":"msg_abc123","type":"message","role":"assistant","content":[],"model":"claude-sonnet-4-6","stop_reason":null,"stop_sequence":null,"stop_details":null,"usage":{"input_tokens":10,"cache_creation_input_tokens":100,"cache_read_input_tokens":200,"cache_creation":{"ephemeral_5m_input_tokens":0},"output_tokens":1,"service_tier":"standard","inference_geo":"not_available"}}}}`

	result := streamAndCapture(t, cliEvent, "claude-sonnet-4")

	events := parseSSEEvents(t, result)
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}

	ev := events[0]
	if ev.name != "message_start" {
		t.Fatalf("expected event name 'message_start', got %q", ev.name)
	}

	// Parse the cleaned event data
	var data map[string]interface{}
	if err := json.Unmarshal([]byte(ev.data), &data); err != nil {
		t.Fatalf("failed to parse event data: %v", err)
	}

	msg := data["message"].(map[string]interface{})

	// Model should be remapped to API model
	if msg["model"] != "claude-sonnet-4" {
		t.Errorf("expected model 'claude-sonnet-4', got %v", msg["model"])
	}

	// Non-standard fields should be absent
	if _, ok := msg["stop_details"]; ok {
		t.Error("stop_details should be stripped from message")
	}

	usage := msg["usage"].(map[string]interface{})
	for _, field := range []string{"cache_creation_input_tokens", "cache_read_input_tokens", "cache_creation", "service_tier", "inference_geo"} {
		if _, ok := usage[field]; ok {
			t.Errorf("non-standard field %q should be stripped from usage", field)
		}
	}

	// Standard fields should be present
	if usage["input_tokens"] != float64(10) {
		t.Errorf("expected input_tokens=10, got %v", usage["input_tokens"])
	}
	if usage["output_tokens"] != float64(1) {
		t.Errorf("expected output_tokens=1, got %v", usage["output_tokens"])
	}
}

func TestStreamResponse_CleansMessageDelta(t *testing.T) {
	cliEvent := `{"type":"stream_event","event":{"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null,"stop_details":null},"usage":{"input_tokens":10,"output_tokens":50,"cache_creation_input_tokens":0,"cache_read_input_tokens":0},"context_management":{"applied_edits":[]}}}`

	result := streamAndCapture(t, cliEvent, "claude-sonnet-4")

	events := parseSSEEvents(t, result)
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}

	var data map[string]interface{}
	if err := json.Unmarshal([]byte(events[0].data), &data); err != nil {
		t.Fatalf("failed to parse event data: %v", err)
	}

	// Non-standard top-level fields should be absent
	if _, ok := data["context_management"]; ok {
		t.Error("context_management should be stripped")
	}

	delta := data["delta"].(map[string]interface{})
	if _, ok := delta["stop_details"]; ok {
		t.Error("stop_details should be stripped from delta")
	}
	if delta["stop_reason"] != "end_turn" {
		t.Errorf("expected stop_reason 'end_turn', got %v", delta["stop_reason"])
	}

	usage := data["usage"].(map[string]interface{})
	if usage["output_tokens"] != float64(50) {
		t.Errorf("expected output_tokens=50, got %v", usage["output_tokens"])
	}
	// Usage should only have output_tokens for message_delta
	if _, ok := usage["input_tokens"]; ok {
		t.Error("input_tokens should not be in message_delta usage")
	}
}

func TestStreamResponse_PassesThroughContentBlockEvents(t *testing.T) {
	cliEvent := `{"type":"stream_event","event":{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}}`

	result := streamAndCapture(t, cliEvent, "claude-sonnet-4")

	events := parseSSEEvents(t, result)
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}

	// Content block events should pass through unchanged
	var original, cleaned map[string]interface{}
	origJSON := `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}`
	json.Unmarshal([]byte(origJSON), &original)
	json.Unmarshal([]byte(events[0].data), &cleaned)

	origBytes, _ := json.Marshal(original)
	cleanBytes, _ := json.Marshal(cleaned)
	if string(origBytes) != string(cleanBytes) {
		t.Errorf("content_block_delta should pass through unchanged.\nExpected: %s\nGot:      %s", origBytes, cleanBytes)
	}
}

func TestStreamResponse_SkipsNonStreamEvents(t *testing.T) {
	input := strings.Join([]string{
		`{"type":"system","message":"hello"}`,
		`{"type":"stream_event","event":{"type":"ping"}}`,
		`{"type":"result","result":"done"}`,
	}, "\n")

	result := streamAndCapture(t, input, "claude-sonnet-4")

	events := parseSSEEvents(t, result)
	if len(events) != 1 {
		t.Fatalf("expected 1 event (ping), got %d", len(events))
	}
	if events[0].name != "ping" {
		t.Errorf("expected ping event, got %q", events[0].name)
	}
}

// --- helpers ---

type sseEvent struct {
	name string
	data string
}

func streamAndCapture(t *testing.T, input string, apiModel string) string {
	t.Helper()
	recorder := httptest.NewRecorder()
	reader := strings.NewReader(input)

	err := StreamResponse(context.Background(), apiModel, reader, recorder)
	if err != nil {
		t.Fatalf("StreamResponse error: %v", err)
	}

	if ct := recorder.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("expected Content-Type 'text/event-stream', got %q", ct)
	}

	return recorder.Body.String()
}

func parseSSEEvents(t *testing.T, raw string) []sseEvent {
	t.Helper()
	var events []sseEvent
	blocks := strings.Split(strings.TrimSpace(raw), "\n\n")
	for _, block := range blocks {
		if block == "" {
			continue
		}
		var ev sseEvent
		for _, line := range strings.Split(block, "\n") {
			if strings.HasPrefix(line, "event: ") {
				ev.name = strings.TrimPrefix(line, "event: ")
			} else if strings.HasPrefix(line, "data: ") {
				ev.data = strings.TrimPrefix(line, "data: ")
			}
		}
		events = append(events, ev)
	}
	return events
}

// Ensure httptest.ResponseRecorder implements http.Flusher (compile-time check).
var _ http.Flusher = httptest.NewRecorder()
