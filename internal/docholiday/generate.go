package docholiday

import (
	"context"
	"fmt"
	"strings"
)

// Completer is the minimal LLM surface this package needs: one flat-string
// completion. *analyzer.BifrostClient (via analyzer.LLMClient) satisfies it;
// the CLI passes tiering.Typical().
type Completer interface {
	Complete(ctx context.Context, prompt string) (string, error)
}

func skillRawFor(c Category) string {
	if c == CategoryMissing {
		return newFeatureSkillRaw
	}
	return staleDocsSkillRaw
}

// generateOne authors the Doc Holiday prompt for a single unit. The LLM prompt
// is the skill body (instructions) followed by the unit's findings.
func generateOne(ctx context.Context, gen Completer, u unit) (Prompt, error) {
	// PROMPT: Concatenates the embedded agent-skill instructions (how to write
	// a Doc Holiday prompt) with this unit's concrete findings; the model
	// returns the finished Doc Holiday prompt text.
	prompt := fmt.Sprintf("%s\n\n---\n\n%s", skillBody(skillRawFor(u.category)), renderUnitFindings(u))
	body, err := gen.Complete(ctx, prompt)
	if err != nil {
		return Prompt{}, fmt.Errorf("generate %s prompt for %q: %w", u.category, unitHeading(u), err)
	}
	return Prompt{
		Category: u.category,
		Heading:  unitHeading(u),
		Note:     unitNote(u),
		Priority: u.priority,
		Body:     strings.TrimSpace(body),
	}, nil
}
