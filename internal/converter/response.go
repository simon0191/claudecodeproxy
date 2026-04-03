package converter

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"claudecodeproxy/internal/types"
)

// AnthropicResponse is the Anthropic Messages API response format.
type AnthropicResponse struct {
	ID         string                `json:"id"`
	Type       string                `json:"type"`
	Role       string                `json:"role"`
	Content    []AnthropicContent    `json:"content"`
	Model      string                `json:"model"`
	StopReason string                `json:"stop_reason"`
	Usage      AnthropicUsage        `json:"usage"`
}

type AnthropicContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type AnthropicUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// AnthropicError is an Anthropic API error response.
type AnthropicError struct {
	Type  string              `json:"type"`
	Error AnthropicErrorDetail `json:"error"`
}

type AnthropicErrorDetail struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

// BuildResponse converts a CLIResult into an Anthropic Messages API response.
func BuildResponse(model string, result *types.CLIResult) *AnthropicResponse {
	return &AnthropicResponse{
		ID:         "msg_" + randomHex(12),
		Type:       "message",
		Role:       "assistant",
		Content:    []AnthropicContent{{Type: "text", Text: result.Result}},
		Model:      model,
		StopReason: result.StopReason,
		Usage: AnthropicUsage{
			InputTokens:  result.Usage.InputTokens,
			OutputTokens: result.Usage.OutputTokens,
		},
	}
}

// BuildErrorResponse creates an Anthropic-format error response.
func BuildErrorResponse(errType string, message string) []byte {
	resp := AnthropicError{
		Type: "error",
		Error: AnthropicErrorDetail{
			Type:    errType,
			Message: message,
		},
	}
	data, _ := json.Marshal(resp)
	return data
}

func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%024x", 0)
	}
	return hex.EncodeToString(b)
}
