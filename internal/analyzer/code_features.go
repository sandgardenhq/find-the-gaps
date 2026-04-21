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
func ExtractFeaturesFromCode(ctx context.Context, client LLMClient, scan *scanner.ProjectScan) ([]string, error) {
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
		return []string{}, nil
	}

	// Approximate token cost of the fixed prompt preamble so batchSymLines
	// leaves room for the preamble plus the symbol lines.
	preamblePrompt := `You are analyzing a codebase to identify the product features it implements.

Code files and their exported symbols (format: "file/path: Symbol1, Symbol2"):


Based on the exported symbols and file structure, return a JSON array of product features this codebase implements.
Each feature should be a short noun phrase (max 8 words) describing a user-facing capability.
Deduplicate and sort alphabetically.

Respond with only the JSON array. No markdown code fences. No prose.`
	preambleTokens := countTokens(preamblePrompt)
	batches := batchSymLines(symLines, preambleTokens, MapperTokenBudget)
	featSet := make(map[string]struct{})

	for i, batch := range batches {
		log.Infof("  extracting features from code batch %d/%d (%d symbol groups)", i+1, len(batches), len(batch))

		// PROMPT: Identifies product features implemented by a portion of the codebase. Returns a JSON array of short noun-phrase feature names.
		prompt := fmt.Sprintf(`You are analyzing a codebase to identify the product features it implements.

Code files and their exported symbols (format: "file/path: Symbol1, Symbol2"):
%s

Based on the exported symbols and file structure, return a JSON array of product features this codebase implements.
Each feature should be a short noun phrase (max 8 words) describing a user-facing capability.
Deduplicate and sort alphabetically.

Respond with only the JSON array. No markdown code fences. No prose.`, strings.Join(batch, "\n"))

		raw, err := client.Complete(ctx, prompt)
		if err != nil {
			return nil, fmt.Errorf("ExtractFeaturesFromCode: %w", err)
		}

		var features []string
		if err := json.Unmarshal([]byte(raw), &features); err != nil {
			return nil, fmt.Errorf("ExtractFeaturesFromCode: invalid JSON response: %w", err)
		}

		for _, f := range features {
			if f != "" {
				featSet[f] = struct{}{}
			}
		}
	}

	result := make([]string, 0, len(featSet))
	for f := range featSet {
		result = append(result, f)
	}
	sort.Strings(result)
	return result, nil
}
