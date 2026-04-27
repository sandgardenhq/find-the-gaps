package site

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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

// HugoBin is the executable name shelled out for the build. Override in tests.
var HugoBin = "hugo"

// ErrHugoMissing is returned when the `hugo` binary cannot be located on $PATH.
var ErrHugoMissing = errors.New("hugo not found on PATH")

// Build materializes a Hugo source tree and shells out to `hugo` to produce
// the static site at <opts.ProjectDir>/site/.
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
