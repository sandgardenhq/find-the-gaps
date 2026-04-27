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
