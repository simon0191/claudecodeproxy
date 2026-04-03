package converter

import (
	"strings"
	"testing"

	"claudecodeproxy/internal/types"
)

func TestBuildResponse(t *testing.T) {
	result := &types.CLIResult{
		Result:     "Hello! How can I help?",
		StopReason: "end_turn",
		Usage: types.CLIUsage{
			InputTokens:  100,
			OutputTokens: 25,
		},
	}
	resp := BuildResponse("claude-sonnet-4", result)

	if !strings.HasPrefix(resp.ID, "msg_") {
		t.Fatalf("expected ID to start with 'msg_', got %q", resp.ID)
	}
	if resp.Type != "message" {
		t.Fatalf("expected type 'message', got %q", resp.Type)
	}
	if resp.Role != "assistant" {
		t.Fatalf("expected role 'assistant', got %q", resp.Role)
	}
	if len(resp.Content) != 1 || resp.Content[0].Text != "Hello! How can I help?" {
		t.Fatalf("unexpected content: %+v", resp.Content)
	}
	if resp.Model != "claude-sonnet-4" {
		t.Fatalf("expected model 'claude-sonnet-4', got %q", resp.Model)
	}
	if resp.StopReason != "end_turn" {
		t.Fatalf("expected stop_reason 'end_turn', got %q", resp.StopReason)
	}
	if resp.Usage.InputTokens != 100 || resp.Usage.OutputTokens != 25 {
		t.Fatalf("unexpected usage: %+v", resp.Usage)
	}
}

func TestBuildErrorResponse(t *testing.T) {
	data := BuildErrorResponse("invalid_request_error", "model is required")
	expected := `{"type":"error","error":{"type":"invalid_request_error","message":"model is required"}}`
	if string(data) != expected {
		t.Fatalf("expected %s, got %s", expected, string(data))
	}
}
