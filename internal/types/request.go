package types

import "encoding/json"

// MessagesRequest is an incoming Anthropic Messages API request.
type MessagesRequest struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	System      Content   `json:"system,omitempty"`
	MaxTokens   int       `json:"max_tokens"`
	Stream      bool      `json:"stream,omitempty"`
	Temperature *float64  `json:"temperature,omitempty"`
}

// Message is a single message in the conversation.
type Message struct {
	Role    string  `json:"role"`
	Content Content `json:"content"`
}

// ContentBlock is a single content part within a message.
type ContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`

	// Media fields (for image, audio, video, document blocks)
	Source *MediaSource `json:"source,omitempty"`
}

// MediaSource holds base64-encoded media data.
type MediaSource struct {
	Type      string `json:"type"`       // "base64"
	MediaType string `json:"media_type"` // e.g. "image/png", "audio/wav"
	Data      string `json:"data"`       // base64-encoded content
}

// Content handles the Anthropic API's flexible content field,
// which can be either a plain string or an array of ContentBlocks.
type Content struct {
	Blocks []ContentBlock
}

func (c *Content) UnmarshalJSON(data []byte) error {
	// Try string first
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		c.Blocks = []ContentBlock{{Type: "text", Text: s}}
		return nil
	}

	// Try array of content blocks
	var blocks []ContentBlock
	if err := json.Unmarshal(data, &blocks); err != nil {
		return err
	}
	c.Blocks = blocks
	return nil
}

func (c Content) MarshalJSON() ([]byte, error) {
	return json.Marshal(c.Blocks)
}

// TextContent returns the concatenated text from all text blocks.
func (c Content) TextContent() string {
	var text string
	for _, b := range c.Blocks {
		if b.Type == "text" {
			text += b.Text
		}
	}
	return text
}

// MediaBlocks returns all non-text content blocks.
func (c Content) MediaBlocks() []ContentBlock {
	var blocks []ContentBlock
	for _, b := range c.Blocks {
		if b.Type != "text" && b.Source != nil {
			blocks = append(blocks, b)
		}
	}
	return blocks
}
