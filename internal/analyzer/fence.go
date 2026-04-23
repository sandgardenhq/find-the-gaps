package analyzer

import "strings"

// stripCodeFence removes surrounding markdown code fences from an LLM response.
// Handles both bare (```) and tagged (```json, ```JSON, etc.) fences. Returns
// the inner content trimmed of whitespace. If the input is not fenced, it is
// returned trimmed unchanged so downstream json.Unmarshal still gets a useful
// error on genuine junk.
func stripCodeFence(s string) string {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "```") {
		return s
	}
	s = strings.TrimPrefix(s, "```")
	if nl := strings.IndexByte(s, '\n'); nl >= 0 {
		s = s[nl+1:]
	}
	s = strings.TrimSpace(s)
	s = strings.TrimSuffix(s, "```")
	return strings.TrimSpace(s)
}
