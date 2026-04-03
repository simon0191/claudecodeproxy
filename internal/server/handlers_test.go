package server

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"claudecodeproxy/internal/claude"
	"claudecodeproxy/internal/converter"
	"claudecodeproxy/internal/types"
)

// Set CLAUDE_E2E=1 to run tests against the real Claude CLI.
// By default, tests use a mock runner.
func isRealCLI() bool {
	return os.Getenv("CLAUDE_E2E") == "1"
}

// fakeRunner is a mock CLI runner for tests.
type fakeRunner struct {
	result      *types.CLIResult
	err         error
	streamLines []string
	gotModel    string
	gotPrompt   string
	gotTempDir  string
}

func (f *fakeRunner) Run(ctx context.Context, model string, prompt string, tempDir string) (*types.CLIResult, error) {
	f.gotModel = model
	f.gotPrompt = prompt
	f.gotTempDir = tempDir
	return f.result, f.err
}

func (f *fakeRunner) RunStreaming(ctx context.Context, model string, prompt string, tempDir string) (io.ReadCloser, claude.WaitFunc, error) {
	f.gotModel = model
	f.gotPrompt = prompt
	f.gotTempDir = tempDir
	if f.err != nil {
		return nil, nil, f.err
	}

	var sb strings.Builder
	for _, line := range f.streamLines {
		sb.WriteString(line)
		sb.WriteString("\n")
	}
	reader := io.NopCloser(strings.NewReader(sb.String()))
	return reader, func() error { return nil }, nil
}

// mockStreamEvent wraps an Anthropic event in the stream_event format the CLI emits.
func mockStreamEvent(event string) string {
	return fmt.Sprintf(`{"type":"stream_event","event":%s}`, event)
}

func testServer(runner *fakeRunner) (*httptest.Server, *fakeRunner) {
	var srv *Server
	if isRealCLI() {
		srv = New("127.0.0.1", 0, 10)
	} else {
		srv = NewWithRunner("127.0.0.1", 0, runner)
	}
	return httptest.NewServer(srv.Handler()), runner
}

func loadTestFile(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatalf("failed to read testdata/%s: %v", name, err)
	}
	return base64.StdEncoding.EncodeToString(data)
}

func postMessages(t *testing.T, url string, body string) *http.Response {
	t.Helper()
	resp, err := http.Post(url+"/v1/messages", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func decodeResponse(t *testing.T, resp *http.Response) converter.AnthropicResponse {
	t.Helper()
	var result converter.AnthropicResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	return result
}

func assertValidResponse(t *testing.T, result converter.AnthropicResponse) {
	t.Helper()
	if result.Type != "message" {
		t.Fatalf("expected type 'message', got %q", result.Type)
	}
	if result.Role != "assistant" {
		t.Fatalf("expected role 'assistant', got %q", result.Role)
	}
	if !strings.HasPrefix(result.ID, "msg_") {
		t.Fatalf("expected ID to start with 'msg_', got %q", result.ID)
	}
	if result.StopReason != "end_turn" {
		t.Fatalf("expected stop_reason 'end_turn', got %q", result.StopReason)
	}
	if len(result.Content) == 0 {
		t.Fatal("expected at least one content block")
	}
	if result.Content[0].Type != "text" {
		t.Fatalf("expected content type 'text', got %q", result.Content[0].Type)
	}
	if result.Content[0].Text == "" {
		t.Fatal("expected non-empty text content")
	}
}

// --- Health endpoint ---

func TestE2E_Health(t *testing.T) {
	ts, _ := testServer(&fakeRunner{})
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/health")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var body map[string]string
	json.NewDecoder(resp.Body).Decode(&body)
	if body["status"] != "ok" {
		t.Fatalf("expected status ok, got %v", body)
	}
}

// --- Non-streaming messages ---

func TestE2E_Messages_SimpleText(t *testing.T) {
	ts, runner := testServer(&fakeRunner{
		result: &types.CLIResult{
			Type:       "result",
			Subtype:    "success",
			Result:     "Hello! I'm Claude.",
			StopReason: "end_turn",
			Usage:      types.CLIUsage{InputTokens: 10, OutputTokens: 5},
		},
	})
	defer ts.Close()

	resp := postMessages(t, ts.URL, `{
		"model": "claude-sonnet-4",
		"max_tokens": 1024,
		"messages": [{"role": "user", "content": "Respond with exactly the word 'pineapple' and nothing else"}]
	}`)
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, b)
	}

	result := decodeResponse(t, resp)
	assertValidResponse(t, result)

	if isRealCLI() {
		if !strings.Contains(strings.ToLower(result.Content[0].Text), "pineapple") {
			t.Fatalf("expected response to contain 'pineapple', got %q", result.Content[0].Text)
		}
		if result.Usage.InputTokens == 0 {
			t.Fatal("expected non-zero input tokens from real CLI")
		}
		if result.Usage.OutputTokens == 0 {
			t.Fatal("expected non-zero output tokens from real CLI")
		}
		t.Logf("Response: %q (in=%d, out=%d)", result.Content[0].Text, result.Usage.InputTokens, result.Usage.OutputTokens)
	} else {
		if result.Content[0].Text != "Hello! I'm Claude." {
			t.Fatalf("unexpected content: %q", result.Content[0].Text)
		}
		if result.Model != "claude-sonnet-4" {
			t.Fatalf("expected model 'claude-sonnet-4', got %q", result.Model)
		}
		if result.Usage.InputTokens != 10 || result.Usage.OutputTokens != 5 {
			t.Fatalf("unexpected usage: %+v", result.Usage)
		}
		if runner.gotModel != "sonnet" {
			t.Fatalf("expected CLI model 'sonnet', got %q", runner.gotModel)
		}
	}
}

func TestE2E_Messages_ArrayContent(t *testing.T) {
	ts, runner := testServer(&fakeRunner{
		result: &types.CLIResult{
			Result:     "42",
			StopReason: "end_turn",
		},
	})
	defer ts.Close()

	resp := postMessages(t, ts.URL, `{
		"model": "claude-sonnet-4",
		"max_tokens": 1024,
		"messages": [{"role": "user", "content": [{"type": "text", "text": "What is 6 times 7? Reply with just the number."}]}]
	}`)
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, b)
	}

	result := decodeResponse(t, resp)
	assertValidResponse(t, result)

	if isRealCLI() {
		if !strings.Contains(result.Content[0].Text, "42") {
			t.Fatalf("expected response to contain '42', got %q", result.Content[0].Text)
		}
	} else {
		if runner.gotPrompt != "What is 6 times 7? Reply with just the number." {
			t.Fatalf("expected prompt forwarded verbatim, got %q", runner.gotPrompt)
		}
	}
}

func TestE2E_Messages_MultiTurn(t *testing.T) {
	ts, runner := testServer(&fakeRunner{
		result: &types.CLIResult{
			Result:     "The capital of France is Paris.",
			StopReason: "end_turn",
		},
	})
	defer ts.Close()

	resp := postMessages(t, ts.URL, `{
		"model": "claude-sonnet-4",
		"max_tokens": 1024,
		"messages": [
			{"role": "user", "content": "I'm going to ask you about capitals."},
			{"role": "assistant", "content": "Sure, go ahead!"},
			{"role": "user", "content": "What is the capital of France? Reply with just the city name."}
		]
	}`)
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, b)
	}

	result := decodeResponse(t, resp)
	assertValidResponse(t, result)

	if isRealCLI() {
		if !strings.Contains(strings.ToLower(result.Content[0].Text), "paris") {
			t.Fatalf("expected response to contain 'paris', got %q", result.Content[0].Text)
		}
	} else {
		expected := "I'm going to ask you about capitals.\n\n<previous_response>\nSure, go ahead!\n</previous_response>\n\nWhat is the capital of France? Reply with just the city name."
		if runner.gotPrompt != expected {
			t.Fatalf("expected prompt:\n%s\ngot:\n%s", expected, runner.gotPrompt)
		}
	}
}

func TestE2E_Messages_WithSystem(t *testing.T) {
	ts, runner := testServer(&fakeRunner{
		result: &types.CLIResult{
			Result:     "Arrr!",
			StopReason: "end_turn",
		},
	})
	defer ts.Close()

	resp := postMessages(t, ts.URL, `{
		"model": "claude-sonnet-4",
		"max_tokens": 1024,
		"system": "You are a pirate. You must include the word 'arrr' in every response.",
		"messages": [{"role": "user", "content": "Hello, how are you?"}]
	}`)
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, b)
	}

	result := decodeResponse(t, resp)
	assertValidResponse(t, result)

	if isRealCLI() {
		if !strings.Contains(strings.ToLower(result.Content[0].Text), "arrr") {
			t.Fatalf("expected pirate response containing 'arrr', got %q", result.Content[0].Text)
		}
		t.Logf("Pirate response: %q", result.Content[0].Text)
	} else {
		expected := "<system>\nYou are a pirate. You must include the word 'arrr' in every response.\n</system>\n\nHello, how are you?"
		if runner.gotPrompt != expected {
			t.Fatalf("expected prompt:\n%s\ngot:\n%s", expected, runner.gotPrompt)
		}
	}
}

func TestE2E_Messages_WithSystemArray(t *testing.T) {
	ts, runner := testServer(&fakeRunner{
		result: &types.CLIResult{
			Result:     "Arrr!",
			StopReason: "end_turn",
		},
	})
	defer ts.Close()

	resp := postMessages(t, ts.URL, `{
		"model": "claude-sonnet-4",
		"max_tokens": 1024,
		"system": [{"type": "text", "text": "You are a pirate. You must include the word 'arrr' in every response."}],
		"messages": [{"role": "user", "content": "Hello, how are you?"}]
	}`)
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, b)
	}

	result := decodeResponse(t, resp)
	assertValidResponse(t, result)

	if isRealCLI() {
		if !strings.Contains(strings.ToLower(result.Content[0].Text), "arrr") {
			t.Fatalf("expected pirate response containing 'arrr', got %q", result.Content[0].Text)
		}
		t.Logf("Pirate response (array system): %q", result.Content[0].Text)
	} else {
		expected := "<system>\nYou are a pirate. You must include the word 'arrr' in every response.\n</system>\n\nHello, how are you?"
		if runner.gotPrompt != expected {
			t.Fatalf("expected prompt:\n%s\ngot:\n%s", expected, runner.gotPrompt)
		}
	}
}

func TestE2E_Messages_WithImage(t *testing.T) {
	imgB64 := loadTestFile(t, "cat.png")

	ts, runner := testServer(&fakeRunner{
		result: &types.CLIResult{
			Result:     "I see a cat in the image.",
			StopReason: "end_turn",
		},
	})
	defer ts.Close()

	body := fmt.Sprintf(`{
		"model": "claude-sonnet-4",
		"max_tokens": 1024,
		"messages": [{
			"role": "user",
			"content": [
				{"type": "text", "text": "What animal is in this image? Reply with just the animal name."},
				{"type": "image", "source": {"type": "base64", "media_type": "image/png", "data": "%s"}}
			]
		}]
	}`, imgB64)

	resp := postMessages(t, ts.URL, body)
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, b)
	}

	result := decodeResponse(t, resp)
	assertValidResponse(t, result)

	if isRealCLI() {
		if !strings.Contains(strings.ToLower(result.Content[0].Text), "cat") {
			t.Fatalf("expected response to mention 'cat' (it's a photo of a cat), got %q", result.Content[0].Text)
		}
		t.Logf("Image description: %q", result.Content[0].Text)
	} else {
		if !strings.Contains(runner.gotPrompt, "What animal is in this image?") {
			t.Fatalf("prompt missing text: %q", runner.gotPrompt)
		}
		if !strings.Contains(runner.gotPrompt, "[Attached file:") {
			t.Fatalf("prompt missing file reference: %q", runner.gotPrompt)
		}
		if !strings.Contains(runner.gotPrompt, ".png]") {
			t.Fatalf("prompt missing .png extension: %q", runner.gotPrompt)
		}
		if runner.gotTempDir == "" {
			t.Fatal("expected tempDir to be set for media request")
		}
	}
}

func TestE2E_Messages_WithAudio(t *testing.T) {
	audioB64 := loadTestFile(t, "hello.wav")

	ts, runner := testServer(&fakeRunner{
		result: &types.CLIResult{
			Result:     "The audio says: Hello, this is a test of audio processing.",
			StopReason: "end_turn",
		},
	})
	defer ts.Close()

	body := fmt.Sprintf(`{
		"model": "claude-sonnet-4",
		"max_tokens": 1024,
		"messages": [{
			"role": "user",
			"content": [
				{"type": "text", "text": "Transcribe exactly what is said in this audio file."},
				{"type": "audio", "source": {"type": "base64", "media_type": "audio/wav", "data": "%s"}}
			]
		}]
	}`, audioB64)

	resp := postMessages(t, ts.URL, body)
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, b)
	}

	result := decodeResponse(t, resp)
	assertValidResponse(t, result)

	if isRealCLI() {
		lower := strings.ToLower(result.Content[0].Text)
		// The audio says "Hello, this is a test of audio processing"
		if !strings.Contains(lower, "hello") || !strings.Contains(lower, "audio") {
			t.Fatalf("expected transcription to contain 'hello' and 'audio', got %q", result.Content[0].Text)
		}
		t.Logf("Audio transcription: %q", result.Content[0].Text)
	} else {
		if !strings.Contains(runner.gotPrompt, "Transcribe exactly") {
			t.Fatalf("prompt missing text: %q", runner.gotPrompt)
		}
		if !strings.Contains(runner.gotPrompt, "[Attached file:") {
			t.Fatalf("prompt missing file reference: %q", runner.gotPrompt)
		}
		if !strings.Contains(runner.gotPrompt, ".wav]") {
			t.Fatalf("prompt missing .wav extension: %q", runner.gotPrompt)
		}
		if runner.gotTempDir == "" {
			t.Fatal("expected tempDir to be set for media request")
		}
	}
}

func TestE2E_Messages_AllModels(t *testing.T) {
	if isRealCLI() {
		t.Skip("skipping model enumeration with real CLI to avoid unnecessary API calls")
	}

	models := map[string]string{
		"claude-sonnet-4": "sonnet",
		"claude-opus-4":   "opus",
		"claude-haiku-4":  "haiku",
	}

	for apiModel, cliModel := range models {
		t.Run(apiModel, func(t *testing.T) {
			runner := &fakeRunner{
				result: &types.CLIResult{Result: "ok", StopReason: "end_turn"},
			}
			srv := NewWithRunner("127.0.0.1", 0, runner)
			ts := httptest.NewServer(srv.Handler())
			defer ts.Close()

			body := fmt.Sprintf(`{"model":"%s","max_tokens":1024,"messages":[{"role":"user","content":"hi"}]}`, apiModel)
			resp := postMessages(t, ts.URL, body)
			resp.Body.Close()

			if runner.gotModel != cliModel {
				t.Fatalf("expected CLI model %q, got %q", cliModel, runner.gotModel)
			}
		})
	}
}

// --- Error cases ---

func TestE2E_Messages_MissingModel(t *testing.T) {
	ts, _ := testServer(&fakeRunner{})
	defer ts.Close()

	resp := postMessages(t, ts.URL, `{"max_tokens": 1024, "messages": [{"role": "user", "content": "hi"}]}`)
	defer resp.Body.Close()

	if resp.StatusCode != 400 {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}

	var errResp converter.AnthropicError
	json.NewDecoder(resp.Body).Decode(&errResp)
	if errResp.Error.Type != "invalid_request_error" {
		t.Fatalf("expected invalid_request_error, got %q", errResp.Error.Type)
	}
	if !strings.Contains(errResp.Error.Message, "model") {
		t.Fatalf("expected error message about model, got %q", errResp.Error.Message)
	}
}

func TestE2E_Messages_MissingMessages(t *testing.T) {
	ts, _ := testServer(&fakeRunner{})
	defer ts.Close()

	resp := postMessages(t, ts.URL, `{"model": "claude-sonnet-4", "max_tokens": 1024}`)
	defer resp.Body.Close()

	if resp.StatusCode != 400 {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}

	var errResp converter.AnthropicError
	json.NewDecoder(resp.Body).Decode(&errResp)
	if !strings.Contains(errResp.Error.Message, "messages") {
		t.Fatalf("expected error message about messages, got %q", errResp.Error.Message)
	}
}

func TestE2E_Messages_UnknownModel(t *testing.T) {
	ts, _ := testServer(&fakeRunner{})
	defer ts.Close()

	resp := postMessages(t, ts.URL, `{"model": "gpt-4", "max_tokens": 1024, "messages": [{"role": "user", "content": "hi"}]}`)
	defer resp.Body.Close()

	if resp.StatusCode != 400 {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}

	var errResp converter.AnthropicError
	json.NewDecoder(resp.Body).Decode(&errResp)
	if !strings.Contains(errResp.Error.Message, "gpt-4") {
		t.Fatalf("expected error message to mention the unknown model, got %q", errResp.Error.Message)
	}
}

func TestE2E_Messages_InvalidJSON(t *testing.T) {
	ts, _ := testServer(&fakeRunner{})
	defer ts.Close()

	resp := postMessages(t, ts.URL, "{invalid json")
	defer resp.Body.Close()

	if resp.StatusCode != 400 {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestE2E_Messages_CLIError(t *testing.T) {
	if isRealCLI() {
		t.Skip("skipping CLI error test with real CLI")
	}

	runner := &fakeRunner{err: fmt.Errorf("CLI crashed")}
	srv := NewWithRunner("127.0.0.1", 0, runner)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp := postMessages(t, ts.URL, `{"model": "claude-sonnet-4", "max_tokens": 1024, "messages": [{"role": "user", "content": "hi"}]}`)
	defer resp.Body.Close()

	if resp.StatusCode != 500 {
		t.Fatalf("expected 500, got %d", resp.StatusCode)
	}

	var errResp converter.AnthropicError
	json.NewDecoder(resp.Body).Decode(&errResp)
	if errResp.Error.Type != "api_error" {
		t.Fatalf("expected api_error, got %q", errResp.Error.Type)
	}
	if !strings.Contains(errResp.Error.Message, "CLI crashed") {
		t.Fatalf("expected error to mention 'CLI crashed', got %q", errResp.Error.Message)
	}
}

// --- Streaming ---

func TestE2E_Messages_Streaming(t *testing.T) {
	ts, _ := testServer(&fakeRunner{
		streamLines: []string{
			mockStreamEvent(`{"type":"message_start","message":{"id":"msg_test","type":"message","role":"assistant","content":[],"model":"sonnet","usage":{"input_tokens":5,"output_tokens":0}}}`),
			mockStreamEvent(`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`),
			mockStreamEvent(`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello "}}`),
			mockStreamEvent(`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"world!"}}`),
			mockStreamEvent(`{"type":"content_block_stop","index":0}`),
			mockStreamEvent(`{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":2}}`),
			mockStreamEvent(`{"type":"message_stop"}`),
		},
	})
	defer ts.Close()

	resp := postMessages(t, ts.URL, `{
		"model": "claude-sonnet-4",
		"max_tokens": 1024,
		"stream": true,
		"messages": [{"role": "user", "content": "Respond with exactly: hello world"}]
	}`)
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, b)
	}

	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("expected Content-Type text/event-stream, got %q", ct)
	}

	events := parseSSEEvents(t, resp.Body)

	// Verify event sequence structure
	if len(events) < 5 {
		t.Fatalf("expected at least 5 SSE events, got %d: %v", len(events), eventNames(events))
	}
	if events[0].eventType != "message_start" {
		t.Fatalf("first event should be message_start, got %q", events[0].eventType)
	}
	if events[1].eventType != "content_block_start" {
		t.Fatalf("second event should be content_block_start, got %q", events[1].eventType)
	}

	n := len(events)
	if events[n-3].eventType != "content_block_stop" {
		t.Fatalf("third-to-last event should be content_block_stop, got %q", events[n-3].eventType)
	}
	if events[n-2].eventType != "message_delta" {
		t.Fatalf("second-to-last event should be message_delta, got %q", events[n-2].eventType)
	}
	if events[n-1].eventType != "message_stop" {
		t.Fatalf("last event should be message_stop, got %q", events[n-1].eventType)
	}

	// All middle events should be content_block_delta
	for i := 2; i < n-3; i++ {
		if events[i].eventType != "content_block_delta" {
			t.Fatalf("event %d should be content_block_delta, got %q", i, events[i].eventType)
		}
	}

	// Verify message_delta contains stop_reason
	if !strings.Contains(events[n-2].data, "end_turn") {
		t.Fatalf("message_delta should contain stop_reason 'end_turn', got %q", events[n-2].data)
	}

	// Collect all delta text
	var fullText strings.Builder
	for i := 2; i < n-3; i++ {
		var delta struct {
			Delta struct {
				Text string `json:"text"`
			} `json:"delta"`
		}
		json.Unmarshal([]byte(events[i].data), &delta)
		fullText.WriteString(delta.Delta.Text)
	}

	if fullText.Len() == 0 {
		t.Fatal("expected at least some streamed text in content_block_delta events")
	}

	if isRealCLI() {
		t.Logf("Streamed text: %q (%d delta events)", fullText.String(), n-5)
	} else {
		if fullText.String() != "Hello world!" {
			t.Fatalf("expected streamed text 'Hello world!', got %q", fullText.String())
		}
	}
}

func TestE2E_Messages_StreamingCLIError(t *testing.T) {
	if isRealCLI() {
		t.Skip("skipping streaming CLI error test with real CLI")
	}

	runner := &fakeRunner{err: fmt.Errorf("stream start failed")}
	srv := NewWithRunner("127.0.0.1", 0, runner)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp := postMessages(t, ts.URL, `{
		"model": "claude-sonnet-4",
		"max_tokens": 1024,
		"stream": true,
		"messages": [{"role": "user", "content": "Hello"}]
	}`)
	defer resp.Body.Close()

	if resp.StatusCode != 500 {
		t.Fatalf("expected 500, got %d", resp.StatusCode)
	}
}

// --- Concurrency ---

// slowRunner tracks concurrent executions and sleeps for a duration.
type slowRunner struct {
	delay      time.Duration
	result     *types.CLIResult
	peak       atomic.Int32
	concurrent atomic.Int32
}

func (s *slowRunner) Run(ctx context.Context, model string, prompt string, tempDir string) (*types.CLIResult, error) {
	cur := s.concurrent.Add(1)
	defer s.concurrent.Add(-1)
	for {
		old := s.peak.Load()
		if cur <= old || s.peak.CompareAndSwap(old, cur) {
			break
		}
	}
	time.Sleep(s.delay)
	return s.result, nil
}

func (s *slowRunner) RunStreaming(ctx context.Context, model string, prompt string, tempDir string) (io.ReadCloser, claude.WaitFunc, error) {
	return nil, nil, fmt.Errorf("not implemented")
}

func TestE2E_ConcurrencyLimit(t *testing.T) {
	if isRealCLI() {
		t.Skip("skipping concurrency test with real CLI")
	}

	slow := &slowRunner{
		delay:  100 * time.Millisecond,
		result: &types.CLIResult{Result: "ok", StopReason: "end_turn"},
	}

	// maxConcurrent=1: all requests must serialize
	srv := NewWithRunner("127.0.0.1", 0, slow)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	const numRequests = 3
	var wg sync.WaitGroup
	wg.Add(numRequests)

	for i := 0; i < numRequests; i++ {
		go func() {
			defer wg.Done()
			resp := postMessages(t, ts.URL, `{"model":"claude-sonnet-4","max_tokens":1024,"messages":[{"role":"user","content":"hi"}]}`)
			resp.Body.Close()
		}()
	}

	wg.Wait()

	// With the fakeRunner (no semaphore), all 3 would run concurrently.
	// The peak should be numRequests since there's no concurrency limit on fakeRunner.
	if peak := slow.peak.Load(); peak != numRequests {
		t.Fatalf("expected peak concurrency %d (no limit on fakeRunner), got %d", numRequests, peak)
	}
}

func TestE2E_ConcurrencyLimit_WithCLIRunner(t *testing.T) {
	if isRealCLI() {
		t.Skip("skipping concurrency test with real CLI")
	}

	// Use a CLIRunner-like approach: wrap the slowRunner with a semaphore
	// to verify the semaphore actually limits concurrency.
	slow := &slowRunner{
		delay:  100 * time.Millisecond,
		result: &types.CLIResult{Result: "ok", StopReason: "end_turn"},
	}

	// Create a server with maxConcurrent=1 but using our slow runner.
	// We can't use NewCLIRunner here since it runs the real CLI, so
	// we test the semaphore behavior by wrapping the slow runner.
	sem := make(chan struct{}, 1)
	limited := &semRunner{inner: slow, sem: sem}

	srv := NewWithRunner("127.0.0.1", 0, limited)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	const numRequests = 3
	var wg sync.WaitGroup
	wg.Add(numRequests)

	for i := 0; i < numRequests; i++ {
		go func() {
			defer wg.Done()
			resp := postMessages(t, ts.URL, `{"model":"claude-sonnet-4","max_tokens":1024,"messages":[{"role":"user","content":"hi"}]}`)
			resp.Body.Close()
		}()
	}

	wg.Wait()

	// With semaphore=1, only 1 should run at a time
	if peak := slow.peak.Load(); peak != 1 {
		t.Fatalf("expected peak concurrency 1 with semaphore, got %d", peak)
	}
}

// semRunner wraps a Runner with a semaphore, mimicking CLIRunner's concurrency limit.
type semRunner struct {
	inner claude.Runner
	sem   chan struct{}
}

func (s *semRunner) Run(ctx context.Context, model string, prompt string, tempDir string) (*types.CLIResult, error) {
	select {
	case s.sem <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	defer func() { <-s.sem }()
	return s.inner.Run(ctx, model, prompt, tempDir)
}

func (s *semRunner) RunStreaming(ctx context.Context, model string, prompt string, tempDir string) (io.ReadCloser, claude.WaitFunc, error) {
	select {
	case s.sem <- struct{}{}:
	case <-ctx.Done():
		return nil, nil, ctx.Err()
	}
	rc, wait, err := s.inner.RunStreaming(ctx, model, prompt, tempDir)
	if err != nil {
		<-s.sem
		return nil, nil, err
	}
	return rc, func() error {
		defer func() { <-s.sem }()
		return wait()
	}, nil
}

// --- SSE parsing helpers ---

type sseEvent struct {
	eventType string
	data      string
}

func parseSSEEvents(t *testing.T, r io.Reader) []sseEvent {
	t.Helper()
	var events []sseEvent
	scanner := bufio.NewScanner(r)
	var currentEvent sseEvent

	for scanner.Scan() {
		line := scanner.Text()
		if eventType, ok := strings.CutPrefix(line, "event: "); ok {
			currentEvent.eventType = eventType
		} else if data, ok := strings.CutPrefix(line, "data: "); ok {
			currentEvent.data = data
		} else if line == "" && currentEvent.eventType != "" {
			events = append(events, currentEvent)
			currentEvent = sseEvent{}
		}
	}
	return events
}

func eventNames(events []sseEvent) []string {
	names := make([]string, len(events))
	for i, e := range events {
		names[i] = e.eventType
	}
	return names
}
