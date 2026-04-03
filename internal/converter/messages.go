package converter

import (
	"fmt"
	"strings"

	"claudecodeproxy/internal/media"
	"claudecodeproxy/internal/types"
)

// ConvertMessages converts Anthropic Messages API messages into a single prompt
// string for the Claude CLI. Media content blocks are saved as temp files and
// referenced in the prompt. Returns the prompt string and a list of temp file
// paths that should be cleaned up after the CLI completes.
func ConvertMessages(system string, messages []types.Message) (string, []string, error) {
	var parts []string
	var tempFiles []string

	if system != "" {
		parts = append(parts, "<system>\n"+system+"\n</system>")
	}

	for _, msg := range messages {
		switch msg.Role {
		case "user":
			text, files, err := convertUserContent(msg.Content)
			if err != nil {
				return "", tempFiles, err
			}
			tempFiles = append(tempFiles, files...)
			parts = append(parts, text)
		case "assistant":
			parts = append(parts, "<previous_response>\n"+msg.Content.TextContent()+"\n</previous_response>")
		}
	}

	return strings.Join(parts, "\n\n"), tempFiles, nil
}

func convertUserContent(content types.Content) (string, []string, error) {
	var parts []string
	var tempFiles []string

	for _, block := range content.Blocks {
		switch block.Type {
		case "text":
			parts = append(parts, block.Text)
		default:
			if block.Source != nil {
				path, err := media.SaveMedia(block)
				if err != nil {
					return "", tempFiles, fmt.Errorf("saving media: %w", err)
				}
				tempFiles = append(tempFiles, path)
				parts = append(parts, fmt.Sprintf("[Attached file: %s]", path))
			}
		}
	}

	return strings.Join(parts, "\n"), tempFiles, nil
}
