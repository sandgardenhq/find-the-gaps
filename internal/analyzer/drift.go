package analyzer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/charmbracelet/log"
)

const (
	// driftBudgetExpectedFindings is the headroom reserved for note_observation
	// tool calls when computing a feature's drift investigator round budget.
	driftBudgetExpectedFindings = 5

	// driftBudgetSlack covers re-reads, the closing plain-text turn, and any
	// other non-read overhead the investigator incurs during a drift check.
	driftBudgetSlack = 3

	// driftBudgetCeiling is the hard upper bound on the per-feature drift
	// investigator round budget. Protects against runaway cost when a feature
	// mapping has unrealistically many files or pages.
	driftBudgetCeiling = 100
)

// CachedDriftEntry is one feature's persisted drift result, used by
// DetectDrift to short-circuit the investigator+judge when inputs are
// unchanged. Files and FilteredPages must be sorted ascending; the lookup
// compares them as sorted sets against the current run's inputs.
//
// FilteredPages is the post-filterDriftPages, pre-classifyDriftPages list
// and is the cache key for the page side of the lookup. Pages is the
// post-classification list passed to the investigator+judge; it is retained
// for forward compatibility and debugging but the cache no longer keys on it.
// Old caches written before FilteredPages existed load with FilteredPages
// == nil and miss the cache once, then repopulate.
type CachedDriftEntry struct {
	Files         []string
	FilteredPages []string
	Pages         []string
	Issues        []DriftIssue
}

// DriftFeatureDoneFunc fires after DetectDrift decides a feature's drift
// result, whether the result came from a cache hit or a fresh investigate+judge.
// Implementations typically persist the result so a future run can resume.
//
// Files, filteredPages, and pages are sorted ascending. filteredPages is the
// post-filterDriftPages, pre-classify list used as the page side of the
// cache key. pages is the post-classify list that the investigator+judge
// actually saw. On a cache hit, pages is the previously persisted value.
//
// Return non-nil to abort detection.
type DriftFeatureDoneFunc func(feature string, files, filteredPages, pages []string, issues []DriftIssue) error

// budgetForFeature returns the investigator round budget for a single
// feature's drift check. Each read_file and read_page tool call costs one
// round; each note_observation call costs one round; slack covers re-reads
// and the closing turn. The result is clamped at driftBudgetCeiling to bound
// runaway cost when a feature has an unrealistic number of inputs.
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
//
// cached supplies prior drift results keyed by feature name; pass nil to run
// every feature fresh. A cache hit reuses Issues without invoking the
// investigator or judge. (Lookup logic is wired in a follow-up commit.)
//
// onFinding fires after each feature whose result has at least one issue,
// receiving the accumulated findings slice; pass nil to skip incremental
// callbacks. onFeatureDone fires after every completed feature regardless
// of issue count (cache-hit or fresh), receiving sorted files and pages plus
// the resolved issues; pass nil to skip persistence callbacks. Returning an
// error from either callback aborts detection.
func DetectDrift(
	ctx context.Context,
	tiering LLMTiering,
	featureMap FeatureMap,
	docsMap DocsFeatureMap,
	pageReader func(url string) (string, error),
	repoRoot string,
	cached map[string]CachedDriftEntry,
	onFinding DriftProgressFunc,
	onFeatureDone DriftFeatureDoneFunc,
) ([]DriftFinding, error) {
	investigator, ok := tiering.Typical().(ToolLLMClient)
	if !ok {
		return nil, fmt.Errorf("DetectDrift: typical tier does not support tool use (required for the drift investigator); configure --llm-typical with a tool-use-capable provider (anthropic or openai)")
	}
	judge := tiering.Large()
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

		sortedFiles := sortedCopy(entry.Files)
		sortedFiltered := sortedCopy(pages)

		if cached != nil {
			if c, ok := cached[entry.Feature.Name]; ok &&
				equalStringSlice(c.Files, sortedFiles) &&
				equalStringSlice(c.FilteredPages, sortedFiltered) &&
				!cacheNeedsRecompute(c) {
				log.Debugf("  drift cache hit: %s", entry.Feature.Name)
				issues := c.Issues
				if len(issues) > 0 {
					findings = append(findings, DriftFinding{Feature: entry.Feature.Name, Issues: issues})
					if onFinding != nil {
						if err := onFinding(findings); err != nil {
							return nil, fmt.Errorf("DetectDrift: onFinding: %w", err)
						}
					}
				}
				if onFeatureDone != nil {
					if err := onFeatureDone(entry.Feature.Name, sortedFiles, sortedFiltered, c.Pages, issues); err != nil {
						return nil, fmt.Errorf("DetectDrift: onFeatureDone: %w", err)
					}
				}
				continue
			}
		}

		// Cache miss: classify, then investigate+judge.
		pages = classifyDriftPages(ctx, classifier, pages, pageReader)
		if len(pages) == 0 {
			// Every page classified as release notes. Persist a cache entry
			// keyed on FilteredPages with empty Pages so the next run skips
			// the classifier instead of re-running it on the same content.
			if onFeatureDone != nil {
				if err := onFeatureDone(entry.Feature.Name, sortedFiles, sortedFiltered, []string{}, nil); err != nil {
					return nil, fmt.Errorf("DetectDrift: onFeatureDone: %w", err)
				}
			}
			continue
		}
		sortedPages := sortedCopy(pages)

		observations, err := investigateFeatureDrift(ctx, investigator, entry, pages, pageReader, repoRoot)
		if err != nil {
			return nil, fmt.Errorf("DetectDrift %q: %w", entry.Feature.Name, err)
		}
		issues, err := judgeFeatureDrift(ctx, judge, entry.Feature, observations)
		if err != nil {
			// A single transient judge failure (e.g. malformed JSON the
			// CompleteJSON retry-with-correction couldn't recover) must not
			// abort the run — work on every other feature would be lost.
			// Log a warning and continue. We deliberately do NOT call
			// onFeatureDone for this feature: the cache write is skipped so
			// the next run re-investigates from scratch instead of inheriting
			// a permanent silent zero-finding cache that would mask a
			// recurring model regression. Investigator errors above keep
			// their hard-fail behavior — those signal something more serious
			// than a single bad judge response.
			log.Warnf("drift judge failed for feature %q: %v; skipping feature, no cache entry written",
				entry.Feature.Name, err)
			continue
		}

		if len(issues) > 0 {
			findings = append(findings, DriftFinding{Feature: entry.Feature.Name, Issues: issues})
			if onFinding != nil {
				if err := onFinding(findings); err != nil {
					return nil, fmt.Errorf("DetectDrift: onFinding: %w", err)
				}
			}
		}
		if onFeatureDone != nil {
			if err := onFeatureDone(entry.Feature.Name, sortedFiles, sortedFiltered, sortedPages, issues); err != nil {
				return nil, fmt.Errorf("DetectDrift: onFeatureDone: %w", err)
			}
		}
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

// driftObservation is one piece of evidence the investigator surfaces for the
// judge to adjudicate. Both quotes are required and must be verbatim — they are
// the entire input the judge sees about this candidate.
type driftObservation struct {
	Page      string `json:"page"`
	DocQuote  string `json:"doc_quote"`
	CodeQuote string `json:"code_quote"`
	Concern   string `json:"concern"`
}

// noteObservationTool returns a Tool that appends each LLM-recorded observation
// to out. Bad arguments are reported back to the LLM as a tool result string so
// the loop continues.
func noteObservationTool(out *[]driftObservation) Tool {
	return Tool{
		Name:        "note_observation",
		Description: "Record one piece of evidence about possible documentation drift. Both doc_quote and code_quote must be verbatim. Call once per distinct observation. When you have nothing more to record, reply with plain text.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"page":       map[string]any{"type": "string", "description": "Doc page URL the observation refers to, or empty string if page-agnostic."},
				"doc_quote":  map[string]any{"type": "string", "description": "Verbatim passage from the docs."},
				"code_quote": map[string]any{"type": "string", "description": "Verbatim excerpt from the source code."},
				"concern":    map[string]any{"type": "string", "description": "One sentence: what looks off."},
			},
			"required": []string{"page", "doc_quote", "code_quote", "concern"},
		},
		Execute: func(_ context.Context, rawArgs string) (string, error) {
			var o driftObservation
			if err := json.Unmarshal([]byte(rawArgs), &o); err != nil {
				return fmt.Sprintf("invalid arguments: %v", err), nil
			}
			*out = append(*out, o)
			return "recorded", nil
		},
	}
}

// investigateFeatureDrift runs the agent loop with read_file, read_page, and
// note_observation tools, returning the raw observations the LLM surfaced. It
// gathers evidence; the judge stage adjudicates separately.
func investigateFeatureDrift(
	ctx context.Context,
	client ToolLLMClient,
	entry FeatureEntry,
	pages []string,
	pageReader func(url string) (string, error),
	repoRoot string,
) ([]driftObservation, error) {
	var pageSummaries []string
	for _, url := range pages {
		pageSummaries = append(pageSummaries, fmt.Sprintf("- %s", url))
	}

	var observations []driftObservation
	tools := []Tool{
		readFileTool(repoRoot),
		readPageTool(pageReader),
		noteObservationTool(&observations),
	}

	// PROMPT: Investigates a feature for documentation drift by reading source files and doc pages, recording each piece of evidence via note_observation. The investigator gathers; it does not adjudicate.
	systemPrompt := fmt.Sprintf(`You are investigating documentation accuracy for a software feature.

Feature: %s
Code description: %s
Implemented in: %s
Symbols: %s

Documentation pages:
%s

You have tools available to read source files and documentation pages in full.
Use them to investigate as needed.

Your job is to surface candidate documentation drift. For each thing that
*might* be wrong or missing in the docs, call note_observation with:
- page: the doc URL (or empty string)
- doc_quote: the exact passage from the docs that concerns you
- code_quote: the exact excerpt from the source code that contradicts or
  is missing from the docs
- concern: one sentence describing what looks off

Quote verbatim. Include enough context in code_quote that someone reading
just the observation can understand the contradiction (e.g. include the full
function signature line, not just an identifier).

Do not decide whether something IS drift — just record what looks suspicious.
A reviewer will adjudicate later.

When you have nothing more to record, reply with plain text (e.g. "done").
If you find nothing suspicious, reply with plain text immediately without
calling note_observation at all.`,
		entry.Feature.Name,
		entry.Feature.Description,
		strings.Join(entry.Files, ", "),
		strings.Join(entry.Symbols, ", "),
		strings.Join(pageSummaries, "\n"),
	)

	messages := []ChatMessage{{Role: "user", Content: systemPrompt, CacheBreakpoint: true}}

	budget := budgetForFeature(len(entry.Files), len(pages))
	log.Infof("  investigating drift for feature %q (%d files, %d pages, budget %d rounds)",
		entry.Feature.Name, len(entry.Files), len(pages), budget)
	_, err := client.CompleteWithTools(ctx, messages, tools, WithMaxRounds(budget))
	if errors.Is(err, ErrMaxRounds) {
		log.Warnf("drift investigator exceeded budget of %d rounds for feature %q (%d files, %d pages); handing %d observations to judge",
			budget, entry.Feature.Name, len(entry.Files), len(pages), len(observations))
		return observations, nil
	}
	if err != nil {
		return nil, err
	}
	return observations, nil
}

// judgeSchema is the structured-output contract for judgeFeatureDrift. The
// judge returns an "issues" array of DriftIssue-shaped objects; an empty array
// means "every observation was a false alarm".
var judgeSchema = JSONSchema{
	Name: "drift_judge_issues",
	Doc: json.RawMessage(`{
      "type": "object",
      "properties": {
        "issues": {
          "type": "array",
          "items": {
            "type": "object",
            "properties": {
              "page":            {"type": "string"},
              "issue":           {"type": "string"},
              "priority":        {"type": "string", "enum": ["large", "medium", "small"]},
              "priority_reason": {"type": "string"}
            },
            "required": ["page", "issue", "priority", "priority_reason"],
            "additionalProperties": false
          }
        }
      },
      "required": ["issues"],
      "additionalProperties": false
    }`),
}

// validateDriftIssues fails closed when the LLM returns an issue without a
// valid priority enum value or with an empty priority_reason. The four
// values strings.TrimSpace removes match the same strings the JSON Schema
// enum constraint allows; this is belt-and-suspenders against providers that
// silently let the schema slip.
func validateDriftIssues(issues []DriftIssue) error {
	for i, iss := range issues {
		switch iss.Priority {
		case PriorityLarge, PriorityMedium, PrioritySmall:
		default:
			return fmt.Errorf("issue %d: invalid priority %q", i, iss.Priority)
		}
		if strings.TrimSpace(iss.PriorityReason) == "" {
			return fmt.Errorf("issue %d: empty priority_reason", i)
		}
	}
	return nil
}

// uniqueObservationPages returns the set of non-empty page URLs that appear
// across observations, preserving first-seen order.
func uniqueObservationPages(observations []driftObservation) []string {
	seen := map[string]bool{}
	var out []string
	for _, o := range observations {
		if o.Page == "" || seen[o.Page] {
			continue
		}
		seen[o.Page] = true
		out = append(out, o.Page)
	}
	return out
}

// cacheNeedsRecompute reports whether a cached drift entry must be discarded
// because at least one of its issues lacks a valid priority. Older caches
// written before the priority feature shipped fall through this path; on a
// rerun we recompute the issues so they pick up priorities.
func cacheNeedsRecompute(entry CachedDriftEntry) bool {
	for _, iss := range entry.Issues {
		switch iss.Priority {
		case PriorityLarge, PriorityMedium, PrioritySmall:
			continue
		default:
			return true
		}
	}
	return false
}

// pageRoleSummary returns a human-readable list of "<url> -> <role>" lines
// for the pages observed during drift investigation. Fed into the judge
// prompt so the priority rubric can weight prominent pages higher.
func pageRoleSummary(pages []string) string {
	if len(pages) == 0 {
		return "Page role hints: (no specific pages)"
	}
	var b strings.Builder
	b.WriteString("Page role hints:\n")
	for _, p := range pages {
		fmt.Fprintf(&b, "- %s -> %s\n", p, pageRole(p))
	}
	return b.String()
}

// judgeResponse mirrors judgeSchema for unmarshaling.
type judgeResponse struct {
	Issues []DriftIssue `json:"issues"`
}

// judgeFeatureDrift adjudicates the investigator's observations for one feature
// in a single non-tool CompleteJSON call. With zero observations it short-
// circuits and returns nil without calling the LLM.
func judgeFeatureDrift(
	ctx context.Context,
	client LLMClient,
	feature CodeFeature,
	observations []driftObservation,
) ([]DriftIssue, error) {
	if len(observations) == 0 {
		return nil, nil
	}

	var b strings.Builder
	for i, o := range observations {
		fmt.Fprintf(&b, "[%d] page: %s\n    docs say: %q\n    code shows: %q\n    concern: %s\n",
			i+1, o.Page, o.DocQuote, o.CodeQuote, o.Concern)
	}

	// PROMPT: Adjudicates a list of candidate drift observations for one feature, dropping false alarms, collapsing observations that describe the same docs problem into one issue, and emitting actionable documentation feedback as DriftIssues with a user-impact priority rating.
	prompt := fmt.Sprintf(`You are reviewing candidate documentation drift observations for one software feature.

Feature: %s
Description: %s

Candidate drift observations from investigation:
%s

%s

For each observation, decide whether it represents real documentation drift or
a false alarm. Drop false alarms entirely. If multiple observations describe
the same documentation problem, emit a single DriftIssue covering them all —
do not emit one issue per observation.

Each emitted issue must be actionable documentation feedback — describe what
is wrong or missing in the docs, not what the code does. One or two sentences.

%s

Output only the fields defined in the schema (page, issue, priority,
priority_reason). Do not add any other fields.

If every observation is a false alarm, emit an empty "issues" array.`,
		feature.Name, feature.Description, b.String(),
		pageRoleSummary(uniqueObservationPages(observations)),
		priorityRubric)

	raw, err := client.CompleteJSON(ctx, prompt, judgeSchema)
	if err != nil {
		return nil, fmt.Errorf("judgeFeatureDrift %q: %w", feature.Name, err)
	}
	var resp judgeResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("judgeFeatureDrift %q: invalid JSON response: %w", feature.Name, err)
	}
	if err := validateDriftIssues(resp.Issues); err != nil {
		return nil, fmt.Errorf("judgeFeatureDrift %q: %w", feature.Name, err)
	}
	return resp.Issues, nil
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

// sortedCopy returns a sorted copy of s. The input is not modified.
func sortedCopy(s []string) []string {
	out := append([]string(nil), s...)
	sort.Strings(out)
	return out
}

// equalStringSlice reports whether a and b are equal element-wise. Both
// must already be sorted; this is not a set comparison.
func equalStringSlice(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
