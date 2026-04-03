package types

import (
	"encoding/json"
	"testing"
)

func TestCLIResult_Parse(t *testing.T) {
	input := `{
		"type": "result",
		"subtype": "success",
		"is_error": false,
		"duration_ms": 19252,
		"result": "Hello! How can I help you?",
		"stop_reason": "end_turn",
		"session_id": "d4a0ee0b-04b0-4208-9284-01d291bfebcf",
		"total_cost_usd": 0.025798,
		"usage": {
			"input_tokens": 100,
			"output_tokens": 50,
			"cache_read_input_tokens": 34616,
			"cache_creation_input_tokens": 0
		}
	}`
	var result CLIResult
	if err := json.Unmarshal([]byte(input), &result); err != nil {
		t.Fatal(err)
	}
	if result.Type != "result" {
		t.Fatalf("expected type 'result', got %q", result.Type)
	}
	if result.IsError {
		t.Fatal("expected is_error to be false")
	}
	if result.Result != "Hello! How can I help you?" {
		t.Fatalf("unexpected result: %q", result.Result)
	}
	if result.StopReason != "end_turn" {
		t.Fatalf("expected stop_reason 'end_turn', got %q", result.StopReason)
	}
	if result.Usage.InputTokens != 100 {
		t.Fatalf("expected 100 input tokens, got %d", result.Usage.InputTokens)
	}
	if result.Usage.OutputTokens != 50 {
		t.Fatalf("expected 50 output tokens, got %d", result.Usage.OutputTokens)
	}
	if result.Usage.CacheReadInputTokens != 34616 {
		t.Fatalf("expected 34616 cache_read, got %d", result.Usage.CacheReadInputTokens)
	}
}
