package analyzer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/charmbracelet/log"

	"github.com/sandgardenhq/find-the-gaps/internal/chunker"
	"github.com/sandgardenhq/find-the-gaps/internal/parallel"
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

	// driftJudgeMaxAttempts is the total number of times the judge LLM call
	// will be tried (1 initial + up to 3 retries) before giving up on the
	// feature. Covers transient provider failures and malformed JSON
	// responses without retrying long enough to drag the run out.
	driftJudgeMaxAttempts = 4
)

// ErrLLMRetriesExhausted is wrapped into the error returned by the drift judge
// when every retry attempt against the LLM provider failed. The CLI checks for
// it via errors.Is to surface a restart-friendly message — completed features
// are persisted to the drift cache and a re-run resumes from them.
var ErrLLMRetriesExhausted = errors.New("llm retries exhausted")

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
	// RolesHash is the hex SHA-256 fingerprint of the URL→role mapping for
	// FilteredPages, as produced by FilteredPagesRolesHash. The judge prompt
	// embeds these roles in its priority rubric, so a stale RolesHash means
	// the cached Issues' priorities reflect a prior classification and must
	// be recomputed. Empty (the zero value) on entries written before this
	// field shipped — those entries always miss and recompute once.
	RolesHash string
	Issues    []DriftIssue
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
// rolesHash is the FilteredPagesRolesHash computed against the run's role
// resolver and the same sorted filteredPages list. Persisters MUST store it
// so the next run can invalidate when role classification changes.
//
// Return non-nil to abort detection.
type DriftFeatureDoneFunc func(feature string, files, filteredPages, pages []string, rolesHash string, issues []DriftIssue) error

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
	roles RoleResolver,
	repoRoot string,
	workers int,
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

	// Build the work list up-front. Features that fail the early skip
	// conditions (no files, no pages, all pages release-note-shaped) are
	// dropped here so each parallel worker has a uniform body.
	type driftJob struct {
		entry          FeatureEntry
		pages          []string
		sortedFiles    []string
		sortedFiltered []string
	}
	jobs := make([]driftJob, 0, len(featureMap))
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
		jobs = append(jobs, driftJob{
			entry:          entry,
			pages:          pages,
			sortedFiles:    sortedCopy(entry.Files),
			sortedFiltered: sortedCopy(pages),
		})
	}

	var (
		findingsMu sync.Mutex
		findings   []DriftFinding
	)

	// appendAndSnapshot appends f to findings under findingsMu and returns a
	// stable copy of the accumulated slice. The snapshot lets callers fire
	// onFinding outside the lock without seeing concurrent mutations.
	appendAndSnapshot := func(f DriftFinding) []DriftFinding {
		findingsMu.Lock()
		defer findingsMu.Unlock()
		findings = append(findings, f)
		return append([]DriftFinding(nil), findings...)
	}

	err := parallel.Run(ctx, jobs, workers, func(ctx context.Context, job driftJob) error {
		entry := job.entry
		sortedFiles := job.sortedFiles
		sortedFiltered := job.sortedFiltered
		pages := job.pages

		// Fingerprint of the URL→role mapping the judge prompt will see.
		// Folded into the cache key so a role reclassification on stable
		// files+pages forces a re-judge instead of replaying stale
		// priorities. See FilteredPagesRolesHash.
		currentRolesHash := FilteredPagesRolesHash(roles, sortedFiltered)

		if cached != nil {
			if c, ok := cached[entry.Feature.Name]; ok &&
				equalStringSlice(c.Files, sortedFiles) &&
				equalStringSlice(c.FilteredPages, sortedFiltered) &&
				c.RolesHash == currentRolesHash &&
				!cacheNeedsRecompute(c) {
				log.Debugf("  drift cache hit: %s", entry.Feature.Name)
				issues := c.Issues
				if len(issues) > 0 {
					snapshot := appendAndSnapshot(DriftFinding{Feature: entry.Feature.Name, Issues: issues})
					if onFinding != nil {
						if err := onFinding(snapshot); err != nil {
							return fmt.Errorf("DetectDrift: onFinding: %w", err)
						}
					}
				}
				if onFeatureDone != nil {
					if err := onFeatureDone(entry.Feature.Name, sortedFiles, sortedFiltered, c.Pages, currentRolesHash, issues); err != nil {
						return fmt.Errorf("DetectDrift: onFeatureDone: %w", err)
					}
				}
				return nil
			}
		}

		// Cache miss: classify, then investigate+judge.
		pages = classifyDriftPages(ctx, classifier, pages, pageReader)
		if len(pages) == 0 {
			// Every page classified as release notes. Persist a cache entry
			// keyed on FilteredPages with empty Pages so the next run skips
			// the classifier instead of re-running it on the same content.
			if onFeatureDone != nil {
				if err := onFeatureDone(entry.Feature.Name, sortedFiles, sortedFiltered, []string{}, currentRolesHash, nil); err != nil {
					return fmt.Errorf("DetectDrift: onFeatureDone: %w", err)
				}
			}
			return nil
		}
		sortedPages := sortedCopy(pages)

		observations, err := investigateFeatureDrift(ctx, investigator, entry, pages, pageReader, repoRoot)
		if err != nil {
			// A typed budget error from the investigator means the very
			// first turn was already over budget — no observations were
			// recorded. Log + skip the feature without writing a drift
			// cache entry; a re-run with --llm-typical=<bigger-model>
			// retries cleanly. Do NOT abort the run; other features still
			// get a chance.
			if errors.Is(err, ErrTokenBudgetExceeded{}) {
				log.Warnf("drift investigator could not start for feature %q: %v; skipping (no cache entry written)", entry.Feature.Name, err)
				return nil
			}
			return fmt.Errorf("DetectDrift %q: %w", entry.Feature.Name, err)
		}
		issues, err := judgeFeatureDrift(ctx, judge, entry.Feature, observations, roles)
		if err != nil {
			return fmt.Errorf("DetectDrift %q: %w", entry.Feature.Name, err)
		}

		if len(issues) > 0 {
			snapshot := appendAndSnapshot(DriftFinding{Feature: entry.Feature.Name, Issues: issues})
			if onFinding != nil {
				if err := onFinding(snapshot); err != nil {
					return fmt.Errorf("DetectDrift: onFinding: %w", err)
				}
			}
		}
		if onFeatureDone != nil {
			if err := onFeatureDone(entry.Feature.Name, sortedFiles, sortedFiltered, sortedPages, currentRolesHash, issues); err != nil {
				return fmt.Errorf("DetectDrift: onFeatureDone: %w", err)
			}
		}
		return nil
	})
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

// listFeatureSymbolsDefaultLimit is the default page size when the investigator
// calls list_feature_symbols / list_feature_pages without supplying a limit.
// Picked to stay well under the agent-loop tool-result clip while still
// covering most features in one call.
const listFeatureSymbolsDefaultLimit = 50

// listFeatureSymbolsMaxLimit caps the page size on either pagination tool. A
// pathological limit (e.g. 10_000) would force the agent loop to clip the tool
// result mid-message and waste the round.
const listFeatureSymbolsMaxLimit = 200

// listFeatureSymbolsTool returns a Tool that paginates entry.Symbols. Inputs
// are offset (default 0), limit (default 50, max 200), and filter (case-
// insensitive substring on the symbol name). The result is a plain-text list,
// one symbol per line, headed by "<N> of <total> symbols[ matching '<filter>']:".
func listFeatureSymbolsTool(entry FeatureEntry) Tool {
	return Tool{
		Name:        "list_feature_symbols",
		Description: "List symbols belonging to this feature with offset/limit pagination and optional case-insensitive substring filter on the symbol name. Use when the compressed system prompt's entry-point list is not enough.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"offset": map[string]any{"type": "integer", "minimum": 0, "description": "Number of symbols to skip from the start. Default 0."},
				"limit":  map[string]any{"type": "integer", "minimum": 1, "maximum": listFeatureSymbolsMaxLimit, "description": "Max symbols to return. Default 50, max 200."},
				"filter": map[string]any{"type": "string", "description": "Optional case-insensitive substring match on the symbol name."},
			},
		},
		Execute: func(_ context.Context, rawArgs string) (string, error) {
			var args struct {
				Offset int    `json:"offset"`
				Limit  int    `json:"limit"`
				Filter string `json:"filter"`
			}
			if rawArgs != "" {
				if err := json.Unmarshal([]byte(rawArgs), &args); err != nil {
					return fmt.Sprintf("error parsing arguments: %v", err), nil
				}
			}
			return renderSymbolPage(entry.Symbols, args.Offset, args.Limit, args.Filter), nil
		},
	}
}

// listFeaturePagesTool returns a Tool that paginates the doc page URLs scoped
// to this feature. Same shape as listFeatureSymbolsTool minus the filter (page
// URLs are noisy and filtering them rarely helps the investigator).
func listFeaturePagesTool(pages []string) Tool {
	return Tool{
		Name:        "list_feature_pages",
		Description: "List documentation page URLs scoped to this feature with offset/limit pagination. Use when the compressed system prompt indicates more pages exist than the investigator has seen.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"offset": map[string]any{"type": "integer", "minimum": 0, "description": "Number of pages to skip from the start. Default 0."},
				"limit":  map[string]any{"type": "integer", "minimum": 1, "maximum": listFeatureSymbolsMaxLimit, "description": "Max pages to return. Default 50, max 200."},
			},
		},
		Execute: func(_ context.Context, rawArgs string) (string, error) {
			var args struct {
				Offset int `json:"offset"`
				Limit  int `json:"limit"`
			}
			if rawArgs != "" {
				if err := json.Unmarshal([]byte(rawArgs), &args); err != nil {
					return fmt.Sprintf("error parsing arguments: %v", err), nil
				}
			}
			return renderPageList(pages, args.Offset, args.Limit), nil
		},
	}
}

// renderSymbolPage formats one page of the symbol list. Bounds are clamped:
// offset past the end yields an empty slice (header still rendered); limit is
// clamped to listFeatureSymbolsMaxLimit; negative values are treated as the
// default. Filter is case-insensitive substring match on the symbol name.
func renderSymbolPage(syms []string, offset, limit int, filter string) string {
	filtered := syms
	if filter != "" {
		needle := strings.ToLower(filter)
		filtered = filtered[:0:0]
		for _, s := range syms {
			if strings.Contains(strings.ToLower(s), needle) {
				filtered = append(filtered, s)
			}
		}
	}
	total := len(filtered)
	start, end := clampWindow(total, offset, limit)
	window := filtered[start:end]

	var b strings.Builder
	if filter == "" {
		fmt.Fprintf(&b, "%d of %d symbols:\n", len(window), total)
	} else {
		fmt.Fprintf(&b, "%d of %d symbols matching %q:\n", len(window), total, filter)
	}
	for _, s := range window {
		fmt.Fprintf(&b, "- %s\n", s)
	}
	return b.String()
}

// renderPageList formats one page of the doc-page list. Bounds-clamping rules
// mirror renderSymbolPage.
func renderPageList(pages []string, offset, limit int) string {
	total := len(pages)
	start, end := clampWindow(total, offset, limit)
	window := pages[start:end]

	var b strings.Builder
	fmt.Fprintf(&b, "%d of %d pages:\n", len(window), total)
	for _, p := range window {
		fmt.Fprintf(&b, "- %s\n", p)
	}
	return b.String()
}

// clampWindow normalizes an (offset, limit) request against a slice of length
// total. offset past the end returns [total, total) so callers can slice with
// no panic. limit <= 0 is replaced with the default; limit above the max is
// capped. Used by both list_feature_symbols and list_feature_pages so the two
// tools share the same overflow semantics.
func clampWindow(total, offset, limit int) (int, int) {
	if offset < 0 {
		offset = 0
	}
	if offset > total {
		offset = total
	}
	if limit <= 0 {
		limit = listFeatureSymbolsDefaultLimit
	}
	if limit > listFeatureSymbolsMaxLimit {
		limit = listFeatureSymbolsMaxLimit
	}
	end := offset + limit
	if end > total {
		end = total
	}
	return offset, end
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
	var observations []driftObservation
	tools := []Tool{
		readFileTool(repoRoot),
		readPageTool(pageReader),
		listFeatureSymbolsTool(entry),
		listFeaturePagesTool(pages),
		noteObservationTool(&observations),
	}

	systemPrompt := buildInvestigatorSystemPrompt(entry, pages)

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
	if errors.Is(err, ErrTokenBudgetExceeded{}) {
		// Two distinct shapes:
		//   1. observations > 0 — earlier rounds recorded evidence before
		//      the budget hook refused the next turn. Same shape as the
		//      ErrMaxRounds path: log and hand the partial set to the judge.
		//   2. observations == 0 — the very first turn was already over
		//      budget (system prompt + tool defs alone). Return a typed
		//      error so DetectDrift skips writing a "no drift" cache entry
		//      for this feature — a re-run with a larger model retries
		//      cleanly.
		if len(observations) == 0 {
			return nil, fmt.Errorf("investigateFeatureDrift %q: %w", entry.Feature.Name, err)
		}
		log.Warnf("drift investigator hit token budget for feature %q (%d files, %d pages); handing %d observations to judge",
			entry.Feature.Name, len(entry.Files), len(pages), len(observations))
		return observations, nil
	}
	if err != nil {
		return nil, err
	}
	return observations, nil
}

// descriptionBudgetTokens caps the inline feature description embedded in the
// investigator's system prompt. A pathological description (e.g. an entire
// README pasted into the feature mapping) is trimmed with chunker.Fit so the
// prompt stays bounded regardless of input shape.
const descriptionBudgetTokens = 800

// entryPointCap is the maximum number of symbol names embedded in the
// investigator's system prompt as starting suggestions. The full symbol list
// remains available through the list_feature_symbols tool.
const entryPointCap = 10

// buildInvestigatorSystemPrompt returns the compressed system prompt used by
// the drift investigator. The prompt embeds counts (symbols, files, pages) and
// a short list of entry-point symbol names — it never inlines the full symbol
// or page enumeration. Investigators consult the full lists through the
// list_feature_symbols and list_feature_pages tools.
//
// PROMPT: Investigates a feature for documentation drift by reading source
// files and doc pages, recording each piece of evidence via note_observation.
// The investigator gathers; it does not adjudicate.
func buildInvestigatorSystemPrompt(entry FeatureEntry, pages []string) string {
	description := chunker.Fit(entry.Feature.Description, descriptionBudgetTokens)
	entries := topEntryPoints(entry.Symbols, entryPointCap)
	entriesText := strings.Join(entries, ", ")
	if entriesText == "" {
		entriesText = "(none recorded for this feature)"
	}

	return fmt.Sprintf(`You are investigating documentation accuracy for the software feature %q.

Description: %s

Scope: %d symbols across %d files in this codebase. %d pages of documentation reference this feature.

Entry-point symbols you may want to start with: %s

You have tools available:
- read_file(path)               — read the full source of a repo-relative file.
- read_page(url)                — read the full cached content of a doc page.
- list_feature_symbols(offset, limit, filter)
                                — paginate the full symbol list for this feature.
- list_feature_pages(offset, limit)
                                — paginate the full doc-page list for this feature.
- note_observation(...)         — record one piece of candidate drift evidence.

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
		description,
		len(entry.Symbols), len(entry.Files), len(pages),
		entriesText,
	)
}

// topEntryPoints returns up to n symbol names from syms, preferring exported
// (Go-style capitalized) identifiers first. Stable: relative order within the
// exported and unexported groups is preserved. Returns an empty slice when
// syms is empty or n <= 0.
//
// Heuristic caveat: the "first char in [A-Z]" test is Go-centric. For Python
// (snake_case), JavaScript (camelCase), Java (camelCase methods), etc. the
// partition degenerates — Python features look entirely "unexported" and the
// preference becomes a no-op. The investigator still receives n entry-point
// names, just in source order. Acceptable as a best-effort hint; the
// investigator can paginate the full symbol list via list_feature_symbols if
// the prompt's entry points don't cover what it needs.
func topEntryPoints(syms []string, n int) []string {
	if n <= 0 || len(syms) == 0 {
		return nil
	}
	var exported, rest []string
	for _, s := range syms {
		if len(s) > 0 && s[0] >= 'A' && s[0] <= 'Z' {
			exported = append(exported, s)
		} else {
			rest = append(rest, s)
		}
	}
	all := append(exported, rest...)
	if len(all) > n {
		all = all[:n]
	}
	return all
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

// FilteredPagesRolesHash returns a deterministic hex SHA-256 over the
// URL→role mapping that the judge prompt will see for sortedFilteredPages.
// The drift cache stores this fingerprint per feature so a role
// reclassification on stable files+pages (or the first warm-cache run after
// upgrading from URL-heuristic to content-based roles) forces a re-judge
// instead of replaying stale priorities. sortedFilteredPages MUST be sorted
// ascending; callers in DetectDrift use sortedCopy(filteredPages).
//
// Exported so persisters and tests can recompute the same fingerprint
// without reimplementing the encoding.
func FilteredPagesRolesHash(roles RoleResolver, sortedFilteredPages []string) string {
	if roles == nil {
		roles = NewRoleResolver(nil)
	}
	type entry struct {
		URL  string `json:"url"`
		Role string `json:"role"`
	}
	out := make([]entry, 0, len(sortedFilteredPages))
	for _, p := range sortedFilteredPages {
		out = append(out, entry{URL: p, Role: roles(p)})
	}
	data, _ := json.Marshal(out)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
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
// for the pages observed during drift investigation. Roles come from the
// per-run RoleResolver (built from the page-analysis cache). Fed into the
// judge prompt so the priority rubric can weight prominent pages higher.
func pageRoleSummary(roles RoleResolver, pages []string) string {
	if len(pages) == 0 {
		return "Page role hints: (no specific pages)"
	}
	var b strings.Builder
	b.WriteString("Page role hints:\n")
	for _, p := range pages {
		fmt.Fprintf(&b, "- %s -> %s\n", p, roles(p))
	}
	return b.String()
}

// judgeResponse mirrors judgeSchema for unmarshaling.
type judgeResponse struct {
	Issues []DriftIssue `json:"issues"`
}

// judgeFeatureDrift adjudicates the investigator's observations for one
// feature. The function estimates the assembled prompt's token cost via
// chunker.EstimateTokens BEFORE the first LLM call. When the rendered
// observation set fits within the model's input budget (the common
// case), the judge is invoked exactly once. When it would not fit, the
// observations are greedy-packed into the smallest number of groups
// whose rendered prompts each fit, the judge is invoked once per group,
// and per-chunk issue lists are concatenated. Lossless at the
// observation level: every observation is still seen by the judge.
//
// With zero observations the function short-circuits and returns nil
// without calling the LLM.
//
// The ErrTokenBudgetExceeded backstop in runJudgeOnce should be
// unreachable once preemptive sizing is in place — it is retained as
// defense-in-depth so estimator-vs-tokenizer drift surfaces as a loud
// warning instead of a silent skip.
func judgeFeatureDrift(
	ctx context.Context,
	client LLMClient,
	feature CodeFeature,
	observations []driftObservation,
	roles RoleResolver,
) ([]DriftIssue, error) {
	if len(observations) == 0 {
		return nil, nil
	}

	groups := chunkObservationsForJudge(client, feature, observations, roles)
	if len(groups) <= 1 {
		return runJudgeOnce(ctx, client, feature, observations, roles)
	}
	log.Debugf("judge prompt for %q split into %d chunks (preemptive sizing)", feature.Name, len(groups))

	var all []DriftIssue
	for i, chunk := range groups {
		chunkIssues, err := runJudgeOnce(ctx, client, feature, chunk, roles)
		if err != nil {
			return nil, fmt.Errorf("judgeFeatureDrift %q: chunk %d/%d: %w", feature.Name, i+1, len(groups), err)
		}
		all = append(all, chunkIssues...)
	}
	return dedupeDriftIssues(all), nil
}

// runJudgeOnce renders the judge prompt for a fixed observation set,
// invokes the judge LLM with the existing retry loop, and validates the
// returned issues. It is the single-call body shared by the fast path
// (one observation set) and the preemptive-chunking path (one
// observation group per chunk).
//
// An ErrTokenBudgetExceeded from the LLM client at this layer should be
// unreachable once preemptive sizing in judgeFeatureDrift is in place.
// It is surfaced as a loud warning so estimator-vs-tokenizer drift is
// visible rather than silently retried.
func runJudgeOnce(
	ctx context.Context,
	client LLMClient,
	feature CodeFeature,
	observations []driftObservation,
	roles RoleResolver,
) ([]DriftIssue, error) {
	prompt := renderJudgePrompt(feature, observations, roles)

	// Retry on transport error / malformed JSON / schema-validation
	// failure. ErrTokenBudgetExceeded is NOT retried — it's deterministic
	// for a given prompt and indicates that preemptive sizing in
	// judgeFeatureDrift mis-estimated the cost. Log loudly and surface.
	var lastErr error
	for attempt := 1; attempt <= driftJudgeMaxAttempts; attempt++ {
		raw, err := client.CompleteJSON(ctx, prompt, judgeSchema)
		if err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return nil, ctxErr
			}
			if errors.Is(err, ErrTokenBudgetExceeded{}) {
				log.Warnf("judge prompt for %q hit token budget despite preemptive sizing (should be unreachable): %v", feature.Name, err)
				return nil, err
			}
			lastErr = err
			log.Warnf("judge attempt %d/%d for %q failed: %v", attempt, driftJudgeMaxAttempts, feature.Name, err)
			continue
		}
		var resp judgeResponse
		if err := json.Unmarshal(raw, &resp); err != nil {
			lastErr = fmt.Errorf("invalid JSON response: %w", err)
			log.Warnf("judge attempt %d/%d for %q failed: %v", attempt, driftJudgeMaxAttempts, feature.Name, lastErr)
			continue
		}
		if err := validateDriftIssues(resp.Issues); err != nil {
			lastErr = err
			log.Warnf("judge attempt %d/%d for %q failed: %v", attempt, driftJudgeMaxAttempts, feature.Name, err)
			continue
		}
		return resp.Issues, nil
	}
	return nil, fmt.Errorf("judgeFeatureDrift %q: %w: %s", feature.Name, ErrLLMRetriesExhausted, lastErr)
}

// dedupeDriftIssues collapses duplicate issues that the LLM may emit
// when the same docs problem appears in observations spread across
// multiple chunks. The dedupe key is (page, issue) — pages are stable
// URLs and the issue text is the actionable feedback that would render
// in gaps.md, so two issues with the same key would render as one
// finding for the reader anyway. Description paraphrasing is tolerated:
// the first-seen issue text wins. Priority/PriorityReason are not part
// of the key — if two chunks judge the same docs problem at different
// priorities the first-seen priority wins (judges agree on priorities
// in practice; this is belt-and-suspenders).
func dedupeDriftIssues(issues []DriftIssue) []DriftIssue {
	if len(issues) <= 1 {
		return issues
	}
	seen := make(map[string]bool, len(issues))
	out := issues[:0]
	for _, iss := range issues {
		key := iss.Page + "|" + iss.Issue
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, iss)
	}
	return out
}

// renderJudgePrompt builds the judge-stage prompt for one feature and a
// specific observation set. Extracted from runJudgeOnce so
// chunkObservationsForJudge can size candidate chunks against the same
// rendering used at send time.
func renderJudgePrompt(feature CodeFeature, observations []driftObservation, roles RoleResolver) string {
	var b strings.Builder
	for i, o := range observations {
		fmt.Fprintf(&b, "[%d] page: %s\n    docs say: %q\n    code shows: %q\n    concern: %s\n",
			i+1, o.Page, o.DocQuote, o.CodeQuote, o.Concern)
	}

	// PROMPT: Adjudicates a list of candidate drift observations for one feature, dropping false alarms, collapsing observations that describe the same docs problem into one issue, and emitting actionable documentation feedback as DriftIssues with a user-impact priority rating.
	return fmt.Sprintf(`You are reviewing candidate documentation drift observations for one software feature.

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
		pageRoleSummary(roles, uniqueObservationPages(observations)),
		priorityRubric)
}

const clipQuoteMaxChars = 1500

// clipObservationQuotes truncates DocQuote/CodeQuote on a single
// observation to max characters with a "[…]" marker. Used by
// chunkObservationsForJudge before greedy packing so a single bloated
// observation doesn't single-handedly overflow a chunk.
func clipObservationQuotes(o driftObservation, max int) driftObservation {
	if len(o.DocQuote) > max {
		o.DocQuote = truncateAtRuneBoundary(o.DocQuote, max) + " […]"
	}
	if len(o.CodeQuote) > max {
		o.CodeQuote = truncateAtRuneBoundary(o.CodeQuote, max) + " […]"
	}
	return o
}

// chunkObservationsForJudge greedy-packs observations into the smallest
// number of groups whose rendered judge prompts each fit within
// 0.9 × Capabilities().MaxInputTokens. The packing is preemptive: the
// caller (judgeFeatureDrift) consults this BEFORE the first LLM call,
// so the reactive "send and catch ErrTokenBudgetExceeded" path no
// longer exists.
//
// Per-observation quotes are clipped to clipQuoteMaxChars first so a
// single pathological observation cannot single-handedly overflow a
// chunk. When the model exposes no budget (MaxInputTokens == 0,
// self-hosted ollama/lmstudio), returns one chunk containing every
// observation — without a budget we have nothing to pack against and
// the upstream client will surface any real overflow.
func chunkObservationsForJudge(client LLMClient, feature CodeFeature, obs []driftObservation, roles RoleResolver) [][]driftObservation {
	caps := client.Capabilities()
	if caps.MaxInputTokens <= 0 {
		return [][]driftObservation{obs}
	}
	budget := int(0.9 * float64(caps.MaxInputTokens))

	clipped := make([]driftObservation, len(obs))
	for i, o := range obs {
		clipped[i] = clipObservationQuotes(o, clipQuoteMaxChars)
	}

	// Single-call fast path: if the full rendered prompt fits, emit one
	// chunk and let judgeFeatureDrift call the judge once.
	if chunker.EstimateTokens(renderJudgePrompt(feature, clipped, roles)) <= budget {
		return [][]driftObservation{clipped}
	}

	var chunks [][]driftObservation
	var cur []driftObservation
	for _, o := range clipped {
		candidate := append([]driftObservation{}, cur...)
		candidate = append(candidate, o)
		if chunker.EstimateTokens(renderJudgePrompt(feature, candidate, roles)) > budget && len(cur) > 0 {
			// `cur` was the last chunk that fit; flush it.
			chunks = append(chunks, cur)
			cur = []driftObservation{o}
			continue
		}
		cur = candidate
	}
	if len(cur) > 0 {
		chunks = append(chunks, cur)
	}
	return chunks
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
