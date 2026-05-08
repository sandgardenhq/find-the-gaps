# Halt on Unsupported-Language Repo — Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Halt `ftg analyze` with exit 1 and an informative message when the codebase scan finds zero files matched by a dedicated language extractor.

**Architecture:** Post-scan, pre-LLM filter. After `scanner.Scan()` returns, drop the `"Generic"` entry from `scan.Languages`. If the result is empty, print a stderr message naming the file count and the supported-language list, then return a non-`nil` error so Cobra exits 1. The scan's `project.md` and cache are still written (intentional — they document what we found).

**Tech Stack:** Go 1.26+, Cobra, testify, `testscript` (for end-to-end CLI fixtures). Reference design: `.plans/2026-05-08-halt-on-unsupported-language-design.md`.

**TDD discipline:** Per `CLAUDE.md`, every task is RED → verify-RED → GREEN → verify-GREEN → REFACTOR → COMMIT. No production code without a failing test first.

---

## Task 1: Expose the registry's language list as `lang.Languages()`

The error message must enumerate every supported language. Reading from the registry keeps the message in sync if a new extractor is added.

**Files:**
- Modify: `internal/scanner/lang/detect.go`
- Test: `internal/scanner/lang/detect_test.go` (existing — add new test)

**Step 1: Write the failing test**

Add to `internal/scanner/lang/detect_test.go`:

```go
func TestLanguages_returnsRegistryEntriesInRegistrationOrder(t *testing.T) {
	got := Languages()
	want := []string{
		"Go", "Python", "TypeScript", "Rust", "Java",
		"C#", "Kotlin", "Swift", "Scala", "PHP", "Ruby", "C", "C++",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Languages() = %v, want %v", got, want)
	}
}
```

If `reflect` isn't already imported in this test file, add it. The exact `want` slice must match the names returned by each extractor's `Language()` — verify by reading each file under `internal/scanner/lang/` if uncertain.

**Step 2: Run test to verify it fails**

```
go test ./internal/scanner/lang/ -run TestLanguages_returnsRegistryEntriesInRegistrationOrder -count=1
```

Expected: FAIL with `undefined: Languages`.

**Step 3: Write minimal implementation**

In `internal/scanner/lang/detect.go`, append:

```go
// Languages returns the human-readable names of every registered extractor,
// in the order they were registered. Used by callers that need to enumerate
// the set of supported languages (e.g. error messages).
func Languages() []string {
	out := make([]string, 0, len(registry))
	for _, e := range registry {
		out = append(out, e.Language())
	}
	return out
}
```

**Step 4: Run test to verify it passes**

```
go test ./internal/scanner/lang/ -run TestLanguages_returnsRegistryEntriesInRegistrationOrder -count=1
```

Expected: PASS.

**Step 5: Commit**

```bash
git add internal/scanner/lang/detect.go internal/scanner/lang/detect_test.go
git commit -m "$(cat <<'EOF'
feat(lang): expose registry as Languages()

- RED: new test asserts Languages() returns 13 names in registration order
- GREEN: enumerate `registry` and project to Language() names
- Status: package tests pass

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
EOF
)"
```

---

## Task 2: Add `supportedLanguages` filter helper in `analyze.go`

**Files:**
- Modify: `internal/cli/analyze.go`
- Test: `internal/cli/analyze_unsupported_lang_test.go` (new)

**Step 1: Write the failing test**

Create `internal/cli/analyze_unsupported_lang_test.go`:

```go
package cli

import (
	"reflect"
	"testing"

	"github.com/sandgardenhq/find-the-gaps/internal/scanner"
)

func TestSupportedLanguages_dropsGenericEntry(t *testing.T) {
	scan := &scanner.ProjectScan{Languages: []string{"Go", "Generic", "Python"}}
	got := supportedLanguages(scan)
	want := []string{"Go", "Python"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("supportedLanguages() = %v, want %v", got, want)
	}
}

func TestSupportedLanguages_returnsEmptyWhenOnlyGeneric(t *testing.T) {
	scan := &scanner.ProjectScan{Languages: []string{"Generic"}}
	if got := supportedLanguages(scan); len(got) != 0 {
		t.Fatalf("supportedLanguages() = %v, want []", got)
	}
}

func TestSupportedLanguages_returnsEmptyWhenNoLanguages(t *testing.T) {
	scan := &scanner.ProjectScan{Languages: nil}
	if got := supportedLanguages(scan); len(got) != 0 {
		t.Fatalf("supportedLanguages() = %v, want []", got)
	}
}
```

**Step 2: Run test to verify it fails**

```
go test ./internal/cli/ -run TestSupportedLanguages -count=1
```

Expected: FAIL with `undefined: supportedLanguages`.

**Step 3: Write minimal implementation**

Append to `internal/cli/analyze.go` (near the other small helpers like `formatScanSummary`, ~line 731):

```go
// supportedLanguages returns the entries of scan.Languages with the
// "Generic" placeholder removed. An empty result means the codebase
// contained nothing that any dedicated extractor could parse.
func supportedLanguages(scan *scanner.ProjectScan) []string {
	out := make([]string, 0, len(scan.Languages))
	for _, l := range scan.Languages {
		if l == "Generic" {
			continue
		}
		out = append(out, l)
	}
	return out
}
```

**Step 4: Run test to verify it passes**

```
go test ./internal/cli/ -run TestSupportedLanguages -count=1
```

Expected: PASS, all three subtests green.

**Step 5: Commit**

```bash
git add internal/cli/analyze.go internal/cli/analyze_unsupported_lang_test.go
git commit -m "$(cat <<'EOF'
feat(cli): add supportedLanguages filter helper

- RED: three tests cover (Go+Generic+Python), (Generic-only), and (nil)
- GREEN: filter the "Generic" placeholder out of scan.Languages
- Status: package tests pass

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
EOF
)"
```

---

## Task 3: Halt analyze when `supportedLanguages` is empty

This is the integration step. We assert end-to-end that running `analyze` against a markdown-only repo exits non-zero with the expected stderr message and that no docs ingestion or LLM call fires (proven indirectly by the absence of an `mdfetch`-related precheck error — the halt happens *before* the precheck).

**Files:**
- Modify: `internal/cli/analyze.go` (insert the check after the scan)
- Test: `internal/cli/analyze_unsupported_lang_test.go` (extend)

**Step 1: Write the failing test**

Append to `internal/cli/analyze_unsupported_lang_test.go`:

```go
import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	// ...keep prior imports
)

func TestAnalyze_haltsOnUnsupportedLanguageRepo(t *testing.T) {
	repo := t.TempDir()
	cacheBase := t.TempDir()

	// Markdown + JSON only. No file matches any dedicated extractor.
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("# hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "data.json"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := run(&stdout, &stderr, []string{
		"analyze",
		"--repo", repo,
		"--cache-dir", cacheBase,
		"--docs-url", "https://example.com/docs",
	})
	if code == 0 {
		t.Fatalf("expected non-zero exit; stdout=%q stderr=%q", stdout.String(), stderr.String())
	}

	se := stderr.String()
	for _, want := range []string{
		"no supported programming languages",
		"Go, Python, TypeScript",
		"https://github.com/sandgardenhq/find-the-gaps/issues",
	} {
		if !strings.Contains(se, want) {
			t.Errorf("stderr missing %q\nfull stderr:\n%s", want, se)
		}
	}

	// project.md SHOULD have been written by the scan (we deliberately let
	// the scan persist its output before the halt — it documents what we
	// found).
	projectMD := filepath.Join(cacheBase, filepath.Base(repo), "scan", "project.md")
	if _, err := os.Stat(projectMD); err != nil {
		t.Errorf("expected project.md to exist after halt: %v", err)
	}

	// mapping.md / gaps.md MUST NOT exist — the LLM passes never ran.
	for _, name := range []string{"mapping.md", "gaps.md"} {
		p := filepath.Join(cacheBase, filepath.Base(repo), name)
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("unexpected %s exists after halt (err=%v)", name, err)
		}
	}
}
```

If the imports block becomes a duplicate, merge them into a single `import (...)` block at the top of the file.

**Step 2: Run test to verify it fails**

```
go test ./internal/cli/ -run TestAnalyze_haltsOnUnsupportedLanguageRepo -count=1 -v
```

Expected: FAIL. Most likely the run will get past the scan and hit the mdfetch precheck (since `mdfetch` may not be on the test PATH) — the failure message will not match `"no supported programming languages"`. That's the right kind of RED for this test.

**Step 3: Write minimal implementation**

In `internal/cli/analyze.go`, immediately after the existing `log.Debug("scan complete", ...)` line (~line 144) and before the `formatScanSummary` print (~line 146), insert:

```go
if langs := supportedLanguages(scan); len(langs) == 0 {
	supported := strings.Join(lang.Languages(), ", ")
	return fmt.Errorf( //nolint:staticcheck // ST1005: proper-noun lead-in
		"no supported programming languages detected in %s.\n\n"+
			"Find the Gaps walked %d files but found no %s source.\n\n"+
			"If your repo uses an unsupported language, please open an issue:\n"+
			"https://github.com/sandgardenhq/find-the-gaps/issues",
		repoPath, stats.Scanned, supported)
}
```

Add the import for the lang package at the top of the file:

```go
"github.com/sandgardenhq/find-the-gaps/internal/scanner/lang"
```

(`strings` is already imported.)

Note the placement: the check fires *after* the scan summary's `log.Debug` and *before* `formatScanSummary` writes to stdout. This means the user does not see the partial scan summary before the error, which keeps the error self-contained. The scan cache was already saved by `scanner.Scan()`, so `project.md` exists.

**Step 4: Run test to verify it passes**

```
go test ./internal/cli/ -run TestAnalyze_haltsOnUnsupportedLanguageRepo -count=1 -v
```

Expected: PASS.

Also run the full package to confirm nothing else regressed:

```
go test ./internal/cli/ -count=1
```

Expected: all green.

**Step 5: Commit**

```bash
git add internal/cli/analyze.go internal/cli/analyze_unsupported_lang_test.go
git commit -m "$(cat <<'EOF'
feat(analyze): halt with informative error on unsupported-language repo

- RED: integration test asserts exit!=0, message names languages and file
  count, project.md exists, mapping.md/gaps.md do NOT
- GREEN: filter scan.Languages, halt before docs ingestion when only
  Generic-text files were found
- Status: cli package tests pass

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
EOF
)"
```

---

## Task 4: End-to-end testscript fixture

Mirrors the behavior of `cmd/ftg/testdata/script/analyze_missing_mdfetch.txtar`.

**Files:**
- Create: `cmd/ftg/testdata/script/analyze_unsupported_language.txtar`

**Step 1: Write the failing test**

Create the file with this content:

```
# analyze halts before docs ingestion when the repo contains no source
# files matched by a dedicated language extractor. Markdown + JSON only.
mkdir repo
cp README.md repo/README.md
cp data.json repo/data.json

! exec ftg analyze --repo repo --cache-dir $WORK/cache --docs-url https://example.com/docs
stderr 'no supported programming languages'
stderr 'Go, Python, TypeScript'
stderr 'https://github.com/sandgardenhq/find-the-gaps/issues'

# Scan output exists; LLM-pass output does not.
exists $WORK/cache/repo/scan/project.md
! exists $WORK/cache/repo/mapping.md
! exists $WORK/cache/repo/gaps.md

-- README.md --
# hi
-- data.json --
{}
```

**Step 2: Run test to verify it fails (only meaningful before Task 3 lands)**

If this task is run after Task 3, the testscript will already pass — that is fine because the assertion now exercises the wired-in behavior end-to-end through the real `ftg` binary path. To confirm the script is actually exercising the code, mutate one assertion (e.g. change `'Go, Python, TypeScript'` to `'XYZ'`), run, see FAIL, then revert.

```
go test ./cmd/ftg/ -run TestScript -count=1
```

Expected after the deliberate mutation: FAIL on the new script. Revert and re-run.

**Step 3: (no-op — production code already exists from Task 3)**

**Step 4: Run test to verify it passes**

```
go test ./cmd/ftg/ -run TestScript -count=1
```

Expected: PASS, including the new fixture.

**Step 5: Commit**

```bash
git add cmd/ftg/testdata/script/analyze_unsupported_language.txtar
git commit -m "$(cat <<'EOF'
test(cli): testscript fixture for unsupported-language halt

Asserts end-to-end that ftg analyze on a markdown-only repo exits
non-zero with the documented stderr message and writes no LLM-pass
output.

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
EOF
)"
```

---

## Task 5: Add Scenario 17 to the verification plan

**Files:**
- Modify: `.plans/VERIFICATION_PLAN.md` (append after Scenario 16)

**Step 1: Append the scenario**

Append below Scenario 16, before the `## Verification Rules` section:

```markdown
### Scenario 17: Unsupported-Language Repo

**Context**: A repository whose contents do not match any of the 13 dedicated language extractors (Go, Python, TypeScript, Rust, Java, C#, Kotlin, Swift, Scala, PHP, Ruby, C, C++). `ftg analyze` should halt before any docs ingestion or LLM call.

**Steps**:
1. Create a fixture directory containing only `README.md` + a few `.json` / `.yaml` files (no source code).
2. Run `find-the-gaps analyze --repo <fixture> --docs-url https://example.com/docs -v`.
3. Inspect the exit code and stderr.
4. Inspect the project directory under `<cache>/<projectName>/`.

**Success Criteria**:
- [ ] Exit code is non-zero (1).
- [ ] Stderr contains `no supported programming languages detected in <fixture>`.
- [ ] Stderr lists every supported language (`Go, Python, TypeScript, Rust, Java, C#, Kotlin, Swift, Scala, PHP, Ruby, C, C++`).
- [ ] Stderr links to the GitHub issues page.
- [ ] `<cache>/<projectName>/scan/project.md` exists (the scan ran and produced its report).
- [ ] `<cache>/<projectName>/mapping.md` and `gaps.md` do NOT exist.
- [ ] `mdfetch` is not invoked (no entry in the verbose log).

**If Blocked**: If the halt fires on a repo that does contain supported source (e.g. a single Go file is ignored), the `Generic` filter is wrong — capture `scan.Languages` from `<cache>/<projectName>/scan/scan.json` and ask before adjusting.
```

**Step 2: Commit**

```bash
git add .plans/VERIFICATION_PLAN.md
git commit -m "$(cat <<'EOF'
docs(plans): add Scenario 17 — unsupported-language repo halt

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
EOF
)"
```

---

## Task 6: Final verification

**Step 1: Run the full suite**

```
go test ./... -count=1
```

Expected: all green.

**Step 2: Lint**

```
golangci-lint run
```

Expected: no errors.

**Step 3: Coverage**

```
go test -cover ./internal/cli/ ./internal/scanner/lang/
```

Expected: both packages remain at or above their pre-change coverage; new helpers are fully exercised.

**Step 4: Build**

```
go build ./...
```

Expected: succeeds.

**Step 5: Update PROGRESS.md**

Append a new section per the template in `CLAUDE.md`:

```markdown
## Task: Halt on Unsupported-Language Repo - COMPLETE
- Started: 2026-05-08
- Tests: <N> passing, 0 failing (added 5 unit tests + 1 testscript fixture)
- Coverage: <fill in from go test -cover>
- Build: ✅ Successful
- Linting: ✅ Clean
- Completed: 2026-05-08
- Notes: Strict halt; no override flag. Scan cache and project.md still
  written before halt by design. Verification Scenario 17 added.
```

**Step 6: Commit**

```bash
git add PROGRESS.md
git commit -m "$(cat <<'EOF'
docs: log halt-on-unsupported-language task complete

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
EOF
)"
```

---

## Done When

- All six tasks committed in order.
- `go test ./...`, `golangci-lint run`, and `go build ./...` all pass.
- The branch is ready for PR against `main`. Per CLAUDE.md, PRs use **merge commits** (no squash) and the description references the design doc at `.plans/2026-05-08-halt-on-unsupported-language-design.md`.
