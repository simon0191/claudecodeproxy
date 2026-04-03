package media

import (
	"encoding/base64"
	"os"
	"strings"
	"testing"

	"claudecodeproxy/internal/types"
)

func TestSaveMedia(t *testing.T) {
	content := []byte("fake png data")
	b64 := base64.StdEncoding.EncodeToString(content)

	block := types.ContentBlock{
		Type: "image",
		Source: &types.MediaSource{
			Type:      "base64",
			MediaType: "image/png",
			Data:      b64,
		},
	}

	path, err := SaveMedia(block)
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(path)

	if !strings.HasSuffix(path, ".png") {
		t.Fatalf("expected .png extension, got %q", path)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "fake png data" {
		t.Fatalf("unexpected file content: %q", string(data))
	}
}

func TestSaveMedia_NoSource(t *testing.T) {
	block := types.ContentBlock{Type: "image"}
	_, err := SaveMedia(block)
	if err == nil {
		t.Fatal("expected error for block with no source")
	}
}

func TestCleanup(t *testing.T) {
	f, err := os.CreateTemp("", "test-cleanup-*")
	if err != nil {
		t.Fatal(err)
	}
	path := f.Name()
	f.Close()

	Cleanup([]string{path})

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected file to be removed, but it still exists")
	}
}

func TestExtForMediaType(t *testing.T) {
	tests := []struct {
		mediaType string
		want      string
	}{
		{"image/png", ".png"},
		{"image/jpeg", ".jpg"},
		{"audio/wav", ".wav"},
		{"video/mp4", ".mp4"},
		{"application/pdf", ".pdf"},
		{"image/bmp", ".bmp"},       // fallback
		{"something/xyz", ".xyz"},   // fallback
	}
	for _, tt := range tests {
		got := extForMediaType(tt.mediaType)
		if got != tt.want {
			t.Errorf("extForMediaType(%q) = %q, want %q", tt.mediaType, got, tt.want)
		}
	}
}
