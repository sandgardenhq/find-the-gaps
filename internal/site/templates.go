package site

import (
	"bytes"
	"fmt"
	"io/fs"
	"text/template"
	"time"
)

// hugoConfigData drives renderHugoConfig.
//
// Mode is translated into the Expanded boolean flag at render time so that
// templates can compare against named semantics (`{{ if .Expanded }}`) rather
// than rely on integer values of the Mode iota — which would silently break
// if Mode constants are ever reordered.
type hugoConfigData struct {
	Title          string
	Mode           Mode
	ScreenshotsRan bool
}

// hugoConfigView is the data shape templates actually see. It carries the
// derived Expanded flag so templates never branch on raw Mode values.
type hugoConfigView struct {
	Title          string
	Expanded       bool
	ScreenshotsRan bool
}

// tmpl is the parsed embedded template set. Parsing happens once at package
// init; if the embedded templates fail to parse, that is a programmer error
// and we panic so it surfaces at startup rather than at first use.
var tmpl = mustParseTemplates(templatesFS)

func mustParseTemplates(efs fs.FS) *template.Template {
	t, err := parseTemplates(efs)
	if err != nil {
		panic(fmt.Sprintf("parse embedded templates: %v", err))
	}
	return t
}

func parseTemplates(efs fs.FS) (*template.Template, error) {
	return template.New("site").Funcs(template.FuncMap{
		// add helpers here as needed
	}).ParseFS(efs, "assets/templates/*.tmpl")
}

func renderHugoConfig(data hugoConfigData) (string, error) {
	view := hugoConfigView{
		Title:          data.Title,
		Expanded:       data.Mode == ModeExpanded,
		ScreenshotsRan: data.ScreenshotsRan,
	}
	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, "hugo.toml.tmpl", view); err != nil {
		return "", fmt.Errorf("render hugo.toml: %w", err)
	}
	return buf.String(), nil
}

// homeData drives renderHome.
//
// Mode is translated into the Expanded boolean flag at render time so that
// templates can compare against named semantics (`{{ if .Expanded }}`) rather
// than rely on integer values of the Mode iota — which would silently break
// if Mode constants are ever reordered.
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

// homeView is the data shape templates actually see. It carries the derived
// Expanded flag so templates never branch on raw Mode values.
type homeView struct {
	ProjectName           string
	GeneratedAt           time.Time
	Summary               string
	FeatureCount          int
	UndocumentedUserCount int
	DriftCount            int
	ScreenshotGapCount    int
	ScreenshotsRan        bool
	Expanded              bool
}

func renderHome(d homeData) (string, error) {
	view := homeView{
		ProjectName:           d.ProjectName,
		GeneratedAt:           d.GeneratedAt,
		Summary:               d.Summary,
		FeatureCount:          d.FeatureCount,
		UndocumentedUserCount: d.UndocumentedUserCount,
		DriftCount:            d.DriftCount,
		ScreenshotGapCount:    d.ScreenshotGapCount,
		ScreenshotsRan:        d.ScreenshotsRan,
		Expanded:              d.Mode == ModeExpanded,
	}
	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, "home.md.tmpl", view); err != nil {
		return "", fmt.Errorf("render home: %w", err)
	}
	return buf.String(), nil
}

// driftIssue describes a single drift finding linking a doc page to an issue.
type driftIssue struct {
	Page  string
	Issue string
}

// featureData drives renderFeature. Unlike the home/config templates, this
// template branches purely on per-feature attributes (Documented, UserFacing,
// presence of Files/Symbols/etc.) so there is no Mode-derived view layer.
type featureData struct {
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

// mappingFeature is one row in the mirror-mode mapping page.
type mappingFeature struct {
	Name        string
	Description string
	Layer       string
	UserFacing  bool
	Documented  bool
	Files       []string
	Symbols     []string
	DocURLs     []string
}

type mappingPageData struct {
	Summary  string
	Features []mappingFeature
}

func renderMappingPage(d mappingPageData) (string, error) {
	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, "mapping_page.md.tmpl", d); err != nil {
		return "", fmt.Errorf("render mapping_page: %w", err)
	}
	return buf.String(), nil
}

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

// screenshotsMirrorPage groups one or more gaps under the page they belong to.
type screenshotsMirrorPage struct {
	PageURL string
	Gaps    []screenshotGap
}

// screenshotsMirrorData drives renderScreenshotsMirror — the mirror-mode
// single-page screenshots.md rendered from data so the website can use a
// fenced passage + Hextra callout layout while the standalone reporter file
// stays unchanged.
type screenshotsMirrorData struct {
	Pages []screenshotsMirrorPage
}

func renderScreenshotsMirror(d screenshotsMirrorData) (string, error) {
	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, "screenshots_page_mirror.md.tmpl", d); err != nil {
		return "", fmt.Errorf("render screenshots_page_mirror: %w", err)
	}
	return buf.String(), nil
}

func renderScreenshotPage(d screenshotPageData) (string, error) {
	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, "screenshot_page.md.tmpl", d); err != nil {
		return "", fmt.Errorf("render screenshot_page: %w", err)
	}
	return buf.String(), nil
}
