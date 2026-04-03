package proxy

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
)

// Augmented wraps a Passthrough proxy, injecting model-appropriate beta headers
// before forwarding. This gives clients the same API behavior that Claude Code gets.
type Augmented struct {
	pass *Passthrough
}

// NewAugmented creates an augmented proxy. If baseURL is empty, defaults to
// https://api.anthropic.com.
func NewAugmented(auth AuthConfig, baseURL string) *Augmented {
	return &Augmented{pass: NewPassthrough(auth, baseURL)}
}

// Handle reads the request to extract the model name, injects beta headers,
// then delegates to the passthrough handler.
func (a *Augmented) Handle(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeProxyError(w, http.StatusBadRequest, "invalid_request_error", "failed to read request body")
		return
	}

	// Extract model from request (minimal parse)
	var partial struct {
		Model string `json:"model"`
	}
	json.Unmarshal(body, &partial) // best-effort; passthrough will get the real error from upstream

	// Inject beta headers
	betas := betaHeadersForModel(partial.Model)
	existing := r.Header.Get("anthropic-beta")
	merged := mergeBetas(existing, betas)
	if merged != "" {
		r.Header.Set("anthropic-beta", merged)
	}

	// Restore body and delegate
	r.Body = io.NopCloser(bytes.NewReader(body))
	a.pass.Handle(w, r)
}

// betaHeadersForModel returns the beta headers that Claude Code would send for
// the given model. Derived from claude-code/constants/betas.ts and utils/betas.ts.
func betaHeadersForModel(model string) []string {
	var betas []string
	lower := strings.ToLower(model)

	// claude-code-20250219: core Claude Code behavior (non-haiku models)
	if !strings.Contains(lower, "haiku") {
		betas = append(betas, "claude-code-20250219")
	}

	// interleaved-thinking-2025-05-14: extended thinking (claude-4+ models)
	if !strings.Contains(lower, "claude-3-") {
		betas = append(betas, "interleaved-thinking-2025-05-14")
	}

	// context-management-2025-06-27: thinking preservation (claude-4+ models)
	if !strings.Contains(lower, "claude-3-") {
		betas = append(betas, "context-management-2025-06-27")
	}

	return betas
}

// mergeBetas combines existing comma-separated betas with additional ones,
// removing duplicates.
func mergeBetas(existing string, additional []string) string {
	seen := make(map[string]bool)
	var result []string

	// Parse existing
	if existing != "" {
		for _, b := range strings.Split(existing, ",") {
			b = strings.TrimSpace(b)
			if b != "" && !seen[b] {
				seen[b] = true
				result = append(result, b)
			}
		}
	}

	// Add new ones
	for _, b := range additional {
		if !seen[b] {
			seen[b] = true
			result = append(result, b)
		}
	}

	return strings.Join(result, ",")
}
