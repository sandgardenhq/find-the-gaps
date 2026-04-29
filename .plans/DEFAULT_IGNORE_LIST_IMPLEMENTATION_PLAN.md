# Default Ignore List Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use `superpowers:executing-plans` to implement this plan task-by-task.

**Goal:** Replace the 5-entry hardcoded `skippedDirs` map in `internal/scanner/walker.go` with a layered ignore matcher: an embedded curated defaults file, the project's `.gitignore`, and an optional `.ftgignore` override at the repo root. Add a one-line scan summary to `analyze` output.

**Architecture:** New package `internal/scanner/ignore` owns a `Matcher` that composes three independently-compiled gitignore layers and decides per path whether to skip it. `Walk` returns `(Stats, error)` — the CLI uses the stats to print `scanned N files, skipped M (defaults: X, .gitignore: Y, .ftgignore: Z)`. Defaults live in `internal/scanner/defaults.ftgignore` and ship inside the binary via `//go:embed`.

**Tech Stack:** Go 1.26+, `github.com/sabhiram/go-gitignore` (already in `go.mod`), `//go:embed`, table-driven tests, `t.TempDir()` integration tests, `testscript` for end-to-end CLI scenarios.

**Source design:** [.plans/2026-04-29-default-ignore-list-design.md](./2026-04-29-default-ignore-list-design.md)

**Project rules (from CLAUDE.md):**
- TDD is mandatory: RED → verify RED → GREEN → verify GREEN → REFACTOR → commit.
- ≥90% statement coverage per package.
- Commit after every successful TDD cycle.
- Branch: `default-ignore-list` (already on it).
- Plans live in `.plans/` (already saved here).

---

## Task 1: Bootstrap the `ignore` package with `Decision` and a stub `Matcher`

**Files:**
- Create: `internal/scanner/ignore/matcher.go`
- Create: `internal/scanner/ignore/matcher_test.go`

**Step 1: Write the failing test**

`internal/scanner/ignore/matcher_test.go`:

```go
package ignore

import "testing"

func TestMatch_emptyMatcher_returnsNoSkip(t *testing.T) {
	m := &Matcher{}
	got := m.Match("main.go", false)
	if got.Skip {
		t.Errorf("empty matcher should not skip; got %+v", got)
	}
	if got.Reason != "" {
		t.Errorf("empty matcher reason should be empty; got %q", got.Reason)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/scanner/ignore/...`
Expected: FAIL — package does not exist yet.

**Step 3: Write minimal implementation**

`internal/scanner/ignore/matcher.go`:

```go
// Package ignore composes layered gitignore-style rules and decides whether a
// path should be skipped during a repository walk.
package ignore

// Decision is the result of testing a path against the layered rules.
type Decision struct {
	Skip   bool
	Reason string // name of the layer that decided, "" if no layer matched
}

// Matcher evaluates paths against an ordered list of gitignore layers.
type Matcher struct {
	layers []layer
}

type layer struct {
	name string
}

// Match reports whether relPath should be skipped.
func (m *Matcher) Match(relPath string, isDir bool) Decision {
	return Decision{}
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/scanner/ignore/...`
Expected: PASS.

**Step 5: Commit**

```bash
git add internal/scanner/ignore/matcher.go internal/scanner/ignore/matcher_test.go
git commit -m "$(cat <<'EOF'
feat(scanner/ignore): bootstrap Matcher and Decision

- RED: TestMatch_emptyMatcher_returnsNoSkip
- GREEN: Decision struct + Matcher stub returning zero value
- Status: 1 test passing, build successful

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
EOF
)"
```

---

## Task 2: Add a single-layer matcher that delegates to `sabhiram/go-gitignore`

**Files:**
- Modify: `internal/scanner/ignore/matcher.go`
- Modify: `internal/scanner/ignore/matcher_test.go`

**Step 1: Write the failing test**

Append to `matcher_test.go`:

```go
func TestMatch_singleLayer_matchesPositive(t *testing.T) {
	m, err := newMatcherFromLayers(map[string]string{
		"defaults": "*.log\n",
	}, []string{"defaults"})
	if err != nil {
		t.Fatalf("newMatcherFromLayers: %v", err)
	}
	got := m.Match("app.log", false)
	if !got.Skip {
		t.Errorf("expected skip for app.log; got %+v", got)
	}
	if got.Reason != "defaults" {
		t.Errorf("reason = %q, want %q", got.Reason, "defaults")
	}
}

func TestMatch_singleLayer_noMatch(t *testing.T) {
	m, err := newMatcherFromLayers(map[string]string{
		"defaults": "*.log\n",
	}, []string{"defaults"})
	if err != nil {
		t.Fatalf("newMatcherFromLayers: %v", err)
	}
	got := m.Match("main.go", false)
	if got.Skip {
		t.Errorf("expected no skip for main.go; got %+v", got)
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/scanner/ignore/...`
Expected: FAIL — `newMatcherFromLayers` undefined.

**Step 3: Write minimal implementation**

Replace `matcher.go` body. Note: Task 1 left `Matcher` empty and there is no `layer` type yet — both are added here, alongside their first consumer (`newMatcherFromLayers`). This is intentional to keep `golangci-lint` happy at every commit boundary.

```go
package ignore

import gitignore "github.com/sabhiram/go-gitignore"

type Decision struct {
	Skip   bool
	Reason string
}

type Matcher struct {
	layers []layer
}

type layer struct {
	name string
	gi   *gitignore.GitIgnore
}

// newMatcherFromLayers compiles the given source strings in the given order.
// Exposed for tests only — production code uses Load.
//
// The (*Matcher, error) signature is forward-looking: today every path
// succeeds (sabhiram/go-gitignore's CompileIgnoreLines does not return
// an error), but Task 6's Load needs the error channel for os.ReadFile.
// Keeping it now avoids a churning signature change later.
func newMatcherFromLayers(sources map[string]string, order []string) (*Matcher, error) {
	m := &Matcher{}
	for _, name := range order {
		src, ok := sources[name]
		if !ok {
			continue
		}
		gi := gitignore.CompileIgnoreLines(splitLines(src)...)
		m.layers = append(m.layers, layer{name: name, gi: gi})
	}
	return m, nil
}

func splitLines(s string) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		out = append(out, s[start:])
	}
	return out
}

func (m *Matcher) Match(relPath string, isDir bool) Decision {
	d := Decision{}
	for _, l := range m.layers {
		if l.gi.MatchesPath(relPath) {
			d = Decision{Skip: true, Reason: l.name}
		}
	}
	return d
}
```

**Step 4: Run tests to verify they pass**

Run: `go test ./internal/scanner/ignore/...`
Expected: PASS (3 tests).

**Step 5: Commit**

```bash
git add internal/scanner/ignore/matcher.go internal/scanner/ignore/matcher_test.go
git commit -m "$(cat <<'EOF'
feat(scanner/ignore): single-layer match via go-gitignore

- RED: TestMatch_singleLayer_{matchesPositive,noMatch}
- GREEN: layer compiled via CompileIgnoreLines; Match iterates layers in order
- Status: 3 tests passing

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
EOF
)"
```

---

## Task 3: Cross-layer negation (later layer's `!pattern` re-includes path skipped by earlier layer)

**Files:**
- Modify: `internal/scanner/ignore/matcher.go`
- Modify: `internal/scanner/ignore/matcher_test.go`

**Step 1: Write the failing test**

Append to `matcher_test.go`:

```go
func TestMatch_laterLayerNegatesEarlier(t *testing.T) {
	m, err := newMatcherFromLayers(map[string]string{
		"defaults":   "vendor/\n",
		".ftgignore": "!vendor/\n",
	}, []string{"defaults", ".ftgignore"})
	if err != nil {
		t.Fatalf("newMatcherFromLayers: %v", err)
	}
	got := m.Match("vendor/lib.go", false)
	if got.Skip {
		t.Errorf("later !vendor/ should re-include; got %+v", got)
	}
	if got.Reason != ".ftgignore" {
		t.Errorf("reason = %q, want %q", got.Reason, ".ftgignore")
	}
}

func TestMatch_earlierLayerCannotNegateLater(t *testing.T) {
	// Sanity: a defaults negation does NOT undo a .ftgignore positive match.
	m, err := newMatcherFromLayers(map[string]string{
		"defaults":   "!something\n",
		".ftgignore": "something\n",
	}, []string{"defaults", ".ftgignore"})
	if err != nil {
		t.Fatalf("newMatcherFromLayers: %v", err)
	}
	got := m.Match("something", false)
	if !got.Skip {
		t.Errorf("later positive should win; got %+v", got)
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/scanner/ignore/...`
Expected: FAIL — `MatchesPath` returns true for both positive and negative matches; current `Match` ignores the negation.

**Step 3: Write minimal implementation**

Replace the `Match` method and add a per-layer helper that returns a tri-state:

```go
type layerResult int

const (
	layerNoMatch layerResult = iota
	layerSkip
	layerNegate
)

func (l layer) check(relPath string) layerResult {
	matches, pattern := l.gi.MatchesPathHow(relPath)
	if !matches {
		return layerNoMatch
	}
	if pattern != nil && pattern.Negate {
		return layerNegate
	}
	return layerSkip
}

func (m *Matcher) Match(relPath string, isDir bool) Decision {
	d := Decision{}
	for _, l := range m.layers {
		switch l.check(relPath) {
		case layerSkip:
			d = Decision{Skip: true, Reason: l.name}
		case layerNegate:
			d = Decision{Skip: false, Reason: l.name}
		}
	}
	return d
}
```

> Note: `sabhiram/go-gitignore` exposes `MatchesPathHow` returning `(bool, *IgnorePattern)` where `IgnorePattern.Negate` is set for `!`-prefixed lines. Confirm by reading `vendor or go module cache` if you haven't seen this API; the contract is stable in `v0.0.0-20210923224102-525f6e181f06`.

**Step 4: Run tests to verify they pass**

Run: `go test ./internal/scanner/ignore/...`
Expected: PASS (5 tests).

**Step 5: Commit**

```bash
git add internal/scanner/ignore/matcher.go internal/scanner/ignore/matcher_test.go
git commit -m "$(cat <<'EOF'
feat(scanner/ignore): cross-layer negation via MatchesPathHow

- RED: TestMatch_laterLayerNegatesEarlier, TestMatch_earlierLayerCannotNegateLater
- GREEN: per-layer tri-state (NoMatch/Skip/Negate); Match folds in order
- Status: 5 tests passing

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
EOF
)"
```

---

## Task 4: Directory matching — gitignore's trailing-slash semantics

> **Plan correction (2026-04-29):** the plan originally framed this as test-only.
> `sabhiram/go-gitignore`'s API is string-only — it does NOT honour an `isDir`
> hint internally. Real fix: in `Match`, when `isDir` is true and `relPath`
> doesn't already end in `/`, append `/` to the probe before per-layer `check`.
> Implemented in commit `be4d759`.

**Files:**
- Modify: `internal/scanner/ignore/matcher_test.go`
- Modify: `internal/scanner/ignore/matcher.go` (4-line probe construction in `Match`)

**Step 1: Write the failing test**

Append to `matcher_test.go`:

```go
func TestMatch_directoryPattern(t *testing.T) {
	m, err := newMatcherFromLayers(map[string]string{
		"defaults": "build/\n",
	}, []string{"defaults"})
	if err != nil {
		t.Fatalf("newMatcherFromLayers: %v", err)
	}
	if got := m.Match("build", true); !got.Skip {
		t.Errorf("build/ should match dir 'build'; got %+v", got)
	}
	if got := m.Match("build/output.txt", false); !got.Skip {
		t.Errorf("build/ should match files inside; got %+v", got)
	}
	// Sanity: a file literally named "build" (no slash) — gitignore semantics
	// say `build/` matches dirs only. We accept whatever the lib decides; this
	// test pins current behaviour.
}

func TestMatch_floatingBasename(t *testing.T) {
	m, err := newMatcherFromLayers(map[string]string{
		"defaults": "node_modules/\n",
	}, []string{"defaults"})
	if err != nil {
		t.Fatalf("newMatcherFromLayers: %v", err)
	}
	if got := m.Match("pkg/node_modules", true); !got.Skip {
		t.Errorf("nested node_modules dir should match; got %+v", got)
	}
}
```

**Step 2: Run tests**

Run: `go test ./internal/scanner/ignore/...`
Expected: PASS (current code should already handle this — the lib does the work). If a test fails, the negation refactor in Task 3 is the suspect; revisit.

**Step 3: Commit (test-only)**

```bash
git add internal/scanner/ignore/matcher_test.go
git commit -m "$(cat <<'EOF'
test(scanner/ignore): pin directory and floating-basename semantics

- Pinning behaviour we depend on from sabhiram/go-gitignore
- Status: 7 tests passing

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
EOF
)"
```

---

## Task 5: `Stats` struct and per-layer counts

**Files:**
- Modify: `internal/scanner/ignore/matcher.go`
- Modify: `internal/scanner/ignore/matcher_test.go`

**Step 1: Write the failing test**

Append to `matcher_test.go`:

```go
func TestStats_initialState(t *testing.T) {
	var s Stats
	if s.Scanned != 0 {
		t.Errorf("Scanned = %d, want 0", s.Scanned)
	}
	if got := s.SkippedTotal(); got != 0 {
		t.Errorf("SkippedTotal = %d, want 0", got)
	}
}

func TestStats_recordSkip(t *testing.T) {
	var s Stats
	s.RecordSkip("defaults")
	s.RecordSkip("defaults")
	s.RecordSkip(".gitignore")
	if got := s.SkippedTotal(); got != 3 {
		t.Errorf("SkippedTotal = %d, want 3", got)
	}
	if got := s.Skipped["defaults"]; got != 2 {
		t.Errorf("Skipped[defaults] = %d, want 2", got)
	}
}

func TestStats_recordScanned(t *testing.T) {
	var s Stats
	s.RecordScanned()
	s.RecordScanned()
	if s.Scanned != 2 {
		t.Errorf("Scanned = %d, want 2", s.Scanned)
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/scanner/ignore/...`
Expected: FAIL — `Stats` undefined.

**Step 3: Write minimal implementation**

Add to `matcher.go`:

```go
// Stats tallies scanned and skipped paths during a walk.
type Stats struct {
	Scanned int
	Skipped map[string]int
}

func (s *Stats) RecordScanned() {
	s.Scanned++
}

func (s *Stats) RecordSkip(reason string) {
	if s.Skipped == nil {
		s.Skipped = make(map[string]int)
	}
	s.Skipped[reason]++
}

func (s *Stats) SkippedTotal() int {
	total := 0
	for _, n := range s.Skipped {
		total += n
	}
	return total
}
```

**Step 4: Run tests**

Run: `go test ./internal/scanner/ignore/...`
Expected: PASS (10 tests).

**Step 5: Commit**

```bash
git add internal/scanner/ignore/matcher.go internal/scanner/ignore/matcher_test.go
git commit -m "$(cat <<'EOF'
feat(scanner/ignore): Stats with per-layer skip counts

- RED: TestStats_{initialState,recordSkip,recordScanned}
- GREEN: Stats struct + RecordScanned/RecordSkip/SkippedTotal
- Status: 10 tests passing

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
EOF
)"
```

---

## Task 6: `Load(repoRoot)` — embedded defaults + on-disk `.gitignore` and `.ftgignore`

**Files:**
- Create: `internal/scanner/defaults.ftgignore` (placeholder; real contents in Task 7)
- Create: `internal/scanner/ignore/defaults.go`
- Modify: `internal/scanner/ignore/matcher.go`
- Modify: `internal/scanner/ignore/matcher_test.go`

**Step 1: Write the failing test**

Append to `matcher_test.go`:

```go
func TestLoad_noFiles(t *testing.T) {
	dir := t.TempDir()
	m, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if m == nil {
		t.Fatal("Load returned nil matcher")
	}
	// Defaults are always present; behaviour validated in Task 8.
	if got := m.Match("README.md", false); got.Skip {
		t.Errorf("README.md should not be skipped by minimal defaults; got %+v", got)
	}
}

func TestLoad_loadsGitignore(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("custom.txt\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	got := m.Match("custom.txt", false)
	if !got.Skip || got.Reason != ".gitignore" {
		t.Errorf("expected skip via .gitignore; got %+v", got)
	}
}

func TestLoad_loadsFtgignore(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".ftgignore"), []byte("custom.txt\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	got := m.Match("custom.txt", false)
	if !got.Skip || got.Reason != ".ftgignore" {
		t.Errorf("expected skip via .ftgignore; got %+v", got)
	}
}

func TestLoad_ftgignoreNegatesGitignore(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("data/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".ftgignore"), []byte("!data/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	got := m.Match("data/x.txt", false)
	if got.Skip {
		t.Errorf("data/ should be re-included; got %+v", got)
	}
}

func TestLoad_ftgignoreSyntaxError(t *testing.T) {
	// sabhiram/go-gitignore is permissive about syntax; pick a case it rejects.
	// If no realistic syntax error exists, this test stays pending — see Task 9.
	t.Skip("revisit after confirming what go-gitignore rejects")
}
```

Add at the top of `matcher_test.go`:
```go
import (
	"os"
	"path/filepath"
	"testing"
)
```

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/scanner/ignore/...`
Expected: FAIL — `Load` undefined.

**Step 3: Write minimal implementation**

`internal/scanner/defaults.ftgignore` (placeholder for now — Task 7 fills it):

```
# Placeholder — replaced in Task 7.
*.tmp
```

`internal/scanner/ignore/defaults.go`:

```go
package ignore

import _ "embed"

//go:embed defaults.ftgignore
var defaultsContent string
```

> Note: `//go:embed` requires the file to live in the SAME package as the embed directive. Since `defaults.ftgignore` is referenced from `internal/scanner/ignore/defaults.go` it must live at `internal/scanner/ignore/defaults.ftgignore`. The design doc said `internal/scanner/defaults.ftgignore` — that was wrong. **Use `internal/scanner/ignore/defaults.ftgignore`.** Update the design doc when you fix this.

Add `Load` to `matcher.go`:

```go
import (
	"fmt"
	"os"
	"path/filepath"

	gitignore "github.com/sabhiram/go-gitignore"
)

// Load constructs a Matcher from the embedded defaults plus any .gitignore
// and .ftgignore files at repoRoot.
func Load(repoRoot string) (*Matcher, error) {
	sources := map[string]string{"defaults": defaultsContent}
	order := []string{"defaults"}

	for _, name := range []string{".gitignore", ".ftgignore"} {
		path := filepath.Join(repoRoot, name)
		data, err := os.ReadFile(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", name, err)
		}
		sources[name] = string(data)
		order = append(order, name)
	}

	return newMatcherFromLayers(sources, order)
}
```

**Step 4: Run tests**

Run: `go test ./internal/scanner/ignore/...`
Expected: PASS (skipped test counted, others green).

**Step 5: Commit**

```bash
git add internal/scanner/ignore internal/scanner/ignore/defaults.ftgignore
git commit -m "$(cat <<'EOF'
feat(scanner/ignore): Load embeds defaults + reads .gitignore/.ftgignore

- RED: TestLoad_{noFiles,loadsGitignore,loadsFtgignore,ftgignoreNegatesGitignore}
- GREEN: //go:embed defaults; Load reads optional layer files; missing file is fine
- Status: 14 tests passing

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
EOF
)"
```

After committing, fix the design doc:
```bash
sed -i '' 's|internal/scanner/defaults\.ftgignore|internal/scanner/ignore/defaults.ftgignore|g' .plans/2026-04-29-default-ignore-list-design.md
git add .plans/2026-04-29-default-ignore-list-design.md
git commit -m "docs: correct embed path in design doc"
```

---

## Task 7: Populate the real defaults file

**Files:**
- Modify: `internal/scanner/ignore/defaults.ftgignore`
- Create: `internal/scanner/ignore/defaults_test.go`

**Step 1: Write the failing test**

`internal/scanner/ignore/defaults_test.go`:

```go
package ignore

import (
	"strings"
	"testing"

	gitignore "github.com/sabhiram/go-gitignore"
)

func TestDefaults_everyLineCompiles(t *testing.T) {
	for i, line := range strings.Split(defaultsContent, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if _, err := gitignore.CompileIgnoreLines(trimmed); err != nil {
			t.Errorf("line %d %q: %v", i+1, line, err)
		}
	}
}

func TestDefaults_skipsRepresentativeFiles(t *testing.T) {
	cases := []struct {
		path  string
		isDir bool
	}{
		{"node_modules", true},
		{"node_modules/foo.js", false},
		{"vendor/x/y.go", false},
		{"__pycache__/bar.pyc", false},
		{"dist/main.js", false},
		{"target/debug/foo", false},
		{".idea", true},
		{"package-lock.json", false},
		{"go.sum", false},
		{"Cargo.lock", false},
		{"foo_test.go", false},
		{"bar.test.ts", false},
		{"BazTest.java", false},
		{"api.pb.go", false},
		{"models_pb2.py", false},
		{"bundle.min.js", false},
		{"logo.png", false},
		{"data.zip", false},
		{".DS_Store", false},
		{"app.log", false},
	}
	m, err := Load(t.TempDir())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	for _, c := range cases {
		got := m.Match(c.path, c.isDir)
		if !got.Skip {
			t.Errorf("%s should be skipped by defaults; got %+v", c.path, got)
		}
		if got.Reason != "defaults" {
			t.Errorf("%s reason = %q, want defaults", c.path, got.Reason)
		}
	}
}

func TestDefaults_keepsRepresentativeFiles(t *testing.T) {
	keeps := []string{
		"main.go",
		"README.md",
		"docs/intro.md",
		"examples/quickstart.go",
		"package.json",
		"go.mod",
		"pyproject.toml",
		"Cargo.toml",
		"src/lib/foo.ts",
	}
	m, err := Load(t.TempDir())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	for _, p := range keeps {
		if got := m.Match(p, false); got.Skip {
			t.Errorf("%s should NOT be skipped; got %+v", p, got)
		}
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/scanner/ignore/...`
Expected: FAIL — current defaults file is just `*.tmp`.

**Step 3: Write minimal implementation**

Replace `internal/scanner/ignore/defaults.ftgignore` with:

```
# Find the Gaps default ignore list.
# Composed in order with .gitignore (next) and .ftgignore (last).
# Use ! in .ftgignore to re-include anything below.

# Category: VCS / IDE
.git/
.svn/
.hg/
.idea/
.vscode/
.vs/

# Category: dependencies
node_modules/
vendor/
bower_components/
.bundle/
.pnpm/

# Category: Python
venv/
.venv/
env/
__pycache__/
.pytest_cache/
.mypy_cache/
.ruff_cache/
.tox/

# Category: build artifacts
dist/
build/
target/
out/
bin/
obj/
_build/
cmake-build-*/
Build/

# Category: JVM
.gradle/
.mvn/
classes/

# Category: coverage
coverage/
.nyc_output/
htmlcov/

# Category: site / doc generators
_site/
public/
.docusaurus/
.next/
.nuxt/
.svelte-kit/

# Category: test scaffolding
tests/
__tests__/
spec/
testdata/
__fixtures__/
__mocks__/

# Category: lockfiles
package-lock.json
yarn.lock
pnpm-lock.yaml
go.sum
Cargo.lock
Gemfile.lock
Pipfile.lock
poetry.lock
composer.lock
*.lockb

# Category: test files
*_test.go
*.test.ts
*.test.js
*.spec.ts
*.spec.js
*Test.java
*Tests.swift
*_spec.rb
test_*.py
*_test.py

# Category: generated code
*.pb.go
*_pb2.py
*_pb2.pyi
*_pb.d.ts
*.gen.go
*.gen.ts
*_generated.go
*.g.dart

# Category: minified / bundled
*.min.js
*.min.css
*.bundle.js
*.bundle.css
*.map

# Category: binaries / assets
*.png
*.jpg
*.jpeg
*.gif
*.svg
*.webp
*.ico
*.pdf
*.zip
*.tar
*.tar.gz
*.tgz
*.7z
*.rar
*.woff
*.woff2
*.ttf
*.eot
*.mp3
*.mp4
*.mov
*.wav
*.exe
*.dll
*.so
*.dylib
*.class
*.jar
*.war
*.o
*.a
*.wasm
*.bin
*.dat

# Category: OS noise
.DS_Store
Thumbs.db
desktop.ini

# Category: logs / dumps
*.log
*.tmp
*.cache
```

**Step 4: Run tests**

Run: `go test ./internal/scanner/ignore/...`
Expected: PASS. If any path in `TestDefaults_skipsRepresentativeFiles` fails, the corresponding line is missing or wrongly anchored — fix and re-run.

**Step 5: Commit**

```bash
git add internal/scanner/ignore/defaults.ftgignore internal/scanner/ignore/defaults_test.go
git commit -m "$(cat <<'EOF'
feat(scanner/ignore): curated default ignore list

- RED: TestDefaults_{everyLineCompiles,skipsRepresentativeFiles,keepsRepresentativeFiles}
- GREEN: full categorised defaults file (VCS, deps, build, lockfiles, tests, generated, binaries, OS noise)
- Status: 17 tests passing

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
EOF
)"
```

---

## Task 8: Refactor `Walk` to use the matcher and return `Stats`

**Files:**
- Modify: `internal/scanner/walker.go`
- Modify: `internal/scanner/walker_test.go`
- Modify: `internal/scanner/scanner.go`

**Step 1: Write the failing test**

Replace the entire `internal/scanner/walker_test.go` with the following (preserve the `writeFile` helper):

```go
package scanner

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func TestWalk_findsFiles(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "main.go", "")
	writeFile(t, dir, "util.go", "")

	var found []string
	stats, err := Walk(dir, func(path string, _ os.FileInfo) error {
		found = append(found, path)
		return nil
	})
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	sort.Strings(found)
	if got := []string{"main.go", "util.go"}; !equal(found, got) {
		t.Fatalf("found %v, want %v", found, got)
	}
	if stats.Scanned != 2 {
		t.Errorf("Scanned = %d, want 2", stats.Scanned)
	}
}

func TestWalk_skipsDefaults(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "main.go", "")
	writeFile(t, dir, "package-lock.json", "")
	writeFile(t, dir, "logo.png", "")
	if err := os.MkdirAll(filepath.Join(dir, "node_modules"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, dir, "node_modules/lib.js", "")

	var found []string
	stats, err := Walk(dir, func(path string, _ os.FileInfo) error {
		found = append(found, path)
		return nil
	})
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	for _, f := range found {
		if f != "main.go" {
			t.Errorf("only main.go should survive, found %q", f)
		}
	}
	if stats.Skipped["defaults"] == 0 {
		t.Errorf("expected non-zero defaults skips, got %v", stats.Skipped)
	}
}

func TestWalk_respectsGitignore(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, ".gitignore", "secret.txt\n")
	writeFile(t, dir, "main.go", "")
	writeFile(t, dir, "secret.txt", "")

	var found []string
	stats, err := Walk(dir, func(path string, _ os.FileInfo) error {
		found = append(found, path)
		return nil
	})
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	for _, f := range found {
		if f == "secret.txt" {
			t.Errorf(".gitignore not respected: %q", f)
		}
	}
	if stats.Skipped[".gitignore"] != 1 {
		t.Errorf("Skipped[.gitignore] = %d, want 1", stats.Skipped[".gitignore"])
	}
}

func TestWalk_ftgignoreNegatesDefaults(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "vendor"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, dir, "vendor/x.go", "")
	writeFile(t, dir, ".ftgignore", "!vendor/\n")
	writeFile(t, dir, "main.go", "")

	var found []string
	if _, err := Walk(dir, func(path string, _ os.FileInfo) error {
		found = append(found, path)
		return nil
	}); err != nil {
		t.Fatalf("Walk: %v", err)
	}
	hasVendor := false
	for _, f := range found {
		if strings.HasPrefix(f, "vendor/") {
			hasVendor = true
		}
	}
	if !hasVendor {
		t.Errorf("vendor/x.go should be re-included; got %v", found)
	}
}

func TestWalk_skipsGitDir(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, dir, ".git/config", "")
	writeFile(t, dir, "main.go", "")

	var found []string
	if _, err := Walk(dir, func(path string, _ os.FileInfo) error {
		found = append(found, path)
		return nil
	}); err != nil {
		t.Fatalf("Walk: %v", err)
	}
	for _, f := range found {
		if strings.HasPrefix(f, ".git") {
			t.Errorf(".git should be skipped, found %q", f)
		}
	}
}

func TestWalk_callbackError(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "main.go", "")
	if _, err := Walk(dir, func(_ string, _ os.FileInfo) error {
		return os.ErrInvalid
	}); err == nil {
		t.Error("expected callback error to propagate")
	}
}

func equal(a, b []string) bool {
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

// writeFile is a test helper that creates a file with the given content.
func writeFile(t *testing.T, dir, rel, content string) {
	t.Helper()
	full := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/scanner/...`
Expected: FAIL — `Walk` returns 1 value, not 2; `TestWalk_skipsDefaults` and `TestWalk_ftgignoreNegatesDefaults` fail compilation.

**Step 3: Write minimal implementation**

Replace `internal/scanner/walker.go` entirely:

```go
package scanner

import (
	"os"
	"path/filepath"

	"github.com/sandgardenhq/find-the-gaps/internal/scanner/ignore"
)

// Walk recursively walks repoRoot, calling fn for each non-skipped file.
// Paths passed to fn are relative to repoRoot. It composes the embedded
// default ignore list with the project's .gitignore and .ftgignore (if any)
// and returns a Stats summary.
func Walk(repoRoot string, fn func(path string, info os.FileInfo) error) (ignore.Stats, error) {
	stats := ignore.Stats{}

	matcher, err := ignore.Load(repoRoot)
	if err != nil {
		return stats, err
	}

	walkErr := filepath.Walk(repoRoot, func(absPath string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		rel, err := filepath.Rel(repoRoot, absPath)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}

		decision := matcher.Match(rel, info.IsDir())
		if decision.Skip {
			stats.RecordSkip(decision.Reason)
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		if info.IsDir() {
			return nil
		}

		stats.RecordScanned()
		return fn(rel, info)
	})

	return stats, walkErr
}
```

Update `internal/scanner/scanner.go` so it consumes the new return value. Replace lines 33–58 (the `if err := Walk(...)` block) with:

```go
	if _, err := Walk(repoRoot, func(relPath string, info os.FileInfo) error {
		ext := lang.Detect(relPath)
		if ext == nil {
			return nil
		}
		absPath := filepath.Join(repoRoot, relPath)
		content, err := os.ReadFile(absPath)
		if err != nil {
			return nil
		}
		symbols, imports, err := ext.Extract(relPath, content)
		if err != nil {
			return nil
		}
		langSet[ext.Language()] = true
		files = append(files, ScannedFile{
			Path:     relPath,
			Language: ext.Language(),
			Lines:    countLines(content),
			Symbols:  symbols,
			Imports:  imports,
		})
		return nil
	}); err != nil {
		return nil, err
	}
```

**Step 4: Run tests**

Run: `go test ./internal/scanner/... ./internal/...`
Expected: PASS. Coverage check: `go test -coverprofile=coverage.out ./internal/scanner/... && go tool cover -func=coverage.out | grep total` — confirm ≥90% for `internal/scanner/ignore`.

**Step 5: Commit**

```bash
git add internal/scanner/walker.go internal/scanner/walker_test.go internal/scanner/scanner.go
git commit -m "$(cat <<'EOF'
refactor(scanner): Walk uses layered ignore matcher and returns Stats

- RED: rewritten walker_test.go expects (Stats, error) return
- GREEN: Walk delegates to ignore.Matcher; Stats tally per-layer skips
- Removes hardcoded skippedDirs and inline .gitignore loading
- scanner.Scan ignores Stats for now (Task 9 wires CLI summary)
- Status: walker tests passing; scanner integration green

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
EOF
)"
```

---

## Task 9: Surface scan summary line in `analyze`

**Files:**
- Modify: `internal/scanner/scanner.go`
- Modify: `internal/cli/analyze.go`

**Step 1: Write the failing test**

Add to `internal/cli/analyze_test.go` (find the file, look at existing structure, add a sibling test). If the file does not exist yet for this case, create `internal/cli/analyze_summary_test.go`:

```go
package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAnalyze_printsScanSummary(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "package-lock.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	cmd := NewAnalyzeCmd()
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"--repo", dir, "--cache-dir", filepath.Join(t.TempDir(), "cache")})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "scanned ") || !strings.Contains(out, "skipped ") {
		t.Errorf("expected scan summary in output; got:\n%s", out)
	}
	if !strings.Contains(out, "defaults:") {
		t.Errorf("expected defaults segment in summary; got:\n%s", out)
	}
}

func TestAnalyze_quietSuppressesSummary(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FIND_THE_GAPS_QUIET", "1")

	var buf bytes.Buffer
	cmd := NewAnalyzeCmd()
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"--repo", dir, "--cache-dir", filepath.Join(t.TempDir(), "cache")})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if strings.Contains(buf.String(), "skipped ") {
		t.Errorf("FIND_THE_GAPS_QUIET should suppress summary; got:\n%s", buf.String())
	}
}
```

> Verify the analyze constructor name. If it's not `NewAnalyzeCmd`, find it via:
> `grep -n 'cobra.Command{' internal/cli/analyze.go`

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/cli/...`
Expected: FAIL — current code prints `scanned %d files\n` only when `docsURL == ""`; no skip counts present.

**Step 3: Write minimal implementation**

(a) Have `scanner.Scan` return the `ignore.Stats` so the CLI can format it. Update `internal/scanner/scanner.go`:

```go
// Add to top imports:
//   "github.com/sandgardenhq/find-the-gaps/internal/scanner/ignore"
// Change ProjectScan or return signature — pick one. Simpler: return Stats alongside scan.

func Scan(repoRoot string, opts Options) (*ProjectScan, ignore.Stats, error) {
    // ... existing body ...
    stats, err := Walk(repoRoot, func(relPath string, info os.FileInfo) error {
        // existing callback body
    })
    if err != nil {
        return nil, stats, err
    }
    // ... rest ...
    return scan, stats, nil
}
```

> Every caller of `scanner.Scan` must be updated. Today the only caller is `internal/cli/analyze.go:121`. Use `grep -rn 'scanner\.Scan(' .` to confirm.

(b) Update `internal/cli/analyze.go`. Replace the existing scan block roughly at lines 116–129:

```go
log.Info("scanning repository", "path", repoPath)
scanOpts := scanner.Options{
    CacheDir: filepath.Join(projectDir, "scan"),
    NoCache:  noCache,
}
scan, stats, err := scanner.Scan(repoPath, scanOpts)
if err != nil {
    return fmt.Errorf("scan failed: %w", err)
}
log.Debug("scan complete", "files", len(scan.Files))

if os.Getenv("FIND_THE_GAPS_QUIET") != "1" {
    fmt.Fprintln(cmd.OutOrStdout(), formatScanSummary(stats))
}

if docsURL == "" {
    return nil
}
```

(c) Add `formatScanSummary` to `internal/cli/analyze.go` (or a new sibling file `internal/cli/scan_summary.go` if you prefer):

```go
func formatScanSummary(s ignore.Stats) string {
    parts := []string{}
    for _, name := range []string{"defaults", ".gitignore", ".ftgignore"} {
        if n := s.Skipped[name]; n > 0 {
            parts = append(parts, fmt.Sprintf("%s: %d", name, n))
        }
    }
    skipped := s.SkippedTotal()
    if skipped == 0 {
        return fmt.Sprintf("scanned %d files, skipped 0", s.Scanned)
    }
    return fmt.Sprintf("scanned %d files, skipped %d (%s)", s.Scanned, skipped, strings.Join(parts, ", "))
}
```

Add the necessary imports (`strings`, `internal/scanner/ignore`).

**Step 4: Run tests**

Run: `go test ./...`
Expected: PASS.

**Step 5: Commit**

```bash
git add internal/scanner/scanner.go internal/cli/analyze.go internal/cli/analyze_summary_test.go
git commit -m "$(cat <<'EOF'
feat(cli): print scan summary with per-layer skip counts

- RED: TestAnalyze_{printsScanSummary,quietSuppressesSummary}
- GREEN: scanner.Scan returns ignore.Stats; formatScanSummary builds the line
- FIND_THE_GAPS_QUIET=1 suppresses the line
- Status: all tests passing, build clean

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
EOF
)"
```

---

## Task 10: testscript end-to-end

**Files:**
- Create: `cmd/find-the-gaps/testdata/script/ignore_summary.txtar`
- Create: `cmd/find-the-gaps/testdata/script/ftgignore_negation.txtar`

**Step 1: Write the failing test**

`cmd/find-the-gaps/testdata/script/ignore_summary.txtar`:

```txtar
# Verify analyze prints the scan summary with per-layer skip counts.
exec find-the-gaps analyze --repo repo --cache-dir cache
stdout 'scanned 1 files, skipped 1 .defaults: 1.'

-- repo/main.go --
package main

func main() {}
-- repo/package-lock.json --
{}
```

`cmd/find-the-gaps/testdata/script/ftgignore_negation.txtar`:

```txtar
# Verify .ftgignore negation re-includes a default-skipped path.
exec find-the-gaps analyze --repo repo --cache-dir cache
stdout 'scanned 2 files'
stdout '\.ftgignore: 1'

-- repo/main.go --
package main

func main() {}
-- repo/.ftgignore --
!vendor/
-- repo/vendor/lib.go --
package vendor
```

> Look at an existing `*.txtar` (e.g. `analyze_stub.txtar`) to confirm the pattern syntax and any required setup commands. Adjust the regex above if testscript needs escaping.

**Step 2: Run tests to verify they fail or pass**

Run: `go test ./cmd/find-the-gaps/...`
Expected: PASS if Task 9 was correct. If not, fix the regex or summary format until they match.

**Step 3: Commit**

```bash
git add cmd/find-the-gaps/testdata/script/ignore_summary.txtar cmd/find-the-gaps/testdata/script/ftgignore_negation.txtar
git commit -m "$(cat <<'EOF'
test(cli): testscript scenarios for scan summary and .ftgignore negation

- ignore_summary: verifies summary line + per-layer count
- ftgignore_negation: verifies !pattern re-includes defaults-skipped path
- Status: all testscript scenarios passing

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
EOF
)"
```

---

## Task 11: Coverage gate, lint, README mention

**Files:**
- Verify coverage on `internal/scanner/ignore/` and `internal/scanner/`
- Modify: `README.md` (one short paragraph)
- Modify: `PROGRESS.md` (per CLAUDE.md rule #8)

**Step 1: Verify coverage**

```bash
go test -coverprofile=coverage.out ./internal/scanner/... && go tool cover -func=coverage.out | grep -E 'internal/scanner/(ignore|walker)'
```

Required: every package ≥ 90.0%. If a function is below threshold, add a focused test in the appropriate `_test.go` file.

**Step 2: Run lint**

```bash
golangci-lint run
```

Expected: no errors.

**Step 3: Update README**

Add a short subsection under the existing "How it works" or "Configuration" area. Find the right home with:
```bash
grep -n '^##' README.md
```

Insert paragraph (approximate; adapt to existing tone):

```markdown
### Ignored files

Find the Gaps ships a curated list of files it never analyses — lockfiles,
build artifacts, generated bindings, binary assets, test files, and the
usual VCS / IDE noise. The full list is `internal/scanner/ignore/defaults.ftgignore`.

Override the defaults with a `.ftgignore` at your repo root. It uses gitignore
syntax, including `!` to re-include something the defaults skip:

    # .ftgignore
    !vendor/
    !*_test.go
    custom_build_dir/

The scan summary printed by `ftg analyze` shows how many files each layer
skipped:

    scanned 412 files, skipped 1,847 (defaults: 1,801, .gitignore: 38, .ftgignore: 8)
```

**Step 4: Update PROGRESS.md**

Append:

```markdown
## Default Ignore List - COMPLETE
- Started: 2026-04-29
- Finished: <date>
- Tests: <N> passing, 0 failing
- Coverage: internal/scanner/ignore X%, internal/scanner Y%
- Build: ✅ Successful
- Linting: ✅ Clean
- Notes: New `internal/scanner/ignore` package layers embedded defaults +
  .gitignore + .ftgignore. Walk now returns ignore.Stats. Analyze prints a
  per-layer skip summary. README + .ftgignore negation documented.
```

**Step 5: Commit**

```bash
git add README.md PROGRESS.md
git commit -m "$(cat <<'EOF'
docs: document default ignore list and .ftgignore override

- README: how to override defaults with .ftgignore
- PROGRESS: log completion, coverage, status
- Status: feature complete, ready for PR

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
EOF
)"
```

---

## Final verification

Run the full project gate before opening a PR:

```bash
gofmt -w . && goimports -w .
go test ./...
go test -coverprofile=coverage.out ./... && go tool cover -func=coverage.out | tail -5
golangci-lint run
go build ./...
```

All four commands must succeed. Then push the branch and open a PR against `main` with a summary mirroring the design doc and a "Closes #" only if an issue exists.

## Notes for the executor

- Stay strictly in the order above. Tasks 1–6 build the matcher in isolated, testable slices; Task 7 plugs in real defaults; Task 8 swaps the walker; Task 9 wires the CLI. Reordering risks RED tests passing for the wrong reason.
- If `sabhiram/go-gitignore`'s `MatchesPathHow` API is named differently or returns a different shape than Task 3 assumes, read the dep's source under `~/go/pkg/mod/github.com/sabhiram/!go-!git!ignore*` before adapting. Do not invent a name.
- The design doc said defaults live at `internal/scanner/defaults.ftgignore`. That was wrong — `//go:embed` requires same-package placement. Defaults live at `internal/scanner/ignore/defaults.ftgignore`. Task 6 includes a one-line fix to the design doc.
- No public API surface change is exposed beyond `scanner.Walk`'s signature and `scanner.Scan`'s return tuple; both are internal. No CLI flag changes. No config file added.
