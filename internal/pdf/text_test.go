package pdf

import "testing"

func TestSanitize(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"hello", "hello"},
		{"em — dash", "em - dash"},
		{"en – dash", "en - dash"},
		{"curly ‘quote’", "curly 'quote'"},
		{"curly “double”", `curly "double"`},
		{"trailing…", "trailing..."},
		{"arrow →", "arrow ->"},
		{"bullet • point", "bullet * point"},
		{"non break", "non break"},
		{"bel\x07char", "belchar"}, // BEL is stripped
		{"", ""},
		{"latin1 ñ ö é", "latin1 ñ ö é"}, // Latin-1 supplement passes through
	}
	for _, tc := range tests {
		got := sanitize(tc.in)
		if got != tc.want {
			t.Errorf("sanitize(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
