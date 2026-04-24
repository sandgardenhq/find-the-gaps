package analyzer

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/charmbracelet/log"
	"github.com/sandgardenhq/find-the-gaps/internal/scanner"
)

// MapperTokenBudget is the maximum tokens per MapFeaturesToCode LLM call.
// Set well below the model maximum (1M) to leave room for the response.
const MapperTokenBudget = 80_000

// MapProgressFunc is called with the accumulated results after each LLM batch.
// Returning a non-nil error aborts the mapping.
type MapProgressFunc func(partial FeatureMap) error

type mapEntry struct {
	Feature string   `json:"feature"`
	Files   []string `json:"files"`
	Symbols []string `json:"symbols,omitempty"`
}

// mapResponse wraps the mapEntry array because provider tool-call
// input_schemas must be JSON objects at the root.
type mapResponse struct {
	Entries []mapEntry `json:"entries"`
}

// PROMPT SCHEMA: output shape for MapFeaturesToCode (symbols optional so the
// same schema serves both filesOnly and full modes).
var mapSchema = JSONSchema{
	Name: "map_response",
	Doc: json.RawMessage(`{
      "type": "object",
      "properties": {
        "entries": {
          "type": "array",
          "items": {
            "type": "object",
            "properties": {
              "feature": {"type": "string"},
              "files":   {"type": "array", "items": {"type": "string"}},
              "symbols": {"type": "array", "items": {"type": "string"}}
            },
            "required": ["feature", "files"],
            "additionalProperties": false
          }
        }
      },
      "required": ["entries"],
      "additionalProperties": false
    }`),
}

// accEntry accumulates files and symbols for one feature across multiple batches.
type accEntry struct {
	files   map[string]struct{}
	symbols map[string]struct{}
}

// MapFeaturesToCode maps a list of product features to code files and symbols in scan.
// It batches the symbol index into token-budget-sized chunks and merges results.
// When filesOnly is true, the LLM prompt contains only file paths (no symbol names) and
// symbols are not accumulated even if the LLM response includes them.
// onBatch, if non-nil, is called with the accumulated results after each LLM call.
// Dispatches through the Large tier — feature-to-code mapping is the hardest mapper call.
func MapFeaturesToCode(ctx context.Context, tiering LLMTiering, features []CodeFeature, scan *scanner.ProjectScan, tokenBudget int, filesOnly bool, onBatch MapProgressFunc) (FeatureMap, error) {
	client := tiering.Large()
	counter := tiering.LargeCounter()
	if len(features) == 0 {
		return FeatureMap{}, nil
	}

	// Extract feature names for use in prompts and accumulator keying.
	featureNames := make([]string, len(features))
	for i, f := range features {
		featureNames[i] = f.Name
	}

	// Build the code index sent to the LLM.
	// In filesOnly mode, send only file paths; otherwise send "path: Symbol1, Symbol2".
	var symLines []string
	for _, f := range scan.Files {
		if len(f.Symbols) == 0 {
			continue
		}
		if filesOnly {
			symLines = append(symLines, f.Path)
		} else {
			names := make([]string, len(f.Symbols))
			for i, s := range f.Symbols {
				names[i] = s.Name
			}
			symLines = append(symLines, fmt.Sprintf("%s: %s", f.Path, strings.Join(names, ", ")))
		}
	}

	if len(symLines) == 0 {
		return FeatureMap{}, nil
	}

	featuresJSON, _ := json.Marshal(featureNames)
	featuresTokens := countTokens(string(featuresJSON))

	// Initial batches using tiktoken estimates.
	initialBatches := batchSymLines(symLines, featuresTokens, tokenBudget)

	// Accumulate results keyed by feature name.
	acc := make(map[string]*accEntry, len(features))
	for _, feat := range features {
		acc[feat.Name] = &accEntry{
			files:   make(map[string]struct{}),
			symbols: make(map[string]struct{}),
		}
	}

	// Process batches using an index-based queue to allow split-and-retry.
	queue := initialBatches
	for i := 0; i < len(queue); i++ {
		batch := queue[i]

		var promptText string
		if filesOnly {
			// PROMPT: Maps product features to code files only (symbol analysis disabled).
			promptText = fmt.Sprintf(`You are mapping product features to their code implementations.

Product features:
%s

Code files:
%s

For each feature, identify which code files are most relevant to implementing it.
Populate "entries" with one object per feature, where each object has:
- "feature": the feature name exactly as provided
- "files": list of relevant file paths (empty array if none)`, string(featuresJSON), strings.Join(batch, "\n"))
		} else {
			// PROMPT: Maps product features to the code files and symbols most likely to implement them.
			promptText = fmt.Sprintf(`You are mapping product features to their code implementations.

Product features:
%s

Code symbols (format: "file/path: Symbol1, Symbol2"):
%s

For each feature, identify which code files and exported symbols are most relevant to implementing it.
Populate "entries" with one object per feature, where each object has:
- "feature": the feature name exactly as provided
- "files": list of relevant file paths (empty array if none)
- "symbols": list of relevant exported symbol names (empty array if none)`, string(featuresJSON), strings.Join(batch, "\n"))
		}

		// Validate with provider-exact token count; split if over budget.
		tokenCount, err := counter.CountTokens(ctx, promptText)
		if err != nil {
			return nil, fmt.Errorf("MapFeaturesToCode: count tokens: %w", err)
		}
		if tokenCount > tokenBudget && len(batch) > 1 {
			mid := len(batch) / 2
			first := append([]string(nil), batch[:mid]...)
			second := append([]string(nil), batch[mid:]...)
			newQueue := make([][]string, 0, len(queue)-1+2)
			newQueue = append(newQueue, queue[:i]...)
			newQueue = append(newQueue, first, second)
			newQueue = append(newQueue, queue[i+1:]...)
			queue = newQueue
			i-- // re-process position i so the loop counter increment lands on `first`
			continue
		}

		batchKind := "symbol groups"
		if filesOnly {
			batchKind = "files"
		}
		log.Infof("  batch %d/%d: %d %s", i+1, len(queue), len(batch), batchKind)

		raw, err := client.CompleteJSON(ctx, promptText, mapSchema)
		if err != nil {
			return nil, fmt.Errorf("MapFeaturesToCode: %w", err)
		}

		var resp mapResponse
		if err := json.Unmarshal(raw, &resp); err != nil {
			return nil, fmt.Errorf("MapFeaturesToCode: invalid JSON response: %w", err)
		}

		for _, e := range resp.Entries {
			entry, ok := acc[e.Feature]
			if !ok {
				// Feature returned by LLM not in our list — skip.
				continue
			}
			for _, f := range e.Files {
				entry.files[f] = struct{}{}
			}
			if !filesOnly {
				for _, s := range e.Symbols {
					entry.symbols[s] = struct{}{}
				}
			}
		}

		if onBatch != nil {
			partial := accToFeatureMap(acc, features)
			if err := onBatch(partial); err != nil {
				return nil, fmt.Errorf("MapFeaturesToCode: onBatch: %w", err)
			}
		}
	}

	return accToFeatureMap(acc, features), nil
}

func accToFeatureMap(acc map[string]*accEntry, features []CodeFeature) FeatureMap {
	out := make(FeatureMap, 0, len(features))
	for _, feat := range features {
		e := acc[feat.Name] // always present — acc is pre-seeded from features
		files := make([]string, 0, len(e.files))
		for f := range e.files {
			files = append(files, f)
		}
		symbols := make([]string, 0, len(e.symbols))
		for s := range e.symbols {
			symbols = append(symbols, s)
		}
		out = append(out, FeatureEntry{
			Feature: feat,
			Files:   files,
			Symbols: symbols,
		})
	}
	return out
}
