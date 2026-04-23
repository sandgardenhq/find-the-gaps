package cli

import "testing"

func TestParseTierString(t *testing.T) {
	cases := []struct {
		in           string
		wantProvider string
		wantModel    string
		wantErr      bool
	}{
		{"anthropic/claude-haiku-4-5", "anthropic", "claude-haiku-4-5", false},
		{"openai/gpt-5.4-mini", "openai", "gpt-5.4-mini", false},
		{"claude-haiku-4-5", "anthropic", "claude-haiku-4-5", false},      // bare model → anthropic
		{"ollama/llama3.1:8b", "ollama", "llama3.1:8b", false},            // first-slash split
		{"  anthropic/claude-opus-4-7  ", "anthropic", "claude-opus-4-7", false}, // trim whitespace
		{"foo//bar", "foo", "/bar", false}, // only first slash splits; rest preserved
		{"", "", "", true},              // empty string
		{"   ", "", "", true},           // whitespace only
		{"anthropic/", "", "", true},    // missing model
		{"/claude-haiku", "", "", true}, // missing provider
	}
	for _, tc := range cases {
		gotProv, gotModel, err := parseTierString(tc.in)
		if tc.wantErr {
			if err == nil {
				t.Errorf("parseTierString(%q): want error, got nil", tc.in)
			}
			continue
		}
		if err != nil {
			t.Errorf("parseTierString(%q): unexpected error: %v", tc.in, err)
			continue
		}
		if gotProv != tc.wantProvider || gotModel != tc.wantModel {
			t.Errorf("parseTierString(%q): got %q/%q; want %q/%q", tc.in, gotProv, gotModel, tc.wantProvider, tc.wantModel)
		}
	}
}
