package docholiday

import (
	_ "embed"
	"strings"
)

// PROMPT: Meta-prompt instructing the LLM how to author a Doc Holiday prompt
// that fixes stale documentation. Source of truth is the agent-skill markdown
// at skills/fix-stale-docs/SKILL.md.
//
//go:embed skills/fix-stale-docs/SKILL.md
var staleDocsSkillRaw string

// PROMPT: Meta-prompt instructing the LLM how to author a Doc Holiday prompt
// that documents a brand-new feature. Source of truth is the agent-skill
// markdown at skills/document-new-feature/SKILL.md.
//
//go:embed skills/document-new-feature/SKILL.md
var newFeatureSkillRaw string

// skillBody returns the markdown body of an agent-skill file with its leading
// YAML frontmatter block stripped. A frontmatter block is a "---" line at the
// very start of the file, its content, and a closing "---" line. Input without
// a leading frontmatter fence is returned unchanged.
func skillBody(raw string) string {
	s := strings.TrimLeft(raw, "\uFEFF") // tolerate a UTF-8 BOM
	if !strings.HasPrefix(s, "---\n") && !strings.HasPrefix(s, "---\r\n") {
		return raw
	}
	// Find the closing fence after the opening one.
	rest := s[strings.IndexByte(s, '\n')+1:]
	idx := strings.Index(rest, "\n---")
	if idx < 0 {
		return raw // malformed; leave as-is rather than nuke content
	}
	after := rest[idx+len("\n---"):]
	return strings.TrimPrefix(strings.TrimLeft(after, "\r"), "\n")
}
