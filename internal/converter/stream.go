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

// StreamResponse reads stream-json lines from the CLI and writes SSE events
// in the Anthropic streaming protocol.
func StreamResponse(ctx context.Context, model string, stdout io.Reader, w http.ResponseWriter) error {
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

		// Write the inner event directly as an SSE event -- it's already in Anthropic format
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", et.Type, se.Event)
		flusher.Flush()
	}

	return scanner.Err()
}
