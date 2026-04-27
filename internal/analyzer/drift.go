package analyzer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/log"
)

const (
	// driftBudgetExpectedFindings is the headroom reserved for add_finding
	// tool calls when computing a feature's drift agent round budget.
	driftBudgetExpectedFindings = 5

	// driftBudgetSlack covers re-reads, the closing plain-text turn, and any
	// other non-read overhead the agent incurs during a drift check.
	driftBudgetSlack = 3

	// driftBudgetCeiling is the hard upper bound on the per-feature drift
	// agent round budget. Protects against runaway cost when a feature
	// mapping has unrealistically many files or pages.
	driftBudgetCeiling = 100

	// driftMaxRounds is retained as a temporary alias during migration. It
	// is removed in a later task once all call sites use budgetForFeature.
	driftMaxRounds = 30
)

// budgetForFeature returns the agent round budget for a single feature's
// drift check. Each read_file and read_page tool call costs one round; each
// add_finding call costs one round; slack covers re-reads and the closing
// turn. The result is clamped at driftBudgetCeiling to bound runaway cost
// when a feature has an unrealistic number of inputs.
func budgetForFeature(files, pages int) int {
	budget := files + pages + driftBudgetExpectedFindings + driftBudgetSlack
	if budget > driftBudgetCeiling {
		return driftBudgetCeiling
	}
	return budget
}

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
	tiering LLMTiering,
	featureMap FeatureMap,
	docsMap DocsFeatureMap,
	pageReader func(url string) (string, error),
	repoRoot string,
	onFinding DriftProgressFunc,
) ([]DriftFinding, error) {
	toolClient, ok := tiering.Large().(ToolLLMClient)
	if !ok {
		return nil, fmt.Errorf("DetectDrift: large tier does not support tool use (required for drift detection); configure --llm-large with a tool-use-capable provider (anthropic or openai)")
	}
	classifier := tiering.Small()

	// Index docsMap by feature name for fast lookup.
	docPages := make(map[string][]string, len(docsMap))
	for _, entry := range docsMap {
		if len(entry.Pages) > 0 {
			docPages[entry.Feature] = entry.Pages
		}
	}

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
		pages = classifyDriftPages(ctx, classifier, pages, pageReader)
		if len(pages) == 0 {
			continue
		}

		log.Infof("  checking drift for feature %q (%d pages)", entry.Feature.Name, len(pages))
		issues, err := detectDriftForFeature(ctx, toolClient, entry, pages, pageReader, repoRoot)
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

	var findings []DriftIssue
	tools := []Tool{
		readFileTool(repoRoot),
		readPageTool(pageReader),
		addFindingTool(&findings),
	}

	// PROMPT: Reviews documentation accuracy for one feature using tool calls to read source files and cached doc pages. The agent reports each issue as it finds it via add_finding, then ends the conversation with a plain-text confirmation.
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

Each time you identify a documentation issue, call the add_finding tool with the
"page" (URL or empty string) and "issue" (one or two sentences). Call
add_finding once per issue. When you have no more issues to report, reply with
plain text confirming you are done (e.g. "done"). If you find no issues, reply
with plain text immediately without calling add_finding at all.`,
		entry.Feature.Name,
		entry.Feature.Description,
		strings.Join(entry.Files, ", "),
		strings.Join(entry.Symbols, ", "),
		strings.Join(pageSummaries, "\n"),
	)

	messages := []ChatMessage{{Role: "user", Content: systemPrompt}}

	_, err := client.CompleteWithTools(ctx, messages, tools, WithMaxRounds(driftMaxRounds))
	if errors.Is(err, ErrMaxRounds) {
		log.Warnf("drift agent exceeded %d rounds for feature %q; returning %d accumulated findings", driftMaxRounds, entry.Feature.Name, len(findings))
		return findings, nil
	}
	if err != nil {
		return nil, err
	}
	return findings, nil
}

// readFileTool returns a Tool that reads files within repoRoot. The Execute
// closure rejects paths that escape the root.
func readFileTool(repoRoot string) Tool {
	return Tool{
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
		Execute: func(_ context.Context, rawArgs string) (string, error) {
			var args struct {
				Path string `json:"path"`
			}
			if err := json.Unmarshal([]byte(rawArgs), &args); err != nil {
				return fmt.Sprintf("error parsing arguments: %v", err), nil
			}
			abs := filepath.Join(repoRoot, args.Path)
			rel, err := filepath.Rel(repoRoot, abs)
			if err != nil || strings.HasPrefix(rel, "..") {
				return "access denied: path is outside the repository root", nil
			}
			content, err := os.ReadFile(abs)
			if err != nil {
				return fmt.Sprintf("error reading file: %v", err), nil
			}
			return string(content), nil
		},
	}
}

// readPageTool returns a Tool that reads cached documentation pages via pageReader.
func readPageTool(pageReader func(url string) (string, error)) Tool {
	return Tool{
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
		Execute: func(_ context.Context, rawArgs string) (string, error) {
			var args struct {
				URL string `json:"url"`
			}
			if err := json.Unmarshal([]byte(rawArgs), &args); err != nil {
				return fmt.Sprintf("error parsing arguments: %v", err), nil
			}
			content, err := pageReader(args.URL)
			if err != nil {
				return fmt.Sprintf("page not available: %v", err), nil
			}
			return content, nil
		},
	}
}

// addFindingTool returns a Tool that appends each LLM-reported drift issue to
// out. Bad arguments are reported back to the LLM as a tool result string so
// the loop continues; only catastrophic errors would abort.
func addFindingTool(out *[]DriftIssue) Tool {
	return Tool{
		Name:        "add_finding",
		Description: "Record one documentation accuracy issue. Call once per distinct issue. When you have no more issues to report, reply with plain text instead of calling this tool.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"page": map[string]any{
					"type":        "string",
					"description": "URL of the doc page the issue refers to, or empty string if page-agnostic.",
				},
				"issue": map[string]any{
					"type":        "string",
					"description": "One or two sentences describing the documentation issue.",
				},
			},
			"required": []string{"page", "issue"},
		},
		Execute: func(_ context.Context, rawArgs string) (string, error) {
			var f DriftIssue
			if err := json.Unmarshal([]byte(rawArgs), &f); err != nil {
				return fmt.Sprintf("invalid arguments: %v", err), nil
			}
			*out = append(*out, f)
			return "recorded", nil
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
