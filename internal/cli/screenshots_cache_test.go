package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/sandgardenhq/find-the-gaps/internal/analyzer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestScreenshotsCache_roundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "screenshots.json")
	in := map[string]screenshotsCacheEntry{
		screenshotsCacheKey("https://docs.example.com/auth", "hash-auth", "other"): {
			URL:         "https://docs.example.com/auth",
			ContentHash: "hash-auth",
			Role:        "other",
			Stats: analyzer.ScreenshotPageStats{
				PageURL:            "https://docs.example.com/auth",
				VisionEnabled:      true,
				RelevanceBatches:   2,
				ImagesSeen:         7,
				ImageIssues:        1,
				MissingScreenshots: 1,
				PossiblyCovered:    0,
			},
			Missing: []analyzer.ScreenshotGap{{
				PageURL:        "https://docs.example.com/auth",
				PagePath:       "/auth.md",
				QuotedPassage:  "Click the login button.",
				ShouldShow:     "the login button.",
				SuggestedAlt:   "Login button",
				InsertionHint:  "after the quoted passage",
				Priority:       analyzer.PriorityMedium,
				PriorityReason: "moderate impact",
			}},
			Possibly: []analyzer.ScreenshotGap{},
			ImageIssues: []analyzer.ImageIssue{{
				PageURL:         "https://docs.example.com/auth",
				Index:           "img-1",
				Src:             "/img/auth.png",
				Reason:          "shows wrong screen",
				SuggestedAction: "replace with login screenshot",
				Priority:        analyzer.PriorityLarge,
				PriorityReason:  "misleading users",
			}},
		},
		screenshotsCacheKey("https://docs.example.com/search", "hash-search", "other"): {
			URL:         "https://docs.example.com/search",
			ContentHash: "hash-search",
			Role:        "other",
			Stats: analyzer.ScreenshotPageStats{
				PageURL:       "https://docs.example.com/search",
				VisionEnabled: false,
			},
			Missing:     []analyzer.ScreenshotGap{},
			Possibly:    []analyzer.ScreenshotGap{},
			ImageIssues: []analyzer.ImageIssue{},
		},
	}
	require.NoError(t, saveScreenshotsCache(path, in))

	got, ok := loadScreenshotsCache(path)
	require.True(t, ok)
	require.Len(t, got, 2)

	authKey := screenshotsCacheKey("https://docs.example.com/auth", "hash-auth", "other")
	require.Contains(t, got, authKey)
	assert.Equal(t, in[authKey].URL, got[authKey].URL)
	assert.Equal(t, in[authKey].ContentHash, got[authKey].ContentHash)
	assert.Equal(t, in[authKey].Stats, got[authKey].Stats)
	assert.Equal(t, in[authKey].Missing, got[authKey].Missing)
	assert.Equal(t, in[authKey].ImageIssues, got[authKey].ImageIssues)
}

func TestScreenshotsCache_loadMissing_returnsFalse(t *testing.T) {
	_, ok := loadScreenshotsCache(filepath.Join(t.TempDir(), "screenshots.json"))
	assert.False(t, ok)

	_, fileOk := loadScreenshotsCacheFile(filepath.Join(t.TempDir(), "screenshots.json"))
	assert.False(t, fileOk)
}

func TestScreenshotsCache_loadCorrupt_returnsFalse(t *testing.T) {
	path := filepath.Join(t.TempDir(), "screenshots.json")
	require.NoError(t, os.WriteFile(path, []byte(`{"not valid":`), 0o644))
	_, ok := loadScreenshotsCache(path)
	assert.False(t, ok)

	_, fileOk := loadScreenshotsCacheFile(path)
	assert.False(t, fileOk)
}

func TestScreenshotsCachePersister_concurrentCallersDoNotLoseUpdates(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "screenshots.json")
	live := map[string]screenshotsCacheEntry{}
	persist := newScreenshotsCachePersister(live, path)

	var wg sync.WaitGroup
	for i := range 32 {
		wg.Go(func() {
			url := fmt.Sprintf("https://docs.example.com/p%02d", i)
			hash := fmt.Sprintf("h%02d", i)
			entry := screenshotsCacheEntry{
				URL:         url,
				ContentHash: hash,
				Stats: analyzer.ScreenshotPageStats{
					PageURL: url,
				},
				Missing:     []analyzer.ScreenshotGap{},
				Possibly:    []analyzer.ScreenshotGap{},
				ImageIssues: []analyzer.ImageIssue{},
			}
			_ = persist(entry)
		})
	}
	wg.Wait()

	file, ok := loadScreenshotsCacheFile(path)
	require.True(t, ok)
	assert.Len(t, file.Entries, 32)
}

func TestScreenshotsCacheComplete_roundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "screenshots.json")
	in := map[string]screenshotsCacheEntry{
		screenshotsCacheKey("https://x/auth", "h1", "other"): {
			URL:         "https://x/auth",
			ContentHash: "h1",
			Role:        "other",
			Stats:       analyzer.ScreenshotPageStats{PageURL: "https://x/auth"},
			Missing:     []analyzer.ScreenshotGap{},
			Possibly:    []analyzer.ScreenshotGap{},
			ImageIssues: []analyzer.ImageIssue{},
		},
	}
	now := time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC)
	complete := &screenshotsComplete{Hash: "input-hash", CompletedAt: now}
	require.NoError(t, saveScreenshotsCacheComplete(path, in, complete))

	file, ok := loadScreenshotsCacheFile(path)
	require.True(t, ok)
	require.NotNil(t, file.Complete)
	assert.Equal(t, "input-hash", file.Complete.Hash)
	assert.True(t, file.Complete.CompletedAt.Equal(now))
}

func TestScreenshotsCache_entriesAreSortedByURL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "screenshots.json")
	in := map[string]screenshotsCacheEntry{
		screenshotsCacheKey("https://docs.example.com/zebra", "hz", "other"): {
			URL: "https://docs.example.com/zebra", ContentHash: "hz", Role: "other",
		},
		screenshotsCacheKey("https://docs.example.com/alpha", "ha", "other"): {
			URL: "https://docs.example.com/alpha", ContentHash: "ha", Role: "other",
		},
		screenshotsCacheKey("https://docs.example.com/mango", "hm", "other"): {
			URL: "https://docs.example.com/mango", ContentHash: "hm", Role: "other",
		},
	}
	require.NoError(t, saveScreenshotsCache(path, in))

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	str := string(data)
	iAlpha := indexOf(str, "alpha")
	iMango := indexOf(str, "mango")
	iZebra := indexOf(str, "zebra")
	require.True(t, iAlpha >= 0 && iMango >= 0 && iZebra >= 0)
	assert.Less(t, iAlpha, iMango)
	assert.Less(t, iMango, iZebra)
}

// TestScreenshotsCachedFromCli_preservesRole guards against an asymmetry
// between the cli<->analyzer adapter pair: screenshotsCacheEntryFromAnalyzer
// already copies Role, but screenshotsCachedFromCli used to drop it. Cache
// lookup keys embed role separately so the bug was latent at runtime, but
// any future reader of ScreenshotsCachedPage.Role on a cache-hit path would
// see the zero value instead of the actual stored role.
func TestScreenshotsCachedFromCli_preservesRole(t *testing.T) {
	in := map[string]screenshotsCacheEntry{
		screenshotsCacheKey("https://docs.example.com/q", "h1", "quickstart"): {
			URL:         "https://docs.example.com/q",
			ContentHash: "h1",
			Role:        "quickstart",
			Stats:       analyzer.ScreenshotPageStats{PageURL: "https://docs.example.com/q"},
			Missing:     []analyzer.ScreenshotGap{},
			Possibly:    []analyzer.ScreenshotGap{},
			ImageIssues: []analyzer.ImageIssue{},
		},
	}
	out := screenshotsCachedFromCli(in)
	require.Len(t, out, 1)
	for _, v := range out {
		assert.Equal(t, "quickstart", v.Role)
	}
}

// TestComputeScreenshotsInputHash_roleAffectsHash guards the completion
// sentinel against a silent-drop trap: when a docs page's role is
// reclassified between runs (content unchanged, model unchanged), the
// sentinel hash MUST change so the screenshot pass re-runs the page.
// Without role in the sentinel, the per-page cache's role-aware key
// would miss while the sentinel match short-circuits the whole pass —
// dropping the page's findings entirely.
func TestComputeScreenshotsInputHash_roleAffectsHash(t *testing.T) {
	pagesA := []analyzer.DocPage{{
		URL:     "https://docs.example.com/p",
		Content: "# Page\n\nHello.\n",
		Role:    "quickstart",
	}}
	pagesB := []analyzer.DocPage{{
		URL:     "https://docs.example.com/p",
		Content: "# Page\n\nHello.\n",
		Role:    "reference",
	}}
	llmSmall := "anthropic/claude-haiku-4-5"

	hashA := computeScreenshotsInputHash(pagesA, llmSmall)
	hashB := computeScreenshotsInputHash(pagesB, llmSmall)
	assert.NotEqual(t, hashA, hashB,
		"hash must differ when only Role differs; otherwise a role reclassification silently drops findings")
}

func TestScreenshotsCachePages_listedSortedByURL(t *testing.T) {
	// Pages list mirrors sorted entry URLs.
	path := filepath.Join(t.TempDir(), "screenshots.json")
	in := map[string]screenshotsCacheEntry{
		screenshotsCacheKey("https://x/zebra", "hz", "other"): {URL: "https://x/zebra", ContentHash: "hz", Role: "other"},
		screenshotsCacheKey("https://x/alpha", "ha", "other"): {URL: "https://x/alpha", ContentHash: "ha", Role: "other"},
	}
	require.NoError(t, saveScreenshotsCache(path, in))
	file, ok := loadScreenshotsCacheFile(path)
	require.True(t, ok)
	require.Len(t, file.Pages, 2)
	assert.Equal(t, "https://x/alpha", file.Pages[0])
	assert.Equal(t, "https://x/zebra", file.Pages[1])
}
