# Skip Drift on No-Op Re-run — Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** When `find-the-gaps analyze` re-runs with unchanged inputs and `gaps.md` already exists, skip the drift-detection pass entirely and leave `gaps.md` alone.

**Architecture:** Add a `complete` sentinel to `drift.json` that records a SHA-256 hash of the drift inputs (featureMap files+symbols, docsFeatureMap pages). On re-run, the CLI compares a freshly computed hash to the sentinel; on a match (with both upstream map caches hit, `--no-cache` unset, and `gaps.md` present) the drift call and the `gaps.md` rewrite are skipped. Findings are rebuilt purely in-memory from the existing per-feature cache so downstream consumers see byte-identical state.

**Tech Stack:** Go 1.26+, stdlib `crypto/sha256` and `encoding/json`, testify for assertions, no new dependencies.

**Design doc:** `.plans/2026-04-30-skip-drift-on-rerun-design.md`

**Working dir:** `/Users/brittcrawford/conductor/workspaces/find-the-gaps/madison`

**Branch:** `skip-gaps-rerun`

---

## Project Conventions (read before starting)

- **TDD is mandatory.** Every production line follows RED → verify RED → GREEN → verify GREEN → REFACTOR. See `CLAUDE.md` "ABSOLUTE RULES".
- Test file lives next to production file: `internal/cli/drift_cache.go` ↔ `internal/cli/drift_cache_test.go`. Same package (white-box).
- Commit after each completed RED-GREEN-REFACTOR cycle. Conventional commits, e.g. `feat(cli): hash drift inputs for completion sentinel`.
- Coverage target: ≥90% statement coverage per package.
- Branch is already `skip-gaps-rerun` (renamed at session start). Do not push or open a PR until the user asks.
- `PROGRESS.md` does not need an entry per task in this repo — recent commits show that practice has lapsed. Skip it unless the user asks.
- Never use mocks for the drift LLM tier in integration tests; this repo's verification plan forbids them. The integration test below uses a counting stub `ToolLLMClient` that satisfies the same interface used by existing `internal/analyzer/drift_test.go`. That is acceptable per existing precedent in `analyze_test.go` (which already uses stub clients for unit-style coverage of the CLI).

## Existing Types You Will Touch

- `internal/cli/drift_cache.go::driftCacheFile` — extend with `Complete *driftComplete`.
- `internal/cli/drift_cache.go::loadDriftCache(path) (map[string]analyzer.CachedDriftEntry, bool)` — leave alone; add a sibling `loadDriftCacheFile`.
- `internal/cli/drift_cache.go::saveDriftCache(path, current)` — keep, and add `saveDriftCacheComplete(path, current, complete)` (or a single `save` that takes an optional `*driftComplete`).
- `internal/analyzer/types.go::FeatureMap = []FeatureEntry` where `FeatureEntry = { Feature CodeFeature, Files []string, Symbols []string }`.
- `internal/analyzer/types.go::DocsFeatureMap = []DocsFeatureEntry` where `DocsFeatureEntry = { Feature string, Pages []string }`.
- `internal/analyzer/drift.go::CachedDriftEntry = { Files []string, Pages []string, Issues []DriftIssue }`.
- `internal/cli/analyze.go` drift block at lines ~329–375 (relative to current `main` HEAD `057a06e`).

## Commands You Will Run Constantly

```bash
# Run a single test by name
go test ./internal/cli -run TestComputeDriftInputHash_Deterministic -v

# Run the cli package tests
go test ./internal/cli -count=1 -v

# Whole repo
go test ./... -count=1

# Coverage on the cli package
go test -coverprofile=coverage.out ./internal/cli && go tool cover -func=coverage.out | tail -5

# Lint and build
golangci-lint run
go build ./...
```

---

## Task 1: Hash function — determinism

**Files:**
- Test: `internal/cli/drift_cache_test.go` (append)
- Create later: `internal/cli/drift_cache.go` (the function)

**Step 1: Write the failing test**

Append to `internal/cli/drift_cache_test.go`:

```go
func TestComputeDriftInputHash_Deterministic(t *testing.T) {
	fm := analyzer.FeatureMap{
		{Feature: analyzer.CodeFeature{Name: "auth"}, Files: []string{"auth.go", "session.go"}, Symbols: []string{"Login", "Logout"}},
		{Feature: analyzer.CodeFeature{Name: "search"}, Files: []string{"search.go"}, Symbols: []string{"Query"}},
	}
	dm := analyzer.DocsFeatureMap{
		{Feature: "auth", Pages: []string{"https://docs.example.com/auth"}},
		{Feature: "search", Pages: []string{"https://docs.example.com/search"}},
	}
	h1 := computeDriftInputHash(fm, dm)
	h2 := computeDriftInputHash(fm, dm)
	assert.Equal(t, h1, h2)
	assert.NotEmpty(t, h1)
	assert.Len(t, h1, 64, "expect hex SHA-256")
}
```

**Step 2: Run test, expect fail**

```
go test ./internal/cli -run TestComputeDriftInputHash_Deterministic -v
```

Expected: compile error `undefined: computeDriftInputHash`.

**Step 3: Minimal implementation**

Append to `internal/cli/drift_cache.go`:

```go
import (
	"crypto/sha256"
	"encoding/hex"
	// ... existing imports
)

// computeDriftInputHash returns a hex SHA-256 of the inputs that the drift
// pass consumes from upstream (featureMap files+symbols, docsMap pages).
// It is independent of map iteration order — entries are sorted by feature
// name and slice contents are sorted before hashing.
func computeDriftInputHash(fm analyzer.FeatureMap, dm analyzer.DocsFeatureMap) string {
	type fEntry struct {
		Name    string   `json:"name"`
		Files   []string `json:"files"`
		Symbols []string `json:"symbols"`
	}
	type dEntry struct {
		Name  string   `json:"name"`
		Pages []string `json:"pages"`
	}
	type payload struct {
		Features []fEntry `json:"features"`
		Docs     []dEntry `json:"docs"`
	}

	feats := make([]fEntry, 0, len(fm))
	for _, e := range fm {
		files := append([]string(nil), e.Files...)
		syms := append([]string(nil), e.Symbols...)
		sort.Strings(files)
		sort.Strings(syms)
		feats = append(feats, fEntry{Name: e.Feature.Name, Files: files, Symbols: syms})
	}
	sort.Slice(feats, func(i, j int) bool { return feats[i].Name < feats[j].Name })

	docs := make([]dEntry, 0, len(dm))
	for _, e := range dm {
		pages := append([]string(nil), e.Pages...)
		sort.Strings(pages)
		docs = append(docs, dEntry{Name: e.Feature, Pages: pages})
	}
	sort.Slice(docs, func(i, j int) bool { return docs[i].Name < docs[j].Name })

	data, _ := json.Marshal(payload{Features: feats, Docs: docs})
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
```

**Step 4: Run test, expect pass**

Same command. Expected: PASS.

**Step 5: Commit**

```bash
git add internal/cli/drift_cache.go internal/cli/drift_cache_test.go
git commit -m "feat(cli): hash drift inputs for completion sentinel"
```

---

## Task 2: Hash function — order-independence

**Files:**
- Test: `internal/cli/drift_cache_test.go`

**Step 1: Add failing test**

```go
func TestComputeDriftInputHash_OrderIndependent(t *testing.T) {
	fm1 := analyzer.FeatureMap{
		{Feature: analyzer.CodeFeature{Name: "alpha"}, Files: []string{"a1.go", "a2.go"}, Symbols: []string{"A1"}},
		{Feature: analyzer.CodeFeature{Name: "beta"}, Files: []string{"b1.go"}, Symbols: []string{"B1"}},
	}
	fm2 := analyzer.FeatureMap{
		{Feature: analyzer.CodeFeature{Name: "beta"}, Files: []string{"b1.go"}, Symbols: []string{"B1"}},
		{Feature: analyzer.CodeFeature{Name: "alpha"}, Files: []string{"a2.go", "a1.go"}, Symbols: []string{"A1"}},
	}
	dm1 := analyzer.DocsFeatureMap{
		{Feature: "alpha", Pages: []string{"https://x/1", "https://x/2"}},
	}
	dm2 := analyzer.DocsFeatureMap{
		{Feature: "alpha", Pages: []string{"https://x/2", "https://x/1"}},
	}
	assert.Equal(t, computeDriftInputHash(fm1, dm1), computeDriftInputHash(fm2, dm2))
}
```

**Step 2: Run, expect pass** (already passes from Task 1's sort logic; this test pins the contract).

```
go test ./internal/cli -run TestComputeDriftInputHash_OrderIndependent -v
```

Expected: PASS.

**Step 3: Commit**

```bash
git add internal/cli/drift_cache_test.go
git commit -m "test(cli): pin order-independence of drift input hash"
```

---

## Task 3: Hash function — sensitivity

**Files:**
- Test: `internal/cli/drift_cache_test.go`

**Step 1: Add failing test**

```go
func TestComputeDriftInputHash_DifferentInputsDiffer(t *testing.T) {
	base := analyzer.FeatureMap{
		{Feature: analyzer.CodeFeature{Name: "auth"}, Files: []string{"auth.go"}, Symbols: []string{"Login"}},
	}
	dm := analyzer.DocsFeatureMap{
		{Feature: "auth", Pages: []string{"https://x/auth"}},
	}
	h0 := computeDriftInputHash(base, dm)

	// Change a file
	c1 := analyzer.FeatureMap{
		{Feature: analyzer.CodeFeature{Name: "auth"}, Files: []string{"auth.go", "session.go"}, Symbols: []string{"Login"}},
	}
	assert.NotEqual(t, h0, computeDriftInputHash(c1, dm), "files change must change hash")

	// Change a symbol
	c2 := analyzer.FeatureMap{
		{Feature: analyzer.CodeFeature{Name: "auth"}, Files: []string{"auth.go"}, Symbols: []string{"Login", "Logout"}},
	}
	assert.NotEqual(t, h0, computeDriftInputHash(c2, dm), "symbols change must change hash")

	// Change a page
	dm2 := analyzer.DocsFeatureMap{
		{Feature: "auth", Pages: []string{"https://x/auth", "https://x/auth2"}},
	}
	assert.NotEqual(t, h0, computeDriftInputHash(base, dm2), "pages change must change hash")

	// Change a feature name
	c3 := analyzer.FeatureMap{
		{Feature: analyzer.CodeFeature{Name: "AUTH"}, Files: []string{"auth.go"}, Symbols: []string{"Login"}},
	}
	assert.NotEqual(t, h0, computeDriftInputHash(c3, dm), "feature name change must change hash")
}
```

**Step 2: Run, expect pass**

```
go test ./internal/cli -run TestComputeDriftInputHash_DifferentInputsDiffer -v
```

Expected: PASS (same logic, different inputs).

**Step 3: Commit**

```bash
git add internal/cli/drift_cache_test.go
git commit -m "test(cli): pin drift hash sensitivity to input changes"
```

---

## Task 4: Sentinel shape — driftComplete + loadDriftCacheFile

**Files:**
- Modify: `internal/cli/drift_cache.go`
- Test: `internal/cli/drift_cache_test.go`

**Step 1: Add failing tests**

```go
func TestLoadDriftCacheFile_OldShape_NoComplete(t *testing.T) {
	// Old drift.json (written by saveDriftCache today) must load with Complete == nil.
	path := filepath.Join(t.TempDir(), "drift.json")
	in := map[string]analyzer.CachedDriftEntry{
		"auth": {Files: []string{"auth.go"}, Pages: []string{"https://x/auth"}, Issues: []analyzer.DriftIssue{}},
	}
	require.NoError(t, saveDriftCache(path, in))

	file, ok := loadDriftCacheFile(path)
	require.True(t, ok)
	assert.Nil(t, file.Complete)
	require.Len(t, file.Entries, 1)
	assert.Equal(t, "auth", file.Entries[0].Feature)
}

func TestSaveLoadDriftCacheComplete_RoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "drift.json")
	in := map[string]analyzer.CachedDriftEntry{
		"auth": {Files: []string{"auth.go"}, Pages: []string{"https://x/auth"}, Issues: []analyzer.DriftIssue{}},
	}
	now := time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC)
	complete := &driftComplete{Hash: "abc123", CompletedAt: now}
	require.NoError(t, saveDriftCacheComplete(path, in, complete))

	file, ok := loadDriftCacheFile(path)
	require.True(t, ok)
	require.NotNil(t, file.Complete)
	assert.Equal(t, "abc123", file.Complete.Hash)
	assert.True(t, file.Complete.CompletedAt.Equal(now))
}

func TestLoadDriftCacheFile_FileNotExist_ReturnsFalse(t *testing.T) {
	_, ok := loadDriftCacheFile(filepath.Join(t.TempDir(), "drift.json"))
	assert.False(t, ok)
}
```

(Add `"time"` to the test file's imports if not already present.)

**Step 2: Run, expect fail**

```
go test ./internal/cli -run 'TestLoadDriftCacheFile|TestSaveLoadDriftCacheComplete' -v
```

Expected: compile errors (`undefined: driftComplete`, `loadDriftCacheFile`, `saveDriftCacheComplete`).

**Step 3: Implement**

In `internal/cli/drift_cache.go`:

```go
import "time"

type driftComplete struct {
	Hash        string    `json:"hash"`
	CompletedAt time.Time `json:"completedAt"`
}

// Update existing struct:
type driftCacheFile struct {
	Features []string          `json:"features"`
	Entries  []driftCacheEntry `json:"entries"`
	Complete *driftComplete    `json:"complete,omitempty"`
}

// loadDriftCacheFile returns the full driftCacheFile (entries + sentinel).
// Returns (zero, false) on missing file, parse error, or any I/O error.
func loadDriftCacheFile(path string) (driftCacheFile, bool) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return driftCacheFile{}, false
	}
	if err != nil {
		return driftCacheFile{}, false
	}
	var f driftCacheFile
	if err := json.Unmarshal(data, &f); err != nil {
		return driftCacheFile{}, false
	}
	return f, true
}

// saveDriftCacheComplete writes the cache atomically with a completion sentinel.
// Pass nil to write without one (equivalent to saveDriftCache).
func saveDriftCacheComplete(path string, current map[string]analyzer.CachedDriftEntry, complete *driftComplete) error {
	names := make([]string, 0, len(current))
	for k := range current {
		names = append(names, k)
	}
	sort.Strings(names)

	entries := make([]driftCacheEntry, 0, len(names))
	for _, name := range names {
		c := current[name]
		issues := c.Issues
		if issues == nil {
			issues = []analyzer.DriftIssue{}
		}
		entries = append(entries, driftCacheEntry{
			Feature: name,
			Files:   c.Files,
			Pages:   c.Pages,
			Issues:  issues,
		})
	}
	f := driftCacheFile{Features: names, Entries: entries, Complete: complete}

	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".drift-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return err
	}
	if err := tmp.Chmod(0o644); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	return nil
}
```

Refactor `saveDriftCache` to delegate to `saveDriftCacheComplete(path, current, nil)` — DRY.

**Step 4: Run all drift cache tests**

```
go test ./internal/cli -run Drift -v
```

Expected: all PASS, including the existing `TestSaveAndLoadDriftCache_RoundTrip` and friends.

**Step 5: Commit**

```bash
git add internal/cli/drift_cache.go internal/cli/drift_cache_test.go
git commit -m "feat(cli): driftComplete sentinel and loadDriftCacheFile"
```

---

## Task 5: Rebuild findings from cache

**Files:**
- Modify: `internal/cli/drift_cache.go`
- Test: `internal/cli/drift_cache_test.go`

**Step 1: Add failing tests**

```go
func TestDriftFindingsFromCache_ReturnsOnlyFeaturesInFeatureMap(t *testing.T) {
	fm := analyzer.FeatureMap{
		{Feature: analyzer.CodeFeature{Name: "auth"}, Files: []string{"auth.go"}},
		{Feature: analyzer.CodeFeature{Name: "search"}, Files: []string{"search.go"}},
	}
	cache := map[string]analyzer.CachedDriftEntry{
		"auth": {
			Issues: []analyzer.DriftIssue{{Page: "https://x/auth", Issue: "Stale."}},
		},
		"search": {
			Issues: []analyzer.DriftIssue{}, // zero issues — not a finding
		},
		"removed": {
			Issues: []analyzer.DriftIssue{{Page: "https://x/r", Issue: "Should not appear."}},
		},
	}
	out := driftFindingsFromCache(cache, fm)
	require.Len(t, out, 1)
	assert.Equal(t, "auth", out[0].Feature)
	assert.Equal(t, "Stale.", out[0].Issues[0].Issue)
}

func TestDriftFindingsFromCache_EmptyCache_ReturnsNil(t *testing.T) {
	fm := analyzer.FeatureMap{
		{Feature: analyzer.CodeFeature{Name: "auth"}, Files: []string{"auth.go"}},
	}
	out := driftFindingsFromCache(map[string]analyzer.CachedDriftEntry{}, fm)
	assert.Empty(t, out)
}
```

**Step 2: Run, expect fail (compile error)**

```
go test ./internal/cli -run TestDriftFindingsFromCache -v
```

**Step 3: Implement**

In `internal/cli/drift_cache.go`:

```go
// driftFindingsFromCache rebuilds DriftFindings from per-feature cache
// entries, restricted to features present in featureMap. Features with
// zero issues do not produce a finding (matches DetectDrift's contract).
// Output is sorted by feature name for stable diffs.
func driftFindingsFromCache(cache map[string]analyzer.CachedDriftEntry, fm analyzer.FeatureMap) []analyzer.DriftFinding {
	if len(cache) == 0 {
		return nil
	}
	names := make([]string, 0, len(fm))
	for _, e := range fm {
		names = append(names, e.Feature.Name)
	}
	sort.Strings(names)
	out := make([]analyzer.DriftFinding, 0)
	for _, name := range names {
		c, ok := cache[name]
		if !ok || len(c.Issues) == 0 {
			continue
		}
		out = append(out, analyzer.DriftFinding{Feature: name, Issues: c.Issues})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
```

**Step 4: Run, expect pass**

Same command. Expected: PASS.

**Step 5: Commit**

```bash
git add internal/cli/drift_cache.go internal/cli/drift_cache_test.go
git commit -m "feat(cli): rebuild drift findings from cache"
```

---

## Task 6: Wire skip path into analyze.go

**Files:**
- Modify: `internal/cli/analyze.go`
- Test: `internal/cli/analyze_skip_drift_test.go` (new file)

**Step 1: Write the failing integration test**

Create `internal/cli/analyze_skip_drift_test.go`. The pattern below mirrors how existing tests in this package construct a fake project tree and call `runAnalyze` (or whatever helper they use). Open `internal/cli/analyze_test.go` and `internal/cli/analyze_parallel_test.go` first — copy the harness, do not invent a new one.

The test must:

1. Create a temporary `projectDir` with prepopulated caches: `featuremap.json`, `docsfeaturemap.json`, `codefeatures.json` matching a small fixture.
2. Run analyze once with a counting stub `ToolLLMClient` (`investigatorCalls int`). Confirm calls > 0 and `gaps.md` is written.
3. Capture the mtime of `gaps.md`.
4. Run analyze a second time with the SAME stub. Confirm:
   - `investigatorCalls` did not increase.
   - `drift.json.complete` is present and matches the freshly computed hash.
   - `gaps.md` mtime is unchanged (skip path did NOT rewrite it).
5. Mutate `featuremap.json` (add a file to a feature). Run a third time. Confirm `investigatorCalls` increased and `drift.json.complete.hash` changed.
6. Restore unchanged inputs, delete `gaps.md`, run a fourth time. Confirm `investigatorCalls` increased (gaps.md missing forces re-run).

If the existing analyze tests stub LLM clients differently (e.g., they go through `newLLMTiering` only and there's no clean injection point), prefer adding a small package-private hook (e.g., a `tieringFactory` var in `analyze.go` defaulted to `newLLMTiering` and overridable in tests) over rewriting the harness from scratch.

**Step 2: Run, expect fail**

```
go test ./internal/cli -run TestAnalyzeSkipsDriftOnSecondRun -v
```

Expected: FAIL — second run still calls the investigator (skip path not yet wired).

**Step 3: Implement the skip path**

In `internal/cli/analyze.go`, replace the existing drift block (lines ~329–375) with:

```go
log.Infof("detecting documentation drift...")
pageReader := func(url string) (string, error) {
    path, ok := idx.FilePath(url)
    if !ok {
        return "", fmt.Errorf("page not in cache: %s", url)
    }
    data, err := os.ReadFile(path)
    return string(data), err
}
docCoveredFeatures := make([]string, 0, len(docsFeatureMap))
for _, entry := range docsFeatureMap {
    if len(entry.Pages) > 0 {
        docCoveredFeatures = append(docCoveredFeatures, entry.Feature)
    }
}

driftCachePath := filepath.Join(projectDir, "drift.json")
gapsPath := filepath.Join(projectDir, "gaps.md")
wantHash := computeDriftInputHash(featureMap, docsFeatureMap)

driftSkipped := false
var driftFindings []analyzer.DriftFinding

if !noCache && codeMapCached && docsMapCached {
    if file, ok := loadDriftCacheFile(driftCachePath); ok && file.Complete != nil && file.Complete.Hash == wantHash {
        if _, err := os.Stat(gapsPath); err == nil {
            cachedMap := driftCacheEntriesToMap(file.Entries)
            driftFindings = driftFindingsFromCache(cachedMap, featureMap)
            driftSkipped = true
            log.Infof("drift: cache complete, skipping (hash %s…)", wantHash[:8])
        }
    }
}

if !driftSkipped {
    driftOnFinding := func(accumulated []analyzer.DriftFinding) error {
        return reporter.WriteGaps(projectDir, featureMap, docCoveredFeatures, accumulated)
    }

    var cached map[string]analyzer.CachedDriftEntry
    if !noCache {
        if loaded, ok := loadDriftCache(driftCachePath); ok {
            cached = loaded
            log.Infof("using cached drift results (%d features)", len(cached))
        }
    }

    liveCache := seedDriftLiveCache(cached, featureMap)
    hits, fresh := 0, 0
    onFeatureDone := newDriftCachePersister(cached, liveCache, driftCachePath, &hits, &fresh)

    var err error
    driftFindings, err = analyzer.DetectDrift(
        ctx, tiering, featureMap, docsFeatureMap,
        pageReader, repoPath,
        cached, driftOnFinding, onFeatureDone,
    )
    if err != nil {
        return fmt.Errorf("detect drift: %w", err)
    }
    log.Infof("drift cache: %d hits, %d fresh", hits, fresh)
    log.Debugf("drift detection complete: %d findings", len(driftFindings))

    // Stamp completion sentinel.
    if err := saveDriftCacheComplete(driftCachePath, liveCache, &driftComplete{
        Hash:        wantHash,
        CompletedAt: time.Now(),
    }); err != nil {
        return fmt.Errorf("save drift completion: %w", err)
    }
}
```

Add a small helper at the bottom of `internal/cli/drift_cache.go`:

```go
// driftCacheEntriesToMap converts the on-disk slice form back to a map keyed
// by feature name.
func driftCacheEntriesToMap(entries []driftCacheEntry) map[string]analyzer.CachedDriftEntry {
	m := make(map[string]analyzer.CachedDriftEntry, len(entries))
	for _, e := range entries {
		issues := e.Issues
		if issues == nil {
			issues = []analyzer.DriftIssue{}
		}
		files := e.Files
		if files == nil {
			files = []string{}
		}
		pages := e.Pages
		if pages == nil {
			pages = []string{}
		}
		m[e.Feature] = analyzer.CachedDriftEntry{Files: files, Pages: pages, Issues: issues}
	}
	return m
}
```

Then later in `analyze.go`, gate the WriteGaps call:

```go
if err := reporter.WriteMapping(projectDir, productSummary, featureMap, docsFeatureMap); err != nil {
    return fmt.Errorf("write mapping: %w", err)
}
if !driftSkipped {
    if err := reporter.WriteGaps(projectDir, featureMap, docCoveredFeatures, driftFindings); err != nil {
        return fmt.Errorf("write gaps: %w", err)
    }
}
```

And update the `reports:` line:

```go
gapsLine := "  " + projectDir + "/gaps.md"
if driftSkipped {
    gapsLine += " (cached, drift unchanged)"
}
// ... use gapsLine in the Fprintf
```

(Adjust the format string accordingly so `gaps.md` is a `%s` slot now.)

**Step 4: Run, expect pass**

```
go test ./internal/cli -run TestAnalyzeSkipsDriftOnSecondRun -count=1 -v
go test ./... -count=1
```

Expected: all PASS.

**Step 5: Commit**

```bash
git add internal/cli/analyze.go internal/cli/drift_cache.go internal/cli/analyze_skip_drift_test.go
git commit -m "feat(cli): skip drift detection on no-op re-run

When drift.json carries a completion sentinel matching the current
featureMap+docsFeatureMap inputs, gaps.md exists, both upstream maps
were cache hits, and --no-cache is unset, skip the entire DetectDrift
call and the gaps.md rewrite. Findings are rebuilt from the existing
per-feature cache so site.Build sees byte-identical state."
```

---

## Task 7: Verify lint, build, full test suite

**Files:** none — verification only.

**Step 1: Lint**

```
golangci-lint run
```

Expected: no errors. If any are introduced (typically unused imports after the refactor in Task 4), fix them and amend the most recent commit only if it's the broken commit; otherwise create a `chore(cli): satisfy linter` commit.

**Step 2: Build**

```
go build ./...
```

Expected: success.

**Step 3: Full test suite**

```
go test ./... -count=1
```

Expected: all PASS.

**Step 4: Coverage check**

```
go test -coverprofile=coverage.out ./internal/cli && go tool cover -func=coverage.out | tail -20
```

Expected: ≥90% on the new functions (`computeDriftInputHash`, `loadDriftCacheFile`, `saveDriftCacheComplete`, `driftFindingsFromCache`, `driftCacheEntriesToMap`).

**Step 5: Manual smoke test (optional but recommended)**

If a fixture project exists locally:

```
./bin/find-the-gaps analyze --repo <fixture> --docs-url <url>   # cold run
./bin/find-the-gaps analyze --repo <fixture> --docs-url <url>   # should print "drift: cache complete, skipping" and "(cached, drift unchanged)"
touch <fixture>/some_file.go                                     # invalidates featuremap upstream
./bin/find-the-gaps analyze --repo <fixture> --docs-url <url>   # should NOT skip
```

**Step 6: No commit needed if everything was clean.**

---

## Done When

- [ ] All tests in Tasks 1–6 are committed and pass.
- [ ] `golangci-lint run` is clean.
- [ ] `go test ./... -count=1` is green.
- [ ] Coverage on `internal/cli` stays ≥90% statement coverage.
- [ ] Manual smoke test shows the skip log line and `(cached, drift unchanged)` annotation on the second run.
- [ ] Branch `skip-gaps-rerun` has a clean commit history; no `--amend` of pushed commits.

## Out of Scope (do not touch this branch)

- Skipping `site.Build` on no-op re-runs.
- Skipping `WriteMapping`.
- A new CLI flag to force-skip drift (use `--no-cache` to force re-run; that is sufficient).
- Caching `classifyDriftPages` results separately. The completion sentinel makes this unnecessary for the skip path.
- Updating `PROGRESS.md` (project has stopped tracking per-task; reintroduce only if user asks).
