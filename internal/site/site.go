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
	// ModeMirror produces a site whose top-level pages mirror the markdown reports.
	ModeMirror Mode = iota
	// ModeExpanded produces a site with a feature index and per-feature pages.
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
