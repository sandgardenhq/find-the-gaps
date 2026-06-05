package docholiday

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/charmbracelet/log"

	"github.com/sandgardenhq/find-the-gaps/internal/parallel"
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

// Options configures a GeneratePrompts run.
type Options struct {
	ProjectDir string // where prompts-cache.json lives
	Workers    int    // bounded worker pool size (<=0 → serial)
	NoCache    bool   // ignore and overwrite any existing cache
}

// GeneratePrompts authors one Doc Holiday prompt per work-unit derived from in,
// reusing cached prompts when their unit content + skill version are unchanged.
// Results are returned sorted (SortPrompts order). The cache is flushed after
// every fresh unit so a SIGINT leaves a valid partial prompts-cache.json.
//
// A unit whose Completer call fails is logged and skipped; it is not cached, so
// a later run retries it. Only a cache-persistence failure aborts the run.
//
// Orphaned cache entries (units that disappeared upstream between runs) are
// tolerated by design — they linger in prompts-cache.json and are pruned only
// by a NoCache run, which rebuilds the cache from scratch.
func GeneratePrompts(ctx context.Context, gen Completer, in Input, opts Options) ([]Prompt, error) {
	units := append(staleUnits(in.Drift, maxIssuesPerPrompt), missingUnits(in.Undocumented, in.Rationales)...)

	var c *cache
	if !opts.NoCache {
		if loaded, ok := loadCache(opts.ProjectDir); ok {
			c = loaded
		}
	}
	if c == nil {
		c = newCache(nil)
	}

	var (
		mu  sync.Mutex
		out = make([]Prompt, 0, len(units))
	)
	err := parallel.Run(ctx, units, opts.Workers, func(ctx context.Context, u unit) error {
		key := unitKey(u)
		if p, ok := c.get(key); ok {
			mu.Lock()
			out = append(out, p)
			mu.Unlock()
			return nil
		}
		// The LLM Complete call is intentionally OUTSIDE the cache lock (do not
		// move generation into c.put/flushLocked): a slow completion must not
		// serialize the other workers, and only the just-generated prompt is
		// written under the lock.
		p, err := generateOne(ctx, gen, u)
		if err != nil {
			// Skip-and-continue: one flaky LLM call must not discard the whole
			// prompts phase (matches the page-analysis phase). The unit is not
			// cached, so a later run retries it.
			log.Warnf("skipping doc-holiday prompt for %q: %v", unitHeading(u), err)
			return nil
		}
		if err := c.put(opts.ProjectDir, key, p); err != nil {
			return fmt.Errorf("persist prompts cache: %w", err)
		}
		mu.Lock()
		out = append(out, p)
		mu.Unlock()
		return nil
	})
	if err != nil {
		return nil, err
	}
	SortPrompts(out)
	return out, nil
}
