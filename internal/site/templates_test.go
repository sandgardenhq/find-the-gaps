package site

import (
	"regexp"
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
	if strings.Contains(got, "# demo") {
		t.Errorf("home page must not contain `# demo` H1 (frontmatter title supplies the heading); got:\n%s", got)
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

func TestRenderFeatureFull(t *testing.T) {
	in := featureData{
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

func TestRenderFeatureFrontmatterIsValidTOML(t *testing.T) {
	in := featureData{
		Name:       "User Auth",
		Layer:      "service",
		UserFacing: true,
		Documented: true,
		Files:      []string{"internal/auth/login.go"},
		Symbols:    []string{"Login"},
	}
	got, err := renderFeature(in)
	if err != nil {
		t.Fatal(err)
	}

	// Extract the +++...+++ frontmatter block.
	fmRe := regexp.MustCompile(`(?s)\A\+\+\+\n(.*?)\n\+\+\+`)
	m := fmRe.FindStringSubmatch(got)
	if m == nil {
		t.Fatalf("no +++...+++ frontmatter block found in:\n%s", got)
	}
	frontmatter := m[1]

	// Bug regression check: title and tags MUST be on separate lines.
	// The previous template trimmed the newline after `title = "..."`,
	// producing `title = "X"tags = [...]` and breaking Hugo's TOML parser.
	titleLineRe := regexp.MustCompile(`(?m)^title = "User Auth"\s*$`)
	if !titleLineRe.MatchString(frontmatter) {
		t.Errorf("title line not isolated on its own line; frontmatter:\n%s", frontmatter)
	}
	tagsLineRe := regexp.MustCompile(`(?m)^tags = \["layer:service", "status:documented", "user-facing:yes"\]\s*$`)
	if !tagsLineRe.MatchString(frontmatter) {
		t.Errorf("tags line not isolated on its own line; frontmatter:\n%s", frontmatter)
	}

	// Belt-and-suspenders: assert there is no `"tags` substring (i.e., a closing
	// quote of the title immediately followed by `tags`), which is the exact
	// shape of the bug.
	if strings.Contains(frontmatter, `"tags`) {
		t.Errorf("title and tags collapsed onto one line; frontmatter:\n%s", frontmatter)
	}
}

func TestRenderFeatureUndocumentedCallout(t *testing.T) {
	got, err := renderFeature(featureData{
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

func TestRenderMappingPageNoFeatureMapH1(t *testing.T) {
	got, err := renderMappingPage(mappingPageData{
		Summary: "demo product",
		Features: []mappingFeature{
			{Name: "Alpha", Layer: "ui", UserFacing: true, Documented: true,
				Files: []string{"a.go"}, Symbols: []string{"A"}, DocURLs: []string{"https://example.com/a"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got, "# Feature Map") {
		t.Errorf("mapping page must not contain `# Feature Map` H1; got:\n%s", got)
	}
}

func TestRenderMappingPageSubHeadingLists(t *testing.T) {
	got, err := renderMappingPage(mappingPageData{
		Summary: "demo product",
		Features: []mappingFeature{
			{
				Name:        "User Auth",
				Description: "Login and session management.",
				Layer:       "service",
				UserFacing:  true,
				Documented:  true,
				Files:       []string{"internal/auth/login.go", "internal/auth/session.go"},
				Symbols:     []string{"Login", "Logout"},
				DocURLs:     []string{"https://example.com/docs/auth"},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"## Product Summary",
		"demo product",
		"## Features",
		"### User Auth",
		"> Login and session management.",
		"- **Layer:** service",
		"- **User-facing:** yes",
		"- **Documentation status:** documented",
		"#### Implemented in",
		"- `internal/auth/login.go`",
		"- `internal/auth/session.go`",
		"#### Symbols",
		"- `Login`",
		"- `Logout`",
		"#### Documented on",
		"- [https://example.com/docs/auth](https://example.com/docs/auth)",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

func TestRenderMappingPageEmptyDocURLsRendersNone(t *testing.T) {
	got, err := renderMappingPage(mappingPageData{
		Summary: "demo",
		Features: []mappingFeature{
			{
				Name:       "Orphan",
				Layer:      "service",
				UserFacing: false,
				Documented: false,
				Files:      []string{"orphan.go"},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "#### Documented on") {
		t.Errorf("expected '#### Documented on' heading even when empty; got:\n%s", got)
	}
	if !strings.Contains(got, "_(none)_") {
		t.Errorf("expected '_(none)_' marker for empty DocURLs; got:\n%s", got)
	}
}

func TestRenderMappingPageOmitsEmptyFilesAndSymbols(t *testing.T) {
	got, err := renderMappingPage(mappingPageData{
		Summary: "demo",
		Features: []mappingFeature{
			{
				Name:       "Bare",
				Layer:      "ui",
				UserFacing: true,
				Documented: true,
				DocURLs:    []string{"https://example.com/bare"},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got, "#### Implemented in") {
		t.Errorf("Implemented in should be omitted when Files is empty; got:\n%s", got)
	}
	if strings.Contains(got, "#### Symbols") {
		t.Errorf("Symbols should be omitted when Symbols is empty; got:\n%s", got)
	}
}

func TestRenderScreenshotsMirrorWithGaps(t *testing.T) {
	got, err := renderScreenshotsMirror(screenshotsMirrorData{
		Pages: []screenshotsMirrorPage{
			{
				PageURL: "https://example.com/docs/start",
				Gaps: []screenshotGap{
					{Quoted: "Open the dashboard\n\nClick **Save** to continue.", ShouldShow: "the dashboard view", Alt: "dashboard", Insert: "after first paragraph"},
				},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"### From: https://example.com/docs/start",
		"```markdown",
		"Open the dashboard",
		"Click **Save** to continue.",
		`{{< callout type="info" >}}`,
		"**Screenshot should show:** the dashboard view",
		"**Alt text:** `dashboard`",
		"**Insertion hint:** after first paragraph",
		"{{< /callout >}}",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

func TestRenderScreenshotsMirrorEmpty(t *testing.T) {
	got, err := renderScreenshotsMirror(screenshotsMirrorData{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "_None found._") {
		t.Errorf("expected `_None found._` for empty pages; got:\n%s", got)
	}
	if strings.Contains(got, "{{< callout") {
		t.Errorf("empty data must not emit a callout; got:\n%s", got)
	}
}

func TestRenderScreenshotPage(t *testing.T) {
	got, err := renderScreenshotPage(screenshotPageData{
		PageURL: "https://example.com/docs/start",
		Title:   "Quickstart",
		Gaps: []screenshotGap{
			{Quoted: "open the dashboard\n\nclick **Save**", ShouldShow: "the dashboard view", Alt: "dashboard", Insert: "after first paragraph"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`title = "Quickstart"`,
		"https://example.com/docs/start",
		"```markdown",
		"open the dashboard",
		"click **Save**",
		`{{< callout type="info" >}}`,
		"**Screenshot should show:** the dashboard view",
		"**Alt text:** `dashboard`",
		"**Insertion hint:** after first paragraph",
		"{{< /callout >}}",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	if strings.Contains(got, "# Quickstart") {
		t.Errorf("expanded screenshot page must not contain `# Quickstart` H1; got:\n%s", got)
	}
}

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
