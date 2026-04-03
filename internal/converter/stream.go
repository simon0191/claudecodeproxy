package converter

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// streamEvent is the wrapper emitted by `claude --output-format stream-json --include-partial-messages`.
// The inner Event field contains the raw Anthropic SSE event (message_start, content_block_delta, etc.).
type streamEvent struct {
	Type  string          `json:"type"`
	Event json.RawMessage `json:"event"`
}

// eventType extracts just the "type" field from a raw JSON event.
type eventType struct {
	Type string `json:"type"`
}

// Types used to clean message_start events to spec-compliant format.
type messageStartEvent struct {
	Type    string       `json:"type"`
	Message startMessage `json:"message"`
}

type startMessage struct {
	ID           string             `json:"id"`
	Type         string             `json:"type"`
	Role         string             `json:"role"`
	Content      []AnthropicContent `json:"content"`
	Model        string             `json:"model"`
	StopReason   *string            `json:"stop_reason"`
	StopSequence *string            `json:"stop_sequence"`
	Usage        AnthropicUsage     `json:"usage"`
}

// Types used to clean message_delta events to spec-compliant format.
type messageDeltaEvent struct {
	Type  string         `json:"type"`
	Delta messageDelta   `json:"delta"`
	Usage deltaUsage     `json:"usage"`
}

type messageDelta struct {
	StopReason   *string `json:"stop_reason"`
	StopSequence *string `json:"stop_sequence"`
}

type deltaUsage struct {
	OutputTokens int `json:"output_tokens"`
}

// StreamResponse reads stream-json lines from the CLI and writes SSE events
// in the Anthropic streaming protocol. It cleans events to contain only
// spec-compliant fields and remaps the model name.
func StreamResponse(ctx context.Context, apiModel string, stdout io.Reader, w http.ResponseWriter) error {
	flusher, ok := w.(http.Flusher)
	if !ok {
		return fmt.Errorf("response writer does not support flushing")
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024)

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		line := scanner.Bytes()

		var se streamEvent
		if err := json.Unmarshal(line, &se); err != nil {
			continue
		}

		// Only forward stream_event types -- skip system, assistant, result, etc.
		if se.Type != "stream_event" || se.Event == nil {
			continue
		}

		// Extract the event type to use as the SSE event name
		var et eventType
		if err := json.Unmarshal(se.Event, &et); err != nil {
			continue
		}

		eventData, err := cleanEvent(et.Type, se.Event, apiModel)
		if err != nil {
			continue
		}

		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", et.Type, eventData)
		flusher.Flush()
	}

	return scanner.Err()
}

// cleanEvent strips non-standard fields from events that need it,
// and passes through spec-compliant events unchanged.
func cleanEvent(eventType string, raw json.RawMessage, apiModel string) ([]byte, error) {
	switch eventType {
	case "message_start":
		return cleanMessageStart(raw, apiModel)
	case "message_delta":
		return cleanMessageDelta(raw)
	default:
		// content_block_start, content_block_delta, content_block_stop,
		// message_stop, ping — pass through as-is.
		return raw, nil
	}
}

func cleanMessageStart(raw json.RawMessage, apiModel string) ([]byte, error) {
	// Parse into a generic map to extract fields we need
	var parsed struct {
		Type    string `json:"type"`
		Message struct {
			ID           string             `json:"id"`
			Type         string             `json:"type"`
			Role         string             `json:"role"`
			Content      []AnthropicContent `json:"content"`
			Model        string             `json:"model"`
			StopReason   *string            `json:"stop_reason"`
			StopSequence *string            `json:"stop_sequence"`
			Usage        struct {
				InputTokens  int `json:"input_tokens"`
				OutputTokens int `json:"output_tokens"`
			} `json:"usage"`
		} `json:"message"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, err
	}

	clean := messageStartEvent{
		Type: parsed.Type,
		Message: startMessage{
			ID:           parsed.Message.ID,
			Type:         parsed.Message.Type,
			Role:         parsed.Message.Role,
			Content:      parsed.Message.Content,
			Model:        apiModel,
			StopReason:   parsed.Message.StopReason,
			StopSequence: parsed.Message.StopSequence,
			Usage: AnthropicUsage{
				InputTokens:  parsed.Message.Usage.InputTokens,
				OutputTokens: parsed.Message.Usage.OutputTokens,
			},
		},
	}
	return json.Marshal(clean)
}

func cleanMessageDelta(raw json.RawMessage) ([]byte, error) {
	var parsed struct {
		Type  string `json:"type"`
		Delta struct {
			StopReason   *string `json:"stop_reason"`
			StopSequence *string `json:"stop_sequence"`
		} `json:"delta"`
		Usage struct {
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, err
	}

	clean := messageDeltaEvent{
		Type: parsed.Type,
		Delta: messageDelta{
			StopReason:   parsed.Delta.StopReason,
			StopSequence: parsed.Delta.StopSequence,
		},
		Usage: deltaUsage{
			OutputTokens: parsed.Usage.OutputTokens,
		},
	}
	return json.Marshal(clean)
}
