package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/log"
	"github.com/sandgardenhq/find-the-gaps/internal/analyzer"
	"github.com/sandgardenhq/find-the-gaps/internal/reporter"
	"github.com/sandgardenhq/find-the-gaps/internal/scanner"
	"github.com/sandgardenhq/find-the-gaps/internal/scanner/ignore"
	"github.com/sandgardenhq/find-the-gaps/internal/site"
	"github.com/sandgardenhq/find-the-gaps/internal/spider"
	"github.com/spf13/cobra"
	"golang.org/x/text/language"
	"golang.org/x/text/message"
)

var summaryPrinter = message.NewPrinter(language.English)

type bothMapsResult struct {
	codeMap analyzer.FeatureMap
	err     error
}

type docsMapsResult struct {
	docsMap analyzer.DocsFeatureMap
	err     error
}

// runBothMaps executes MapFeaturesToCode and MapFeaturesToDocs concurrently.
// It returns when both complete. onCodeBatch and onDocsPage are passed through
// to their respective mappers for intermediate persistence; either may be nil.
func runBothMaps(
	ctx context.Context,
	tiering analyzer.LLMTiering,
	features []analyzer.CodeFeature,
	scan *scanner.ProjectScan,
	pages map[string]string,
	workers int,
	docsTokenBudget int,
	filesOnly bool,
	onCodeBatch analyzer.MapProgressFunc,
	onDocsPage analyzer.DocsMapProgressFunc,
) (analyzer.FeatureMap, analyzer.DocsFeatureMap, error) {
	codeCh := make(chan bothMapsResult, 1)
	docsCh := make(chan docsMapsResult, 1)

	featureNames := make([]string, len(features))
	for i, f := range features {
		featureNames[i] = f.Name
	}

	go func() {
		fm, err := analyzer.MapFeaturesToCode(ctx, tiering, features, scan, analyzer.MapperTokenBudget, filesOnly, onCodeBatch)
		codeCh <- bothMapsResult{fm, err}
	}()

	go func() {
		fm, err := analyzer.MapFeaturesToDocs(ctx, tiering, featureNames, pages, workers, docsTokenBudget, onDocsPage)
		docsCh <- docsMapsResult{fm, err}
	}()

	codeRes := <-codeCh
	docsRes := <-docsCh

	if codeRes.err != nil {
		return nil, nil, codeRes.err
	}
	if docsRes.err != nil {
		return nil, nil, docsRes.err
	}
	return codeRes.codeMap, docsRes.docsMap, nil
}

func newAnalyzeCmd() *cobra.Command {
	var (
		docsURL             string
		repoPath            string
		cacheDir            string
		workers             int
		noCache             bool
		noSymbols           bool
		llmSmall            string
		llmTypical          string
		llmLarge            string
		skipScreenshotCheck bool
		siteMode            string
		noSite              bool
		keepSiteSource      bool
	)

	cmd := &cobra.Command{
		Use:   "analyze",
		Short: "Analyze a codebase against its documentation site for gaps.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := context.Background()

			var siteModeVal site.Mode
			switch siteMode {
			case "mirror":
				siteModeVal = site.ModeMirror
			case "expanded":
				siteModeVal = site.ModeExpanded
			default:
				return fmt.Errorf("invalid --site-mode %q (want \"mirror\" or \"expanded\")", siteMode)
			}

			absRepo, err := filepath.Abs(repoPath)
			if err != nil {
				return fmt.Errorf("resolve repo path: %w", err)
			}
			projectName := filepath.Base(absRepo)
			projectDir := filepath.Join(cacheDir, projectName)

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
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), formatScanSummary(stats))
			}

			if docsURL == "" {
				return nil
			}

			if llmSmall == "" {
				llmSmall = os.Getenv("FIND_THE_GAPS_LLM_SMALL")
			}
			if llmTypical == "" {
				llmTypical = os.Getenv("FIND_THE_GAPS_LLM_TYPICAL")
			}
			if llmLarge == "" {
				llmLarge = os.Getenv("FIND_THE_GAPS_LLM_LARGE")
			}
			tiering, err := newLLMTiering(llmSmall, llmTypical, llmLarge)
			if err != nil {
				return fmt.Errorf("LLM client: %w", err)
			}
			defer logLLMCallCounts(tiering)

			log.Infof("crawling %s", docsURL)
			docsDir := filepath.Join(projectDir, "docs")
			spiderOpts := spider.Options{
				CacheDir: docsDir,
				Workers:  workers,
			}
			pages, err := spider.Crawl(docsURL, spiderOpts, spider.MdfetchFetcher(spiderOpts))
			if err != nil {
				return fmt.Errorf("crawl failed: %w", err)
			}
			log.Debug("crawl complete", "pages", len(pages))

			idx, err := spider.LoadIndex(docsDir)
			if err != nil {
				return fmt.Errorf("load index: %w", err)
			}

			// Analyze each page; skip cached results.
			log.Infof("analyzing %d pages...", len(pages))
			var analyses []analyzer.PageAnalysis
			freshCount := 0
			pageNum := 0
			for url, filePath := range pages {
				if summary, features, ok := idx.Analysis(url); ok {
					log.Debug("page cache hit", "url", url)
					analyses = append(analyses, analyzer.PageAnalysis{
						URL:      url,
						Summary:  summary,
						Features: features,
					})
					continue
				}
				content, readErr := os.ReadFile(filePath)
				if readErr != nil {
					continue
				}
				pageNum++
				log.Infof("  [%d] %s", pageNum, url)
				pa, analyzeErr := analyzer.AnalyzePage(ctx, tiering, url, string(content))
				if analyzeErr != nil {
					log.Warnf("skipping %s: %v", url, analyzeErr)
					continue
				}
				if recErr := idx.RecordAnalysis(url, pa.Summary, pa.Features); recErr != nil {
					return fmt.Errorf("record analysis: %w", recErr)
				}
				analyses = append(analyses, pa)
				freshCount++
			}

			if len(analyses) == 0 {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "scanned %d files, fetched %d pages, 0 pages analyzed\n",
					len(scan.Files), len(pages))
				return nil
			}

			// Use cached synthesis when all pages were cache hits.
			log.Infof("synthesizing product from %d pages...", len(analyses))
			var productSummary analyzer.ProductSummary
			cachedDesc, cachedFeatures := idx.ProductInfo()
			if freshCount == 0 && cachedDesc != "" {
				productSummary = analyzer.ProductSummary{
					Description: cachedDesc,
					Features:    cachedFeatures,
				}
			} else {
				productSummary, err = analyzer.SynthesizeProduct(ctx, tiering, analyses)
				if err != nil {
					return fmt.Errorf("synthesize: %w", err)
				}
				if err := idx.SetProductSummary(productSummary.Description, productSummary.Features); err != nil {
					return fmt.Errorf("save product summary: %w", err)
				}
			}

			// Extract the canonical feature list from CODE. These are the features
			// the codebase actually implements — used as the source of truth for gap analysis.
			codeFeaturesPath := filepath.Join(projectDir, "codefeatures.json")
			var codeFeatures []analyzer.CodeFeature
			codeFeaturesCached := !noCache && func() bool {
				if cached, ok := loadCodeFeaturesCache(codeFeaturesPath, scan); ok {
					log.Infof("using cached code features (%d features)", len(cached))
					codeFeatures = cached
					return true
				}
				return false
			}()

			if !codeFeaturesCached {
				log.Infof("extracting features from code...")
				codeFeatures, err = analyzer.ExtractFeaturesFromCode(ctx, tiering, scan)
				if err != nil {
					return fmt.Errorf("extract code features: %w", err)
				}
				log.Debugf("extracted features: %v", codeFeatures)
				log.Infof("extracted %d features from code", len(codeFeatures))
				if err := saveCodeFeaturesCache(codeFeaturesPath, scan, codeFeatures); err != nil {
					return fmt.Errorf("save code features cache: %w", err)
				}
			}

			featureMapCachePath := filepath.Join(projectDir, "featuremap.json")
			docsFeatureMapCachePath := filepath.Join(projectDir, "docsfeaturemap.json")

			// codeFeatureNames extracts plain name strings for APIs that still accept []string.
			codeFeatureNames := make([]string, len(codeFeatures))
			for i, f := range codeFeatures {
				codeFeatureNames[i] = f.Name
			}

			var featureMap analyzer.FeatureMap
			var docsFeatureMap analyzer.DocsFeatureMap

			codeMapCached := !noCache && func() bool {
				if cached, ok := loadFeatureMapCache(featureMapCachePath, codeFeatures); ok {
					log.Infof("using cached feature map (%d features)", len(cached))
					featureMap = cached
					return true
				}
				return false
			}()

			docsMapCached := !noCache && func() bool {
				if cached, ok := loadDocsFeatureMapCache(docsFeatureMapCachePath, codeFeatureNames); ok {
					log.Infof("using cached docs feature map (%d features)", len(cached))
					docsFeatureMap = cached
					return true
				}
				return false
			}()

			if !codeMapCached || !docsMapCached {
				log.Infof("mapping %d features across code and docs in parallel...", len(codeFeatures))
				// Only wire batch callbacks for the maps that are actually being computed;
				// passing a non-nil callback for a cached map would overwrite the cache file.
				var codeBatchFn analyzer.MapProgressFunc
				if !codeMapCached {
					codeBatchFn = func(partial analyzer.FeatureMap) error {
						return saveFeatureMapCache(featureMapCachePath, codeFeatures, partial)
					}
				}
				var docsBatchFn analyzer.DocsMapProgressFunc
				if !docsMapCached {
					docsBatchFn = func(partial analyzer.DocsFeatureMap) error {
						return saveDocsFeatureMapCache(docsFeatureMapCachePath, codeFeatureNames, partial)
					}
				}
				freshCodeMap, freshDocsMap, mapErr := runBothMaps(
					ctx, tiering, codeFeatures,
					scan, pages, workers, analyzer.DocsMapperPageBudget,
					noSymbols,
					codeBatchFn,
					docsBatchFn,
				)
				if mapErr != nil {
					return fmt.Errorf("map features: %w", mapErr)
				}
				if !codeMapCached {
					featureMap = freshCodeMap
					if err := saveFeatureMapCache(featureMapCachePath, codeFeatures, featureMap); err != nil {
						return fmt.Errorf("save feature map cache: %w", err)
					}
				}
				if !docsMapCached {
					docsFeatureMap = freshDocsMap
					if err := saveDocsFeatureMapCache(docsFeatureMapCachePath, codeFeatureNames, docsFeatureMap); err != nil {
						return fmt.Errorf("save docs feature map cache: %w", err)
					}
				}
			}

			log.Debug("feature mapping complete", "code", len(featureMap), "docs", len(docsFeatureMap))

			log.Infof("detecting documentation drift...")
			pageReader := func(url string) (string, error) {
				path, ok := idx.FilePath(url)
				if !ok {
					return "", fmt.Errorf("page not in cache: %s", url)
				}
				data, err := os.ReadFile(path)
				return string(data), err
			}
			docCoveredFeatures := make([]string, 0, len(docsFeatureMap))
			for _, entry := range docsFeatureMap {
				if len(entry.Pages) > 0 {
					docCoveredFeatures = append(docCoveredFeatures, entry.Feature)
				}
			}
			driftOnFinding := func(accumulated []analyzer.DriftFinding) error {
				return reporter.WriteGaps(projectDir, featureMap, docCoveredFeatures, accumulated)
			}

			driftCachePath := filepath.Join(projectDir, "drift.json")

			var cached map[string]analyzer.CachedDriftEntry
			if !noCache {
				if loaded, ok := loadDriftCache(driftCachePath); ok {
					cached = loaded
					log.Infof("using cached drift results (%d features)", len(cached))
				}
			}

			// Seed liveCache with prior cached entries for features still in
			// featureMap so a partial run that crashes mid-drift doesn't evict
			// not-yet-processed entries. Features removed from featureMap are
			// not seeded and so are pruned on the next save.
			liveCache := seedDriftLiveCache(cached, featureMap)
			hits, fresh := 0, 0
			onFeatureDone := newDriftCachePersister(cached, liveCache, driftCachePath, &hits, &fresh)

			driftFindings, err := analyzer.DetectDrift(
				ctx, tiering, featureMap, docsFeatureMap,
				pageReader, repoPath,
				cached, driftOnFinding, onFeatureDone,
			)
			if err != nil {
				return fmt.Errorf("detect drift: %w", err)
			}
			log.Infof("drift cache: %d hits, %d fresh", hits, fresh)
			log.Debugf("drift detection complete: %d findings", len(driftFindings))

			var screenshotGaps []analyzer.ScreenshotGap
			if !skipScreenshotCheck {
				log.Infof("detecting missing screenshots...")
				urls := make([]string, 0, len(pages))
				for url := range pages {
					urls = append(urls, url)
				}
				sort.Strings(urls)

				var docPages []analyzer.DocPage
				for _, url := range urls {
					filePath := pages[url]
					data, readErr := os.ReadFile(filePath)
					if readErr != nil {
						log.Warnf("skip page %s: %v", url, readErr)
						continue
					}
					docPages = append(docPages, analyzer.DocPage{
						URL:     url,
						Path:    filePath,
						Content: string(data),
					})
				}
				progress := func(done, total int, page string) {
					log.Infof("  [%d/%d] %s", done, total, page)
				}
				screenshotGaps, err = analyzer.DetectScreenshotGaps(ctx, tiering.Small(), docPages, progress)
				if err != nil {
					return fmt.Errorf("detect screenshots: %w", err)
				}
				log.Debugf("screenshot-gap detection complete: %d gaps", len(screenshotGaps))
			}

			if err := reporter.WriteMapping(projectDir, productSummary, featureMap, docsFeatureMap); err != nil {
				return fmt.Errorf("write mapping: %w", err)
			}
			if err := reporter.WriteGaps(projectDir, featureMap, docCoveredFeatures, driftFindings); err != nil {
				return fmt.Errorf("write gaps: %w", err)
			}
			if !skipScreenshotCheck {
				if err := reporter.WriteScreenshots(projectDir, screenshotGaps); err != nil {
					return fmt.Errorf("write screenshots: %w", err)
				}
			}

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

			screenshotsLine := fmt.Sprintf("  %s/screenshots.md", projectDir)
			if skipScreenshotCheck {
				screenshotsLine += " (skipped)"
			}
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

			return nil
		},
	}

	cmd.Flags().StringVar(&repoPath, "repo", ".", "path to the repository to analyze")
	cmd.Flags().StringVar(&cacheDir, "cache-dir", ".find-the-gaps", "base directory for all cached results")
	cmd.Flags().BoolVar(&noCache, "no-cache", false, "force full re-scan, ignoring any cached results")
	cmd.Flags().StringVar(&docsURL, "docs-url", "", "URL of the documentation site to analyze")
	cmd.Flags().IntVar(&workers, "workers", 5, "number of parallel mdfetch workers")
	cmd.Flags().StringVar(&llmSmall, "llm-small", "",
		"small-tier model as \"provider/model\" (default: anthropic/claude-haiku-4-5)")
	cmd.Flags().StringVar(&llmTypical, "llm-typical", "",
		"typical-tier model as \"provider/model\" (default: anthropic/claude-sonnet-4-6)")
	cmd.Flags().StringVar(&llmLarge, "llm-large", "",
		"large-tier model as \"provider/model\" (default: anthropic/claude-opus-4-7)")
	cmd.Flags().BoolVar(&noSymbols, "no-symbols", false, "map features to files only, skipping symbol-level analysis")
	cmd.Flags().BoolVar(&skipScreenshotCheck, "skip-screenshot-check", false,
		"skip the missing-screenshot detection pass")
	cmd.Flags().StringVar(&siteMode, "site-mode", "mirror", "site content shape: \"mirror\" or \"expanded\"")
	cmd.Flags().BoolVar(&noSite, "no-site", false, "skip the Hugo site build; markdown reports still emitted")
	cmd.Flags().BoolVar(&keepSiteSource, "keep-site-source", true,
		"preserve generated Hugo source at <projectDir>/site-src/ (default true; pass --keep-site-source=false to discard)")

	return cmd
}

// formatScanSummary builds the one-line summary printed after a scan.
// Zero skips: "scanned %d files, skipped 0".
// Non-zero: "scanned %d files, skipped %d (defaults: X, .gitignore: Y, .ftgignore: Z)"
// with zero-count layer segments suppressed. Counts use English thousands
// separators so large repositories stay readable.
func formatScanSummary(s ignore.Stats) string {
	skipped := s.SkippedTotal()
	if skipped == 0 {
		return summaryPrinter.Sprintf("scanned %d files, skipped 0", s.Scanned)
	}
	parts := make([]string, 0, 3)
	for _, name := range []string{"defaults", ".gitignore", ".ftgignore"} {
		if n := s.Skipped[name]; n > 0 {
			parts = append(parts, summaryPrinter.Sprintf("%s: %d", name, n))
		}
	}
	return summaryPrinter.Sprintf("scanned %d files, skipped %d (%s)", s.Scanned, skipped, strings.Join(parts, ", "))
}

// stringSliceEqual reports element-wise equality. Both inputs must already be
// sorted; this is not a set comparison.
func stringSliceEqual(a, b []string) bool {
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
