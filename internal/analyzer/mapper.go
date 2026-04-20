package analyzer

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/sandgardenhq/find-the-gaps/internal/scanner"
)

type mapEntry struct {
	Feature string   `json:"feature"`
	Files   []string `json:"files"`
	Symbols []string `json:"symbols"`
}

// MapFeaturesToCode maps a list of product features to code files and symbols in scan.
func MapFeaturesToCode(ctx context.Context, client LLMClient, features []string, scan *scanner.ProjectScan) (FeatureMap, error) {
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

	featuresJSON, _ := json.Marshal(features)
	symbolsText := strings.Join(symLines, "\n")

	// PROMPT: Maps product features to the code files and symbols most likely to implement them. Returns a JSON array only.
	prompt := fmt.Sprintf(`You are mapping product features to their code implementations.

Product features:
%s

Code symbols (format: "file/path: Symbol1, Symbol2"):
%s

For each feature, identify which code files and exported symbols are most relevant to implementing it.
Return a JSON array where each element has:
- "feature": the feature name exactly as provided
- "files": list of relevant file paths (empty array if none)
- "symbols": list of relevant exported symbol names (empty array if none)

Respond with only the JSON array. No markdown code fences. No prose.`, string(featuresJSON), symbolsText)

	raw, err := client.Complete(ctx, prompt)
	if err != nil {
		return nil, fmt.Errorf("MapFeaturesToCode: %w", err)
	}

	var entries []mapEntry
	if err := json.Unmarshal([]byte(raw), &entries); err != nil {
		return nil, fmt.Errorf("MapFeaturesToCode: invalid JSON response: %w", err)
	}

	out := make(FeatureMap, len(entries))
	for i, e := range entries {
		if e.Files == nil {
			e.Files = []string{}
		}
		if e.Symbols == nil {
			e.Symbols = []string{}
		}
		out[i] = FeatureEntry(e)
	}
	return out, nil
}
