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

const driftMaxRounds = 20

// DriftProgressFunc is called after each feature's findings are appended to the
// accumulated slice. It receives the full accumulated slice so far. Return a
// non-nil error to abort detection early.
type DriftProgressFunc func(accumulated []DriftFinding) error

// DetectDrift compares each documented feature's code against its doc pages
// and returns a list of specific inaccuracies expressed as documentation feedback.
//
// Only features that have both code files AND at least one matching doc page are
// checked — features with no pages are undocumented (handled by WriteGaps), not
// drift candidates.
//
// pageReader reads the cached content of a doc page by URL. repoRoot is the
// absolute path to the repository root, used to constrain read_file access.
// onFinding is called after each feature with findings is processed; pass nil
// to skip incremental callbacks.
func DetectDrift(
	ctx context.Context,
	client ToolLLMClient,
	featureMap FeatureMap,
	docsMap DocsFeatureMap,
	pageReader func(url string) (string, error),
	repoRoot string,
	onFinding DriftProgressFunc,
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
		pages = filterDriftPages(pages)
		if len(pages) == 0 {
			continue
		}
		pages = classifyDriftPages(ctx, client, pages, pageReader)
		if len(pages) == 0 {
			continue
		}

		log.Infof("  checking drift for feature %q (%d pages)", entry.Feature.Name, len(pages))
		issues, err := detectDriftForFeature(ctx, client, tools, entry, pages, pageReader, repoRoot)
		if err != nil {
			return nil, fmt.Errorf("DetectDrift %q: %w", entry.Feature.Name, err)
		}
		if len(issues) > 0 {
			findings = append(findings, DriftFinding{Feature: entry.Feature.Name, Issues: issues})
			if onFinding != nil {
				if err := onFinding(findings); err != nil {
					return nil, fmt.Errorf("DetectDrift: onFinding: %w", err)
				}
			}
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

// releaseNotePatterns are URL path segments that identify changelog/release-note
// pages. These pages narrate changes over time and are not feature documentation,
// so drift detection skips them.
var releaseNotePatterns = []string{
	"release-note",
	"release_note",
	"changelog",
	"change-log",
	"change_log",
	"what-s-new",
	"whats-new",
	"what_s_new",
	"whats_new",
}

// filterDriftPages returns only the pages whose URLs do not look like release
// note or changelog pages.
func filterDriftPages(pages []string) []string {
	var out []string
	for _, p := range pages {
		lower := strings.ToLower(p)
		skip := false
		for _, pat := range releaseNotePatterns {
			if strings.Contains(lower, pat) {
				skip = true
				break
			}
		}
		if !skip {
			out = append(out, p)
		}
	}
	return out
}

// classifyDriftPages filters pages by reading their content and asking the LLM
// to decide whether each page is release notes or feature documentation. Pages
// where the content cannot be read or the LLM errors are included (fail open).
func classifyDriftPages(ctx context.Context, client LLMClient, pages []string, pageReader func(string) (string, error)) []string {
	var out []string
	for _, page := range pages {
		content, err := pageReader(page)
		if err != nil {
			// Can't read the page for classification; include it so the drift
			// agent can report the read failure via its own tool call.
			out = append(out, page)
			continue
		}
		if !isReleaseNotePage(ctx, client, page, content) {
			out = append(out, page)
		}
	}
	return out
}

// isReleaseNotePage asks the LLM to classify whether the given page content is
// a release notes or changelog page rather than feature documentation. Returns
// false on error so that unclassifiable pages are included in drift detection.
func isReleaseNotePage(ctx context.Context, client LLMClient, url, content string) bool {
	const previewLen = 1000
	preview := content
	if len(preview) > previewLen {
		preview = preview[:previewLen]
	}
	// PROMPT: Classifies whether a documentation page contains release notes, a changelog, or version history rather than current feature documentation.
	prompt := fmt.Sprintf(`Does this page contain release notes, a changelog, or version history? Answer only "yes" or "no".

URL: %s

Content preview:
%s`, url, preview)
	resp, err := client.Complete(ctx, prompt)
	if err != nil {
		return false
	}
	return strings.Contains(strings.ToLower(resp), "yes")
}

// extractJSONArray scans s from right to left, looking for the rightmost '['
// that begins a syntactically valid JSON array (through the end of the string).
// This handles LLM responses that include prose before or after the array, or
// that contain Go slice-type notation (e.g. "[]string") in the prose.
// If no valid array is found, the trimmed input is returned unchanged so the
// caller's json.Unmarshal still produces a useful error.
func extractJSONArray(s string) string {
	s = strings.TrimSpace(s)
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] != '[' {
			continue
		}
		var probe []json.RawMessage
		if json.Unmarshal([]byte(s[i:]), &probe) == nil {
			return s[i:]
		}
	}
	return s
}
