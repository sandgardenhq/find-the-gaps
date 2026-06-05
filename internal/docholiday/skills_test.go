package docholiday

import (
	"os"
	"strings"
	"testing"
)

// TestSkillsSourceCarriesPromptMarker guards the project's `// PROMPT:`
// convention: every prompt string must be marked so prompts stay easy to find
// and review. There are two embedded skill bodies, so we require at least two
// markers — one per embedded var.
func TestSkillsSourceCarriesPromptMarker(t *testing.T) {
	src, err := os.ReadFile("skills.go")
	if err != nil {
		t.Fatalf("reading skills.go: %v", err)
	}
	const marker = "// PROMPT:"
	if !strings.Contains(string(src), marker) {
		t.Fatalf("skills.go is missing the %q convention marker", marker)
	}
	if got := strings.Count(string(src), marker); got < 2 {
		t.Fatalf("skills.go should carry at least 2 %q markers (one per embedded var), got %d", marker, got)
	}
}

func TestEmbeddedSkillsArePresent(t *testing.T) {
	if strings.TrimSpace(staleDocsSkillRaw) == "" {
		t.Fatal("staleDocsSkillRaw is empty; go:embed did not load fix-stale-docs/SKILL.md")
	}
	if strings.TrimSpace(newFeatureSkillRaw) == "" {
		t.Fatal("newFeatureSkillRaw is empty; go:embed did not load document-new-feature/SKILL.md")
	}
}

func TestSkillBodyStripsFrontmatter(t *testing.T) {
	body := skillBody(staleDocsSkillRaw)
	if strings.Contains(body, "name:") || strings.Contains(body, "description:") {
		t.Fatalf("frontmatter not stripped from body:\n%s", body)
	}
	if strings.HasPrefix(strings.TrimSpace(body), "---") {
		t.Fatal("body still begins with frontmatter fence")
	}
	if !strings.Contains(body, "Doc Holiday") {
		t.Fatalf("body missing expected instruction text:\n%s", body)
	}
}

func TestSkillBodyWithoutFrontmatterIsUnchanged(t *testing.T) {
	in := "just a body\nwith no frontmatter\n"
	if got := skillBody(in); got != in {
		t.Fatalf("skillBody altered a frontmatter-free string: %q", got)
	}
}

func TestSkillBodyMalformedFrontmatterIsUnchanged(t *testing.T) {
	in := "---\nname: x\nno closing fence\n"
	if got := skillBody(in); got != in {
		t.Fatalf("skillBody altered a string with no closing fence: %q", got)
	}
}
