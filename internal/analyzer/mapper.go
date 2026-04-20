package analyzer

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/sandgardenhq/find-the-gaps/internal/scanner"
)

// MapperTokenBudget is the maximum tokens per MapFeaturesToCode LLM call.
// Set well below the model maximum (1M) to leave room for the response.
const MapperTokenBudget = 80_000

type mapEntry struct {
	Feature string   `json:"feature"`
	Files   []string `json:"files"`
	Symbols []string `json:"symbols"`
}

// accEntry accumulates files and symbols for one feature across multiple batches.
type accEntry struct {
	files   map[string]struct{}
	symbols map[string]struct{}
}

// MapFeaturesToCode maps a list of product features to code files and symbols in scan.
// It batches the symbol index into token-budget-sized chunks and merges results.
func MapFeaturesToCode(ctx context.Context, client LLMClient, counter TokenCounter, features []string, scan *scanner.ProjectScan, tokenBudget int) (FeatureMap, error) {
	if len(features) == 0 {
		return FeatureMap{}, nil
	}

	// Build a compact symbol index: "path: Symbol1, Symbol2"
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
		return FeatureMap{}, nil
	}

	featuresJSON, _ := json.Marshal(features)
	featuresTokens := countTokens(string(featuresJSON))

	// Initial batches using tiktoken estimates.
	initialBatches := batchSymLines(symLines, featuresTokens, tokenBudget)

	// Accumulate results keyed by feature name.
	acc := make(map[string]*accEntry, len(features))
	for _, feat := range features {
		acc[feat] = &accEntry{
			files:   make(map[string]struct{}),
			symbols: make(map[string]struct{}),
		}
	}

	// Build set of all files that entered the batching pipeline.
	batched := make(map[string]struct{}, len(symLines))
	for _, batch := range initialBatches {
		for _, line := range batch {
			path := strings.SplitN(line, ": ", 2)[0]
			batched[path] = struct{}{}
		}
	}
	// Verify every file with symbols was batched.
	for _, f := range scan.Files {
		if len(f.Symbols) == 0 {
			continue
		}
		if _, ok := batched[f.Path]; !ok {
			return nil, fmt.Errorf("MapFeaturesToCode: file %q was not included in any batch (coverage check failed)", f.Path)
		}
	}

	// Process batches using an index-based queue to allow split-and-retry.
	queue := initialBatches
	for i := 0; i < len(queue); i++ {
		batch := queue[i]

		// PROMPT: Maps product features to the code files and symbols most likely to implement them. Returns a JSON array only.
		promptText := fmt.Sprintf(`You are mapping product features to their code implementations.

Product features:
%s

Code symbols (format: "file/path: Symbol1, Symbol2"):
%s

For each feature, identify which code files and exported symbols are most relevant to implementing it.
Return a JSON array where each element has:
- "feature": the feature name exactly as provided
- "files": list of relevant file paths (empty array if none)
- "symbols": list of relevant exported symbol names (empty array if none)

Respond with only the JSON array. No markdown code fences. No prose.`, string(featuresJSON), strings.Join(batch, "\n"))

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

		raw, err := client.Complete(ctx, promptText)
		if err != nil {
			return nil, fmt.Errorf("MapFeaturesToCode: %w", err)
		}

		var entries []mapEntry
		if err := json.Unmarshal([]byte(raw), &entries); err != nil {
			return nil, fmt.Errorf("MapFeaturesToCode: invalid JSON response: %w", err)
		}

		for _, e := range entries {
			entry, ok := acc[e.Feature]
			if !ok {
				// Feature returned by LLM not in our list — skip.
				continue
			}
			for _, f := range e.Files {
				entry.files[f] = struct{}{}
			}
			for _, s := range e.Symbols {
				entry.symbols[s] = struct{}{}
			}
		}
	}

	// Convert accumulator to FeatureMap in original features order.
	out := make(FeatureMap, 0, len(features))
	for _, feat := range features {
		entry := acc[feat]
		files := make([]string, 0, len(entry.files))
		for f := range entry.files {
			files = append(files, f)
		}
		symbols := make([]string, 0, len(entry.symbols))
		for s := range entry.symbols {
			symbols = append(symbols, s)
		}
		out = append(out, FeatureEntry{
			Feature: feat,
			Files:   files,
			Symbols: symbols,
		})
	}
	return out, nil
}
