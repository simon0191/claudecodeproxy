package proxy

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"net/http"

	"github.com/google/uuid"
)

// claudeCodeVersion is the Claude CLI version we impersonate. Requests
// without a matching User-Agent may be rate-limited differently.
const claudeCodeVersion = "2.1.91"

// AuthConfig holds authentication credentials for the Anthropic API.
// Exactly one of APIKey or OAuthToken should be set.
type AuthConfig struct {
	APIKey     string // uses x-api-key header
	OAuthToken string // uses Authorization: Bearer header + oauth beta
}

// Passthrough is a reverse proxy that forwards requests to the Anthropic API,
// injecting authentication headers. It does not parse or modify request/response bodies.
type Passthrough struct {
	auth      AuthConfig
	baseURL   string
	sessionID string // stable per proxy instance, like Claude Code's session ID
	client    *http.Client
}

// NewPassthrough creates a passthrough proxy. If baseURL is empty, defaults to
// https://api.anthropic.com.
func NewPassthrough(auth AuthConfig, baseURL string) *Passthrough {
	if baseURL == "" {
		baseURL = "https://api.anthropic.com"
	}
	return &Passthrough{
		auth:      auth,
		baseURL:   baseURL,
		sessionID: uuid.New().String(),
		client:    &http.Client{},
	}
}

// Handle forwards the incoming request to the Anthropic API and streams the
// response back. Works for both streaming (SSE) and non-streaming responses.
func (p *Passthrough) Handle(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeProxyError(w, http.StatusBadRequest, "invalid_request_error", "failed to read request body")
		return
	}

	upstream, err := http.NewRequestWithContext(r.Context(), "POST", p.baseURL+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		writeProxyError(w, http.StatusInternalServerError, "api_error", "failed to create upstream request")
		return
	}

	upstream.Header.Set("Content-Type", "application/json")
	upstream.Header.Set("anthropic-version", "2023-06-01")
	upstream.Header.Set("x-app", "cli")
	upstream.Header.Set("User-Agent", "claude-cli/"+claudeCodeVersion+" (external, cli)")
	upstream.Header.Set("X-Claude-Code-Session-Id", p.sessionID)
	upstream.Header.Set("x-client-request-id", uuid.New().String())
	p.auth.setHeaders(upstream.Header)

	// Pass through client-provided beta headers (merge with auth betas)
	if beta := r.Header.Get("anthropic-beta"); beta != "" {
		if existing := upstream.Header.Get("anthropic-beta"); existing != "" {
			upstream.Header.Set("anthropic-beta", existing+","+beta)
		} else {
			upstream.Header.Set("anthropic-beta", beta)
		}
	}

	resp, err := p.client.Do(upstream)
	if err != nil {
		log.Printf("upstream request failed: %v", err)
		writeProxyError(w, http.StatusBadGateway, "api_error", "upstream request failed: "+err.Error())
		return
	}
	defer resp.Body.Close()

	// Copy response headers
	for k, vs := range resp.Header {
		for _, v := range vs {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)

	// Stream response body back, flushing for SSE support
	streamBody(w, resp.Body)
}

// setHeaders sets the appropriate authentication headers on the request.
func (a AuthConfig) setHeaders(h http.Header) {
	if a.OAuthToken != "" {
		h.Set("Authorization", "Bearer "+a.OAuthToken)
		// OAuth requires this beta header
		h.Set("anthropic-beta", "oauth-2025-04-20")
	} else if a.APIKey != "" {
		h.Set("x-api-key", a.APIKey)
	}
}

// streamBody copies src to dst, flushing after each read for SSE support.
func streamBody(w http.ResponseWriter, src io.Reader) {
	flusher, canFlush := w.(http.Flusher)
	buf := make([]byte, 4096)
	for {
		n, err := src.Read(buf)
		if n > 0 {
			w.Write(buf[:n])
			if canFlush {
				flusher.Flush()
			}
		}
		if err != nil {
			break
		}
	}
}

func writeProxyError(w http.ResponseWriter, status int, errType string, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	fmt.Fprintf(w, `{"type":"error","error":{"type":"%s","message":"%s"}}`, errType, message)
}
