package site

import (
	"bytes"
	"fmt"
	"io/fs"
	"text/template"
	"time"

	"github.com/sandgardenhq/find-the-gaps/internal/analyzer"
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
	Page           string
	Issue          string
	Priority       analyzer.Priority
	PriorityReason string
}

// driftBucket holds one priority's worth of drift findings on a feature page.
// The expanded-mode feature template iterates buckets in the canonical
// Large -> Medium -> Small order so the most important findings appear first.
type driftBucket struct {
	Heading string // "Large" / "Medium" / "Small"
	Issues  []driftIssue
}

// bucketDrift returns drift findings grouped by priority in canonical order,
// omitting empty buckets and preserving the input order within each bucket.
func bucketDrift(issues []driftIssue) []driftBucket {
	if len(issues) == 0 {
		return nil
	}
	order := []analyzer.Priority{analyzer.PriorityLarge, analyzer.PriorityMedium, analyzer.PrioritySmall}
	headings := map[analyzer.Priority]string{
		analyzer.PriorityLarge:  "Large",
		analyzer.PriorityMedium: "Medium",
		analyzer.PrioritySmall:  "Small",
	}
	var out []driftBucket
	for _, p := range order {
		var bucket []driftIssue
		for _, i := range issues {
			if i.Priority == p {
				bucket = append(bucket, i)
			}
		}
		if len(bucket) > 0 {
			out = append(out, driftBucket{Heading: headings[p], Issues: bucket})
		}
	}
	return out
}

// featureData drives renderFeature. Unlike the home/config templates, this
// template branches purely on per-feature attributes (Documented, UserFacing,
// presence of Files/Symbols/etc.) so there is no Mode-derived view layer.
type featureData struct {
	Name         string
	Description  string
	Layer        string
	UserFacing   bool
	Documented   bool
	Files        []string
	Symbols      []string
	DocURLs      []string
	Drift        []driftIssue
	DriftBuckets []driftBucket
}

func renderFeature(d featureData) (string, error) {
	// Callers that only populate Drift get auto-bucketed so the
	// priority-aware template still renders every issue. Real callers
	// (materializeExpanded) populate DriftBuckets directly.
	if len(d.DriftBuckets) == 0 && len(d.Drift) > 0 {
		if buckets := bucketDrift(d.Drift); len(buckets) > 0 {
			d.DriftBuckets = buckets
		} else {
			d.DriftBuckets = []driftBucket{{Heading: "All", Issues: d.Drift}}
		}
	}
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
	Quoted         string
	ShouldShow     string
	Alt            string
	Insert         string
	Priority       analyzer.Priority
	PriorityReason string
}

// screenshotBucket holds one priority's worth of gaps on a screenshot page.
type screenshotBucket struct {
	Heading string
	Gaps    []screenshotGap
}

func bucketScreenshotGaps(gaps []screenshotGap) []screenshotBucket {
	if len(gaps) == 0 {
		return nil
	}
	order := []analyzer.Priority{analyzer.PriorityLarge, analyzer.PriorityMedium, analyzer.PrioritySmall}
	headings := map[analyzer.Priority]string{
		analyzer.PriorityLarge:  "Large",
		analyzer.PriorityMedium: "Medium",
		analyzer.PrioritySmall:  "Small",
	}
	var out []screenshotBucket
	for _, p := range order {
		var bucket []screenshotGap
		for _, g := range gaps {
			if g.Priority == p {
				bucket = append(bucket, g)
			}
		}
		if len(bucket) > 0 {
			out = append(out, screenshotBucket{Heading: headings[p], Gaps: bucket})
		}
	}
	return out
}

type screenshotPageData struct {
	PageURL string
	Title   string
	Gaps    []screenshotGap
	Buckets []screenshotBucket
}

func renderScreenshotPage(d screenshotPageData) (string, error) {
	// Callers that only populate Gaps get auto-bucketed into a single
	// catch-all bucket so the priority-aware template still renders
	// every gap. Real callers (materializeExpanded) populate Buckets
	// directly via bucketScreenshotGaps.
	if len(d.Buckets) == 0 && len(d.Gaps) > 0 {
		if buckets := bucketScreenshotGaps(d.Gaps); len(buckets) > 0 {
			d.Buckets = buckets
		} else {
			d.Buckets = []screenshotBucket{{Heading: "All", Gaps: d.Gaps}}
		}
	}
	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, "screenshot_page.md.tmpl", d); err != nil {
		return "", fmt.Errorf("render screenshot_page: %w", err)
	}
	return buf.String(), nil
}
