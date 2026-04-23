package analyzer

import "testing"

func TestStripCodeFence(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"plain", `{"a":1}`, `{"a":1}`},
		{"trimmed", "  {\"a\":1}\n", `{"a":1}`},
		{"tagged fence", "```json\n{\"a\":1}\n```", `{"a":1}`},
		{"tagged fence uppercase", "```JSON\n[1,2]\n```", `[1,2]`},
		{"bare fence", "```\n{\"a\":1}\n```", `{"a":1}`},
		{"fence with trailing newline", "```json\n{\"a\":1}\n```\n", `{"a":1}`},
		{"no closing fence", "```json\n{\"a\":1}", `{"a":1}`},
		{"prose fallback", "here you go: {\"a\":1}", `here you go: {"a":1}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := stripCodeFence(c.in)
			if got != c.want {
				t.Errorf("stripCodeFence(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}
