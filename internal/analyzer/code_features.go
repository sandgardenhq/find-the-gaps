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

// codeFeaturesResponse wraps the CodeFeature array in an object because
// provider tool-call input_schemas must be JSON objects at the root.
type codeFeaturesResponse struct {
	Features []CodeFeature `json:"features"`
}

// PROMPT SCHEMA: output shape for ExtractFeaturesFromCode.
var codeFeaturesSchema = JSONSchema{
	Name: "code_features_response",
	Doc: json.RawMessage(`{
      "type": "object",
      "properties": {
        "features": {
          "type": "array",
          "items": {
            "type": "object",
            "properties": {
              "name":        {"type": "string"},
              "description": {"type": "string"},
              "layer":       {"type": "string"},
              "user_facing": {"type": "boolean"}
            },
            "required": ["name", "description", "layer", "user_facing"],
            "additionalProperties": false
          }
        }
      },
      "required": ["features"],
      "additionalProperties": false
    }`),
}

// ExtractFeaturesFromCode asks the LLM to identify product features from the
// codebase's exported symbol index. It uses the same symbol-line format and
// batching strategy as MapFeaturesToCode. Features from multiple batches are
// deduplicated and sorted.
func ExtractFeaturesFromCode(ctx context.Context, tiering LLMTiering, scan *scanner.ProjectScan) ([]CodeFeature, error) {
	client := tiering.Typical()
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


Populate "features" with product feature objects. Each object must have:
- "name": short noun phrase (max 8 words) naming the feature
- "description": 1-2 sentences describing what the feature does and its role in the product
- "layer": a short label for which part of the system owns this (e.g. "cli", "analysis engine", "caching", "reporting") — choose freely based on the code
- "user_facing": true if an end user interacts with this directly, false if it is internal plumbing

Deduplicate and sort by name alphabetically.`
	preambleTokens := countTokens(preamblePrompt)
	batches := batchSymLines(symLines, preambleTokens, MapperTokenBudget)
	featSet := make(map[string]CodeFeature)

	for i, batch := range batches {
		log.Infof("  extracting features from code batch %d/%d (%d symbol groups)", i+1, len(batches), len(batch))

		// PROMPT: Identifies product features implemented by a portion of the codebase.
		prompt := fmt.Sprintf(`You are analyzing a codebase to identify the product features it implements.

Code files and their exported symbols (format: "file/path: Symbol1, Symbol2"):
%s

Populate "features" with product feature objects. Each object must have:
- "name": short noun phrase (max 8 words) naming the feature
- "description": 1-2 sentences describing what the feature does and its role in the product
- "layer": a short label for which part of the system owns this (e.g. "cli", "analysis engine", "caching", "reporting") — choose freely based on the code
- "user_facing": true if an end user interacts with this directly, false if it is internal plumbing

Deduplicate and sort by name alphabetically.`, strings.Join(batch, "\n"))

		raw, err := client.CompleteJSON(ctx, prompt, codeFeaturesSchema)
		if err != nil {
			return nil, fmt.Errorf("ExtractFeaturesFromCode: %w", err)
		}

		var resp codeFeaturesResponse
		if err := json.Unmarshal(raw, &resp); err != nil {
			return nil, fmt.Errorf("ExtractFeaturesFromCode: invalid JSON response: %w", err)
		}

		for _, f := range resp.Features {
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
