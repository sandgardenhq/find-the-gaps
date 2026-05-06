package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/log"
	"github.com/spf13/cobra"

	"github.com/sandgardenhq/find-the-gaps/internal/analyzer"
	"github.com/sandgardenhq/find-the-gaps/internal/reporter"
	"github.com/sandgardenhq/find-the-gaps/internal/site"
	"github.com/sandgardenhq/find-the-gaps/internal/spider"
)

// newRenderCmd builds the `ftg render` subcommand. It re-emits mapping.md /
// gaps.md / screenshots.md and rebuilds the Hugo site at <projectDir>/site/
// from cached findings — no LLM calls, no network. Use this when the
// reporter or site templates change and you want to pick up the new look
// without paying for a full re-analysis.
//
// Required cache files under <cacheDir>/<project>/:
//   - docs/index.json    (spider index — gives ProductSummary)
//   - featuremap.json    (code feature map)
//   - docsfeaturemap.json (docs feature map)
//   - drift.json         (drift findings) — optional; treated as empty if missing
//   - screenshots.json   (screenshot pass) — optional; the screenshot section
//     is regenerated only when present
func newRenderCmd() *cobra.Command {
	var (
		repoPath       string
		cacheDir       string
		projectFlag    string
		siteMode       string
		keepSiteSource bool
	)

	cmd := &cobra.Command{
		Use:   "render",
		Short: "Re-render the report site from cached findings (no LLM, no network).",
		Long: "Re-emit mapping.md / gaps.md / screenshots.md and rebuild the Hugo " +
			"site from artifacts already on disk. Use after changing the reporter " +
			"templates or site CSS to pick up the new look without re-running analysis.",
		RunE: func(cc *cobra.Command, _ []string) error {
			if projectFlag != "" && cc.Flags().Changed("repo") {
				return fmt.Errorf("--project and --repo are mutually exclusive")
			}

			projectDir, projectName, err := resolveRenderProjectDir(cc, cacheDir, repoPath, projectFlag)
			if err != nil {
				return err
			}

			var siteModeVal site.Mode
			switch siteMode {
			case "mirror":
				siteModeVal = site.ModeMirror
			case "expanded":
				siteModeVal = site.ModeExpanded
			default:
				return fmt.Errorf("invalid --site-mode %q (want \"mirror\" or \"expanded\")", siteMode)
			}

			// Spider index — source of truth for productSummary.
			idx, err := spider.LoadIndex(filepath.Join(projectDir, "docs"))
			if err != nil {
				return fmt.Errorf("load spider index: %w", err)
			}
			desc, feats := idx.ProductInfo()
			if desc == "" && len(feats) == 0 {
				return fmt.Errorf("no product summary cached at %s/docs/index.json — run `ftg analyze` first", projectDir)
			}
			productSummary := analyzer.ProductSummary{Description: desc, Features: feats}

			featureMap, err := loadCachedFeatureMap(filepath.Join(projectDir, "featuremap.json"))
			if err != nil {
				return fmt.Errorf("%w (run `ftg analyze` first)", err)
			}

			docsFeatureMap, err := loadCachedDocsFeatureMap(filepath.Join(projectDir, "docsfeaturemap.json"))
			if err != nil {
				return fmt.Errorf("%w (run `ftg analyze` first)", err)
			}

			docCoveredFeatures := make([]string, 0, len(docsFeatureMap))
			for _, entry := range docsFeatureMap {
				if len(entry.Pages) > 0 {
					docCoveredFeatures = append(docCoveredFeatures, entry.Feature)
				}
			}

			// Drift cache is optional — a project that's never had drift
			// findings still renders cleanly with an empty Stale Documentation
			// section.
			var driftFindings []analyzer.DriftFinding
			if file, ok := loadDriftCacheFile(filepath.Join(projectDir, "drift.json")); ok {
				cachedMap := driftCacheEntriesToMap(file.Entries)
				driftFindings = driftFindingsFromCache(cachedMap, featureMap)
			}

			// Screenshots are gated on a previous experimental run.
			screenshotResult, screenshotsRan, err := reporter.ReadScreenshotsJSON(projectDir)
			if err != nil {
				return fmt.Errorf("load screenshots.json: %w", err)
			}

			log.Infof("rendering reports for %s", projectName)
			if err := reporter.WriteMapping(projectDir, productSummary, featureMap, docsFeatureMap); err != nil {
				return fmt.Errorf("write mapping: %w", err)
			}
			if err := reporter.WriteGaps(projectDir, featureMap, docCoveredFeatures, driftFindings); err != nil {
				return fmt.Errorf("write gaps: %w", err)
			}
			if screenshotsRan {
				if err := reporter.WriteScreenshots(projectDir, screenshotResult); err != nil {
					return fmt.Errorf("write screenshots: %w", err)
				}
				if err := reporter.WriteScreenshotsJSON(projectDir, screenshotResult); err != nil {
					return fmt.Errorf("write screenshots.json: %w", err)
				}
			}

			log.Infof("building site at %s/site/", projectDir)
			err = site.Build(cc.Context(),
				site.Inputs{
					Summary:        productSummary,
					Mapping:        featureMap,
					DocsMap:        docsFeatureMap,
					AllDocFeatures: docCoveredFeatures,
					Drift:          driftFindings,
					Screenshots:    screenshotResult.MissingGaps,
					ImageIssues:    screenshotResult.ImageIssues,
					ScreenshotsRan: screenshotsRan,
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
					return fmt.Errorf("hugo not found on PATH; install via `brew install hugo` (or see https://github.com/gohugoio/hugo/releases)")
				}
				return fmt.Errorf("build site: %w", err)
			}

			_, _ = fmt.Fprintf(cc.OutOrStdout(), "rendered %s/site/\n", projectDir)
			return nil
		},
	}

	cmd.Flags().StringVar(&repoPath, "repo", ".", "path to the repository whose project should be rendered")
	cmd.Flags().StringVar(&cacheDir, "cache-dir", ".find-the-gaps", "base directory containing analyze output")
	cmd.Flags().StringVar(&projectFlag, "project", "", "name of an analyzed project under <cache-dir>/; bypasses the picker")
	cmd.Flags().StringVar(&siteMode, "site-mode", "mirror", "site content shape: \"mirror\" or \"expanded\"")
	cmd.Flags().BoolVar(&keepSiteSource, "keep-site-source", true, "preserve generated Hugo source at <projectDir>/site-src/ (pass --keep-site-source=false to discard)")

	return cmd
}

// resolveRenderProjectDir picks the project directory to render. Order of
// precedence mirrors `serve`:
//  1. --project NAME           → use <cacheDir>/NAME
//  2. --repo PATH (explicit)   → use <cacheDir>/base(PATH)
//  3. neither flag set         → scan <cacheDir>; auto-pick / prompt / error
//
// Returns the absolute project directory and its short name. The directory
// must contain a `docs/` subdir (the spider cache); otherwise it isn't an
// analyzed project.
func resolveRenderProjectDir(cc *cobra.Command, cacheDir, repoPath, projectFlag string) (projectDir, projectName string, err error) {
	if projectFlag != "" {
		dir := filepath.Join(cacheDir, projectFlag)
		if !looksLikeAnalyzedProject(dir) {
			return "", "", fmt.Errorf("no analyzed project at %s — check --project or run `ftg analyze` first", dir)
		}
		return dir, projectFlag, nil
	}

	if cc.Flags().Changed("repo") {
		absRepo, err := filepath.Abs(repoPath)
		if err != nil {
			return "", "", fmt.Errorf("resolve repo path: %w", err)
		}
		name := filepath.Base(absRepo)
		dir := filepath.Join(cacheDir, name)
		if !looksLikeAnalyzedProject(dir) {
			return "", "", fmt.Errorf("no analyzed project at %s — run `ftg analyze` first", dir)
		}
		return dir, name, nil
	}

	projects, err := ListAnalyzedProjects(cacheDir)
	if err != nil {
		return "", "", fmt.Errorf("scan cache dir: %w", err)
	}
	switch len(projects) {
	case 0:
		return "", "", fmt.Errorf("no analyzed projects found in %s — run `ftg analyze` first", cacheDir)
	case 1:
		_, _ = fmt.Fprintf(cc.OutOrStdout(), "found one project: %s\n", projects[0].Name)
		return filepath.Join(cacheDir, projects[0].Name), projects[0].Name, nil
	default:
		if !isInteractive() {
			names := make([]string, len(projects))
			for i, p := range projects {
				names[i] = p.Name
			}
			return "", "", fmt.Errorf("multiple analyzed projects found in %s; re-run with --project NAME (one of: %s)",
				cacheDir, strings.Join(names, ", "))
		}
		chosen, err := pickProject(projects)
		if err != nil {
			return "", "", err
		}
		return filepath.Join(cacheDir, chosen.Name), chosen.Name, nil
	}
}

// looksLikeAnalyzedProject reports whether dir resembles a previously-
// analyzed project. We require the `docs/` spider cache subdirectory: a
// project that ran `analyze` always has it, and its absence is the signal
// that no analysis has happened here yet. The site/ subdir is intentionally
// not required — render's job is to (re)build it.
func looksLikeAnalyzedProject(dir string) bool {
	info, err := os.Stat(filepath.Join(dir, "docs"))
	return err == nil && info.IsDir()
}

// loadCachedFeatureMap loads featuremap.json without the `wantFeatures`
// validation that loadFeatureMapCache performs in the analyze hot path.
// Render cannot reproduce the scan that built the cache, so it trusts the
// stored feature set as-is. A missing file is a hard error: the user must
// run `ftg analyze` at least once before rendering.
func loadCachedFeatureMap(path string) (analyzer.FeatureMap, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("featuremap.json not found at %s", path)
	}
	if err != nil {
		return nil, fmt.Errorf("read featuremap.json: %w", err)
	}
	var cache featureMapCacheFile
	if err := json.Unmarshal(data, &cache); err != nil {
		return nil, fmt.Errorf("parse featuremap.json: %w", err)
	}
	fm := make(analyzer.FeatureMap, 0, len(cache.Entries))
	for _, e := range cache.Entries {
		files := e.Files
		if files == nil {
			files = []string{}
		}
		symbols := e.Symbols
		if symbols == nil {
			symbols = []string{}
		}
		fm = append(fm, analyzer.FeatureEntry{
			Feature: e.Feature,
			Files:   files,
			Symbols: symbols,
		})
	}
	return fm, nil
}

// loadCachedDocsFeatureMap mirrors loadCachedFeatureMap for the docs side.
func loadCachedDocsFeatureMap(path string) (analyzer.DocsFeatureMap, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("docsfeaturemap.json not found at %s", path)
	}
	if err != nil {
		return nil, fmt.Errorf("read docsfeaturemap.json: %w", err)
	}
	var cache docsFeatureMapCacheFile
	if err := json.Unmarshal(data, &cache); err != nil {
		return nil, fmt.Errorf("parse docsfeaturemap.json: %w", err)
	}
	fm := make(analyzer.DocsFeatureMap, 0, len(cache.Entries))
	for _, e := range cache.Entries {
		pages := e.Pages
		if pages == nil {
			pages = []string{}
		}
		fm = append(fm, analyzer.DocsFeatureEntry{Feature: e.Feature, Pages: pages})
	}
	return fm, nil
}
