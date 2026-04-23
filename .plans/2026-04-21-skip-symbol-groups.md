# Skip Symbol Groups Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add a `--no-symbols` flag to `analyze` that maps features to files only, skipping symbol-level analysis and symbol output.

**Architecture:** Three layered changes — (1) add `filesOnly bool` to `MapFeaturesToCode` so it sends only file paths to the LLM and strips symbols from output; (2) add `filesOnly bool` to `WriteGaps` so the Undocumented Code section is replaced with a "not available" note; (3) thread a `--no-symbols` CLI flag through `runBothMaps` to both functions.

**Tech Stack:** Go stdlib, Cobra flags

---

## Context

`MapFeaturesToCode` in `internal/analyzer/mapper.go` currently builds a compact symbol index (`"file/path: Symbol1, Symbol2"`) and sends it to the LLM, which returns both files and symbol names per feature. The goal is an opt-in mode that:

- Sends only file paths (no symbol names) to the LLM
- Ignores any symbols in the LLM response
- Skips the Undocumented Code section of `gaps.md` (it requires symbol data)
- Leaves `mapping.md` unchanged (the reporter already omits the Symbols line when empty)

Files with no symbols are still skipped (current behaviour, unchanged).

---

### Task 1: Add `filesOnly` to `MapFeaturesToCode`

**Files:**
- Modify: `internal/analyzer/mapper.go`
- Modify: `internal/analyzer/mapper_test.go`

---

**Step 1: Write the failing tests**

Add these two tests at the bottom of `internal/analyzer/mapper_test.go` (before the closing brace of the package, no new imports needed — `context`, `strings`, and `scanner` are already imported):

```go
func TestMapFeaturesToCode_FilesOnly_PromptOmitsSymbolNames(t *testing.T) {
	client := &fakeClient{responses: []string{`[{"feature":"auth","files":["auth.go"]}]`}}
	scan := &scanner.ProjectScan{
		Files: []scanner.ScannedFile{
			{Path: "auth.go", Symbols: []scanner.Symbol{{Name: "Login", Kind: scanner.KindFunc}}},
		},
	}
	_, err := MapFeaturesToCode(context.Background(), client, fakeCounter{n: 0}, []string{"auth"}, scan, 80_000, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(client.receivedPrompts) == 0 {
		t.Fatal("expected LLM to be called")
	}
	prompt := client.receivedPrompts[0]
	if strings.Contains(prompt, ": Login") {
		t.Errorf("filesOnly prompt must not contain symbol names, but found ': Login' in:\n%s", prompt)
	}
	if !strings.Contains(prompt, "auth.go") {
		t.Error("filesOnly prompt must still contain file paths")
	}
}

func TestMapFeaturesToCode_FilesOnly_SymbolsAlwaysEmpty(t *testing.T) {
	// Even if the LLM incorrectly returns symbols, they must be stripped in filesOnly mode.
	client := &fakeClient{responses: []string{`[{"feature":"auth","files":["auth.go"],"symbols":["Login"]}]`}}
	scan := &scanner.ProjectScan{
		Files: []scanner.ScannedFile{
			{Path: "auth.go", Symbols: []scanner.Symbol{{Name: "Login", Kind: scanner.KindFunc}}},
		},
	}
	got, err := MapFeaturesToCode(context.Background(), client, fakeCounter{n: 0}, []string{"auth"}, scan, 80_000, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) == 0 {
		t.Fatal("expected one entry")
	}
	if len(got[0].Symbols) != 0 {
		t.Errorf("filesOnly mode must produce empty Symbols, got %v", got[0].Symbols)
	}
	if len(got[0].Files) == 0 {
		t.Error("filesOnly mode must still return matched files")
	}
}
```

**Step 2: Run the tests to verify RED**

```bash
go test ./internal/analyzer/ -run 'TestMapFeaturesToCode_FilesOnly' -v
```

Expected: compile error — `MapFeaturesToCode` currently takes 7 arguments; the tests pass 8. The error will read something like `too many arguments in call to MapFeaturesToCode`.

**Step 3: Implement the changes**

In `internal/analyzer/mapper.go`, make the following changes:

**3a.** Update the `MapFeaturesToCode` signature — add `filesOnly bool` as the 7th argument (before `onBatch`):

```go
func MapFeaturesToCode(ctx context.Context, client LLMClient, counter TokenCounter, features []string, scan *scanner.ProjectScan, tokenBudget int, filesOnly bool, onBatch MapProgressFunc) (FeatureMap, error) {
```

**3b.** Update the `symLines` building loop (currently lines 43–51) to branch on `filesOnly`:

```go
	var symLines []string
	for _, f := range scan.Files {
		if len(f.Symbols) == 0 {
			continue
		}
		if filesOnly {
			symLines = append(symLines, f.Path)
		} else {
			names := make([]string, len(f.Symbols))
			for i, s := range f.Symbols {
				names[i] = s.Name
			}
			symLines = append(symLines, fmt.Sprintf("%s: %s", f.Path, strings.Join(names, ", ")))
		}
	}
```

**3c.** Replace the single `promptText` assignment inside the batch loop with a conditional. The existing PROMPT: comment stays for the `filesOnly=false` branch; add a new one for the `filesOnly=true` branch:

```go
		var promptText string
		if filesOnly {
			// PROMPT: Maps product features to code files only (symbol analysis disabled). Returns a JSON array only.
			promptText = fmt.Sprintf(`You are mapping product features to their code implementations.

Product features:
%s

Code files:
%s

For each feature, identify which code files are most relevant to implementing it.
Return a JSON array where each element has:
- "feature": the feature name exactly as provided
- "files": list of relevant file paths (empty array if none)

Respond with only the JSON array. No markdown code fences. No prose.`, string(featuresJSON), strings.Join(batch, "\n"))
		} else {
			// PROMPT: Maps product features to the code files and symbols most likely to implement them. Returns a JSON array only.
			promptText = fmt.Sprintf(`You are mapping product features to their code implementations.

Product features:
%s

Code symbols (format: "file/path: Symbol1, Symbol2"):
%s

For each feature, identify which code files and exported symbols are most relevant to implementing it.
Return a JSON array where each element has:
- "feature": the feature name exactly as provided
- "files": list of relevant file paths (empty array if none)
- "symbols": list of relevant exported symbol names (empty array if none)

Respond with only the JSON array. No markdown code fences. No prose.`, string(featuresJSON), strings.Join(batch, "\n"))
		}
```

**3d.** In the accumulator loop (the `for _, e := range entries` block), gate symbol accumulation on `!filesOnly`:

```go
		for _, e := range entries {
			entry, ok := acc[e.Feature]
			if !ok {
				continue
			}
			for _, f := range e.Files {
				entry.files[f] = struct{}{}
			}
			if !filesOnly {
				for _, s := range e.Symbols {
					entry.symbols[s] = struct{}{}
				}
			}
		}
```

**3e.** Update all existing `MapFeaturesToCode` call sites in `mapper_test.go` — every existing test that calls `MapFeaturesToCode` must pass `false` as the new 7th argument. Search for all occurrences:

```bash
grep -n "MapFeaturesToCode(" internal/analyzer/mapper_test.go
```

Each call like:
```go
MapFeaturesToCode(ctx, client, counter, features, scan, budget, onBatch)
```
becomes:
```go
MapFeaturesToCode(ctx, client, counter, features, scan, budget, false, onBatch)
```

**Step 4: Run the tests to verify GREEN**

```bash
go test ./internal/analyzer/ -v
```

Expected: all tests pass. The two new `FilesOnly` tests pass; all pre-existing tests pass with `false` threaded through.

**Step 5: Check coverage**

```bash
go test -cover ./internal/analyzer/
```

Expected: ≥90% (previously 93.1%). The new branches add coverage.

**Step 6: Commit**

```bash
git add internal/analyzer/mapper.go internal/analyzer/mapper_test.go
git commit -m "$(cat <<'EOF'
feat(analyzer): add filesOnly mode to MapFeaturesToCode

- RED: TestMapFeaturesToCode_FilesOnly_PromptOmitsSymbolNames
- RED: TestMapFeaturesToCode_FilesOnly_SymbolsAlwaysEmpty
- GREEN: filesOnly bool param; file-path-only symLines; stripped symbol
         accumulation; simplified LLM prompt when filesOnly=true

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>
EOF
)"
```

---

### Task 2: Update `WriteGaps` for files-only mode

**Files:**
- Modify: `internal/reporter/reporter.go`
- Modify: `internal/reporter/reporter_test.go`

---

**Step 1: Write the failing test**

Add this test to `internal/reporter/reporter_test.go`. Check what imports are present; add `"strings"` if missing:

```go
func TestWriteGaps_FilesOnly_ReplacesSymbolSectionWithNote(t *testing.T) {
	dir := t.TempDir()
	scan := &scanner.ProjectScan{
		Files: []scanner.ScannedFile{{
			Path:    "auth.go",
			Symbols: []scanner.Symbol{{Name: "Login", Kind: scanner.KindFunc}},
		}},
	}
	err := WriteGaps(dir, scan, analyzer.FeatureMap{}, []string{}, true)
	if err != nil {
		t.Fatal(err)
	}
	content, readErr := os.ReadFile(filepath.Join(dir, "gaps.md"))
	if readErr != nil {
		t.Fatal(readErr)
	}
	text := string(content)
	if strings.Contains(text, "Login") {
		t.Error("filesOnly mode must not list symbol names in gaps.md")
	}
	if !strings.Contains(text, "not available") {
		t.Error("filesOnly mode must include a 'not available' note in the Undocumented Code section")
	}
}
```

**Step 2: Run the test to verify RED**

```bash
go test ./internal/reporter/ -run TestWriteGaps_FilesOnly -v
```

Expected: compile error — `WriteGaps` currently takes 4 arguments; this test passes 5.

**Step 3: Implement the changes**

In `internal/reporter/reporter.go`:

**3a.** Update the `WriteGaps` signature — add `filesOnly bool` as the last argument:

```go
func WriteGaps(dir string, scan *scanner.ProjectScan, mapping analyzer.FeatureMap, allDocFeatures []string, filesOnly bool) error {
```

**3b.** Replace the Undocumented Code block (currently lines 68–84) with a conditional:

```go
	sb.WriteString("## Undocumented Code\n\n")
	if filesOnly {
		sb.WriteString("_Symbol analysis not available (run without --no-symbols to identify undocumented symbols)._\n")
	} else {
		found := false
		for _, f := range scan.Files {
			for _, sym := range f.Symbols {
				if sym.Kind != scanner.KindFunc && sym.Kind != scanner.KindType && sym.Kind != scanner.KindInterface {
					continue
				}
				if isExported(sym.Name) && !mappedSymbols[sym.Name] {
					fmt.Fprintf(&sb, "- `%s` in `%s` — no documentation page covers this symbol\n", sym.Name, f.Path)
					found = true
				}
			}
		}
		if !found {
			sb.WriteString("_None found._\n")
		}
	}
```

**3c.** Update all existing `WriteGaps` call sites in `reporter_test.go` — every call must pass `false` as the new last argument:

```bash
grep -n "WriteGaps(" internal/reporter/reporter_test.go
```

Each call like:
```go
WriteGaps(dir, scan, mapping, features)
```
becomes:
```go
WriteGaps(dir, scan, mapping, features, false)
```

**Step 4: Run the tests to verify GREEN**

```bash
go test ./internal/reporter/ -v
```

Expected: all tests pass including the new `FilesOnly` test.

**Step 5: Commit**

```bash
git add internal/reporter/reporter.go internal/reporter/reporter_test.go
git commit -m "$(cat <<'EOF'
feat(reporter): omit symbol analysis in WriteGaps when filesOnly

- RED: TestWriteGaps_FilesOnly_ReplacesSymbolSectionWithNote
- GREEN: filesOnly bool param; Undocumented Code section replaced with
         availability note when true

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>
EOF
)"
```

---

### Task 3: Wire `--no-symbols` flag through the CLI

**Files:**
- Modify: `internal/cli/analyze.go`
- Modify: `internal/cli/analyze_parallel_test.go`

---

**Step 1: Write the failing tests**

Add to `internal/cli/analyze_parallel_test.go` (no new imports needed):

```go
func TestRunBothMaps_FilesOnly_PassedThrough(t *testing.T) {
	client := &stubLLMClient{
		codeResp: `[{"feature":"auth","files":["auth.go"]}]`,
		docsResp: `["auth"]`,
	}
	codeMap, _, err := runBothMaps(
		context.Background(),
		client,
		analyzer.NewTiktokenCounter(),
		[]string{"auth"},
		stubScan(),
		map[string]string{
			"https://example.com/auth": writeTempFile(t, "auth content"),
		},
		2,
		10_000,
		true,  // filesOnly
		nil,
		nil,
	)
	require.NoError(t, err)
	require.Len(t, codeMap, 1)
	assert.Empty(t, codeMap[0].Symbols, "filesOnly mode must produce empty Symbols")
}
```

Also add a test for the CLI flag registration. Add to a suitable existing test file — check if `internal/cli/analyze_test.go` exists; if it does, add there. If not, add to `internal/cli/analyze_parallel_test.go`:

```go
func TestAnalyzeCmd_NoSymbolsFlag_Registered(t *testing.T) {
	cmd := newAnalyzeCmd()
	flag := cmd.Flags().Lookup("no-symbols")
	if flag == nil {
		t.Fatal("--no-symbols flag not registered on analyze command")
	}
	if flag.DefValue != "false" {
		t.Errorf("--no-symbols default should be false, got %q", flag.DefValue)
	}
}
```

**Step 2: Run the tests to verify RED**

```bash
go test ./internal/cli/ -run 'TestRunBothMaps_FilesOnly|TestAnalyzeCmd_NoSymbolsFlag' -v
```

Expected: compile errors — `runBothMaps` takes 10 arguments; this test passes 11. `newAnalyzeCmd` has no `--no-symbols` flag yet.

**Step 3: Implement the changes**

In `internal/cli/analyze.go`:

**3a.** Add `filesOnly bool` to `runBothMaps` — insert after `docsTokenBudget int` and before `onCodeBatch`:

```go
func runBothMaps(
	ctx context.Context,
	client analyzer.LLMClient,
	counter analyzer.TokenCounter,
	features []string,
	scan *scanner.ProjectScan,
	pages map[string]string,
	workers int,
	docsTokenBudget int,
	filesOnly bool,
	onCodeBatch analyzer.MapProgressFunc,
	onDocsPage analyzer.DocsMapProgressFunc,
) (analyzer.FeatureMap, analyzer.DocsFeatureMap, error) {
```

**3b.** Inside `runBothMaps`, thread `filesOnly` into the `MapFeaturesToCode` call:

```go
	go func() {
		fm, err := analyzer.MapFeaturesToCode(ctx, client, counter, features, scan, analyzer.MapperTokenBudget, filesOnly, onCodeBatch)
		codeCh <- bothMapsResult{fm, err}
	}()
```

**3c.** In `newAnalyzeCmd`, add the flag variable and register it:

```go
	var (
		...
		noSymbols   bool
	)
	...
	cmd.Flags().BoolVar(&noSymbols, "no-symbols", false, "map features to files only, skipping symbol-level analysis")
```

**3d.** In the `RunE` body, thread `noSymbols` into the `runBothMaps` call (add it after `analyzer.DocsMapperPageBudget`):

```go
			freshCodeMap, freshDocsMap, mapErr := runBothMaps(
				ctx, llmClient, tokenCounter, productSummary.Features,
				scan, pages, workers, analyzer.DocsMapperPageBudget,
				noSymbols,
				func(partial analyzer.FeatureMap) error {
					return saveFeatureMapCache(featureMapCachePath, productSummary.Features, partial)
				},
				func(partial analyzer.DocsFeatureMap) error {
					return saveDocsFeatureMapCache(docsFeatureMapCachePath, productSummary.Features, partial)
				},
			)
```

**3e.** Thread `noSymbols` into the `WriteGaps` call (already at the bottom of `RunE`):

```go
			if err := reporter.WriteGaps(projectDir, scan, featureMap, productSummary.Features, noSymbols); err != nil {
```

**3f.** Update the three existing `runBothMaps` call sites in `analyze_parallel_test.go` — each needs `false` added as the `filesOnly` argument (in the position before `nil, nil` or before `onCodeBatch`):

```bash
grep -n "runBothMaps(" internal/cli/analyze_parallel_test.go
```

Each call like:
```go
runBothMaps(ctx, client, counter, features, scan, pages, workers, budget, onCode, onDocs)
```
becomes:
```go
runBothMaps(ctx, client, counter, features, scan, pages, workers, budget, false, onCode, onDocs)
```

**Step 4: Run the tests to verify GREEN**

```bash
go test ./internal/cli/ -v
```

Expected: all tests pass.

**Step 5: Run the full suite and lint**

```bash
go test ./...
golangci-lint run
```

Expected: all packages green, 0 lint issues.

**Step 6: Commit**

```bash
git add internal/cli/analyze.go internal/cli/analyze_parallel_test.go
git commit -m "$(cat <<'EOF'
feat(cli): add --no-symbols flag to analyze command

Threads filesOnly through runBothMaps → MapFeaturesToCode and WriteGaps.
When set, feature mapping sends file paths only (no symbol names) to the
LLM and the Undocumented Code section of gaps.md is omitted.

- RED: TestRunBothMaps_FilesOnly_PassedThrough
- RED: TestAnalyzeCmd_NoSymbolsFlag_Registered
- GREEN: --no-symbols flag wired end-to-end

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>
EOF
)"
```

---

## Design Notes

- **Why `filesOnly bool` not an options struct?** The function already has many parameters, but adding one bool is simpler than a new type with no other use case yet (YAGNI).
- **Why still skip files with no symbols?** Those files have no exported surface — they're implementation details. Whether or not we send symbol names, they don't belong in a feature map. Consistent with current behavior.
- **`mapping.md` needs no change.** `WriteMapping` already guards the Symbols line with `if len(entry.Symbols) > 0` (reporter.go:28).
- **Cache interaction.** The feature map cache stores `FeatureEntry` including `Symbols`. If the cache was built with `filesOnly=false` and replayed with `filesOnly=true` (or vice versa), the cached result is used as-is. This is acceptable — a stale cache is invalidated by feature set changes, not by flag changes. Users wanting a clean filesOnly run should use `--no-cache`.
