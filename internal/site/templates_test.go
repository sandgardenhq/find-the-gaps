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

// TestRenderHugoConfigMirrorMenuOrder pins that the top nav links are
// emitted in Gaps -> Screenshots -> Mapping order. The order is enforced
// by the menu weights in hugo.toml; the simplest way to assert the
// rendered ordering is to check that each name appears before the next
// in the rendered config string.
func TestRenderHugoConfigMirrorMenuOrder(t *testing.T) {
	got, err := renderHugoConfig(hugoConfigData{
		Title:          "x",
		Mode:           ModeMirror,
		ScreenshotsRan: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	gaps := strings.Index(got, `name = "Gaps"`)
	screenshots := strings.Index(got, `name = "Screenshots"`)
	mapping := strings.Index(got, `name = "Mapping"`)
	if gaps < 0 || screenshots < 0 || mapping < 0 {
		t.Fatalf("expected Gaps, Screenshots, Mapping menu entries; got:\n%s", got)
	}
	if !(gaps < screenshots && screenshots < mapping) {
		t.Errorf("expected menu order Gaps -> Screenshots -> Mapping; positions gaps=%d screenshots=%d mapping=%d; got:\n%s",
			gaps, screenshots, mapping, got)
	}
}

// TestRenderHugoConfigExpandedMenuOrder mirrors the mirror-mode order
// check for expanded mode, where the structural-map entry is "Features"
// instead of "Mapping". Order: Gaps -> Screenshots -> Features.
func TestRenderHugoConfigExpandedMenuOrder(t *testing.T) {
	got, err := renderHugoConfig(hugoConfigData{
		Title:          "x",
		Mode:           ModeExpanded,
		ScreenshotsRan: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	gaps := strings.Index(got, `name = "Gaps"`)
	screenshots := strings.Index(got, `name = "Screenshots"`)
	features := strings.Index(got, `name = "Features"`)
	if gaps < 0 || screenshots < 0 || features < 0 {
		t.Fatalf("expected Gaps, Screenshots, Features menu entries; got:\n%s", got)
	}
	if !(gaps < screenshots && screenshots < features) {
		t.Errorf("expected menu order Gaps -> Screenshots -> Features; positions gaps=%d screenshots=%d features=%d; got:\n%s",
			gaps, screenshots, features, got)
	}
}

// TestRenderHugoConfigDisablesPoweredByHextra pins that the rendered hugo
// config disables Hextra's "Powered by Hextra" footer credit. The default
// is true, so the param has to be explicitly set to false in our config.
func TestRenderHugoConfigDisablesPoweredByHextra(t *testing.T) {
	got, err := renderHugoConfig(hugoConfigData{
		Title: "x",
		Mode:  ModeMirror,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "[params.footer]") {
		t.Errorf("expected [params.footer] section in:\n%s", got)
	}
	if !strings.Contains(got, "displayPoweredBy = false") {
		t.Errorf("expected `displayPoweredBy = false` to suppress Hextra credit; got:\n%s", got)
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

// TestRenderHomeIncludesCounts pins the at-a-glance stat cards. Each metric
// renders as an `<a class="ftg-stat-card ...">` with a big number and a
// label. Mirror mode points the Features card at /mapping/, and the
// undocumented/drift/screenshots cards point at their respective pages.
// Counts of 0 on "bad" metrics carry the --good modifier; counts > 0 carry
// --bad; the Features card is always --neutral.
func TestRenderHomeIncludesCounts(t *testing.T) {
	in := homeData{
		ProjectName:           "demo",
		GeneratedAt:           time.Date(2026, 4, 24, 10, 0, 0, 0, time.UTC),
		Summary:               "A small CLI demo.",
		FeatureCount:          17,
		UndocumentedUserCount: 4,
		DriftCount:            6,
		DriftLargeCount:       1,
		DriftMediumCount:      2,
		DriftSmallCount:       3,
		ScreenshotGapCount:    9,
		ScreenshotLargeCount:  4,
		ScreenshotMediumCount: 3,
		ScreenshotSmallCount:  2,
		ScreenshotsRan:        true,
		Mode:                  ModeMirror,
	}
	got, err := renderHome(in)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"A small CLI demo.",
		"2026-04-24",
		`class="ftg-stats ftg-stats--overview"`,
		`class="ftg-stat-card ftg-stat-card--neutral" href="/mapping/"`,
		`<span class="ftg-stat-num">17</span>`,
		`<span class="ftg-stat-label">Features</span>`,
		`ftg-stat-card--bad" href="/gaps/"`,
		`<span class="ftg-stat-num">4</span>`,
		`<span class="ftg-stat-label">Undocumented user-facing features</span>`,
		// Drift section: heading + three priority cards
		"### Drift findings",
		`class="ftg-stats ftg-stats--priority"`,
		`ftg-stat-card--large" href="/gaps/"`,
		`<span class="ftg-stat-num">1</span>`,
		`<span class="ftg-stat-label">Large</span>`,
		`ftg-stat-card--medium" href="/gaps/"`,
		`<span class="ftg-stat-num">2</span>`,
		`<span class="ftg-stat-label">Medium</span>`,
		`ftg-stat-card--small" href="/gaps/"`,
		`<span class="ftg-stat-num">3</span>`,
		`<span class="ftg-stat-label">Small</span>`,
		// Screenshot section: heading + three priority cards
		"### Missing screenshots",
		`ftg-stat-card--large" href="/screenshots/"`,
		`<span class="ftg-stat-num">4</span>`,
		`ftg-stat-card--medium" href="/screenshots/"`,
		`ftg-stat-card--small" href="/screenshots/"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	// The single combined "Drift findings" / "Missing screenshots" cards
	// were replaced by per-priority cards; the top-level labels must be
	// gone from the at-a-glance card row.
	for _, bad := range []string{
		`<span class="ftg-stat-label">Drift findings</span>`,
		`<span class="ftg-stat-label">Missing screenshots</span>`,
	} {
		if strings.Contains(got, bad) {
			t.Errorf("unexpected %q in:\n%s", bad, got)
		}
	}
	if strings.Contains(got, "# demo") {
		t.Errorf("home page must not contain `# demo` H1 (frontmatter title supplies the heading); got:\n%s", got)
	}
}

// TestRenderHomeZeroCountsAreGood pins the color logic: a zero count on a
// "bad" metric (undocumented, drift, missing screenshots) carries the
// --good modifier so a clean run renders green.
func TestRenderHomeZeroCountsAreGood(t *testing.T) {
	in := homeData{
		ProjectName:           "demo",
		GeneratedAt:           time.Date(2026, 4, 24, 10, 0, 0, 0, time.UTC),
		FeatureCount:          5,
		UndocumentedUserCount: 0,
		DriftCount:            0,
		ScreenshotGapCount:    0,
		ScreenshotsRan:        true,
		Mode:                  ModeMirror,
	}
	got, err := renderHome(in)
	if err != nil {
		t.Fatal(err)
	}
	// Every zero-count card should be --good; none should be --bad or
	// carry a priority modifier.
	for _, bad := range []string{
		"ftg-stat-card--bad",
		"ftg-stat-card--large",
		"ftg-stat-card--medium",
		"ftg-stat-card--small",
	} {
		if strings.Contains(got, bad) {
			t.Errorf("zero counts must not render %q; got:\n%s", bad, got)
		}
	}
	// At least seven --good cards: undocumented + 3 drift priorities +
	// 3 screenshot priorities.
	if c := strings.Count(got, "ftg-stat-card--good"); c < 7 {
		t.Errorf("expected ≥7 --good cards for zero counts, got %d in:\n%s", c, got)
	}
}

// TestRenderHomeGeneratedAtAtBottom pins the home page layout: the
// "Generated ..." timestamp must sit at the bottom of the page (after the
// summary and the at-a-glance section), not at the top.
func TestRenderHomeGeneratedAtAtBottom(t *testing.T) {
	in := homeData{
		ProjectName:  "demo",
		GeneratedAt:  time.Date(2026, 4, 24, 10, 0, 0, 0, time.UTC),
		Summary:      "A small CLI demo.",
		FeatureCount: 3,
		Mode:         ModeMirror,
	}
	got, err := renderHome(in)
	if err != nil {
		t.Fatal(err)
	}
	gen := strings.Index(got, "Generated 2026-04-24")
	stats := strings.Index(got, `class="ftg-stats `)
	sum := strings.Index(got, "A small CLI demo.")
	if gen < 0 || sum < 0 || stats < 0 {
		t.Fatalf("expected timestamp, stats block, and summary in output; got:\n%s", got)
	}
	if gen < sum {
		t.Errorf("timestamp must follow summary; got:\n%s", got)
	}
	if gen < stats {
		t.Errorf("timestamp must follow at-a-glance stats block; got:\n%s", got)
	}
}

// TestRenderHomeIncludesDocHolidayHero pins the marketing hero block that
// must appear between the product summary and the "At a glance" heading.
// Copy is product-approved — the test pins each load-bearing sentence and
// the structural placement (after summary, before stats).
func TestRenderHomeIncludesDocHolidayHero(t *testing.T) {
	in := homeData{
		ProjectName:  "demo",
		GeneratedAt:  time.Date(2026, 4, 24, 10, 0, 0, 0, time.UTC),
		Summary:      "A small CLI demo.",
		FeatureCount: 3,
		Mode:         ModeMirror,
	}
	got, err := renderHome(in)
	if err != nil {
		t.Fatal(err)
	}

	for _, want := range []string{
		`class="ftg-hero"`,
		// "Find the Gaps" is bolded inline, so the literal product name
		// is wrapped in <strong>. The remainder of the lead-in sits
		// outside the tag.
		"<strong>Find the Gaps</strong> is brought to you by Doc Holiday.",
		"Use FTG to identify places where documentation can improve; buy Doc Holiday to ensure it never deviates again.",
		"support@doc.holiday",
		"doc.holiday",
		// The Doc Holiday brand icon is rendered inline next to the copy
		// so the hero matches the global footer treatment. The brand
		// cyan fill is unique to the mark — pin it so a regression that
		// drops the SVG is caught.
		"<svg",
		"#1BB7D1",
		`class="ftg-hero__icon"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}

	hero := strings.Index(got, `class="ftg-hero"`)
	summary := strings.Index(got, "A small CLI demo.")
	glance := strings.Index(got, "## At a glance")
	if hero < 0 || summary < 0 || glance < 0 {
		t.Fatalf("expected hero, summary, and at-a-glance heading in output; got:\n%s", got)
	}
	if summary >= hero || hero >= glance {
		t.Errorf("hero must sit between summary and at-a-glance heading; positions summary=%d hero=%d glance=%d in:\n%s",
			summary, hero, glance, got)
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
		ProjectName:  "demo",
		GeneratedAt:  time.Now(),
		FeatureCount: 5,
		Mode:         ModeExpanded,
	}
	got, err := renderHome(in)
	if err != nil {
		t.Fatal(err)
	}
	// In expanded mode the Features stat card links to /features/
	// (mirror mode points the same card at /mapping/).
	if !strings.Contains(got, `class="ftg-stat-card ftg-stat-card--neutral" href="/features/"`) {
		t.Errorf("expanded home should link Features card to /features/, got:\n%s", got)
	}
	if strings.Contains(got, `href="/mapping/"`) {
		t.Error("expanded home should not link to /mapping/")
	}
}

// TestRenderHomeIncludesDeadLinks pins the at-a-glance Dead Links block.
// Three priority-style cards (Broken / Auth Required / Redirected) sit
// under the section heading, each linking to /links/. The severity palette
// mirrors drift/screenshots:
//
//	Broken         -> ftg-stat-card--large   (red — failing links)
//	Auth Required  -> ftg-stat-card--medium  (amber — manual review)
//	Redirected     -> ftg-stat-card--small   (neutral — soft signal)
//
// Counts of 0 still render the bucket (with --good) so a clean run reads
// as deliberately green rather than missing.
func TestRenderHomeIncludesDeadLinks(t *testing.T) {
	in := homeData{
		ProjectName:         "demo",
		GeneratedAt:         time.Date(2026, 4, 24, 10, 0, 0, 0, time.UTC),
		FeatureCount:        3,
		LinksRan:            true,
		LinkBrokenCount:     2,
		LinkAuthCount:       1,
		LinkRedirectedCount: 4,
		Mode:                ModeMirror,
	}
	got, err := renderHome(in)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"### Dead links",
		`ftg-stat-card--large" href="/links/"`,
		`<span class="ftg-stat-num">2</span>`,
		`<span class="ftg-stat-label">Broken</span>`,
		`ftg-stat-card--medium" href="/links/"`,
		`<span class="ftg-stat-num">1</span>`,
		`<span class="ftg-stat-label">Auth required</span>`,
		`ftg-stat-card--small" href="/links/"`,
		`<span class="ftg-stat-num">4</span>`,
		`<span class="ftg-stat-label">Redirected</span>`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

func TestRenderHomeDeadLinksZeroCountsAreGood(t *testing.T) {
	in := homeData{
		ProjectName:  "demo",
		GeneratedAt:  time.Now(),
		FeatureCount: 3,
		LinksRan:     true,
		Mode:         ModeMirror,
	}
	got, err := renderHome(in)
	if err != nil {
		t.Fatal(err)
	}
	// Three Dead Links buckets must be present and all --good when zero.
	for _, want := range []string{
		"### Dead links",
		`<span class="ftg-stat-label">Broken</span>`,
		`<span class="ftg-stat-label">Auth required</span>`,
		`<span class="ftg-stat-label">Redirected</span>`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	for _, bad := range []string{
		`ftg-stat-card--large" href="/links/"`,
		`ftg-stat-card--medium" href="/links/"`,
		`ftg-stat-card--small" href="/links/"`,
	} {
		if strings.Contains(got, bad) {
			t.Errorf("zero counts must not render %q; got:\n%s", bad, got)
		}
	}
}

func TestRenderHomeOmitsDeadLinksWhenNotRan(t *testing.T) {
	in := homeData{
		ProjectName:  "demo",
		GeneratedAt:  time.Now(),
		FeatureCount: 3,
		LinksRan:     false,
		Mode:         ModeMirror,
	}
	got, err := renderHome(in)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got, "### Dead links") {
		t.Errorf("home should not contain Dead links section when LinksRan=false:\n%s", got)
	}
	if strings.Contains(got, `href="/links/"`) {
		t.Errorf("home should not link to /links/ when LinksRan=false:\n%s", got)
	}
}

// TestRenderHomeOmitsSectionsBlock pins the removal of the dedicated
// `## Sections` list. The at-a-glance bullets carry the navigation links
// now, so a separate Sections block is redundant.
func TestRenderHomeOmitsSectionsBlock(t *testing.T) {
	in := homeData{
		ProjectName:    "demo",
		GeneratedAt:    time.Now(),
		FeatureCount:   3,
		ScreenshotsRan: true,
		Mode:           ModeMirror,
	}
	got, err := renderHome(in)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got, "## Sections") {
		t.Errorf("home should not contain `## Sections`; got:\n%s", got)
	}
}

// TestLayoutsRemoveSidebarEntirely pins that home, list, and single
// layouts do NOT render the Hextra sidebar partial. The report site has
// no nested navigation hierarchy — every link the reader needs is in the
// top navbar — so the sidebar's left column is dead space. Removing the
// partial drops the <aside> element entirely so the article fills the
// available width.
func TestLayoutsRemoveSidebarEntirely(t *testing.T) {
	for _, p := range []string{
		"assets/theme/hextra/layouts/single.html",
		"assets/theme/hextra/layouts/list.html",
		"assets/theme/hextra/layouts/home.html",
	} {
		body, err := themeFS.ReadFile(p)
		if err != nil {
			t.Fatalf("read %s: %v", p, err)
		}
		if strings.Contains(string(body), `partial "sidebar.html"`) {
			t.Errorf("%s must not invoke the sidebar partial; got:\n%s", p, body)
		}
	}
}

// TestSidebarThemeToggleHasNoTopBorder pins removal of Hextra's stock
// `hx:border-t` rule on the sticky bottom panel that holds the language
// switcher and the light/dark/system theme toggle. The border drew a thin
// horizontal line right above the theme indicator that read as a stray
// separator in the rendered site, so we strip it from the sidebar partial.
func TestSidebarThemeToggleHasNoTopBorder(t *testing.T) {
	body, err := themeFS.ReadFile("assets/theme/hextra/layouts/_partials/sidebar.html")
	if err != nil {
		t.Fatal(err)
	}
	src := string(body)

	// The sticky bottom panel is uniquely identified by its
	// `data-toggle-animation="show"` hook. Find that element's opening tag
	// and assert it does NOT carry `hx:border-t`. We restrict the check to
	// that single element so unrelated `hx:border-t` usages elsewhere in
	// the partial (none today, but possible in the future) do not trip us.
	idx := strings.Index(src, `data-toggle-animation="show"`)
	if idx < 0 {
		t.Fatalf("could not find the theme-toggle sticky container marker; sidebar.html may have been restructured")
	}
	tagStart := strings.LastIndex(src[:idx], "<")
	tagEnd := strings.Index(src[idx:], ">")
	if tagStart < 0 || tagEnd < 0 {
		t.Fatalf("could not isolate the theme-toggle container tag")
	}
	tag := src[tagStart : idx+tagEnd+1]
	if strings.Contains(tag, "hx:border-t") {
		t.Errorf("theme-toggle sticky container must not carry hx:border-t (top border above the theme indicator); got tag:\n%s", tag)
	}
}

// TestSingleLayoutH1IsNotCenterAligned pins that the per-page (mapping,
// gaps, screenshots, per-feature) layout uses the default left alignment
// for the page header instead of Hextra's stock `hx:text-center`. The
// home page still uses `home.html` and remains centered — this test only
// guards `single.html`.
func TestSingleLayoutH1IsNotCenterAligned(t *testing.T) {
	body, err := themeFS.ReadFile("assets/theme/hextra/layouts/single.html")
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(body), "\n") {
		if strings.Contains(line, "<h1") && strings.Contains(line, ".Title") {
			if strings.Contains(line, "hx:text-center") {
				t.Errorf("single.html H1 must not use hx:text-center, got:\n%s", line)
			}
			return
		}
	}
	t.Fatalf("could not find the .Title H1 line in single.html")
}

// TestRenderMappingPageWrapsEachFeatureInCard pins that the rendered
// mapping page wraps each feature block in a card-style `<div>` so
// adjacent features are visually separated. Hugo's `markup.goldmark.
// renderer.unsafe = true` (set in our hugo.toml) enables the raw HTML.
func TestRenderMappingPageWrapsEachFeatureInCard(t *testing.T) {
	got, err := renderMappingPage(mappingPageData{
		Summary: "demo",
		Features: []mappingFeature{
			{Name: "Alpha", UserFacing: true, Documented: true, Files: []string{"a.go"}},
			{Name: "Beta", UserFacing: false, Documented: false, Files: []string{"b.go"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	// Each feature gets its own opening `<div class="ftg-feature-card ...">`
	// with card styling. Two features → two opening cards.
	openCount := strings.Count(got, `<div class="ftg-feature-card`)
	if openCount < 2 {
		t.Errorf("expected at least 2 .ftg-feature-card wrapper opens (one per feature), got %d in:\n%s", openCount, got)
	}
	// Each card carries one Documented or Undocumented modifier, and a
	// .ftg-badges row inside.
	if c := strings.Count(got, `ftg-feature-card--documented`); c < 1 {
		t.Errorf("expected at least one ftg-feature-card--documented modifier, got %d in:\n%s", c, got)
	}
	if c := strings.Count(got, `ftg-feature-card--undocumented`); c < 1 {
		t.Errorf("expected at least one ftg-feature-card--undocumented modifier, got %d in:\n%s", c, got)
	}
	if c := strings.Count(got, `<div class="ftg-badges">`); c < 2 {
		t.Errorf("expected per-feature ftg-badges row (one per feature), got %d in:\n%s", c, got)
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
		`<a href="https://example.com/docs/auth" target="_blank" rel="noopener">https://example.com/docs/auth</a>`,
		"old signature",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

// TestRenderFeatureImplementedInAndSymbolsAreCollapsible pins that the
// per-feature page (expanded mode) wraps the file list and symbol list in
// <details>/<summary> so they stay tucked away by default and expand on
// click. The summary text shows the section name + the item count.
func TestRenderFeatureImplementedInAndSymbolsAreCollapsible(t *testing.T) {
	got, err := renderFeature(featureData{
		Name:       "User Auth",
		Layer:      "service",
		UserFacing: true,
		Documented: true,
		Files:      []string{"internal/auth/login.go", "internal/auth/session.go"},
		Symbols:    []string{"Login", "Logout"},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`<details class="ftg-collapse ftg-collapse--files">`,
		`<summary>Implemented in (2)</summary>`,
		`<details class="ftg-collapse ftg-collapse--symbols">`,
		`<summary>Symbols (2)</summary>`,
		"- `internal/auth/login.go`",
		"- `internal/auth/session.go`",
		"- `Login`",
		"- `Logout`",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	// The old plain `## Implemented in` / `## Symbols` markdown headings
	// are replaced by <summary> labels inside <details>. They must not
	// reappear, otherwise the page renders both a heading and a summary.
	for _, bad := range []string{
		"## Implemented in",
		"## Symbols",
	} {
		if strings.Contains(got, bad) {
			t.Errorf("feature page should not retain plain heading %q now that the section is collapsible; got:\n%s", bad, got)
		}
	}
}

// TestRenderMappingPageImplementedInAndSymbolsAreCollapsible mirrors the
// per-feature collapsibility check for the mirror-mode mapping page.
func TestRenderMappingPageImplementedInAndSymbolsAreCollapsible(t *testing.T) {
	got, err := renderMappingPage(mappingPageData{
		Summary: "demo",
		Features: []mappingFeature{{
			Name:       "User Auth",
			Layer:      "service",
			UserFacing: true,
			Documented: true,
			Files:      []string{"internal/auth/login.go", "internal/auth/session.go"},
			Symbols:    []string{"Login", "Logout"},
			DocURLs:    []string{"https://example.com/docs/auth"},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`<details class="ftg-collapse ftg-collapse--files">`,
		`<summary>Implemented in (2)</summary>`,
		`<details class="ftg-collapse ftg-collapse--symbols">`,
		`<summary>Symbols (2)</summary>`,
		"- `internal/auth/login.go`",
		"- `internal/auth/session.go`",
		"- `Login`",
		"- `Logout`",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	for _, bad := range []string{
		"#### Implemented in",
		"#### Symbols",
	} {
		if strings.Contains(got, bad) {
			t.Errorf("mapping page should not retain plain heading %q now that the section is collapsible; got:\n%s", bad, got)
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

// TestRenderFeature_DocumentedOnIsCollapsible pins that the per-feature
// page wraps the "Documented on" list in <details>/<summary>, matching
// the Implemented in / Symbols collapsibles. The summary shows the
// section name plus the count of doc URLs.
func TestRenderFeature_DocumentedOnIsCollapsible(t *testing.T) {
	got, err := renderFeature(featureData{
		Name:       "User Auth",
		Layer:      "service",
		UserFacing: true,
		Documented: true,
		DocURLs:    []string{"https://example.com/docs/auth", "https://example.com/docs/login"},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`<details class="ftg-collapse ftg-collapse--docs">`,
		`<summary>Documented on (2)</summary>`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	if strings.Contains(got, "## Documented on") {
		t.Errorf("feature page should not retain plain heading `## Documented on` now that the section is collapsible; got:\n%s", got)
	}
}

// TestRenderMappingPage_DocumentedOnIsCollapsible mirrors the per-feature
// check on the mirror-mode mapping page. The empty-DocURLs branch keeps
// rendering `_(none)_` (no <details> wrapper) so that the absence is
// visible without requiring the reader to expand anything.
func TestRenderMappingPage_DocumentedOnIsCollapsible(t *testing.T) {
	got, err := renderMappingPage(mappingPageData{
		Summary: "demo",
		Features: []mappingFeature{{
			Name:       "User Auth",
			Layer:      "service",
			UserFacing: true,
			Documented: true,
			DocURLs:    []string{"https://example.com/docs/auth"},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`<details class="ftg-collapse ftg-collapse--docs">`,
		`<summary>Documented on (1)</summary>`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	if strings.Contains(got, "#### Documented on") {
		t.Errorf("mapping page should not retain plain heading `#### Documented on`; got:\n%s", got)
	}
}

// TestRenderMappingPage_DocURLsOpenInNewTab pins that the "Documented on"
// list on the mapping page emits raw <a target="_blank" rel="noopener">
// rather than relying on the Hextra render-link hook to add those
// attributes. The user reports the markdown path was not opening in a
// new tab in practice, so the template now emits the attributes itself.
func TestRenderMappingPage_DocURLsOpenInNewTab(t *testing.T) {
	got, err := renderMappingPage(mappingPageData{
		Summary: "demo",
		Features: []mappingFeature{{
			Name:       "User Auth",
			Layer:      "service",
			UserFacing: true,
			Documented: true,
			DocURLs:    []string{"https://example.com/docs/auth"},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`href="https://example.com/docs/auth"`,
		`target="_blank"`,
		`rel="noopener"`,
		`https://example.com/docs/auth`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

// TestRenderFeature_DocURLsOpenInNewTab mirrors the mapping-page check on
// the per-feature page used in expanded mode.
func TestRenderFeature_DocURLsOpenInNewTab(t *testing.T) {
	got, err := renderFeature(featureData{
		Name:       "User Auth",
		Layer:      "service",
		UserFacing: true,
		Documented: true,
		DocURLs:    []string{"https://example.com/docs/auth"},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`href="https://example.com/docs/auth"`,
		`target="_blank"`,
		`rel="noopener"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

// TestRenderFeature_DriftPageLinkOpensInNewTab pins that the raw-HTML
// "Page" link in the drift findings list carries `target="_blank"` and
// `rel="noopener"`. This link points at the live docs site, so it must
// open in a new tab to keep the report site as the user's anchor.
// Markdown doc URL links elsewhere in the templates flow through Hugo's
// render-link hook (which already does this for external links); the
// drift link is emitted as raw HTML and bypasses that hook, so the
// attributes have to be on the template itself.
func TestRenderFeature_DriftPageLinkOpensInNewTab(t *testing.T) {
	got, err := renderFeature(featureData{
		Name:       "X",
		Documented: true,
		UserFacing: true,
		Layer:      "ui",
		Drift: []driftIssue{{
			Page:  "https://example.com/docs/auth",
			Issue: "old signature",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`href="https://example.com/docs/auth"`,
		`target="_blank"`,
		`rel="noopener"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

// TestRenderFeature_EscapesDriftFields pins that LLM-derived drift fields
// (Issue, PriorityReason, Page) are HTML-escaped before being interpolated
// into the raw-HTML drift cards. text/template does not auto-escape, so a
// `<` or `&` in the model output would otherwise corrupt the rendered page.
func TestRenderFeature_EscapesDriftFields(t *testing.T) {
	got, err := renderFeature(featureData{
		Name:       "X",
		Documented: true,
		UserFacing: true,
		Layer:      "ui",
		Drift: []driftIssue{{
			Page:           "https://example.com/p?a=1&b=<2>",
			Issue:          "signature changed from Foo() to Foo[T any]() <breaking>",
			PriorityReason: "callers must update <T>",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"signature changed from Foo() to Foo[T any]() &lt;breaking&gt;",
		`<p class="ftg-stale-why-text">callers must update &lt;T&gt;</p>`,
		`href="https://example.com/p?a=1&amp;b=&lt;2&gt;"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	for _, bad := range []string{
		"<breaking>",
		"<T>",
	} {
		if strings.Contains(got, bad) {
			t.Errorf("unescaped %q leaked into feature page:\n%s", bad, got)
		}
	}
}

// TestRenderScreenshotPage_EscapesGapFields pins escaping for ShouldShow,
// Alt, Insert, and PriorityReason interpolations on the per-page screenshot
// template. The Quoted passage is rendered inside a markdown code fence so
// goldmark escapes it; the other fields go straight into raw HTML and must
// be escaped at template time.
func TestRenderScreenshotPage_EscapesGapFields(t *testing.T) {
	got, err := renderScreenshotPage(screenshotPageData{
		PageURL: "https://example.com/p",
		Title:   "P",
		Gaps: []screenshotGap{{
			Quoted:         "open dashboard",
			ShouldShow:     "the <Save> button",
			Alt:            "Save & Apply",
			Insert:         "after <h1>",
			PriorityReason: "user-impact <flow>",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"the &lt;Save&gt; button",
		"<code>Save &amp; Apply</code>",
		"after &lt;h1&gt;",
		"user-impact &lt;flow&gt;",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	for _, bad := range []string{
		"<Save>",
		"<h1>",
		"<flow>",
	} {
		if strings.Contains(got, bad) {
			t.Errorf("unescaped %q leaked into screenshot page:\n%s", bad, got)
		}
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
		"<summary>Implemented in (2)</summary>",
		"- `internal/auth/login.go`",
		"- `internal/auth/session.go`",
		"<summary>Symbols (2)</summary>",
		"- `Login`",
		"- `Logout`",
		"<summary>Documented on (1)</summary>",
		`- <a href="https://example.com/docs/auth" target="_blank" rel="noopener">https://example.com/docs/auth</a>`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	// The Layer / User-facing / Documentation status bullet list was a
	// duplicate of the badges row at the top of each feature card; the
	// badges are the canonical surface, so the bullets must be gone.
	for _, bad := range []string{
		"- **Layer:**",
		"- **User-facing:**",
		"- **Documentation status:**",
	} {
		if strings.Contains(got, bad) {
			t.Errorf("mapping page should no longer contain duplicate bullet %q (badges already cover it); got:\n%s", bad, got)
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
	if strings.Contains(got, "ftg-collapse--files") {
		t.Errorf("Implemented in should be omitted when Files is empty; got:\n%s", got)
	}
	if strings.Contains(got, "ftg-collapse--symbols") {
		t.Errorf("Symbols should be omitted when Symbols is empty; got:\n%s", got)
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
		`<div class="ftg-priority ftg-priority--small">`,
		`<div class="ftg-shot-list">`,
		`<div class="ftg-shot ftg-shot--small">`,
		"```markdown",
		"open the dashboard",
		"click **Save**",
		`<span class="ftg-shot-label">Should show</span>the dashboard view`,
		`<span class="ftg-shot-label">Alt text</span><code>dashboard</code>`,
		`<span class="ftg-shot-label">Insert</span>after first paragraph`,
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

// TestRenderHugoConfigIncludesParamsDescription pins that the rendered Hugo
// config carries a `[params]` block with a `description = "..."` so Hextra's
// head partial has a real string to feed into `<meta description>` and the
// OpenGraph fallback. Without it, the homepage description falls back to
// `.Summary`, which on report pages is auto-generated junk.
func TestRenderHugoConfigIncludesParamsDescription(t *testing.T) {
	got, err := renderHugoConfig(hugoConfigData{
		Title:       "Find the Gaps — myrepo",
		Description: "Find the Gaps documentation audit report for myrepo.",
		Mode:        ModeMirror,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "[params]") {
		t.Errorf("expected [params] section in:\n%s", got)
	}
	if !strings.Contains(got, `description = "Find the Gaps documentation audit report for myrepo."`) {
		t.Errorf("expected params.description with rendered project name; got:\n%s", got)
	}
}

// TestRenderHugoConfigEscapesDescription pins that a description containing
// quotes / backslashes is rendered as a valid TOML basic string. The Go
// `%q` verb already does this; the test exists to catch a future change
// that swaps in raw interpolation.
func TestRenderHugoConfigEscapesDescription(t *testing.T) {
	got, err := renderHugoConfig(hugoConfigData{
		Title:       "x",
		Description: `report for "weird\name"`,
		Mode:        ModeMirror,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, `description = "report for \"weird\\name\""`) {
		t.Errorf("expected TOML-escaped description; got:\n%s", got)
	}
}

// TestRenderHomeHasFrontmatterDescription pins that the home page's TOML
// frontmatter includes a `description` line derived from the product
// summary. Hextra's head partial threads `.Description` into both
// `<meta description>` and `og:description`.
func TestRenderHomeHasFrontmatterDescription(t *testing.T) {
	in := homeData{
		ProjectName: "demo",
		GeneratedAt: time.Date(2026, 4, 24, 10, 0, 0, 0, time.UTC),
		Summary:     "A small CLI demo for end-to-end testing.",
		Mode:        ModeMirror,
	}
	got, err := renderHome(in)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, `description = "A small CLI demo for end-to-end testing."`) {
		t.Errorf("expected description derived from summary in frontmatter; got:\n%s", got)
	}
}

// TestRenderHomeDescriptionFallsBack pins that, when the analyzer's summary
// is empty, the home frontmatter uses a sensible project-named fallback so
// `<meta description>` is never blank.
func TestRenderHomeDescriptionFallsBack(t *testing.T) {
	in := homeData{
		ProjectName: "myrepo",
		GeneratedAt: time.Date(2026, 4, 24, 10, 0, 0, 0, time.UTC),
		Summary:     "",
		Mode:        ModeMirror,
	}
	got, err := renderHome(in)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, `description = "Find the Gaps documentation audit report for myrepo."`) {
		t.Errorf("expected fallback description with project name; got:\n%s", got)
	}
}

// TestRenderHomeDescriptionNormalizesWhitespace pins that newlines and
// runs of whitespace in the source summary are collapsed to single spaces
// in the rendered description (otherwise TOML basic-string interpolation
// breaks on embedded newlines).
func TestRenderHomeDescriptionNormalizesWhitespace(t *testing.T) {
	in := homeData{
		ProjectName: "demo",
		GeneratedAt: time.Date(2026, 4, 24, 10, 0, 0, 0, time.UTC),
		Summary:     "Line one.\n\nLine   two\twith\ttabs.",
		Mode:        ModeMirror,
	}
	got, err := renderHome(in)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, `description = "Line one. Line two with tabs."`) {
		t.Errorf("expected collapsed whitespace in description; got:\n%s", got)
	}
}

// TestRenderFeatureHasFrontmatterDescription pins that per-feature pages
// (expanded mode) carry a description derived from the feature description.
func TestRenderFeatureHasFrontmatterDescription(t *testing.T) {
	got, err := renderFeature(featureData{
		Name:        "User Auth",
		Description: "Login and session management.",
		Layer:       "service",
		UserFacing:  true,
		Documented:  true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, `description = "Login and session management."`) {
		t.Errorf("expected description from feature description; got:\n%s", got)
	}
}

// TestRenderFeatureDescriptionFallsBack pins the per-feature fallback when
// the LLM-derived feature description is blank.
func TestRenderFeatureDescriptionFallsBack(t *testing.T) {
	got, err := renderFeature(featureData{
		Name:       "User Auth",
		Layer:      "service",
		UserFacing: true,
		Documented: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, `description = "Documentation status for User Auth."`) {
		t.Errorf("expected fallback description; got:\n%s", got)
	}
}

// TestRenderFeaturesIndexHasFrontmatterDescription pins a static description
// on the features index page.
func TestRenderFeaturesIndexHasFrontmatterDescription(t *testing.T) {
	got, err := renderFeaturesIndex(featuresIndexData{Rows: nil})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, `description = "Index of detected product features and their documentation status."`) {
		t.Errorf("expected static description on features index; got:\n%s", got)
	}
}

// TestRenderScreenshotPageHasFrontmatterDescription pins that per-page
// screenshot detail pages carry a description that names the page they
// audit. The page Title is the document title; the description uses it.
func TestRenderScreenshotPageHasFrontmatterDescription(t *testing.T) {
	got, err := renderScreenshotPage(screenshotPageData{
		PageURL: "https://example.com/docs/start",
		Title:   "Quickstart",
		Gaps:    []screenshotGap{{Quoted: "x", ShouldShow: "y", Alt: "z", Insert: "w"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, `description = "Screenshot suggestions for Quickstart."`) {
		t.Errorf("expected per-page screenshot description; got:\n%s", got)
	}
}
