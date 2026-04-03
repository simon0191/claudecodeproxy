package converter

import (
	"testing"

	"claudecodeproxy/internal/types"
)

func TestConvertMessages_SingleUser(t *testing.T) {
	msgs := []types.Message{
		{Role: "user", Content: types.Content{Blocks: []types.ContentBlock{{Type: "text", Text: "Hello"}}}},
	}
	prompt, files, err := ConvertMessages("", msgs)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 0 {
		t.Fatalf("expected no temp files, got %d", len(files))
	}
	if prompt != "Hello" {
		t.Fatalf("expected 'Hello', got %q", prompt)
	}
}

func TestConvertMessages_WithSystem(t *testing.T) {
	msgs := []types.Message{
		{Role: "user", Content: types.Content{Blocks: []types.ContentBlock{{Type: "text", Text: "Hi"}}}},
	}
	prompt, _, err := ConvertMessages("You are helpful", msgs)
	if err != nil {
		t.Fatal(err)
	}
	expected := "<system>\nYou are helpful\n</system>\n\nHi"
	if prompt != expected {
		t.Fatalf("expected %q, got %q", expected, prompt)
	}
}

func TestConvertMessages_MultiTurn(t *testing.T) {
	msgs := []types.Message{
		{Role: "user", Content: types.Content{Blocks: []types.ContentBlock{{Type: "text", Text: "Hello"}}}},
		{Role: "assistant", Content: types.Content{Blocks: []types.ContentBlock{{Type: "text", Text: "Hi there!"}}}},
		{Role: "user", Content: types.Content{Blocks: []types.ContentBlock{{Type: "text", Text: "How are you?"}}}},
	}
	prompt, _, err := ConvertMessages("", msgs)
	if err != nil {
		t.Fatal(err)
	}
	expected := "Hello\n\n<previous_response>\nHi there!\n</previous_response>\n\nHow are you?"
	if prompt != expected {
		t.Fatalf("expected %q, got %q", expected, prompt)
	}
}

func TestConvertMessages_Empty(t *testing.T) {
	prompt, files, err := ConvertMessages("", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 0 {
		t.Fatalf("expected no temp files, got %d", len(files))
	}
	if prompt != "" {
		t.Fatalf("expected empty prompt, got %q", prompt)
	}
}
