package proxy

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/google/uuid"
)

// Augmented wraps a Passthrough proxy, injecting model-appropriate beta headers
// and modifying the request body to include Claude Code's attribution header,
// system prompt prefix, and metadata. This makes requests look identical to
// those sent by the real Claude Code CLI.
type Augmented struct {
	pass     *Passthrough
	deviceID string // stable per proxy instance
}

// NewAugmented creates an augmented proxy. If baseURL is empty, defaults to
// https://api.anthropic.com.
func NewAugmented(auth AuthConfig, baseURL string) *Augmented {
	return &Augmented{
		pass:     NewPassthrough(auth, baseURL),
		deviceID: uuid.New().String(),
	}
}

// Handle reads the request, injects beta headers, modifies the body to add
// attribution and metadata, then delegates to the passthrough handler.
func (a *Augmented) Handle(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeProxyError(w, http.StatusBadRequest, "invalid_request_error", "failed to read request body")
		return
	}

	// Parse the request body
	var req map[string]json.RawMessage
	if err := json.Unmarshal(body, &req); err != nil {
		// Let passthrough forward the malformed body — upstream will return the error
		r.Body = io.NopCloser(bytes.NewReader(body))
		a.pass.Handle(w, r)
		return
	}

	// Extract model for beta headers
	var model string
	if raw, ok := req["model"]; ok {
		json.Unmarshal(raw, &model)
	}

	// Inject beta headers
	betas := betaHeadersForModel(model)
	existing := r.Header.Get("anthropic-beta")
	merged := mergeBetas(existing, betas)
	if merged != "" {
		r.Header.Set("anthropic-beta", merged)
	}

	// Extract first user message text for fingerprinting
	firstMsgText := extractFirstUserMessageText(req)

	// Compute fingerprint and build attribution line
	fingerprint := computeFingerprint(firstMsgText, claudeCodeVersion)
	attribution := fmt.Sprintf("x-anthropic-billing-header: cc_version=%s.%s; cc_entrypoint=cli;", claudeCodeVersion, fingerprint)

	// Inject attribution + Claude Code prefix into system prompt
	injectSystemPrefix(req, attribution)

	// Inject metadata
	injectMetadata(req, a.deviceID, a.pass.sessionID)

	// Re-serialize and forward
	modified, err := json.Marshal(req)
	if err != nil {
		r.Body = io.NopCloser(bytes.NewReader(body))
		a.pass.Handle(w, r)
		return
	}

	r.Body = io.NopCloser(bytes.NewReader(modified))
	r.ContentLength = int64(len(modified))
	a.pass.Handle(w, r)
}

// fingerprintSalt matches the hardcoded salt in claude-code/utils/fingerprint.ts
const fingerprintSalt = "59cf53e54c78"

// computeFingerprint replicates claude-code's fingerprint algorithm:
// SHA256(salt + msg[4] + msg[7] + msg[20] + version)[:3]
func computeFingerprint(messageText, version string) string {
	indices := [3]int{4, 7, 20}
	var chars [3]byte
	for i, idx := range indices {
		if idx < len(messageText) {
			chars[i] = messageText[idx]
		} else {
			chars[i] = '0'
		}
	}

	input := fmt.Sprintf("%s%s%s", fingerprintSalt, string(chars[:]), version)
	hash := sha256.Sum256([]byte(input))
	return fmt.Sprintf("%x", hash[:2])[:3] // first 3 hex chars
}

// extractFirstUserMessageText finds the text content of the first user message.
func extractFirstUserMessageText(req map[string]json.RawMessage) string {
	raw, ok := req["messages"]
	if !ok {
		return ""
	}

	var messages []struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal(raw, &messages); err != nil {
		return ""
	}

	for _, msg := range messages {
		if msg.Role != "user" {
			continue
		}
		// Content can be a string or array of blocks
		var text string
		if err := json.Unmarshal(msg.Content, &text); err == nil {
			return text
		}
		var blocks []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}
		if err := json.Unmarshal(msg.Content, &blocks); err == nil {
			for _, b := range blocks {
				if b.Type == "text" {
					return b.Text
				}
			}
		}
		return ""
	}
	return ""
}

// injectSystemPrefix prepends the attribution header and Claude Code identity
// to the system prompt. Handles both string and array-of-blocks formats,
// or creates a new system field if none exists.
func injectSystemPrefix(req map[string]json.RawMessage, attribution string) {
	prefix := attribution + "\n" + "You are Claude Code, Anthropic's official CLI for Claude."

	raw, exists := req["system"]
	if !exists {
		// No system prompt — create one
		block, _ := json.Marshal([]map[string]string{
			{"type": "text", "text": prefix},
		})
		req["system"] = block
		return
	}

	// Try as string
	var systemStr string
	if err := json.Unmarshal(raw, &systemStr); err == nil {
		combined := prefix + "\n\n" + systemStr
		encoded, _ := json.Marshal(combined)
		req["system"] = encoded
		return
	}

	// Try as array of text blocks
	var existingBlocks []json.RawMessage
	if err := json.Unmarshal(raw, &existingBlocks); err == nil {
		prefixBlock, _ := json.Marshal(map[string]string{
			"type": "text",
			"text": prefix,
		})
		result := append([]json.RawMessage{prefixBlock}, existingBlocks...)
		encoded, _ := json.Marshal(result)
		req["system"] = encoded
		return
	}

	// Unknown format — prepend as string
	block, _ := json.Marshal([]map[string]string{
		{"type": "text", "text": prefix},
	})
	req["system"] = block
}

// injectMetadata adds the metadata.user_id field matching Claude Code's format.
func injectMetadata(req map[string]json.RawMessage, deviceID, sessionID string) {
	userIDObj := map[string]string{
		"device_id":    deviceID,
		"account_uuid": "",
		"session_id":   sessionID,
	}
	userIDJSON, _ := json.Marshal(userIDObj)
	metadata := map[string]string{
		"user_id": string(userIDJSON),
	}
	encoded, _ := json.Marshal(metadata)
	req["metadata"] = encoded
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
