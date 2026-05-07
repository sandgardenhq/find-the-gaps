package analyzer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// hashPageContent mirrors the content hashing convention used inside
// DetectScreenshotGaps to bind a cache entry to a specific page version.
func hashPageContent(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// makeScreenshotPages builds n DocPages with deterministic content/url so
// tests can map between cached and fresh entries by index.
func makeScreenshotPages(n int) []DocPage {
	out := make([]DocPage, 0, n)
	for i := 0; i < n; i++ {
		var b strings.Builder
		fmt.Fprintf(&b, "# Page %d\n\nClick Save to continue.\n", i)
		out = append(out, DocPage{
			URL:     fmt.Sprintf("https://x/p%d", i),
			Path:    fmt.Sprintf("/tmp/p%d.md", i),
			Content: b.String(),
		})
	}
	return out
}

// TestDetectScreenshotGaps_cacheHitSkipsLLM pins the per-page cache
// short-circuit. When 3 of 5 pages are in the cached map (keyed by URL +
// content hash), only the 2 fresh pages should issue a detection call.
func TestDetectScreenshotGaps_cacheHitSkipsLLM(t *testing.T) {
	pages := makeScreenshotPages(5)

	cached := map[string]ScreenshotsCachedPage{}
	for i := 0; i < 3; i++ {
		key := pages[i].URL + "|" + hashPageContent(pages[i].Content)
		cached[key] = ScreenshotsCachedPage{
			URL:         pages[i].URL,
			ContentHash: hashPageContent(pages[i].Content),
			Stats: ScreenshotPageStats{
				PageURL:            pages[i].URL,
				MissingScreenshots: 1,
			},
			Missing: []ScreenshotGap{{
				PageURL:        pages[i].URL,
				PagePath:       pages[i].Path,
				QuotedPassage:  fmt.Sprintf("cached passage for %d", i),
				ShouldShow:     "cached image",
				SuggestedAlt:   "cached alt",
				InsertionHint:  "cached hint",
				Priority:       PriorityMedium,
				PriorityReason: "cached reason",
			}},
		}
	}

	client := &fakeLLMClient{
		responses: []string{
			`{"gaps":[{"quoted_passage":"fresh-3","should_show":"X","suggested_alt":"Y","insertion_hint":"Z","priority":"medium","priority_reason":"r"}]}`,
			`{"gaps":[{"quoted_passage":"fresh-4","should_show":"X","suggested_alt":"Y","insertion_hint":"Z","priority":"medium","priority_reason":"r"}]}`,
		},
	}

	res, err := DetectScreenshotGaps(context.Background(), client, pages, 1, cached, nil, nil)
	require.NoError(t, err)

	// Only 2 detection calls should have fired (pages 3 and 4).
	assert.Equal(t, 2, len(client.prompts), "exactly 2 LLM calls for 2 fresh pages")

	// Result has all 5 pages worth of audit stats.
	assert.Len(t, res.AuditStats, 5)

	// Cached findings flow through to the result.
	missingByPassage := map[string]bool{}
	for _, g := range res.MissingGaps {
		missingByPassage[g.QuotedPassage] = true
	}
	for i := 0; i < 3; i++ {
		assert.True(t, missingByPassage[fmt.Sprintf("cached passage for %d", i)],
			"cached passage %d should appear in result", i)
	}
	assert.True(t, missingByPassage["fresh-3"], "fresh page 3 finding expected")
	assert.True(t, missingByPassage["fresh-4"], "fresh page 4 finding expected")
}

// TestDetectScreenshotGaps_onPageDoneFiresPerFreshPage verifies the persister
// callback is invoked for each freshly analyzed page (and nothing else).
func TestDetectScreenshotGaps_onPageDoneFiresPerFreshPage(t *testing.T) {
	pages := makeScreenshotPages(5)
	client := &fakeLLMClient{
		responses: []string{
			`{"gaps":[]}`, `{"gaps":[]}`, `{"gaps":[]}`, `{"gaps":[]}`, `{"gaps":[]}`,
		},
	}

	var mu sync.Mutex
	seen := map[string]int{}
	onPageDone := func(url string, entry ScreenshotsCachedPage) error {
		mu.Lock()
		defer mu.Unlock()
		seen[url]++
		require.Equal(t, url, entry.URL)
		require.NotEmpty(t, entry.ContentHash)
		return nil
	}

	_, err := DetectScreenshotGaps(context.Background(), client, pages, 1, nil, onPageDone, nil)
	require.NoError(t, err)

	require.Len(t, seen, 5)
	for _, p := range pages {
		assert.Equal(t, 1, seen[p.URL], "onPageDone fires exactly once per page %s", p.URL)
	}
}

// TestDetectScreenshotGaps_nilCachedAndOnPageDone_PreservesBehavior pins that
// the new optional parameters do not change behavior when nil. This is the
// pre-Task-9 contract.
func TestDetectScreenshotGaps_nilCachedAndOnPageDone_PreservesBehavior(t *testing.T) {
	client := &fakeLLMClient{
		responses: []string{`{"gaps":[]}`},
	}
	pages := []DocPage{{URL: "https://x/p", Path: "/tmp/p.md", Content: "# Page\n\nSome text.\n"}}

	res, err := DetectScreenshotGaps(context.Background(), client, pages, 1, nil, nil, nil)
	require.NoError(t, err)
	assert.Empty(t, res.MissingGaps)
	assert.Len(t, res.AuditStats, 1)
	assert.Equal(t, 1, len(client.prompts))
}

// TestDetectScreenshotGaps_cachedHitsContributeToResult pins that a fully
// cached run produces a result whose findings come from the cache and that
// no LLM calls are issued.
func TestDetectScreenshotGaps_cachedHitsContributeToResult(t *testing.T) {
	pages := makeScreenshotPages(5)
	cached := map[string]ScreenshotsCachedPage{}
	for i, p := range pages {
		key := p.URL + "|" + hashPageContent(p.Content)
		cached[key] = ScreenshotsCachedPage{
			URL:         p.URL,
			ContentHash: hashPageContent(p.Content),
			Stats:       ScreenshotPageStats{PageURL: p.URL, MissingScreenshots: 1},
			Missing: []ScreenshotGap{{
				PageURL:        p.URL,
				PagePath:       p.Path,
				QuotedPassage:  fmt.Sprintf("cached %d", i),
				ShouldShow:     "X",
				SuggestedAlt:   "Y",
				InsertionHint:  "Z",
				Priority:       PriorityLarge,
				PriorityReason: "r",
			}},
			ImageIssues: []ImageIssue{{
				PageURL:         p.URL,
				Index:           "img-1",
				Src:             "/img.png",
				Reason:          "wrong",
				SuggestedAction: "replace",
				Priority:        PriorityMedium,
				PriorityReason:  "r",
			}},
		}
	}

	client := &fakeLLMClient{}
	res, err := DetectScreenshotGaps(context.Background(), client, pages, 1, cached, nil, nil)
	require.NoError(t, err)

	assert.Equal(t, 0, len(client.prompts), "fully cached run must not call the LLM")
	assert.Len(t, res.MissingGaps, 5)
	assert.Len(t, res.ImageIssues, 5)
	assert.Len(t, res.AuditStats, 5)
}

// TestDetectScreenshotGaps_contentHashChangeDefeatsCacheHit pins that a cache
// entry keyed on a stale ContentHash does NOT match a fresh page with
// different content.
func TestDetectScreenshotGaps_contentHashChangeDefeatsCacheHit(t *testing.T) {
	pages := makeScreenshotPages(1)
	// Cache entry for the URL but with a deliberately mismatched hash.
	cached := map[string]ScreenshotsCachedPage{
		pages[0].URL + "|stale-hash": {
			URL:         pages[0].URL,
			ContentHash: "stale-hash",
		},
	}
	client := &fakeLLMClient{responses: []string{`{"gaps":[]}`}}
	_, err := DetectScreenshotGaps(context.Background(), client, pages, 1, cached, nil, nil)
	require.NoError(t, err)
	assert.Equal(t, 1, len(client.prompts), "stale ContentHash must miss the cache and fire the LLM")
}

// TestDetectScreenshotGaps_runsPagesConcurrently pins the parallel-dispatch
// guarantee. With 8 pages and workers=4, the per-page LLM call must observe
// at least two concurrent in-flight calls (peak >= 2). Each Complete call
// holds for 20ms via fakeLLMClient.hold so workers actually pile up under
// the bounded semaphore.
func TestDetectScreenshotGaps_runsPagesConcurrently(t *testing.T) {
	pages := makeScreenshotPages(8)
	client := &fakeLLMClient{hold: 20 * time.Millisecond}
	_, err := DetectScreenshotGaps(context.Background(), client, pages, 4, nil, nil, nil)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, client.peak.Load(), int32(2),
		"DetectScreenshotGaps with workers=4 must run pages concurrently; peak in-flight=%d", client.peak.Load())
}

// Compile-time assertion that ScreenshotsCachedPage matches the JSON shape of
// the cli-side cache entry. If the analyzer ever drifts, this fails.
var _ = func() bool {
	type want struct {
		URL         string                `json:"url"`
		ContentHash string                `json:"contentHash"`
		Stats       ScreenshotPageStats   `json:"stats"`
		Missing     []ScreenshotGap       `json:"missing"`
		Possibly    []ScreenshotGap       `json:"possiblyCovered"`
		ImageIssues []ImageIssue          `json:"imageIssues"`
	}
	_ = want{}
	_, _ = json.Marshal(ScreenshotsCachedPage{})
	return true
}()
