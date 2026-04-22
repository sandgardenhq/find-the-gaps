package analyzer

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/log"
	"github.com/sandgardenhq/find-the-gaps/internal/scanner"
)

// ExtractFeaturesFromCode asks the LLM to identify product features from the
// codebase's exported symbol index. It uses the same symbol-line format and
// batching strategy as MapFeaturesToCode. Features from multiple batches are
// deduplicated and sorted.
func ExtractFeaturesFromCode(ctx context.Context, client LLMClient, scan *scanner.ProjectScan) ([]CodeFeature, error) {
	var symLines []string
	for _, f := range scan.Files {
		if len(f.Symbols) == 0 {
			continue
		}
		names := make([]string, len(f.Symbols))
		for i, s := range f.Symbols {
			names[i] = s.Name
		}
		symLines = append(symLines, fmt.Sprintf("%s: %s", f.Path, strings.Join(names, ", ")))
	}

	if len(symLines) == 0 {
		return []CodeFeature{}, nil
	}

	// Approximate token cost of the fixed prompt preamble so batchSymLines
	// leaves room for the preamble plus the symbol lines.
	preamblePrompt := `You are analyzing a codebase to identify the product features it implements.

Code files and their exported symbols (format: "file/path: Symbol1, Symbol2"):


Return a JSON array of product features. Each element must have:
- "name": short noun phrase (max 8 words) naming the feature
- "description": 1-2 sentences describing what the feature does and its role in the product
- "layer": a short label for which part of the system owns this (e.g. "cli", "analysis engine", "caching", "reporting") — choose freely based on the code
- "user_facing": true if an end user interacts with this directly, false if it is internal plumbing

Deduplicate and sort by name alphabetically.
Respond with only the JSON array. No markdown code fences. No prose.`
	preambleTokens := countTokens(preamblePrompt)
	batches := batchSymLines(symLines, preambleTokens, MapperTokenBudget)
	featSet := make(map[string]CodeFeature)

	for i, batch := range batches {
		log.Infof("  extracting features from code batch %d/%d (%d symbol groups)", i+1, len(batches), len(batch))

		// PROMPT: Identifies product features implemented by a portion of the codebase. Returns a JSON array of objects with name, description, layer, and user_facing fields.
		prompt := fmt.Sprintf(`You are analyzing a codebase to identify the product features it implements.

Code files and their exported symbols (format: "file/path: Symbol1, Symbol2"):
%s

Return a JSON array of product features. Each element must have:
- "name": short noun phrase (max 8 words) naming the feature
- "description": 1-2 sentences describing what the feature does and its role in the product
- "layer": a short label for which part of the system owns this (e.g. "cli", "analysis engine", "caching", "reporting") — choose freely based on the code
- "user_facing": true if an end user interacts with this directly, false if it is internal plumbing

Deduplicate and sort by name alphabetically.
Respond with only the JSON array. No markdown code fences. No prose.`, strings.Join(batch, "\n"))

		raw, err := client.Complete(ctx, prompt)
		if err != nil {
			return nil, fmt.Errorf("ExtractFeaturesFromCode: %w", err)
		}

		var features []CodeFeature
		if err := json.Unmarshal([]byte(raw), &features); err != nil {
			return nil, fmt.Errorf("ExtractFeaturesFromCode: invalid JSON response: %w", err)
		}
		if features == nil {
			features = []CodeFeature{}
		}

		for _, f := range features {
			if f.Name != "" {
				featSet[f.Name] = f
			}
		}
	}

	result := make([]CodeFeature, 0, len(featSet))
	for _, f := range featSet {
		result = append(result, f)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result, nil
}
