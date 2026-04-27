# Hugo Site Publishing Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add a new `internal/site` package that emits a Hextra-themed Hugo static site on every `analyze` run, with `--no-site`, `--keep-site-source`, and `--site-mode={mirror,expanded}` flags. Add `hugo` as a runtime dependency alongside `mdfetch` (Homebrew, doctor, install-deps, first-run banner).

**Architecture:** Pure consumer of analyzer output — `internal/site` reads the same in-memory data the reporter does, materializes Hugo source (config, content, embedded Hextra theme) to disk, shells out to `hugo`, and produces deployable HTML in `<projectDir>/site/`. No coupling to the analyzer; no coupling to `internal/reporter`.

**Tech Stack:** Go 1.26+, `text/template`, `embed`, `os/exec`, Cobra, testify, testscript, Hugo CLI, Hextra theme.

**Source design:** [`.plans/HUGO_SITE_PUBLISHING_DESIGN.md`](./HUGO_SITE_PUBLISHING_DESIGN.md)

---

## Conventions for All Tasks

- **TDD is mandatory.** Write the failing test first, watch it fail, then write the minimal code to pass. No exceptions.
- **One commit per task** unless otherwise noted. Use the project's commit message style: `type(scope): subject`.
- **Run `go test ./...` after every task.** All tests must pass before moving on.
- **Run `golangci-lint run` before committing.** Zero warnings.
- **Coverage gate:** ≥90% statement coverage on `internal/site` (verified at the end of Phase 5).
- **Branch:** `feat/hugo-site-output` (already created).

---

## Phase 0 — Vendoring & Setup

### Task 0.1: Vendor the Hextra theme

**Files:**
- Create: `internal/site/assets/theme/hextra/...` (vendored copy)
- Create: `internal/site/assets/theme/hextra/VERSION` (records the pinned tag/sha)
- Create: `Makefile` target `vendor-hextra` (or extend existing Makefile if present)

**Step 1: Pin a Hextra version**

Use the latest stable release tag from https://github.com/imfing/hextra/releases (verify the tag exists at implementation time — at the time of writing, recent tags are `v0.10.x`).

**Step 2: Vendor the theme**

```bash
TAG=v0.10.0  # confirm latest at implementation time
TMP=$(mktemp -d)
git clone --depth 1 --branch "$TAG" https://github.com/imfing/hextra.git "$TMP/hextra"
mkdir -p internal/site/assets/theme/hextra
# Copy theme contents but NOT the demo content or .git
rsync -a --exclude='.git' --exclude='exampleSite' --exclude='node_modules' "$TMP/hextra/" internal/site/assets/theme/hextra/
echo "$TAG" > internal/site/assets/theme/hextra/VERSION
rm -rf "$TMP"
```

**Step 3: Add re-vendor target to Makefile**

```makefile
.PHONY: vendor-hextra
vendor-hextra:
	@echo "Re-vendoring Hextra theme. Set TAG=<version> to pin." && \
	TAG=$${TAG:-$$(cat internal/site/assets/theme/hextra/VERSION)}; \
	TMP=$$(mktemp -d); \
	git clone --depth 1 --branch "$$TAG" https://github.com/imfing/hextra.git "$$TMP/hextra"; \
	rm -rf internal/site/assets/theme/hextra; \
	mkdir -p internal/site/assets/theme/hextra; \
	rsync -a --exclude='.git' --exclude='exampleSite' --exclude='node_modules' "$$TMP/hextra/" internal/site/assets/theme/hextra/; \
	echo "$$TAG" > internal/site/assets/theme/hextra/VERSION; \
	rm -rf "$$TMP"
```

**Step 4: Commit**

```bash
git add internal/site/assets/theme/hextra Makefile
git commit -m "chore(site): vendor Hextra theme @ <TAG>"
```

---

## Phase 1 — Pure Helpers

### Task 1.1: featureSlug helper (pure function)

**Files:**
- Create: `internal/site/slug.go`
- Create: `internal/site/slug_test.go`

**Step 1: Write the failing tests**

```go
// internal/site/slug_test.go
package site

import "testing"

func TestFeatureSlug(t *testing.T) {
	cases := []struct{ in, want string }{
		{"Simple Name", "simple-name"},
		{"  Trim Edges  ", "trim-edges"},
		{"Mixed CASE", "mixed-case"},
		{"Multiple   Spaces", "multiple-spaces"},
		{"Punctuation!?, here.", "punctuation-here"},
		{"Hyphen--Run", "hyphen-run"},
		{"unicode café", "unicode-cafe"},
		{"123 numbers ok", "123-numbers-ok"},
		{"", ""},
		{"!!!", ""},
	}
	for _, c := range cases {
		got := featureSlug(c.in)
		if got != c.want {
			t.Errorf("featureSlug(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/site/ -run TestFeatureSlug -v`
Expected: FAIL — `undefined: featureSlug`

**Step 3: Write minimal implementation**

```go
// internal/site/slug.go
package site

import (
	"strings"
	"unicode"

	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
)

// featureSlug converts a human-readable feature name into a deterministic
// kebab-case identifier suitable for a URL path segment.
// Empty or all-non-alphanumeric inputs return "".
func featureSlug(s string) string {
	t := transform.Chain(norm.NFD, runes.Remove(runes.In(unicode.Mn)), norm.NFC)
	stripped, _, _ := transform.String(t, s)

	var b strings.Builder
	prevDash := true
	for _, r := range stripped {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(unicode.ToLower(r))
			prevDash = false
		default:
			if !prevDash {
				b.WriteByte('-')
				prevDash = true
			}
		}
	}
	return strings.Trim(b.String(), "-")
}
```

**Step 4: Add the dependency**

```bash
go get golang.org/x/text@latest
go mod tidy
```

**Step 5: Run test to verify it passes**

Run: `go test ./internal/site/ -run TestFeatureSlug -v`
Expected: PASS

**Step 6: Commit**

```bash
git add internal/site/slug.go internal/site/slug_test.go go.mod go.sum
git commit -m "feat(site): add featureSlug helper for deterministic URL slugs

- RED: TestFeatureSlug covers casing, unicode, punctuation, empty input
- GREEN: NFD-normalize, strip combining marks, lowercase, kebab-case"
```

---

### Task 1.2: Slug collision resolver

**Files:**
- Modify: `internal/site/slug.go` (add `resolveSlugs`)
- Modify: `internal/site/slug_test.go`

**Step 1: Write the failing test**

```go
// add to slug_test.go
func TestResolveSlugs(t *testing.T) {
	in := []string{"Foo", "foo", "Bar", "FOO", "foo!"}
	got := resolveSlugs(in)
	want := []string{"foo", "foo-2", "bar", "foo-3", "foo-4"}
	for i := range in {
		if got[in[i]] != want[i] {
			t.Errorf("resolveSlugs[%d] %q = %q, want %q", i, in[i], got[in[i]], want[i])
		}
	}
}

func TestResolveSlugsDeterministic(t *testing.T) {
	// Same inputs in same order must produce same output.
	a := resolveSlugs([]string{"Alpha", "alpha", "ALPHA"})
	b := resolveSlugs([]string{"Alpha", "alpha", "ALPHA"})
	for k, v := range a {
		if b[k] != v {
			t.Errorf("non-deterministic: %q got %q vs %q", k, v, b[k])
		}
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/site/ -run TestResolveSlugs -v`
Expected: FAIL — `undefined: resolveSlugs`

**Step 3: Write minimal implementation**

```go
// add to slug.go

// resolveSlugs returns a name → slug map for the given names. Collisions are
// resolved by appending -2, -3, ... in input order so that the first appearance
// of a slug keeps the unsuffixed form. Names that produce empty slugs map to
// the empty string and are not deduplicated.
func resolveSlugs(names []string) map[string]string {
	out := make(map[string]string, len(names))
	used := make(map[string]int)
	for _, n := range names {
		base := featureSlug(n)
		if base == "" {
			out[n] = ""
			continue
		}
		count := used[base]
		used[base] = count + 1
		if count == 0 {
			out[n] = base
		} else {
			out[n] = base + "-" + itoa(count+1)
		}
	}
	return out
}

func itoa(i int) string {
	const digits = "0123456789"
	if i == 0 {
		return "0"
	}
	var b [20]byte
	pos := len(b)
	for i > 0 {
		pos--
		b[pos] = digits[i%10]
		i /= 10
	}
	return string(b[pos:])
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/site/ -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/site/slug.go internal/site/slug_test.go
git commit -m "feat(site): add deterministic slug collision resolver

- RED: TestResolveSlugs covers case-fold collisions and order stability
- GREEN: input-order traversal, append -2/-3/... for duplicates"
```

---

## Phase 2 — Public Types

### Task 2.1: Public types (Mode, BuildOptions, Inputs)

**Files:**
- Create: `internal/site/site.go` (types only; Build() is a stub)
- Create: `internal/site/site_test.go`

**Step 1: Write the failing test**

```go
// internal/site/site_test.go
package site

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestBuildRejectsUnknownMode(t *testing.T) {
	err := Build(context.Background(), Inputs{}, BuildOptions{Mode: Mode(99)})
	if err == nil {
		t.Fatal("expected error for unknown mode")
	}
	if !errors.Is(err, ErrUnknownMode) {
		t.Errorf("expected ErrUnknownMode, got %v", err)
	}
}

func TestBuildRejectsEmptyProjectDir(t *testing.T) {
	err := Build(context.Background(), Inputs{}, BuildOptions{
		ProjectDir:  "",
		ProjectName: "x",
		Mode:        ModeMirror,
		GeneratedAt: time.Now(),
	})
	if err == nil {
		t.Fatal("expected error for empty ProjectDir")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/site/ -v`
Expected: FAIL — `undefined: Build`

**Step 3: Write minimal implementation**

```go
// internal/site/site.go
package site

import (
	"context"
	"errors"
	"time"

	"github.com/sandgardenhq/find-the-gaps/internal/analyzer"
)

// Mode selects the site's content shape.
type Mode int

const (
	ModeMirror Mode = iota
	ModeExpanded
)

// BuildOptions controls how Build() materializes and renders the site.
type BuildOptions struct {
	ProjectDir  string
	ProjectName string
	KeepSource  bool
	Mode        Mode
	GeneratedAt time.Time
}

// Inputs is the analyzer-side data Build() consumes. It mirrors what the
// reporter package consumes; the two are independent.
type Inputs struct {
	Summary        analyzer.ProductSummary
	Mapping        analyzer.FeatureMap
	DocsMap        analyzer.DocsFeatureMap
	AllDocFeatures []string
	Drift          []analyzer.DriftFinding
	Screenshots    []analyzer.ScreenshotGap
	ScreenshotsRan bool
}

// ErrUnknownMode is returned by Build when opts.Mode is not a recognized value.
var ErrUnknownMode = errors.New("unknown site mode")

// Build materializes a Hugo source tree and shells out to `hugo` to produce
// the static site at <opts.ProjectDir>/site/.
func Build(ctx context.Context, in Inputs, opts BuildOptions) error {
	if opts.Mode != ModeMirror && opts.Mode != ModeExpanded {
		return ErrUnknownMode
	}
	if opts.ProjectDir == "" {
		return errors.New("ProjectDir is required")
	}
	return errors.New("not implemented")
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/site/ -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/site/site.go internal/site/site_test.go
git commit -m "feat(site): public types and Build() stub with input validation

- RED: TestBuildRejectsUnknownMode + TestBuildRejectsEmptyProjectDir
- GREEN: Mode constants, Inputs/BuildOptions structs, ErrUnknownMode"
```

---

## Phase 3 — Embedded Templates

### Task 3.1: Embed all asset trees

**Files:**
- Create: `internal/site/assets.go`
- Create: `internal/site/assets/templates/.gitkeep` (empty templates dir for now)

**Step 1: Write the failing test**

```go
// internal/site/assets_test.go
package site

import (
	"io/fs"
	"testing"
)

func TestThemeFSContainsHugoTomlSchema(t *testing.T) {
	// Hextra ships theme.toml at the theme root.
	_, err := fs.Stat(themeFS, "assets/theme/hextra/theme.toml")
	if err != nil {
		t.Fatalf("themeFS missing theme.toml: %v", err)
	}
}

func TestTemplatesFSExists(t *testing.T) {
	entries, err := fs.ReadDir(templatesFS, "assets/templates")
	if err != nil {
		t.Fatalf("templatesFS read: %v", err)
	}
	_ = entries // length checked in later tasks
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/site/ -v -run TestThemeFS`
Expected: FAIL — `undefined: themeFS`

**Step 3: Write minimal implementation**

```go
// internal/site/assets.go
package site

import "embed"

//go:embed assets/theme/hextra
var themeFS embed.FS

//go:embed assets/templates
var templatesFS embed.FS
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/site/ -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/site/assets.go internal/site/assets_test.go internal/site/assets/templates/.gitkeep
git commit -m "feat(site): embed Hextra theme and template directory

- RED: assert theme.toml present in themeFS, templatesFS readable
- GREEN: //go:embed both trees"
```

---

### Task 3.2: hugo.toml.tmpl renders for mirror mode

**Files:**
- Create: `internal/site/assets/templates/hugo.toml.tmpl`
- Create: `internal/site/templates.go`
- Create: `internal/site/templates_test.go`

**Step 1: Write the failing test**

```go
// internal/site/templates_test.go
package site

import (
	"strings"
	"testing"
	"time"
)

func TestRenderHugoConfigMirror(t *testing.T) {
	got, err := renderHugoConfig(hugoConfigData{
		Title:          "Find the Gaps — myrepo",
		Mode:           ModeMirror,
		ScreenshotsRan: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`baseURL = "/"`,
		`theme = "hextra"`,
		`title = "Find the Gaps — myrepo"`,
		`[[menu.main]]`,
		`name = "Mapping"`,
		`name = "Gaps"`,
		`name = "Screenshots"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	if strings.Contains(got, "[taxonomies]") {
		t.Error("mirror mode must not declare taxonomies")
	}
}

func TestRenderHugoConfigOmitsScreenshotsWhenNotRan(t *testing.T) {
	got, err := renderHugoConfig(hugoConfigData{
		Title:          "x",
		Mode:           ModeMirror,
		ScreenshotsRan: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got, `name = "Screenshots"`) {
		t.Errorf("Screenshots menu should be omitted; got:\n%s", got)
	}
}

func TestRenderHugoConfigExpandedHasTaxonomies(t *testing.T) {
	got, err := renderHugoConfig(hugoConfigData{
		Title:          "x",
		Mode:           ModeExpanded,
		ScreenshotsRan: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`[taxonomies]`,
		`layer        = "layers"`,
		`status       = "statuses"`,
		`user_facing  = "user_facing"`,
		`name = "Features"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	if strings.Contains(got, `name = "Mapping"`) {
		t.Error("expanded mode should use Features, not Mapping")
	}
}

// suppress unused warning while we wire things up
var _ = time.Now
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/site/ -v -run TestRenderHugoConfig`
Expected: FAIL — `undefined: renderHugoConfig`

**Step 3: Write the template**

```toml
# internal/site/assets/templates/hugo.toml.tmpl
baseURL = "/"
languageCode = "en-us"
title = {{ .Title | printf "%q" }}
theme = "hextra"

[markup.goldmark.renderer]
unsafe = true

{{- if eq .Mode 1 }}
[taxonomies]
layer        = "layers"
status       = "statuses"
user_facing  = "user_facing"
{{- end }}

{{- if eq .Mode 0 }}
[[menu.main]]
name   = "Mapping"
url    = "/mapping/"
weight = 10

[[menu.main]]
name   = "Gaps"
url    = "/gaps/"
weight = 20

{{- if .ScreenshotsRan }}
[[menu.main]]
name   = "Screenshots"
url    = "/screenshots/"
weight = 30
{{- end }}
{{- else }}
[[menu.main]]
name   = "Features"
url    = "/features/"
weight = 10

[[menu.main]]
name   = "Gaps"
url    = "/gaps/"
weight = 20

{{- if .ScreenshotsRan }}
[[menu.main]]
name   = "Screenshots"
url    = "/screenshots/"
weight = 30
{{- end }}
{{- end }}
```

**Step 4: Write the renderer**

```go
// internal/site/templates.go
package site

import (
	"bytes"
	"fmt"
	"io/fs"
	"text/template"
)

type hugoConfigData struct {
	Title          string
	Mode           Mode
	ScreenshotsRan bool
}

var tmpl = template.Must(parseTemplates(templatesFS))

func parseTemplates(efs fs.FS) (*template.Template, error) {
	return template.New("site").Funcs(template.FuncMap{
		// add helpers here as needed
	}).ParseFS(efs, "assets/templates/*.tmpl")
}

func renderHugoConfig(data hugoConfigData) (string, error) {
	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, "hugo.toml.tmpl", data); err != nil {
		return "", fmt.Errorf("render hugo.toml: %w", err)
	}
	return buf.String(), nil
}
```

Note: `template.Must(parseTemplates(...))` parses at package init. Since `parseTemplates` returns `(*template.Template, error)`, wrap it:

```go
var tmpl = func() *template.Template {
	t, err := parseTemplates(templatesFS)
	if err != nil {
		panic(fmt.Sprintf("parse embedded templates: %v", err))
	}
	return t
}()
```

**Step 5: Run test to verify it passes**

Run: `go test ./internal/site/ -v`
Expected: PASS

**Step 6: Commit**

```bash
git add internal/site/assets/templates/hugo.toml.tmpl internal/site/templates.go internal/site/templates_test.go
git commit -m "feat(site): hugo.toml template with mirror/expanded branches

- RED: 3 tests for menu items, taxonomies, conditional screenshots
- GREEN: text/template + embedded .tmpl files, mode-conditional output"
```

---

### Task 3.3: home.md.tmpl (synthesized dashboard)

**Files:**
- Create: `internal/site/assets/templates/home.md.tmpl`
- Modify: `internal/site/templates.go` (add `renderHome`)
- Modify: `internal/site/templates_test.go`

**Step 1: Write the failing test**

```go
// add to templates_test.go
func TestRenderHomeIncludesCounts(t *testing.T) {
	in := homeData{
		ProjectName:           "demo",
		GeneratedAt:           time.Date(2026, 4, 24, 10, 0, 0, 0, time.UTC),
		Summary:               "A small CLI demo.",
		FeatureCount:          17,
		UndocumentedUserCount: 4,
		DriftCount:            2,
		ScreenshotGapCount:    3,
		ScreenshotsRan:        true,
		Mode:                  ModeMirror,
	}
	got, err := renderHome(in)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"# demo",
		"A small CLI demo.",
		"17 features",
		"4 undocumented (user-facing)",
		"2 drift findings",
		"3 missing screenshots",
		"2026-04-24",
		"[Mapping](/mapping/)",
		"[Gaps](/gaps/)",
		"[Screenshots](/screenshots/)",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

func TestRenderHomeOmitsScreenshotsWhenNotRan(t *testing.T) {
	in := homeData{
		ProjectName:    "demo",
		GeneratedAt:    time.Now(),
		ScreenshotsRan: false,
		Mode:           ModeMirror,
	}
	got, err := renderHome(in)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got, "/screenshots/") {
		t.Errorf("home should not link to screenshots when not ran:\n%s", got)
	}
}

func TestRenderHomeExpandedLinksToFeatures(t *testing.T) {
	in := homeData{
		ProjectName: "demo",
		GeneratedAt: time.Now(),
		Mode:        ModeExpanded,
	}
	got, err := renderHome(in)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "[Features](/features/)") {
		t.Errorf("expanded home should link to /features/, got:\n%s", got)
	}
	if strings.Contains(got, "[Mapping](/mapping/)") {
		t.Error("expanded home should not link to mapping")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/site/ -v -run TestRenderHome`
Expected: FAIL — `undefined: renderHome`

**Step 3: Write the template**

```markdown
# internal/site/assets/templates/home.md.tmpl
+++
title = "{{ .ProjectName }}"
type = "docs"
+++

# {{ .ProjectName }}

{{ if .Summary }}{{ .Summary }}{{ end }}

_Generated {{ .GeneratedAt.UTC.Format "2006-01-02 15:04 UTC" }}_

## At a glance

- **{{ .FeatureCount }} features**
- **{{ .UndocumentedUserCount }} undocumented (user-facing)**
- **{{ .DriftCount }} drift findings**
{{- if .ScreenshotsRan }}
- **{{ .ScreenshotGapCount }} missing screenshots**
{{- end }}

## Sections

{{ if eq .Mode 1 -}}
- [Features](/features/)
{{- else -}}
- [Mapping](/mapping/)
{{- end }}
- [Gaps](/gaps/)
{{- if .ScreenshotsRan }}
- [Screenshots](/screenshots/)
{{- end }}
```

**Step 4: Add the renderer**

```go
// in templates.go
type homeData struct {
	ProjectName           string
	GeneratedAt           time.Time
	Summary               string
	FeatureCount          int
	UndocumentedUserCount int
	DriftCount            int
	ScreenshotGapCount    int
	ScreenshotsRan        bool
	Mode                  Mode
}

func renderHome(d homeData) (string, error) {
	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, "home.md.tmpl", d); err != nil {
		return "", fmt.Errorf("render home: %w", err)
	}
	return buf.String(), nil
}
```

Add `import "time"` to templates.go.

**Step 5: Run test to verify it passes**

Run: `go test ./internal/site/ -v`
Expected: PASS

**Step 6: Commit**

```bash
git add internal/site/assets/templates/home.md.tmpl internal/site/templates.go internal/site/templates_test.go
git commit -m "feat(site): home.md template with counts and mode-aware links

- RED: 3 tests for headline counts, conditional screenshots, expanded links
- GREEN: home.md.tmpl + renderHome()"
```

---

### Task 3.4: feature.md.tmpl (per-feature page, expanded mode)

**Files:**
- Create: `internal/site/assets/templates/feature.md.tmpl`
- Modify: `internal/site/templates.go` (add `renderFeature`)
- Modify: `internal/site/templates_test.go`

**Step 1: Write the failing test**

```go
// add to templates_test.go
func TestRenderFeatureFull(t *testing.T) {
	in := featureData{
		Slug:        "user-auth",
		Name:        "User Auth",
		Description: "Login and session management.",
		Layer:       "service",
		UserFacing:  true,
		Documented:  true,
		Files:       []string{"internal/auth/login.go", "internal/auth/session.go"},
		Symbols:     []string{"Login", "Logout"},
		DocURLs:     []string{"https://example.com/docs/auth"},
		Drift: []driftIssue{
			{Page: "https://example.com/docs/auth", Issue: "old signature"},
		},
	}
	got, err := renderFeature(in)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`+++`,
		`title = "User Auth"`,
		`tags = ["layer:service", "status:documented", "user-facing:yes"]`,
		"# User Auth",
		"Login and session management.",
		"`internal/auth/login.go`",
		"`internal/auth/session.go`",
		"`Login`",
		"`Logout`",
		"[https://example.com/docs/auth](https://example.com/docs/auth)",
		"old signature",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

func TestRenderFeatureUndocumentedCallout(t *testing.T) {
	got, err := renderFeature(featureData{
		Slug:       "x",
		Name:       "X",
		Documented: false,
		UserFacing: true,
		Layer:      "ui",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, `tags = ["layer:ui", "status:undocumented", "user-facing:yes"]`) {
		t.Errorf("undocumented + user-facing tag missing:\n%s", got)
	}
	if !strings.Contains(got, "**Undocumented**") {
		t.Errorf("expected callout, got:\n%s", got)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/site/ -v -run TestRenderFeature`
Expected: FAIL — `undefined: renderFeature, featureData, driftIssue`

**Step 3: Write the template**

```markdown
# internal/site/assets/templates/feature.md.tmpl
+++
title = {{ .Name | printf "%q" }}
{{- $status := "undocumented" }}{{ if .Documented }}{{ $status = "documented" }}{{ end -}}
{{- $uf := "no" }}{{ if .UserFacing }}{{ $uf = "yes" }}{{ end -}}
tags = ["layer:{{ .Layer }}", "status:{{ $status }}", "user-facing:{{ $uf }}"]
+++

# {{ .Name }}

{{ if .Description }}{{ .Description }}{{ end }}

{{ if not .Documented }}> **Undocumented** — this feature has code but no documentation page.{{ end }}

- **Layer:** {{ .Layer }}
- **User-facing:** {{ if .UserFacing }}yes{{ else }}no{{ end }}
- **Documentation status:** {{ if .Documented }}documented{{ else }}undocumented{{ end }}

{{ if .Files }}
## Implemented in
{{ range .Files }}- `{{ . }}`
{{ end }}{{ end }}

{{ if .Symbols }}
## Symbols
{{ range .Symbols }}- `{{ . }}`
{{ end }}{{ end }}

{{ if .DocURLs }}
## Documented on
{{ range .DocURLs }}- [{{ . }}]({{ . }})
{{ end }}{{ end }}

{{ if .Drift }}
## Drift findings
{{ range .Drift }}- {{ if .Page }}[{{ .Page }}]({{ .Page }}) — {{ end }}{{ .Issue }}
{{ end }}{{ end }}
```

**Step 4: Add the renderer**

```go
// in templates.go
type driftIssue struct {
	Page  string
	Issue string
}

type featureData struct {
	Slug        string
	Name        string
	Description string
	Layer       string
	UserFacing  bool
	Documented  bool
	Files       []string
	Symbols     []string
	DocURLs     []string
	Drift       []driftIssue
}

func renderFeature(d featureData) (string, error) {
	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, "feature.md.tmpl", d); err != nil {
		return "", fmt.Errorf("render feature: %w", err)
	}
	return buf.String(), nil
}
```

**Step 5: Run test to verify it passes**

Run: `go test ./internal/site/ -v`
Expected: PASS

**Step 6: Commit**

```bash
git add internal/site/assets/templates/feature.md.tmpl internal/site/templates.go internal/site/templates_test.go
git commit -m "feat(site): per-feature page template for expanded mode

- RED: TestRenderFeatureFull + TestRenderFeatureUndocumentedCallout
- GREEN: feature.md.tmpl with frontmatter tags, files/symbols/docs/drift sections"
```

---

### Task 3.5: features_index.md.tmpl (sortable feature table)

**Files:**
- Create: `internal/site/assets/templates/features_index.md.tmpl`
- Modify: `internal/site/templates.go` (add `renderFeaturesIndex`)
- Modify: `internal/site/templates_test.go`

**Step 1: Write the failing test**

```go
// add to templates_test.go
func TestRenderFeaturesIndex(t *testing.T) {
	got, err := renderFeaturesIndex(featuresIndexData{
		Rows: []featureRow{
			{Slug: "alpha", Name: "Alpha", Layer: "ui", UserFacing: true, Documented: true, FileCount: 2, DriftCount: 0},
			{Slug: "beta", Name: "Beta", Layer: "service", UserFacing: false, Documented: false, FileCount: 5, DriftCount: 3},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"<table>",
		"<th>Name</th>",
		"<th>Layer</th>",
		"<th>User-facing</th>",
		"<th>Doc status</th>",
		`<a href="/features/alpha/">Alpha</a>`,
		`<a href="/features/beta/">Beta</a>`,
		"<td>ui</td>",
		"<td>service</td>",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/site/ -v -run TestRenderFeaturesIndex`
Expected: FAIL.

**Step 3: Write the template**

```markdown
# internal/site/assets/templates/features_index.md.tmpl
+++
title = "Features"
+++

# Features

<table>
  <thead>
    <tr>
      <th>Name</th>
      <th>Layer</th>
      <th>User-facing</th>
      <th>Doc status</th>
      <th>Files</th>
      <th>Drift</th>
    </tr>
  </thead>
  <tbody>
{{ range .Rows }}    <tr>
      <td><a href="/features/{{ .Slug }}/">{{ .Name }}</a></td>
      <td>{{ .Layer }}</td>
      <td>{{ if .UserFacing }}yes{{ else }}no{{ end }}</td>
      <td>{{ if .Documented }}documented{{ else }}undocumented{{ end }}</td>
      <td>{{ .FileCount }}</td>
      <td>{{ .DriftCount }}</td>
    </tr>
{{ end }}  </tbody>
</table>
```

**Step 4: Add the renderer**

```go
// in templates.go
type featureRow struct {
	Slug       string
	Name       string
	Layer      string
	UserFacing bool
	Documented bool
	FileCount  int
	DriftCount int
}

type featuresIndexData struct {
	Rows []featureRow
}

func renderFeaturesIndex(d featuresIndexData) (string, error) {
	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, "features_index.md.tmpl", d); err != nil {
		return "", fmt.Errorf("render features_index: %w", err)
	}
	return buf.String(), nil
}
```

**Step 5: Run test to verify it passes**

Run: `go test ./internal/site/ -v`
Expected: PASS

**Step 6: Commit**

```bash
git add internal/site/assets/templates/features_index.md.tmpl internal/site/templates.go internal/site/templates_test.go
git commit -m "feat(site): features index template (HTML table for expanded mode)

- RED: TestRenderFeaturesIndex asserts table headers and per-row links
- GREEN: features_index.md.tmpl with <table> + range rows"
```

---

### Task 3.6: screenshot_page.md.tmpl (per-docs-page detail, expanded)

**Files:**
- Create: `internal/site/assets/templates/screenshot_page.md.tmpl`
- Modify: `internal/site/templates.go` (add `renderScreenshotPage`)
- Modify: `internal/site/templates_test.go`

**Step 1: Write the failing test**

```go
// add to templates_test.go
func TestRenderScreenshotPage(t *testing.T) {
	got, err := renderScreenshotPage(screenshotPageData{
		PageURL: "https://example.com/docs/start",
		Title:   "Quickstart",
		Gaps: []screenshotGap{
			{Quoted: "open the dashboard", ShouldShow: "the dashboard view", Alt: "dashboard", Insert: "after first paragraph"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`title = "Quickstart"`,
		"# Quickstart",
		"https://example.com/docs/start",
		"open the dashboard",
		"the dashboard view",
		"after first paragraph",
		"**Alt text:**",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/site/ -v -run TestRenderScreenshotPage`
Expected: FAIL.

**Step 3: Write the template**

```markdown
# internal/site/assets/templates/screenshot_page.md.tmpl
+++
title = {{ .Title | printf "%q" }}
+++

# {{ .Title }}

[{{ .PageURL }}]({{ .PageURL }})

{{ range .Gaps }}- **Passage:** {{ printf "%q" .Quoted }}
  - **Screenshot should show:** {{ .ShouldShow }}
  - **Alt text:** {{ .Alt }}
  - **Insert:** {{ .Insert }}

{{ end }}
```

**Step 4: Add the renderer**

```go
// in templates.go
type screenshotGap struct {
	Quoted     string
	ShouldShow string
	Alt        string
	Insert     string
}

type screenshotPageData struct {
	PageURL string
	Title   string
	Gaps    []screenshotGap
}

func renderScreenshotPage(d screenshotPageData) (string, error) {
	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, "screenshot_page.md.tmpl", d); err != nil {
		return "", fmt.Errorf("render screenshot_page: %w", err)
	}
	return buf.String(), nil
}
```

**Step 5: Run test to verify it passes**

Run: `go test ./internal/site/ -v`
Expected: PASS

**Step 6: Commit**

```bash
git add internal/site/assets/templates/screenshot_page.md.tmpl internal/site/templates.go internal/site/templates_test.go
git commit -m "feat(site): per-docs-page screenshot template

- RED: TestRenderScreenshotPage asserts URL, alt-text, insertion hint
- GREEN: screenshot_page.md.tmpl + renderScreenshotPage()"
```

---

## Phase 4 — Materialize

### Task 4.1: materialize() — mirror mode, no screenshots

**Files:**
- Create: `internal/site/materialize.go`
- Create: `internal/site/materialize_test.go`

**Step 1: Write the failing test**

```go
// internal/site/materialize_test.go
package site

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sandgardenhq/find-the-gaps/internal/analyzer"
)

func TestMaterializeMirrorWritesExpectedTree(t *testing.T) {
	srcDir := t.TempDir()
	projectDir := t.TempDir()

	// Pre-write the user-facing markdown the way the reporter would.
	for _, name := range []string{"mapping.md", "gaps.md"} {
		if err := os.WriteFile(filepath.Join(projectDir, name),
			[]byte("# "+name+"\n\nbody\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	in := Inputs{
		Summary: analyzer.ProductSummary{Description: "demo"},
		Mapping: analyzer.FeatureMap{},
	}
	opts := BuildOptions{
		ProjectDir:  projectDir,
		ProjectName: "demo",
		Mode:        ModeMirror,
		GeneratedAt: time.Date(2026, 4, 24, 0, 0, 0, 0, time.UTC),
	}

	if err := materialize(srcDir, in, opts); err != nil {
		t.Fatal(err)
	}

	// hugo.toml present and contains "Mapping"
	cfg, err := os.ReadFile(filepath.Join(srcDir, "hugo.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if !contains(string(cfg), `name = "Mapping"`) {
		t.Errorf("hugo.toml missing Mapping menu:\n%s", cfg)
	}

	// content/_index.md present
	if _, err := os.Stat(filepath.Join(srcDir, "content", "_index.md")); err != nil {
		t.Error(err)
	}
	// content/mapping.md present and starts with frontmatter
	mapping, err := os.ReadFile(filepath.Join(srcDir, "content", "mapping.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !contains(string(mapping), `title = "Mapping"`) {
		t.Errorf("mapping.md missing title frontmatter:\n%s", mapping)
	}
	// theme present
	if _, err := os.Stat(filepath.Join(srcDir, "themes", "hextra", "theme.toml")); err != nil {
		t.Error(err)
	}
	// screenshots NOT present (ScreenshotsRan = false in this test)
	if _, err := os.Stat(filepath.Join(srcDir, "content", "screenshots.md")); !os.IsNotExist(err) {
		t.Errorf("screenshots.md should not exist when ScreenshotsRan=false; err=%v", err)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || (len(sub) > 0 && (indexOf(s, sub) >= 0)))
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/site/ -v -run TestMaterialize`
Expected: FAIL — `undefined: materialize`.

**Step 3: Write minimal implementation**

```go
// internal/site/materialize.go
package site

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// materialize writes a Hugo source tree into srcDir based on the inputs and options.
// srcDir must exist and be empty.
func materialize(srcDir string, in Inputs, opts BuildOptions) error {
	// 1. theme
	if err := extractEmbedFS(themeFS, "assets/theme/hextra", filepath.Join(srcDir, "themes", "hextra")); err != nil {
		return fmt.Errorf("extract theme: %w", err)
	}

	// 2. hugo.toml
	cfg, err := renderHugoConfig(hugoConfigData{
		Title:          "Find the Gaps — " + opts.ProjectName,
		Mode:           opts.Mode,
		ScreenshotsRan: in.ScreenshotsRan,
	})
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(srcDir, "hugo.toml"), []byte(cfg), 0o644); err != nil {
		return err
	}

	// 3. content/_index.md (home)
	contentDir := filepath.Join(srcDir, "content")
	if err := os.MkdirAll(contentDir, 0o755); err != nil {
		return err
	}
	home, err := renderHome(buildHomeData(in, opts))
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(contentDir, "_index.md"), []byte(home), 0o644); err != nil {
		return err
	}

	// 4. mode-specific content
	switch opts.Mode {
	case ModeMirror:
		if err := materializeMirror(srcDir, contentDir, in, opts); err != nil {
			return err
		}
	case ModeExpanded:
		// Phase 4 follow-up tasks add this branch.
		return fmt.Errorf("expanded mode not yet wired in materialize")
	}
	return nil
}

func materializeMirror(srcDir, contentDir string, in Inputs, opts BuildOptions) error {
	type sec struct {
		src, dst, title string
		weight          int
	}
	secs := []sec{
		{"mapping.md", "mapping.md", "Mapping", 10},
		{"gaps.md", "gaps.md", "Gaps", 20},
	}
	if in.ScreenshotsRan {
		secs = append(secs, sec{"screenshots.md", "screenshots.md", "Screenshots", 30})
	}
	for _, s := range secs {
		body, err := os.ReadFile(filepath.Join(opts.ProjectDir, s.src))
		if err != nil {
			return fmt.Errorf("read %s: %w", s.src, err)
		}
		fm := fmt.Sprintf("+++\ntitle = %q\nweight = %d\n+++\n\n", s.title, s.weight)
		if err := os.WriteFile(filepath.Join(contentDir, s.dst), []byte(fm+string(body)), 0o644); err != nil {
			return err
		}
	}
	return nil
}

func buildHomeData(in Inputs, opts BuildOptions) homeData {
	undoc := 0
	docFeatures := map[string]bool{}
	for _, f := range in.AllDocFeatures {
		docFeatures[f] = true
	}
	for _, e := range in.Mapping {
		if len(e.Files) > 0 && !docFeatures[e.Feature.Name] && e.Feature.UserFacing {
			undoc++
		}
	}
	return homeData{
		ProjectName:           opts.ProjectName,
		GeneratedAt:           opts.GeneratedAt,
		Summary:               in.Summary.Description,
		FeatureCount:          len(in.Mapping),
		UndocumentedUserCount: undoc,
		DriftCount:            len(in.Drift),
		ScreenshotGapCount:    len(in.Screenshots),
		ScreenshotsRan:        in.ScreenshotsRan,
		Mode:                  opts.Mode,
	}
}

// extractEmbedFS copies an embedded subtree to a destination directory on disk.
func extractEmbedFS(efs fs.FS, root, dst string) error {
	return fs.WalkDir(efs, root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel := strings.TrimPrefix(p, root)
		rel = strings.TrimPrefix(rel, "/")
		out := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(out, 0o755)
		}
		data, err := fs.ReadFile(efs, p)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
			return err
		}
		return os.WriteFile(out, data, 0o644)
	})
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/site/ -v -run TestMaterialize`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/site/materialize.go internal/site/materialize_test.go
git commit -m "feat(site): materialize() writes mirror-mode Hugo source tree

- RED: TestMaterializeMirrorWritesExpectedTree asserts theme, config, content
- GREEN: extract embedded theme, render hugo.toml + home + section copies"
```

---

### Task 4.2: materialize() — mirror mode, with screenshots

**Files:**
- Modify: `internal/site/materialize_test.go`

**Step 1: Write the failing test**

```go
// add to materialize_test.go
func TestMaterializeMirrorWithScreenshots(t *testing.T) {
	srcDir := t.TempDir()
	projectDir := t.TempDir()
	for _, name := range []string{"mapping.md", "gaps.md", "screenshots.md"} {
		_ = os.WriteFile(filepath.Join(projectDir, name), []byte("# "+name+"\n"), 0o644)
	}

	err := materialize(srcDir, Inputs{ScreenshotsRan: true}, BuildOptions{
		ProjectDir:  projectDir,
		ProjectName: "demo",
		Mode:        ModeMirror,
		GeneratedAt: time.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(srcDir, "content", "screenshots.md")); err != nil {
		t.Errorf("screenshots.md should exist: %v", err)
	}
	cfg, _ := os.ReadFile(filepath.Join(srcDir, "hugo.toml"))
	if !contains(string(cfg), `name = "Screenshots"`) {
		t.Errorf("hugo.toml should declare Screenshots menu:\n%s", cfg)
	}
}
```

**Step 2: Run test to verify it passes**

(Already covered by Task 4.1 implementation; this test validates the conditional.)
Run: `go test ./internal/site/ -v -run TestMaterialize`
Expected: PASS

**Step 3: Commit**

```bash
git add internal/site/materialize_test.go
git commit -m "test(site): cover materialize() with ScreenshotsRan=true"
```

---

### Task 4.3: materialize() — expanded mode

**Files:**
- Modify: `internal/site/materialize.go`
- Modify: `internal/site/materialize_test.go`

**Step 1: Write the failing test**

```go
// add to materialize_test.go
func TestMaterializeExpandedWritesFeaturePages(t *testing.T) {
	srcDir := t.TempDir()
	projectDir := t.TempDir()
	for _, name := range []string{"mapping.md", "gaps.md"} {
		_ = os.WriteFile(filepath.Join(projectDir, name), []byte("# "+name+"\n"), 0o644)
	}

	in := Inputs{
		Mapping: analyzer.FeatureMap{
			{
				Feature: analyzer.CodeFeature{Name: "User Auth", Layer: "service", UserFacing: true},
				Files:   []string{"internal/auth/login.go"},
				Symbols: []string{"Login"},
			},
			{
				Feature: analyzer.CodeFeature{Name: "user auth", Layer: "ui", UserFacing: true},
				Files:   []string{"web/auth.tsx"},
			},
		},
		AllDocFeatures: []string{"User Auth"},
	}
	err := materialize(srcDir, in, BuildOptions{
		ProjectDir:  projectDir,
		ProjectName: "demo",
		Mode:        ModeExpanded,
		GeneratedAt: time.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}

	// Features index
	if _, err := os.Stat(filepath.Join(srcDir, "content", "features", "_index.md")); err != nil {
		t.Errorf("features/_index.md missing: %v", err)
	}
	// Per-feature pages with collision-resolved slugs
	if _, err := os.Stat(filepath.Join(srcDir, "content", "features", "user-auth.md")); err != nil {
		t.Errorf("user-auth.md missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(srcDir, "content", "features", "user-auth-2.md")); err != nil {
		t.Errorf("user-auth-2.md missing (collision): %v", err)
	}
	// Gaps page (linked feature names rendered into gaps.md content dir)
	gaps, err := os.ReadFile(filepath.Join(srcDir, "content", "gaps.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !contains(string(gaps), "/features/user-auth/") {
		t.Errorf("gaps.md should hyperlink feature names:\n%s", gaps)
	}
	// hugo.toml taxonomies
	cfg, _ := os.ReadFile(filepath.Join(srcDir, "hugo.toml"))
	if !contains(string(cfg), "[taxonomies]") {
		t.Errorf("hugo.toml missing taxonomies:\n%s", cfg)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/site/ -v -run TestMaterializeExpanded`
Expected: FAIL — error from `materializeMirror` because branch returns "not yet wired".

**Step 3: Write the expanded branch**

Replace the `case ModeExpanded:` branch in `materialize()` with a real implementation:

```go
// in materialize.go — replace ModeExpanded branch with:
case ModeExpanded:
    if err := materializeExpanded(srcDir, contentDir, in, opts); err != nil {
        return err
    }
```

```go
// add to materialize.go
func materializeExpanded(srcDir, contentDir string, in Inputs, opts BuildOptions) error {
	// Resolve slugs first; subsequent renders use the same map.
	names := make([]string, 0, len(in.Mapping))
	for _, e := range in.Mapping {
		names = append(names, e.Feature.Name)
	}
	slugs := resolveSlugs(names)

	// docFeatures set for documented status.
	docFeatures := map[string]bool{}
	for _, f := range in.AllDocFeatures {
		docFeatures[f] = true
	}

	// driftByFeature for embedded drift sections.
	driftByFeature := map[string][]driftIssue{}
	for _, d := range in.Drift {
		for _, i := range d.Issues {
			driftByFeature[d.Feature] = append(driftByFeature[d.Feature], driftIssue{Page: i.Page, Issue: i.Issue})
		}
	}

	// docPagesByFeature from DocsMap.
	docPagesByFeature := map[string][]string{}
	for _, e := range in.DocsMap {
		docPagesByFeature[e.Feature] = e.Pages
	}

	featuresDir := filepath.Join(contentDir, "features")
	if err := os.MkdirAll(featuresDir, 0o755); err != nil {
		return err
	}

	// per-feature pages
	rows := make([]featureRow, 0, len(in.Mapping))
	for _, e := range in.Mapping {
		slug := slugs[e.Feature.Name]
		if slug == "" {
			continue // skip features whose name reduces to empty slug
		}
		documented := docFeatures[e.Feature.Name]
		page, err := renderFeature(featureData{
			Slug:        slug,
			Name:        e.Feature.Name,
			Description: e.Feature.Description,
			Layer:       e.Feature.Layer,
			UserFacing:  e.Feature.UserFacing,
			Documented:  documented,
			Files:       e.Files,
			Symbols:     e.Symbols,
			DocURLs:     docPagesByFeature[e.Feature.Name],
			Drift:       driftByFeature[e.Feature.Name],
		})
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(featuresDir, slug+".md"), []byte(page), 0o644); err != nil {
			return err
		}
		rows = append(rows, featureRow{
			Slug: slug, Name: e.Feature.Name, Layer: e.Feature.Layer,
			UserFacing: e.Feature.UserFacing, Documented: documented,
			FileCount: len(e.Files), DriftCount: len(driftByFeature[e.Feature.Name]),
		})
	}

	// features index
	idx, err := renderFeaturesIndex(featuresIndexData{Rows: rows})
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(featuresDir, "_index.md"), []byte(idx), 0o644); err != nil {
		return err
	}

	// gaps with linked feature names — read raw gaps.md and rewrite feature names.
	gapsBody, err := os.ReadFile(filepath.Join(opts.ProjectDir, "gaps.md"))
	if err != nil {
		return fmt.Errorf("read gaps.md: %w", err)
	}
	rewritten := linkFeatureNames(string(gapsBody), slugs)
	gapsFM := "+++\ntitle = \"Gaps\"\nweight = 20\n+++\n\n"
	if err := os.WriteFile(filepath.Join(contentDir, "gaps.md"), []byte(gapsFM+rewritten), 0o644); err != nil {
		return err
	}

	// per-docs-page screenshot pages
	if in.ScreenshotsRan {
		ssDir := filepath.Join(contentDir, "screenshots")
		if err := os.MkdirAll(ssDir, 0o755); err != nil {
			return err
		}
		// group by page
		byPage := map[string][]screenshotGap{}
		var order []string
		seen := map[string]bool{}
		for _, g := range in.Screenshots {
			if !seen[g.PageURL] {
				seen[g.PageURL] = true
				order = append(order, g.PageURL)
			}
			byPage[g.PageURL] = append(byPage[g.PageURL], screenshotGap{
				Quoted: g.QuotedPassage, ShouldShow: g.ShouldShow, Alt: g.SuggestedAlt, Insert: g.InsertionHint,
			})
		}
		pageSlugs := resolveSlugs(order)
		for _, url := range order {
			body, err := renderScreenshotPage(screenshotPageData{
				PageURL: url,
				Title:   url,
				Gaps:    byPage[url],
			})
			if err != nil {
				return err
			}
			slug := pageSlugs[url]
			if slug == "" {
				slug = "page"
			}
			if err := os.WriteFile(filepath.Join(ssDir, slug+".md"), []byte(body), 0o644); err != nil {
				return err
			}
		}
		// also write a section index for screenshots
		ssIdx := "+++\ntitle = \"Screenshots\"\nweight = 30\n+++\n\n# Missing screenshots\n"
		if err := os.WriteFile(filepath.Join(ssDir, "_index.md"), []byte(ssIdx), 0o644); err != nil {
			return err
		}
	}
	return nil
}

// linkFeatureNames replaces a feature heading like `### Foo` or `"Foo"` references
// with markdown links to /features/<slug>/. It rewrites only feature names from
// the slugs map; other content is untouched.
func linkFeatureNames(body string, slugs map[string]string) string {
	out := body
	for name, slug := range slugs {
		if name == "" || slug == "" {
			continue
		}
		// Replace quoted occurrences and heading occurrences.
		quoted := "\"" + name + "\""
		linked := "\"[" + name + "](/features/" + slug + "/)\""
		out = strings.ReplaceAll(out, quoted, linked)
	}
	return out
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/site/ -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/site/materialize.go internal/site/materialize_test.go
git commit -m "feat(site): expanded-mode materialization (per-feature pages + index)

- RED: TestMaterializeExpandedWritesFeaturePages covers slug collisions, gaps cross-links, taxonomies
- GREEN: materializeExpanded() with resolveSlugs, drift inlining, gaps rewriter"
```

---

## Phase 5 — Build()

### Task 5.1: Build() shells out to hugo (mirror happy path)

Requires `hugo` on `$PATH` for the test. Use a `//go:build integration` tag.

**Files:**
- Modify: `internal/site/site.go`
- Create: `internal/site/build_integration_test.go`

**Step 1: Write the failing integration test**

```go
//go:build integration
// +build integration

package site

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sandgardenhq/find-the-gaps/internal/analyzer"
)

func TestBuildMirrorIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("requires hugo on $PATH")
	}
	projectDir := t.TempDir()
	for _, name := range []string{"mapping.md", "gaps.md"} {
		_ = os.WriteFile(filepath.Join(projectDir, name), []byte("# "+name+"\n\nbody\n"), 0o644)
	}
	err := Build(context.Background(),
		Inputs{Summary: analyzer.ProductSummary{Description: "demo"}},
		BuildOptions{
			ProjectDir:  projectDir,
			ProjectName: "demo",
			Mode:        ModeMirror,
			GeneratedAt: time.Now(),
		})
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"site/index.html", "site/mapping/index.html", "site/gaps/index.html"} {
		if _, err := os.Stat(filepath.Join(projectDir, p)); err != nil {
			t.Errorf("missing %s: %v", p, err)
		}
	}
	// site-src must NOT persist by default
	if _, err := os.Stat(filepath.Join(projectDir, "site-src")); !os.IsNotExist(err) {
		t.Errorf("site-src should not exist by default; err=%v", err)
	}
}
```

**Step 2: Run the test to verify it fails**

Run: `go test -tags integration ./internal/site/ -v -run TestBuildMirrorIntegration`
Expected: FAIL — `Build` returns "not implemented".

**Step 3: Implement Build()**

```go
// replace stub Build in site.go

// HugoBin is the executable name shelled out for the build. Override in tests.
var HugoBin = "hugo"

// ErrHugoMissing is returned when the `hugo` binary cannot be located on $PATH.
var ErrHugoMissing = errors.New("hugo not found on PATH")

// Build implementation
func Build(ctx context.Context, in Inputs, opts BuildOptions) error {
	if opts.Mode != ModeMirror && opts.Mode != ModeExpanded {
		return ErrUnknownMode
	}
	if opts.ProjectDir == "" {
		return errors.New("ProjectDir is required")
	}

	// Pick the source dir.
	var srcDir string
	if opts.KeepSource {
		srcDir = filepath.Join(opts.ProjectDir, "site-src")
		if err := os.RemoveAll(srcDir); err != nil {
			return fmt.Errorf("clean site-src: %w", err)
		}
		if err := os.MkdirAll(srcDir, 0o755); err != nil {
			return fmt.Errorf("create site-src: %w", err)
		}
	} else {
		var err error
		srcDir, err = os.MkdirTemp("", "ftg-site-")
		if err != nil {
			return fmt.Errorf("create temp src: %w", err)
		}
	}

	if err := materialize(srcDir, in, opts); err != nil {
		return fmt.Errorf("materialize: %w", err)
	}

	if _, err := exec.LookPath(HugoBin); err != nil {
		return ErrHugoMissing
	}

	dest := filepath.Join(opts.ProjectDir, "site")
	if err := os.RemoveAll(dest); err != nil {
		return fmt.Errorf("clean dest: %w", err)
	}

	cmd := exec.CommandContext(ctx, HugoBin,
		"--source", srcDir,
		"--destination", dest,
		"--minify",
		"--quiet",
		"--baseURL", "/",
	)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		// Preserve srcDir for debugging on failure.
		return fmt.Errorf("hugo build failed (source preserved at %s): %w: %s", srcDir, err, stderr.String())
	}

	// Cleanup if not keeping source.
	if !opts.KeepSource {
		_ = os.RemoveAll(srcDir)
	}
	return nil
}
```

Add imports: `os/exec`, `path/filepath`, `strings`.

**Step 4: Run integration test**

Run: `go test -tags integration ./internal/site/ -v -run TestBuildMirrorIntegration`
Expected: PASS (with hugo installed)

**Step 5: Verify non-integration tests still pass**

Run: `go test -short ./internal/site/ -v`
Expected: PASS, integration test skipped

**Step 6: Commit**

```bash
git add internal/site/site.go internal/site/build_integration_test.go
git commit -m "feat(site): Build() shells out to hugo, mirror happy path

- RED: TestBuildMirrorIntegration (//go:build integration)
- GREEN: tempdir → materialize → exec hugo → cleanup
- Source dir preserved on hugo failure for debugging"
```

---

### Task 5.2: Build() — keep-source flag and expanded mode

**Files:**
- Modify: `internal/site/build_integration_test.go`

**Step 1: Add the failing tests**

```go
// add to build_integration_test.go
func TestBuildKeepSourcePersists(t *testing.T) {
	if testing.Short() {
		t.Skip("requires hugo on $PATH")
	}
	projectDir := t.TempDir()
	for _, name := range []string{"mapping.md", "gaps.md"} {
		_ = os.WriteFile(filepath.Join(projectDir, name), []byte("# "+name+"\n"), 0o644)
	}
	err := Build(context.Background(), Inputs{}, BuildOptions{
		ProjectDir:  projectDir,
		ProjectName: "demo",
		Mode:        ModeMirror,
		KeepSource:  true,
		GeneratedAt: time.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(projectDir, "site-src", "hugo.toml")); err != nil {
		t.Errorf("site-src/hugo.toml missing: %v", err)
	}
}

func TestBuildExpandedFeaturePagesRendered(t *testing.T) {
	if testing.Short() {
		t.Skip("requires hugo on $PATH")
	}
	projectDir := t.TempDir()
	for _, name := range []string{"mapping.md", "gaps.md"} {
		_ = os.WriteFile(filepath.Join(projectDir, name), []byte("# "+name+"\n"), 0o644)
	}
	in := Inputs{
		Mapping: analyzer.FeatureMap{
			{Feature: analyzer.CodeFeature{Name: "Login", Layer: "service", UserFacing: true}, Files: []string{"a.go"}},
		},
	}
	err := Build(context.Background(), in, BuildOptions{
		ProjectDir:  projectDir,
		ProjectName: "demo",
		Mode:        ModeExpanded,
		GeneratedAt: time.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(projectDir, "site", "features", "login", "index.html")); err != nil {
		t.Errorf("expanded feature page missing: %v", err)
	}
}
```

**Step 2: Run tests**

Run: `go test -tags integration ./internal/site/ -v`
Expected: PASS for both new tests (Build() already implements keep-source and expanded paths via materialize).

**Step 3: Commit**

```bash
git add internal/site/build_integration_test.go
git commit -m "test(site): cover --keep-site-source and expanded-mode integration"
```

---

### Task 5.3: Build() — hugo-missing error path

**Files:**
- Modify: `internal/site/site_test.go` (no integration tag — uses `HugoBin` override)

**Step 1: Write the failing test**

```go
// add to site_test.go
func TestBuildReturnsErrHugoMissing(t *testing.T) {
	defer func(orig string) { HugoBin = orig }(HugoBin)
	HugoBin = "ftg-nonexistent-hugo-binary-xyz"

	projectDir := t.TempDir()
	for _, name := range []string{"mapping.md", "gaps.md"} {
		_ = os.WriteFile(filepath.Join(projectDir, name), []byte("# "+name+"\n"), 0o644)
	}
	err := Build(context.Background(), Inputs{}, BuildOptions{
		ProjectDir:  projectDir,
		ProjectName: "demo",
		Mode:        ModeMirror,
		GeneratedAt: time.Now(),
	})
	if !errors.Is(err, ErrHugoMissing) {
		t.Errorf("expected ErrHugoMissing, got %v", err)
	}
}
```

Add imports: `os`, `path/filepath`.

**Step 2: Run the test**

Run: `go test ./internal/site/ -v -run TestBuildReturnsErrHugoMissing`
Expected: PASS (Build() already returns ErrHugoMissing — this is a regression guard).

**Step 3: Commit**

```bash
git add internal/site/site_test.go
git commit -m "test(site): regression for ErrHugoMissing when hugo is absent"
```

---

### Task 5.4: Build() — hugo-fails error path

**Files:**
- Modify: `internal/site/site_test.go`

**Step 1: Write the failing test**

```go
// add to site_test.go
func TestBuildPreservesSourceOnHugoFailure(t *testing.T) {
	// Create a fake hugo that exits non-zero.
	tmpBin := t.TempDir()
	fake := filepath.Join(tmpBin, "hugo-fake")
	script := "#!/bin/sh\necho 'fake error' >&2\nexit 1\n"
	if err := os.WriteFile(fake, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	defer func(orig string) { HugoBin = orig }(HugoBin)
	HugoBin = fake

	projectDir := t.TempDir()
	for _, name := range []string{"mapping.md", "gaps.md"} {
		_ = os.WriteFile(filepath.Join(projectDir, name), []byte("# "+name+"\n"), 0o644)
	}
	err := Build(context.Background(), Inputs{}, BuildOptions{
		ProjectDir:  projectDir,
		ProjectName: "demo",
		Mode:        ModeMirror,
		GeneratedAt: time.Now(),
	})
	if err == nil {
		t.Fatal("expected error from failing hugo")
	}
	if !strings.Contains(err.Error(), "fake error") {
		t.Errorf("error should include stderr: %v", err)
	}
	if !strings.Contains(err.Error(), "source preserved") {
		t.Errorf("error should name preserved source path: %v", err)
	}
}
```

**Step 2: Run the test**

Run: `go test ./internal/site/ -v -run TestBuildPreservesSourceOnHugoFailure`
Expected: PASS

**Step 3: Commit**

```bash
git add internal/site/site_test.go
git commit -m "test(site): preserve source dir and surface stderr when hugo fails"
```

---

### Task 5.5: Coverage gate

**Files:** none — verification step only.

**Step 1: Run coverage**

```bash
go test -short -coverprofile=coverage.out ./internal/site/
go tool cover -func=coverage.out | tail -1
```

Expected: total coverage ≥90%. If below, add unit tests until you cross the threshold (likely missing branches in `materializeExpanded` around empty inputs, in `linkFeatureNames` for features with empty slugs, in `buildHomeData` for screenshots-not-ran edge cases).

**Step 2: Commit any test additions**

```bash
git add internal/site/
git commit -m "test(site): bring coverage above 90% statement threshold"
```

---

## Phase 6 — CLI Wiring

### Task 6.1: Add --site-mode, --no-site, --keep-site-source flags

**Files:**
- Modify: `internal/cli/analyze.go`
- Create: `internal/cli/analyze_site_test.go`

**Step 1: Write the failing test**

```go
// internal/cli/analyze_site_test.go
package cli

import (
	"strings"
	"testing"
)

func TestAnalyzeFlagsSiteMode(t *testing.T) {
	cmd := newAnalyzeCmd()
	for _, name := range []string{"site-mode", "no-site", "keep-site-source"} {
		if cmd.Flags().Lookup(name) == nil {
			t.Errorf("missing flag --%s", name)
		}
	}
	// --site-mode should reject unknown values
	cmd.SetArgs([]string{"--site-mode=bogus", "--repo=.", "--docs-url=http://x"})
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	err := cmd.ParseFlags([]string{"--site-mode=bogus"})
	if err == nil {
		t.Skip("Cobra accepts arbitrary strings; validation happens in RunE")
	}
	_ = strings.Contains
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/ -v -run TestAnalyzeFlagsSiteMode`
Expected: FAIL — flags missing.

**Step 3: Add the flags and validation**

In `internal/cli/analyze.go`, add to the var block at the top of `newAnalyzeCmd`:

```go
var (
    // ... existing ...
    siteMode        string
    noSite          bool
    keepSiteSource  bool
)
```

In the flags block at the bottom of the function:

```go
cmd.Flags().StringVar(&siteMode, "site-mode", "mirror", "site content shape: \"mirror\" or \"expanded\"")
cmd.Flags().BoolVar(&noSite, "no-site", false, "skip the Hugo site build; markdown reports still emitted")
cmd.Flags().BoolVar(&keepSiteSource, "keep-site-source", false, "preserve generated Hugo source at <projectDir>/site-src/")
```

Inside `RunE`, near the top, validate:

```go
var siteModeVal site.Mode
switch siteMode {
case "mirror":
    siteModeVal = site.ModeMirror
case "expanded":
    siteModeVal = site.ModeExpanded
default:
    return fmt.Errorf("invalid --site-mode %q (want \"mirror\" or \"expanded\")", siteMode)
}
_ = siteModeVal // wired up in next task
```

Add import: `"github.com/sandgardenhq/find-the-gaps/internal/site"`.

**Step 4: Run test to verify it passes**

Run: `go test ./internal/cli/ -v -run TestAnalyzeFlagsSiteMode`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/cli/analyze.go internal/cli/analyze_site_test.go
git commit -m "feat(cli): --site-mode, --no-site, --keep-site-source flags

- RED: TestAnalyzeFlagsSiteMode asserts flag presence
- GREEN: register flags, parse --site-mode into site.Mode"
```

---

### Task 6.2: Wire site.Build() into analyze RunE

**Files:**
- Modify: `internal/cli/analyze.go`

**Step 1: Modify the test (or add to it)**

Use `testscript` for the end-to-end behavior — covered in Phase 8. For this task, a unit test is impractical because Build() shells out to hugo. We rely on the testscript suite added in Phase 8 + the existing integration tests in `internal/site/`.

**Step 2: Wire Build()**

Right after the existing `reporter.WriteScreenshots(...)` block:

```go
// Build the Hugo site unless --no-site.
if !noSite {
    err := site.Build(ctx,
        site.Inputs{
            Summary:        productSummary,
            Mapping:        featureMap,
            DocsMap:        docsFeatureMap,
            AllDocFeatures: docCoveredFeatures,
            Drift:          driftFindings,
            Screenshots:    screenshotGaps,
            ScreenshotsRan: !skipScreenshotCheck,
        },
        site.BuildOptions{
            ProjectDir:  projectDir,
            ProjectName: projectName,
            KeepSource:  keepSiteSource,
            Mode:        siteModeVal,
            GeneratedAt: time.Now(),
        })
    if err != nil {
        if errors.Is(err, site.ErrHugoMissing) {
            return fmt.Errorf("hugo not found on PATH; install via `find-the-gaps install-deps` or `brew install hugo`, or pass --no-site to skip site generation")
        }
        return fmt.Errorf("build site: %w", err)
    }
}
```

Add imports: `errors`, `time`. (`time` may already be imported.)

**Step 3: Update the stdout reports block**

Replace the existing `Fprintf(...)` with one that also reports the site:

```go
siteLine := "  " + projectDir + "/site/"
if noSite {
    siteLine += " (skipped)"
}
extraLine := ""
if keepSiteSource && !noSite {
    extraLine = "\n  " + projectDir + "/site-src/"
}
_, _ = fmt.Fprintf(cmd.OutOrStdout(),
    "scanned %d files, fetched %d pages, %d features mapped\nreports:\n  %s/mapping.md\n  %s/gaps.md\n%s\n%s%s\n",
    len(scan.Files), len(pages), len(featureMap),
    projectDir, projectDir, screenshotsLine, siteLine, extraLine)
```

**Step 4: Build & test**

Run: `go build ./... && go test -short ./...`
Expected: PASS, no regressions.

**Step 5: Commit**

```bash
git add internal/cli/analyze.go
git commit -m "feat(cli): wire site.Build() into analyze and report site path

- Skip when --no-site set
- Wrap ErrHugoMissing with an actionable hint pointing at install-deps"
```

---

## Phase 7 — External Dependency Integration

### Task 7.1: doctor reports hugo

**Files:**
- Modify: `internal/doctor/doctor.go`
- Modify: `internal/doctor/doctor_test.go`

**Step 1: Read the existing doctor implementation**

```bash
go doc -all github.com/sandgardenhq/find-the-gaps/internal/doctor
```

Identify how `mdfetch` is detected. Mirror that pattern for `hugo`: a struct entry with a `Name`, a command to run for the version, and a `MissingHint` message.

**Step 2: Write the failing test**

Find the existing `TestRun*` or similar in `doctor_test.go`. Add a test that asserts `Run()` (or whatever the public function is) prints `hugo` alongside `mdfetch`. The exact assertion shape depends on the existing tests; mirror them.

Example shape:

```go
func TestDoctorReportsHugo(t *testing.T) {
	// Use the same harness/test approach already in doctor_test.go.
	// Assert output contains "hugo" and a version string when hugo is on PATH.
}
```

**Step 3: Run test to verify it fails**

Run: `go test ./internal/doctor/ -v -run TestDoctorReportsHugo`
Expected: FAIL.

**Step 4: Add the entry**

Add `hugo` to the list of dependencies the doctor checks. Pattern: `exec.LookPath("hugo")`, on success run `hugo version` and print the first line; on failure print "hugo not found — install via `brew install hugo` or `find-the-gaps install-deps`".

**Step 5: Run all doctor tests**

Run: `go test ./internal/doctor/ -v`
Expected: PASS

**Step 6: Commit**

```bash
git add internal/doctor/
git commit -m "feat(doctor): detect and report hugo version

- RED: TestDoctorReportsHugo
- GREEN: extend dep list with hugo, mirror mdfetch detection"
```

---

### Task 7.2: install-deps installs hugo

**Files:**
- Modify: `internal/cli/install_deps.go`
- Modify: `internal/cli/install_deps_test.go`

**Step 1: Read the existing install_deps implementation**

Identify how `mdfetch` is installed. Mirror the same shape for `hugo`. On macOS: `brew install hugo`. On other platforms: print platform-specific guidance pointing at https://github.com/gohugoio/hugo/releases.

**Step 2: Write the failing test**

Mirror the existing test for mdfetch installation. Assert that the install plan includes `hugo`.

**Step 3: Run test to verify it fails**

Run: `go test ./internal/cli/ -v -run TestInstallDeps`
Expected: FAIL.

**Step 4: Implement**

Add `hugo` to the install list with the same pattern as `mdfetch`.

**Step 5: Run tests**

Run: `go test ./internal/cli/ -v`
Expected: PASS

**Step 6: Commit**

```bash
git add internal/cli/install_deps.go internal/cli/install_deps_test.go
git commit -m "feat(install-deps): include hugo (brew on macOS, link otherwise)"
```

---

### Task 7.3: First-run banner names hugo

**Files:**
- Locate the first-run banner code (likely under `internal/cli/root.go` or `internal/cli/install_deps.go`).
- Modify the banner string to mention both `mdfetch` and `hugo`.
- Add a test (mirroring any existing banner test) asserting both names appear and that `FIND_THE_GAPS_QUIET=1` suppresses output.

**Step 1: grep for existing banner**

```bash
go run ./... # produces banner — read source
```

Or:

```
grep -rn "first-run" internal/cli/
grep -rn "mdfetch" internal/cli/
```

**Step 2: Write the failing test**

```go
func TestFirstRunBannerNamesHugo(t *testing.T) {
	// Mirror existing banner test structure; assert "hugo" appears.
}
```

**Step 3: Update banner & test**

**Step 4: Run tests**

Run: `go test ./internal/cli/ -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/cli/
git commit -m "feat(cli): first-run banner names hugo alongside mdfetch"
```

---

### Task 7.4: goreleaser Homebrew dependency + caveats

**Files:**
- Modify: `.goreleaser.yaml`

**Step 1: Read the current `brews:` block**

```bash
grep -n -A 30 "^brews:" .goreleaser.yaml
```

**Step 2: Edit**

In `brews[].dependencies`, add an entry:

```yaml
dependencies:
  - name: mdfetch  # existing
  - name: hugo
```

In `brews[].caveats`, append a sentence:

```
Find the Gaps shells out to `mdfetch` (docs ingestion) and `hugo`
(static site rendering). Both were installed as dependencies of
this formula.
```

**Step 3: Validate the YAML**

```bash
goreleaser check
```

Expected: no errors.

**Step 4: Commit**

```bash
git add .goreleaser.yaml
git commit -m "build(release): add hugo as Homebrew dependency, update caveats"
```

---

### Task 7.5: README "What this installs" section

**Files:**
- Modify: `README.md`

**Step 1: Read the current docs**

Find the "What this installs" heading.

**Step 2: Add a row for hugo**

```markdown
- **hugo** — static site generator used to render the analyze report as a browsable Hextra-themed site.
```

**Step 3: Commit**

```bash
git add README.md
git commit -m "docs: list hugo under \"What this installs\""
```

---

## Phase 8 — End-to-End Tests (testscript)

### Task 8.1: testscript — analyze emits site (mirror)

**Files:**
- Create: `cmd/find-the-gaps/testdata/analyze_site_mirror.txtar`

**Step 1: Inspect existing testscript fixtures**

```bash
ls cmd/find-the-gaps/testdata/
cat cmd/find-the-gaps/testdata/analyze_*.txtar 2>/dev/null | head -100
```

**Step 2: Write the txtar fixture**

```
# analyze_site_mirror.txtar
[!exec:hugo] skip 'hugo not on PATH'

env FTG_LLM_FAKE=1
exec find-the-gaps analyze --repo=$WORK/repo --docs-url=http://localhost:0 --no-cache
exists out/site/index.html
exists out/site/mapping/index.html
exists out/site/gaps/index.html
! exists out/site-src

-- repo/main.go --
package main

func main() {}
```

(Adjust the `--repo` path, env vars, and fake-LLM mechanism to match the project's existing testscript harness — read existing fixtures to learn the conventions.)

**Step 3: Run**

Run: `go test ./cmd/find-the-gaps/ -run TestScripts/analyze_site_mirror -v`
Expected: PASS

**Step 4: Commit**

```bash
git add cmd/find-the-gaps/testdata/analyze_site_mirror.txtar
git commit -m "test(e2e): analyze emits a mirror-mode site"
```

---

### Task 8.2: testscript — expanded mode

**Files:**
- Create: `cmd/find-the-gaps/testdata/analyze_site_expanded.txtar`

Mirror Task 8.1; add `--site-mode=expanded`. Assert `out/site/features/<some-slug>/index.html` exists.

Commit.

---

### Task 8.3: testscript — --no-site

**Files:**
- Create: `cmd/find-the-gaps/testdata/analyze_no_site.txtar`

Same setup but `--no-site`. Assert `out/site/` does NOT exist; markdown reports DO exist.

Commit.

---

### Task 8.4: testscript — --keep-site-source

**Files:**
- Create: `cmd/find-the-gaps/testdata/analyze_keep_source.txtar`

Same as 8.1 with `--keep-site-source`. Assert `out/site-src/hugo.toml` exists.

Commit.

---

### Task 8.5: testscript — missing hugo

**Files:**
- Create: `cmd/find-the-gaps/testdata/analyze_site_missing_hugo.txtar`

Set `PATH` to a directory without `hugo`. Assert `find-the-gaps analyze` exits non-zero with stderr containing `hugo not found` and an install hint.

Also verify that `--no-site` succeeds with the same scrubbed PATH.

Commit.

---

### Task 8.6: testscript — doctor reports hugo

**Files:**
- Create: `cmd/find-the-gaps/testdata/doctor_hugo.txtar`

Assert `doctor` output contains `hugo` and a version line when on PATH; non-zero exit and an install hint when scrubbed.

Commit.

---

## Phase 9 — Verification & Cleanup

### Task 9.1: Add Scenario 10 to VERIFICATION_PLAN.md

**Files:**
- Modify: `.plans/VERIFICATION_PLAN.md`

**Step 1: Append Scenario 10**

```markdown
### Scenario 10: Hugo Site Output

**Context**: Verify the analyze command produces a deployable Hugo site in both modes.

**Steps**:
1. Run `find-the-gaps analyze --repo ./testdata/fixtures/known-good --docs-url https://<docs>`.
2. Run a static server in `<projectDir>/site/` (e.g., `python3 -m http.server`) and load `/`, `/mapping/`, `/gaps/`.
3. Re-run with `--site-mode=expanded`.
4. Crawl every link in `<projectDir>/site/gaps/index.html` and confirm each resolves.
5. Re-run with `--no-site`.
6. Re-run with `--keep-site-source`.
7. Temporarily remove `hugo` from `$PATH` and re-run.

**Success Criteria**:
- [ ] Step 1: `<projectDir>/site/index.html` exists; loads in a browser; Hextra theme renders.
- [ ] Step 1: `mapping.md`, `gaps.md`, `screenshots.md` (if produced) are still emitted at `<projectDir>/`.
- [ ] Step 3: `<projectDir>/site/features/<slug>/index.html` exists for every feature.
- [ ] Step 4: every `/features/<slug>/` link in the rendered gaps page resolves to a 200.
- [ ] Step 5: `<projectDir>/site/` does NOT exist after the run.
- [ ] Step 6: `<projectDir>/site-src/hugo.toml` is present.
- [ ] Step 7: command exits non-zero; stderr names `hugo` and points at `install-deps`.

**If Blocked**: If any link in step 4 returns 404 or the site is missing pages, capture the directory listing and ask the developer.
```

**Step 2: Commit**

```bash
git add .plans/VERIFICATION_PLAN.md
git commit -m "docs(plans): add Scenario 10 covering Hugo site output"
```

---

### Task 9.2: Final coverage and lint check

**Step 1: Run everything**

```bash
go test -short ./...
go test -tags integration ./internal/site/  # requires hugo on PATH
golangci-lint run
go test -coverprofile=coverage.out ./internal/site/
go tool cover -func=coverage.out | tail -1
```

Expected:
- All non-integration tests pass.
- All integration tests pass.
- Zero lint warnings.
- `internal/site` coverage ≥90%.

**Step 2: If anything fails, fix and re-commit per the project's TDD rules.**

---

### Task 9.3: PROGRESS.md update

**Files:**
- Modify: `PROGRESS.md` (create if absent)

Append a section per CLAUDE.md's PROGRESS.md format covering each phase of this plan, with timestamps, test counts, coverage achieved, and any issues.

Commit.

---

## Done

After all phases complete and verification passes:

1. Run `go test ./... && go test -tags integration ./internal/site/ && golangci-lint run`. All green.
2. Push the branch: `git push -u origin feat/hugo-site-output`.
3. Open a PR titled `feat: Hugo-rendered static site output` with body summarizing the design + linking the design and verification plan.
4. Reference any issue this closes in the PR body (`Closes #<n>` if applicable).
