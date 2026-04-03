package claude

import "testing"

func TestMapModel(t *testing.T) {
	tests := []struct {
		input   string
		want    string
		wantErr bool
	}{
		{"claude-sonnet-4", "sonnet", false},
		{"claude-opus-4", "opus", false},
		{"claude-haiku-4", "haiku", false},
		{"gpt-4", "", true},
		{"", "", true},
	}

	for _, tt := range tests {
		got, err := MapModel(tt.input)
		if (err != nil) != tt.wantErr {
			t.Errorf("MapModel(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			continue
		}
		if got != tt.want {
			t.Errorf("MapModel(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
