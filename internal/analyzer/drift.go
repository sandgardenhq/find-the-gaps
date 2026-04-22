package analyzer

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/log"
)

const driftMaxRounds = 8

// DetectDrift compares each documented feature's code against its doc pages
// and returns a list of specific inaccuracies expressed as documentation feedback.
//
// Only features that have both code files AND at least one matching doc page are
// checked — features with no pages are undocumented (handled by WriteGaps), not
// drift candidates.
//
// pageReader reads the cached content of a doc page by URL. repoRoot is the
// absolute path to the repository root, used to constrain read_file access.
func DetectDrift(
	ctx context.Context,
	client ToolLLMClient,
	featureMap FeatureMap,
	docsMap DocsFeatureMap,
	pageReader func(url string) (string, error),
	repoRoot string,
) ([]DriftFinding, error) {
	// Index docsMap by feature name for fast lookup.
	docPages := make(map[string][]string, len(docsMap))
	for _, entry := range docsMap {
		if len(entry.Pages) > 0 {
			docPages[entry.Feature] = entry.Pages
		}
	}

	tools := driftTools()
	var findings []DriftFinding

	for _, entry := range featureMap {
		if len(entry.Files) == 0 {
			continue
		}
		pages, ok := docPages[entry.Feature.Name]
		if !ok || len(pages) == 0 {
			continue
		}

		log.Infof("  checking drift for feature %q (%d pages)", entry.Feature.Name, len(pages))
		issues, err := detectDriftForFeature(ctx, client, tools, entry, pages, pageReader, repoRoot)
		if err != nil {
			return nil, fmt.Errorf("DetectDrift %q: %w", entry.Feature.Name, err)
		}
		if len(issues) > 0 {
			findings = append(findings, DriftFinding{Feature: entry.Feature.Name, Issues: issues})
		}
	}

	return findings, nil
}

func detectDriftForFeature(
	ctx context.Context,
	client ToolLLMClient,
	tools []Tool,
	entry FeatureEntry,
	pages []string,
	pageReader func(url string) (string, error),
	repoRoot string,
) ([]DriftIssue, error) {
	// Build page summary lines for the initial prompt.
	var pageSummaries []string
	for _, url := range pages {
		pageSummaries = append(pageSummaries, fmt.Sprintf("- %s", url))
	}

	// PROMPT: Reviews documentation accuracy for one feature using tool calls to read source files and cached doc pages. Returns a JSON array of specific inaccuracies expressed as documentation feedback.
	systemPrompt := fmt.Sprintf(`You are reviewing documentation accuracy for a software feature.

Feature: %s
Code description: %s
Implemented in: %s
Symbols: %s

Documentation pages:
%s

You have tools available to read source files and documentation pages in full.
Use them to investigate as needed before producing your findings.

Identify specific inaccuracies, missing information, or outdated content in the
documentation relative to what the code actually does. This includes:
- Features or behaviors documented but no longer present in code
- Parameters, fields, or requirements not mentioned in docs
- Incorrect descriptions of how something works
- Any other misleading or stale content

Do NOT flag entire features as undocumented — only report inaccuracies or gaps
within documentation that already exists for this feature.

Express each finding as documentation feedback — describe what is wrong or
missing in the docs, not what the code does. One finding per specific issue.

When you are done investigating, return a JSON array of objects:
[{"page": "<url or empty string>", "issue": "<one or two sentences>"}]

If no issues are found, return [].
Respond with only the JSON array. No markdown code fences. No prose.`,
		entry.Feature.Name,
		entry.Feature.Description,
		strings.Join(entry.Files, ", "),
		strings.Join(entry.Symbols, ", "),
		strings.Join(pageSummaries, "\n"),
	)

	messages := []ChatMessage{{Role: "user", Content: systemPrompt}}

	for round := 0; round < driftMaxRounds; round++ {
		resp, err := client.CompleteWithTools(ctx, messages, tools)
		if err != nil {
			return nil, err
		}
		messages = append(messages, resp)

		if len(resp.ToolCalls) == 0 {
			// Final response — extract and parse the JSON array.
			// The LLM may include prose before or after the array despite the prompt
			// instruction; find the outermost [...] and parse only that.
			raw := extractJSONArray(resp.Content)
			var issues []DriftIssue
			if err := json.Unmarshal([]byte(raw), &issues); err != nil {
				return nil, fmt.Errorf("invalid JSON drift response: %w (raw: %q)", err, resp.Content)
			}
			return issues, nil
		}

		// Execute each tool call and append results.
		for _, tc := range resp.ToolCalls {
			result := executeTool(tc, pageReader, repoRoot)
			messages = append(messages, ChatMessage{
				Role:       "tool",
				Content:    result,
				ToolCallID: tc.ID,
			})
		}
	}

	return nil, fmt.Errorf("drift agent loop exceeded %d rounds without a final response", driftMaxRounds)
}

// executeTool runs one tool call and returns the result string to feed back to the LLM.
func executeTool(tc ToolCall, pageReader func(url string) (string, error), repoRoot string) string {
	switch tc.Name {
	case "read_file":
		return executeReadFile(tc.Arguments, repoRoot)
	case "read_page":
		return executeReadPage(tc.Arguments, pageReader)
	default:
		return fmt.Sprintf("unknown tool: %q", tc.Name)
	}
}

func executeReadFile(rawArgs, repoRoot string) string {
	var args struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal([]byte(rawArgs), &args); err != nil {
		return fmt.Sprintf("error parsing arguments: %v", err)
	}
	// Resolve to absolute path and verify it stays within repoRoot.
	abs := filepath.Join(repoRoot, args.Path)
	rel, err := filepath.Rel(repoRoot, abs)
	if err != nil || strings.HasPrefix(rel, "..") {
		return "access denied: path is outside the repository root"
	}
	content, err := os.ReadFile(abs)
	if err != nil {
		return fmt.Sprintf("error reading file: %v", err)
	}
	return string(content)
}

func executeReadPage(rawArgs string, pageReader func(url string) (string, error)) string {
	var args struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal([]byte(rawArgs), &args); err != nil {
		return fmt.Sprintf("error parsing arguments: %v", err)
	}
	content, err := pageReader(args.URL)
	if err != nil {
		return fmt.Sprintf("page not available: %v", err)
	}
	return content
}

// driftTools returns the two tool definitions available during drift detection.
func driftTools() []Tool {
	return []Tool{
		{
			Name:        "read_file",
			Description: "Read the full source content of a file in the repository. Use this to inspect implementation details before assessing documentation accuracy.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path": map[string]any{
						"type":        "string",
						"description": "Repository-relative file path, e.g. internal/auth/login.go",
					},
				},
				"required": []string{"path"},
			},
		},
		{
			Name:        "read_page",
			Description: "Read the full cached content of a documentation page. Use this to inspect what the docs currently say before comparing against the code.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"url": map[string]any{
						"type":        "string",
						"description": "The full URL of the documentation page.",
					},
				},
				"required": []string{"url"},
			},
		},
	}
}

// extractJSONArray finds the outermost [...] in s and returns it.
// If no array brackets are found, it returns the trimmed input unchanged so
// the caller's json.Unmarshal still produces a useful error.
func extractJSONArray(s string) string {
	start := strings.Index(s, "[")
	end := strings.LastIndex(s, "]")
	if start == -1 || end == -1 || end < start {
		return strings.TrimSpace(s)
	}
	return s[start : end+1]
}
