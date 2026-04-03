package media

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"claudecodeproxy/internal/types"

	"github.com/google/uuid"
)

const TempDir = "/tmp/claudecodeproxy"

// mediaTypeToExt maps MIME types to file extensions.
var mediaTypeToExt = map[string]string{
	"image/png":       ".png",
	"image/jpeg":      ".jpg",
	"image/gif":       ".gif",
	"image/webp":      ".webp",
	"audio/wav":       ".wav",
	"audio/mp3":       ".mp3",
	"audio/mpeg":      ".mp3",
	"audio/ogg":       ".ogg",
	"audio/webm":      ".webm",
	"video/mp4":       ".mp4",
	"video/webm":      ".webm",
	"video/ogg":       ".ogv",
	"application/pdf": ".pdf",
}

// SaveMedia decodes a base64 media content block and writes it to a temp file.
// Returns the file path of the saved file.
func SaveMedia(block types.ContentBlock) (string, error) {
	if block.Source == nil {
		return "", fmt.Errorf("content block has no source")
	}

	data, err := base64.StdEncoding.DecodeString(block.Source.Data)
	if err != nil {
		return "", fmt.Errorf("decoding base64: %w", err)
	}

	ext := extForMediaType(block.Source.MediaType)
	filename := uuid.NewString() + ext
	path := filepath.Join(TempDir, filename)

	if err := os.MkdirAll(TempDir, 0o700); err != nil {
		return "", fmt.Errorf("creating temp dir: %w", err)
	}

	if err := os.WriteFile(path, data, 0o600); err != nil {
		return "", fmt.Errorf("writing file: %w", err)
	}

	return path, nil
}

// Cleanup removes the given temp files.
func Cleanup(paths []string) {
	for _, p := range paths {
		os.Remove(p)
	}
}

func extForMediaType(mediaType string) string {
	if ext, ok := mediaTypeToExt[mediaType]; ok {
		return ext
	}
	// Fallback: use the subtype as extension
	parts := strings.SplitN(mediaType, "/", 2)
	if len(parts) == 2 {
		return "." + parts[1]
	}
	return ".bin"
}
