package analyzer

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"sync"

	"github.com/charmbracelet/log"
)

// DocsMapperPageBudget is the maximum tokens per mapPageToFeatures LLM call.
// Lower than MapperTokenBudget because we never batch pages — each call carries
// one page's full content plus the feature list.
const DocsMapperPageBudget = 40_000

// DocsMapProgressFunc is called with the accumulated results after each page is processed.
// It is called sequentially (from the result-drain loop), so implementations do not
// need to be goroutine-safe. Returning a non-nil error aborts the mapping.
type DocsMapProgressFunc func(partial DocsFeatureMap) error

// mapPageToFeatures asks the LLM which features from the canonical list are
// covered by a single documentation page. Content is truncated to fit the budget.
//
// featureTokens is the pre-computed token count of featuresJSON (caller computes once).
// tokenBudget is the per-call ceiling.
func mapPageToFeatures(
	ctx context.Context,
	client LLMClient,
	featuresJSON []byte,
	featureTokens int,
	tokenBudget int,
	pageURL, content string,
) ([]string, error) {
	const promptOverhead = 400
	available := tokenBudget - featureTokens - promptOverhead
	if available < 100 {
		return []string{}, nil
	}

	// Use the fast local tiktoken estimator to decide whether to truncate.
	contentTokens := countTokens(content)
	if contentTokens > available {
		keepChars := max(0, int(float64(len(content))*float64(available)/float64(contentTokens)))
		if keepChars > len(content) {
			keepChars = len(content)
		}
		content = content[:keepChars]
	}

	// PROMPT: Maps a single documentation page to the canonical product features it covers. Returns a JSON array of matching feature strings only.
	prompt := fmt.Sprintf(`You are analyzing a documentation page to identify which product features it covers.

Product features:
%s

Documentation page URL: %s

Documentation page content:
%s

Return a JSON array of feature strings (exact matches from the list above) that this page covers.
Only include features that are clearly addressed on this page.
Respond with only the JSON array. No markdown code fences. No prose.`,
		string(featuresJSON), pageURL, content)

	raw, err := client.Complete(ctx, prompt)
	if err != nil {
		return nil, fmt.Errorf("mapPageToFeatures %s: %w", pageURL, err)
	}

	var matched []string
	if err := json.Unmarshal([]byte(raw), &matched); err != nil {
		return nil, fmt.Errorf("mapPageToFeatures %s: invalid JSON response: %w", pageURL, err)
	}
	if matched == nil {
		matched = []string{}
	}
	return matched, nil
}

// pageResult is the outcome of processing one doc page.
type pageResult struct {
	url      string
	features []string
	err      error
}

// MapFeaturesToDocs maps each product feature to the documentation pages that cover it.
// Each page is processed by an individual LLM call. Up to workers pages are processed
// concurrently. pages is a map of URL → local file path (as returned by spider.Crawl).
// onPage, if non-nil, is called with the accumulated results after each page completes.
func MapFeaturesToDocs(
	ctx context.Context,
	client LLMClient,
	features []string,
	pages map[string]string,
	workers int,
	tokenBudget int,
	onPage DocsMapProgressFunc,
) (DocsFeatureMap, error) {
	if len(features) == 0 || len(pages) == 0 {
		return emptyDocsFeatureMap(features), nil
	}

	featuresJSON, _ := json.Marshal(features)
	featureTokens := countTokens(string(featuresJSON))

	resultCh := make(chan pageResult, len(pages))
	sem := make(chan struct{}, workers)

	total := len(pages)
	var wg sync.WaitGroup
	for url, filePath := range pages {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			content, err := readPageContent(filePath)
			if err != nil {
				resultCh <- pageResult{url: url, err: err}
				return
			}
			log.Infof("  mapping page %s", url)
			matched, err := mapPageToFeatures(ctx, client, featuresJSON, featureTokens, tokenBudget, url, content)
			resultCh <- pageResult{url: url, features: matched, err: err}
		}()
	}

	go func() {
		wg.Wait()
		close(resultCh)
	}()

	// Accumulate: feature → set of pages that cover it.
	// The drain loop is sequential, so onPage can be called here without locking.
	acc := make(map[string]map[string]struct{}, len(features))
	for _, feat := range features {
		acc[feat] = make(map[string]struct{})
	}

	completed := 0
	for res := range resultCh {
		if res.err != nil {
			log.Warnf("docs mapping: skipping %s: %v", res.url, res.err)
			continue
		}
		for _, feat := range res.features {
			if _, known := acc[feat]; known {
				acc[feat][res.url] = struct{}{}
			}
		}
		completed++
		log.Infof("  [%d/%d] %s → %d features matched", completed, total, res.url, len(res.features))
		if onPage != nil {
			partial := docsAccToFeatureMap(acc, features)
			if err := onPage(partial); err != nil {
				return nil, fmt.Errorf("MapFeaturesToDocs: onPage: %w", err)
			}
		}
	}

	return docsAccToFeatureMap(acc, features), nil
}

func readPageContent(filePath string) (string, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func docsAccToFeatureMap(acc map[string]map[string]struct{}, features []string) DocsFeatureMap {
	out := make(DocsFeatureMap, 0, len(features))
	for _, feat := range features {
		pages := make([]string, 0, len(acc[feat]))
		for p := range acc[feat] {
			pages = append(pages, p)
		}
		sort.Strings(pages)
		out = append(out, DocsFeatureEntry{Feature: feat, Pages: pages})
	}
	return out
}

func emptyDocsFeatureMap(features []string) DocsFeatureMap {
	out := make(DocsFeatureMap, 0, len(features))
	for _, feat := range features {
		out = append(out, DocsFeatureEntry{Feature: feat, Pages: []string{}})
	}
	return out
}
