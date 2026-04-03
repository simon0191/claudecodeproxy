package proxy

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// --- Passthrough tests ---

func TestPassthrough_ForwardsRequestBody(t *testing.T) {
	var gotBody string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"id":"msg_test","type":"message","role":"assistant","content":[{"type":"text","text":"hello"}],"model":"claude-sonnet-4","stop_reason":"end_turn"}`))
	}))
	defer upstream.Close()

	p := NewPassthrough(AuthConfig{APIKey: "test-key"}, upstream.URL)
	req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(`{"model":"claude-sonnet-4","messages":[{"role":"user","content":"hi"}]}`))
	w := httptest.NewRecorder()

	p.Handle(w, req)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if gotBody != `{"model":"claude-sonnet-4","messages":[{"role":"user","content":"hi"}]}` {
		t.Fatalf("request body not forwarded correctly: %s", gotBody)
	}
}

func TestPassthrough_InjectsAuthHeaders(t *testing.T) {
	var gotHeaders http.Header
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeaders = r.Header.Clone()
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"type":"message"}`))
	}))
	defer upstream.Close()

	p := NewPassthrough(AuthConfig{APIKey: "sk-ant-test-key"}, upstream.URL)
	req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(`{}`))
	w := httptest.NewRecorder()

	p.Handle(w, req)

	if got := gotHeaders.Get("x-api-key"); got != "sk-ant-test-key" {
		t.Fatalf("expected x-api-key 'sk-ant-test-key', got %q", got)
	}
	if got := gotHeaders.Get("anthropic-version"); got != "2023-06-01" {
		t.Fatalf("expected anthropic-version '2023-06-01', got %q", got)
	}
	if got := gotHeaders.Get("Content-Type"); got != "application/json" {
		t.Fatalf("expected Content-Type 'application/json', got %q", got)
	}
}

func TestPassthrough_PassesThroughClientBetaHeaders(t *testing.T) {
	var gotBeta string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBeta = r.Header.Get("anthropic-beta")
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"type":"message"}`))
	}))
	defer upstream.Close()

	p := NewPassthrough(AuthConfig{APIKey: "test-key"}, upstream.URL)
	req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(`{}`))
	req.Header.Set("anthropic-beta", "my-custom-beta-2025-01-01")
	w := httptest.NewRecorder()

	p.Handle(w, req)

	if gotBeta != "my-custom-beta-2025-01-01" {
		t.Fatalf("expected client beta header passed through, got %q", gotBeta)
	}
}

func TestPassthrough_ForwardsResponseHeaders(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("x-request-id", "req-123")
		w.Write([]byte(`{"type":"message"}`))
	}))
	defer upstream.Close()

	p := NewPassthrough(AuthConfig{APIKey: "test-key"}, upstream.URL)
	req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(`{}`))
	w := httptest.NewRecorder()

	p.Handle(w, req)

	if got := w.Header().Get("x-request-id"); got != "req-123" {
		t.Fatalf("expected response header x-request-id 'req-123', got %q", got)
	}
}

func TestPassthrough_ForwardsUpstreamErrors(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(400)
		w.Write([]byte(`{"type":"error","error":{"type":"invalid_request_error","message":"model is required"}}`))
	}))
	defer upstream.Close()

	p := NewPassthrough(AuthConfig{APIKey: "test-key"}, upstream.URL)
	req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(`{}`))
	w := httptest.NewRecorder()

	p.Handle(w, req)

	if w.Code != 400 {
		t.Fatalf("expected 400, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "model is required") {
		t.Fatalf("expected upstream error forwarded, got %s", w.Body.String())
	}
}

func TestPassthrough_StreamingSSE(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("expected flusher support")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)

		events := []string{
			`event: message_start` + "\n" + `data: {"type":"message_start"}` + "\n\n",
			`event: content_block_delta` + "\n" + `data: {"type":"content_block_delta","delta":{"text":"Hi"}}` + "\n\n",
			`event: message_stop` + "\n" + `data: {"type":"message_stop"}` + "\n\n",
		}
		for _, e := range events {
			fmt.Fprint(w, e)
			flusher.Flush()
		}
	}))
	defer upstream.Close()

	p := NewPassthrough(AuthConfig{APIKey: "test-key"}, upstream.URL)
	req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(`{"stream":true}`))
	w := httptest.NewRecorder()

	p.Handle(w, req)

	if ct := w.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("expected Content-Type text/event-stream, got %q", ct)
	}

	events := parseSSE(t, w.Body.String())
	if len(events) != 3 {
		t.Fatalf("expected 3 SSE events, got %d: %v", len(events), events)
	}
	if events[0].eventType != "message_start" {
		t.Fatalf("expected first event message_start, got %q", events[0].eventType)
	}
	if events[2].eventType != "message_stop" {
		t.Fatalf("expected last event message_stop, got %q", events[2].eventType)
	}
}

func TestPassthrough_UpstreamConnectionError(t *testing.T) {
	// Point to a non-existent server
	p := NewPassthrough(AuthConfig{APIKey: "test-key"}, "http://127.0.0.1:1")
	req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(`{}`))
	w := httptest.NewRecorder()

	p.Handle(w, req)

	if w.Code != 502 {
		t.Fatalf("expected 502, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "upstream request failed") {
		t.Fatalf("expected upstream error message, got %s", w.Body.String())
	}
}

func TestPassthrough_ClaudeCodeHeaders(t *testing.T) {
	var gotHeaders http.Header
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeaders = r.Header.Clone()
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"type":"message"}`))
	}))
	defer upstream.Close()

	p := NewPassthrough(AuthConfig{APIKey: "test-key"}, upstream.URL)
	req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(`{}`))
	w := httptest.NewRecorder()

	p.Handle(w, req)

	if got := gotHeaders.Get("x-app"); got != "cli" {
		t.Fatalf("expected x-app 'cli', got %q", got)
	}
	if got := gotHeaders.Get("User-Agent"); !strings.HasPrefix(got, "claude-cli/") {
		t.Fatalf("expected User-Agent starting with 'claude-cli/', got %q", got)
	}
	if !strings.Contains(gotHeaders.Get("User-Agent"), "(external, cli)") {
		t.Fatalf("expected User-Agent to contain '(external, cli)', got %q", gotHeaders.Get("User-Agent"))
	}
	if got := gotHeaders.Get("X-Claude-Code-Session-Id"); got == "" {
		t.Fatal("expected X-Claude-Code-Session-Id to be set")
	}
	if got := gotHeaders.Get("x-client-request-id"); got == "" {
		t.Fatal("expected x-client-request-id to be set")
	}
}

func TestPassthrough_SessionIdConsistent(t *testing.T) {
	var ids []string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ids = append(ids, r.Header.Get("X-Claude-Code-Session-Id"))
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"type":"message"}`))
	}))
	defer upstream.Close()

	p := NewPassthrough(AuthConfig{APIKey: "test-key"}, upstream.URL)

	for range 3 {
		req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(`{}`))
		w := httptest.NewRecorder()
		p.Handle(w, req)
	}

	if len(ids) != 3 {
		t.Fatalf("expected 3 requests, got %d", len(ids))
	}
	if ids[0] != ids[1] || ids[1] != ids[2] {
		t.Fatalf("expected same session ID across requests, got %v", ids)
	}
}

func TestPassthrough_RequestIdUnique(t *testing.T) {
	var ids []string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ids = append(ids, r.Header.Get("x-client-request-id"))
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"type":"message"}`))
	}))
	defer upstream.Close()

	p := NewPassthrough(AuthConfig{APIKey: "test-key"}, upstream.URL)

	for range 3 {
		req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(`{}`))
		w := httptest.NewRecorder()
		p.Handle(w, req)
	}

	if len(ids) != 3 {
		t.Fatalf("expected 3 requests, got %d", len(ids))
	}
	if ids[0] == ids[1] || ids[1] == ids[2] {
		t.Fatalf("expected unique request IDs, got %v", ids)
	}
}

func TestPassthrough_DefaultBaseURL(t *testing.T) {
	p := NewPassthrough(AuthConfig{APIKey: "test-key"}, "")
	if p.baseURL != "https://api.anthropic.com" {
		t.Fatalf("expected default base URL, got %q", p.baseURL)
	}
}

// --- OAuth auth tests ---

func TestPassthrough_OAuthAuth(t *testing.T) {
	var gotHeaders http.Header
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeaders = r.Header.Clone()
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"type":"message"}`))
	}))
	defer upstream.Close()

	p := NewPassthrough(AuthConfig{OAuthToken: "my-oauth-token"}, upstream.URL)
	req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(`{}`))
	w := httptest.NewRecorder()

	p.Handle(w, req)

	if got := gotHeaders.Get("Authorization"); got != "Bearer my-oauth-token" {
		t.Fatalf("expected Authorization 'Bearer my-oauth-token', got %q", got)
	}
	if got := gotHeaders.Get("x-api-key"); got != "" {
		t.Fatalf("expected no x-api-key with OAuth, got %q", got)
	}
	// OAuth beta header must be present
	assertBetaContains(t, gotHeaders.Get("anthropic-beta"), "oauth-2025-04-20")
}

func TestPassthrough_OAuthMergesClientBetas(t *testing.T) {
	var gotBeta string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBeta = r.Header.Get("anthropic-beta")
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"type":"message"}`))
	}))
	defer upstream.Close()

	p := NewPassthrough(AuthConfig{OAuthToken: "token"}, upstream.URL)
	req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(`{}`))
	req.Header.Set("anthropic-beta", "my-custom-beta")
	w := httptest.NewRecorder()

	p.Handle(w, req)

	assertBetaContains(t, gotBeta, "oauth-2025-04-20")
	assertBetaContains(t, gotBeta, "my-custom-beta")
}

func TestAugmented_OAuthIncludesAllBetas(t *testing.T) {
	var gotBeta string
	var gotAuth string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBeta = r.Header.Get("anthropic-beta")
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"type":"message"}`))
	}))
	defer upstream.Close()

	a := NewAugmented(AuthConfig{OAuthToken: "my-token"}, upstream.URL)
	req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(`{"model":"claude-sonnet-4","messages":[{"role":"user","content":"hi"}]}`))
	w := httptest.NewRecorder()

	a.Handle(w, req)

	if gotAuth != "Bearer my-token" {
		t.Fatalf("expected Bearer auth, got %q", gotAuth)
	}
	// Should have both OAuth beta and model betas
	assertBetaContains(t, gotBeta, "oauth-2025-04-20")
	assertBetaContains(t, gotBeta, "claude-code-20250219")
	assertBetaContains(t, gotBeta, "interleaved-thinking-2025-05-14")
}

// --- Augmented tests ---

func TestAugmented_InjectsBetaHeaders_Sonnet(t *testing.T) {
	var gotBeta string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBeta = r.Header.Get("anthropic-beta")
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"type":"message"}`))
	}))
	defer upstream.Close()

	a := NewAugmented(AuthConfig{APIKey: "test-key"}, upstream.URL)
	req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(`{"model":"claude-sonnet-4-20250514","messages":[{"role":"user","content":"hi"}]}`))
	w := httptest.NewRecorder()

	a.Handle(w, req)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	assertBetaContains(t, gotBeta, "claude-code-20250219")
	assertBetaContains(t, gotBeta, "interleaved-thinking-2025-05-14")
	assertBetaContains(t, gotBeta, "context-management-2025-06-27")
}

func TestAugmented_InjectsBetaHeaders_Opus(t *testing.T) {
	var gotBeta string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBeta = r.Header.Get("anthropic-beta")
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"type":"message"}`))
	}))
	defer upstream.Close()

	a := NewAugmented(AuthConfig{APIKey: "test-key"}, upstream.URL)
	req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(`{"model":"claude-opus-4-20250514","messages":[{"role":"user","content":"hi"}]}`))
	w := httptest.NewRecorder()

	a.Handle(w, req)

	assertBetaContains(t, gotBeta, "claude-code-20250219")
	assertBetaContains(t, gotBeta, "interleaved-thinking-2025-05-14")
}

func TestAugmented_HaikuSkipsClaudeCodeBeta(t *testing.T) {
	var gotBeta string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBeta = r.Header.Get("anthropic-beta")
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"type":"message"}`))
	}))
	defer upstream.Close()

	a := NewAugmented(AuthConfig{APIKey: "test-key"}, upstream.URL)
	req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(`{"model":"claude-haiku-4-5-20251001","messages":[{"role":"user","content":"hi"}]}`))
	w := httptest.NewRecorder()

	a.Handle(w, req)

	assertBetaNotContains(t, gotBeta, "claude-code-20250219")
	// Haiku 4.5 is still claude-4+, so it gets thinking headers
	assertBetaContains(t, gotBeta, "interleaved-thinking-2025-05-14")
}

func TestAugmented_Claude3SkipsThinkingBetas(t *testing.T) {
	var gotBeta string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBeta = r.Header.Get("anthropic-beta")
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"type":"message"}`))
	}))
	defer upstream.Close()

	a := NewAugmented(AuthConfig{APIKey: "test-key"}, upstream.URL)
	req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(`{"model":"claude-3-5-sonnet-20241022","messages":[{"role":"user","content":"hi"}]}`))
	w := httptest.NewRecorder()

	a.Handle(w, req)

	// Claude 3 models get claude-code but not thinking/context-management
	assertBetaContains(t, gotBeta, "claude-code-20250219")
	assertBetaNotContains(t, gotBeta, "interleaved-thinking-2025-05-14")
	assertBetaNotContains(t, gotBeta, "context-management-2025-06-27")
}

func TestAugmented_MergesClientBetas(t *testing.T) {
	var gotBeta string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBeta = r.Header.Get("anthropic-beta")
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"type":"message"}`))
	}))
	defer upstream.Close()

	a := NewAugmented(AuthConfig{APIKey: "test-key"}, upstream.URL)
	req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(`{"model":"claude-sonnet-4","messages":[{"role":"user","content":"hi"}]}`))
	req.Header.Set("anthropic-beta", "my-custom-beta")
	w := httptest.NewRecorder()

	a.Handle(w, req)

	// Client beta should be preserved
	assertBetaContains(t, gotBeta, "my-custom-beta")
	// Injected betas should also be present
	assertBetaContains(t, gotBeta, "claude-code-20250219")
}

func TestAugmented_NoDuplicateBetas(t *testing.T) {
	var gotBeta string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBeta = r.Header.Get("anthropic-beta")
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"type":"message"}`))
	}))
	defer upstream.Close()

	a := NewAugmented(AuthConfig{APIKey: "test-key"}, upstream.URL)
	// Client already provides claude-code-20250219
	req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(`{"model":"claude-sonnet-4","messages":[{"role":"user","content":"hi"}]}`))
	req.Header.Set("anthropic-beta", "claude-code-20250219")
	w := httptest.NewRecorder()

	a.Handle(w, req)

	// Should only appear once
	count := strings.Count(gotBeta, "claude-code-20250219")
	if count != 1 {
		t.Fatalf("expected claude-code-20250219 exactly once, found %d times in %q", count, gotBeta)
	}
}

func TestAugmented_ForwardsRequestBody(t *testing.T) {
	var gotBody string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"type":"message"}`))
	}))
	defer upstream.Close()

	a := NewAugmented(AuthConfig{APIKey: "test-key"}, upstream.URL)
	body := `{"model":"claude-sonnet-4","messages":[{"role":"user","content":"hello"}]}`
	req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(body))
	w := httptest.NewRecorder()

	a.Handle(w, req)

	if gotBody != body {
		t.Fatalf("request body not forwarded correctly:\nexpected: %s\ngot:      %s", body, gotBody)
	}
}

// --- betaHeadersForModel tests ---

func TestBetaHeadersForModel(t *testing.T) {
	tests := []struct {
		model    string
		contains []string
		excludes []string
	}{
		{
			model:    "claude-sonnet-4-20250514",
			contains: []string{"claude-code-20250219", "interleaved-thinking-2025-05-14", "context-management-2025-06-27"},
		},
		{
			model:    "claude-opus-4",
			contains: []string{"claude-code-20250219", "interleaved-thinking-2025-05-14", "context-management-2025-06-27"},
		},
		{
			model:    "claude-haiku-4-5-20251001",
			contains: []string{"interleaved-thinking-2025-05-14", "context-management-2025-06-27"},
			excludes: []string{"claude-code-20250219"},
		},
		{
			model:    "claude-3-5-sonnet-20241022",
			contains: []string{"claude-code-20250219"},
			excludes: []string{"interleaved-thinking-2025-05-14", "context-management-2025-06-27"},
		},
		{
			model:    "",
			contains: []string{"claude-code-20250219", "interleaved-thinking-2025-05-14"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			betas := betaHeadersForModel(tt.model)
			betaSet := make(map[string]bool)
			for _, b := range betas {
				betaSet[b] = true
			}

			for _, want := range tt.contains {
				if !betaSet[want] {
					t.Errorf("expected beta %q for model %q, got %v", want, tt.model, betas)
				}
			}
			for _, notWant := range tt.excludes {
				if betaSet[notWant] {
					t.Errorf("did not expect beta %q for model %q, got %v", notWant, tt.model, betas)
				}
			}
		})
	}
}

// --- mergeBetas tests ---

func TestMergeBetas(t *testing.T) {
	tests := []struct {
		name       string
		existing   string
		additional []string
		want       string
	}{
		{"empty both", "", nil, ""},
		{"empty existing", "", []string{"a", "b"}, "a,b"},
		{"empty additional", "a,b", nil, "a,b"},
		{"merge without duplicates", "a,b", []string{"c", "d"}, "a,b,c,d"},
		{"dedup", "a,b", []string{"b", "c"}, "a,b,c"},
		{"whitespace handling", " a , b ", []string{"c"}, "a,b,c"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := mergeBetas(tt.existing, tt.additional)
			if got != tt.want {
				t.Fatalf("mergeBetas(%q, %v) = %q, want %q", tt.existing, tt.additional, got, tt.want)
			}
		})
	}
}

// --- Integration: server-level tests ---

func TestPassthrough_ViaServer(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id": "msg_test", "type": "message", "role": "assistant",
			"content":     []map[string]string{{"type": "text", "text": "hello from upstream"}},
			"model":       "claude-sonnet-4",
			"stop_reason": "end_turn",
		})
	}))
	defer upstream.Close()

	p := NewPassthrough(AuthConfig{APIKey: "test-key"}, upstream.URL)
	ts := httptest.NewServer(http.HandlerFunc(p.Handle))
	defer ts.Close()

	resp, err := http.Post(ts.URL, "application/json", strings.NewReader(`{"model":"claude-sonnet-4","messages":[{"role":"user","content":"hi"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
	}

	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)
	if result["type"] != "message" {
		t.Fatalf("expected type 'message', got %v", result["type"])
	}
}

// --- SSE parsing helper ---

type sseEvent struct {
	eventType string
	data      string
}

func parseSSE(t *testing.T, body string) []sseEvent {
	t.Helper()
	var events []sseEvent
	scanner := bufio.NewScanner(strings.NewReader(body))
	var current sseEvent
	for scanner.Scan() {
		line := scanner.Text()
		if after, ok := strings.CutPrefix(line, "event: "); ok {
			current.eventType = after
		} else if after, ok := strings.CutPrefix(line, "data: "); ok {
			current.data = after
		} else if line == "" && current.eventType != "" {
			events = append(events, current)
			current = sseEvent{}
		}
	}
	return events
}

func assertBetaContains(t *testing.T, betaHeader, want string) {
	t.Helper()
	for _, b := range strings.Split(betaHeader, ",") {
		if strings.TrimSpace(b) == want {
			return
		}
	}
	t.Errorf("expected beta header to contain %q, got %q", want, betaHeader)
}

func assertBetaNotContains(t *testing.T, betaHeader, notWant string) {
	t.Helper()
	for _, b := range strings.Split(betaHeader, ",") {
		if strings.TrimSpace(b) == notWant {
			t.Errorf("expected beta header NOT to contain %q, got %q", notWant, betaHeader)
			return
		}
	}
}
