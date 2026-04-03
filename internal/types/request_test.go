package types

import (
	"encoding/json"
	"testing"
)

func TestContentUnmarshal_String(t *testing.T) {
	input := `"hello world"`
	var c Content
	if err := json.Unmarshal([]byte(input), &c); err != nil {
		t.Fatal(err)
	}
	if len(c.Blocks) != 1 {
		t.Fatalf("expected 1 block, got %d", len(c.Blocks))
	}
	if c.Blocks[0].Type != "text" || c.Blocks[0].Text != "hello world" {
		t.Fatalf("unexpected block: %+v", c.Blocks[0])
	}
}

func TestContentUnmarshal_Array(t *testing.T) {
	input := `[{"type":"text","text":"hello"},{"type":"text","text":" world"}]`
	var c Content
	if err := json.Unmarshal([]byte(input), &c); err != nil {
		t.Fatal(err)
	}
	if len(c.Blocks) != 2 {
		t.Fatalf("expected 2 blocks, got %d", len(c.Blocks))
	}
	if c.TextContent() != "hello world" {
		t.Fatalf("expected 'hello world', got %q", c.TextContent())
	}
}

func TestContentUnmarshal_WithMedia(t *testing.T) {
	input := `[
		{"type":"text","text":"describe this"},
		{"type":"image","source":{"type":"base64","media_type":"image/png","data":"iVBOR..."}}
	]`
	var c Content
	if err := json.Unmarshal([]byte(input), &c); err != nil {
		t.Fatal(err)
	}
	if len(c.Blocks) != 2 {
		t.Fatalf("expected 2 blocks, got %d", len(c.Blocks))
	}
	if c.TextContent() != "describe this" {
		t.Fatalf("expected 'describe this', got %q", c.TextContent())
	}
	media := c.MediaBlocks()
	if len(media) != 1 {
		t.Fatalf("expected 1 media block, got %d", len(media))
	}
	if media[0].Source.MediaType != "image/png" {
		t.Fatalf("expected image/png, got %s", media[0].Source.MediaType)
	}
}

func TestMessagesRequest_SystemAsString(t *testing.T) {
	input := `{
		"model": "claude-sonnet-4",
		"max_tokens": 1024,
		"system": "You are helpful",
		"messages": [{"role": "user", "content": "Hello"}]
	}`
	var req MessagesRequest
	if err := json.Unmarshal([]byte(input), &req); err != nil {
		t.Fatal(err)
	}
	if req.System.TextContent() != "You are helpful" {
		t.Fatalf("expected 'You are helpful', got %q", req.System.TextContent())
	}
}

func TestMessagesRequest_SystemAsArray(t *testing.T) {
	input := `{
		"model": "claude-sonnet-4",
		"max_tokens": 1024,
		"system": [{"type": "text", "text": "You are helpful."}],
		"messages": [{"role": "user", "content": "Hello"}]
	}`
	var req MessagesRequest
	if err := json.Unmarshal([]byte(input), &req); err != nil {
		t.Fatal(err)
	}
	if req.System.TextContent() != "You are helpful." {
		t.Fatalf("expected 'You are helpful.', got %q", req.System.TextContent())
	}
	if len(req.System.Blocks) != 1 {
		t.Fatalf("expected 1 block, got %d", len(req.System.Blocks))
	}
}

func TestMessagesRequest_FullParse(t *testing.T) {
	input := `{
		"model": "claude-sonnet-4",
		"max_tokens": 1024,
		"messages": [
			{"role": "user", "content": "Hello"},
			{"role": "assistant", "content": "Hi there!"},
			{"role": "user", "content": [{"type": "text", "text": "How are you?"}]}
		]
	}`
	var req MessagesRequest
	if err := json.Unmarshal([]byte(input), &req); err != nil {
		t.Fatal(err)
	}
	if req.Model != "claude-sonnet-4" {
		t.Fatalf("expected claude-sonnet-4, got %s", req.Model)
	}
	if len(req.Messages) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(req.Messages))
	}
	if req.Messages[0].Content.TextContent() != "Hello" {
		t.Fatalf("expected 'Hello', got %q", req.Messages[0].Content.TextContent())
	}
	if req.Messages[2].Content.TextContent() != "How are you?" {
		t.Fatalf("expected 'How are you?', got %q", req.Messages[2].Content.TextContent())
	}
}
