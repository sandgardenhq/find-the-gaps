package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/log"
	"github.com/sandgardenhq/find-the-gaps/internal/analyzer"
	"github.com/sandgardenhq/find-the-gaps/internal/doctor"
	"github.com/sandgardenhq/find-the-gaps/internal/forge"
	"github.com/sandgardenhq/find-the-gaps/internal/parallel"
	"github.com/sandgardenhq/find-the-gaps/internal/pdf"
	"github.com/sandgardenhq/find-the-gaps/internal/reporter"
	"github.com/sandgardenhq/find-the-gaps/internal/scanner"
	"github.com/sandgardenhq/find-the-gaps/internal/scanner/ignore"
	"github.com/sandgardenhq/find-the-gaps/internal/scanner/lang"
	"github.com/sandgardenhq/find-the-gaps/internal/site"
	"github.com/sandgardenhq/find-the-gaps/internal/spider"
	"github.com/spf13/cobra"
	"golang.org/x/text/language"
	"golang.org/x/text/message"
)

var summaryPrinter = message.NewPrinter(language.English)

// tieringFactory builds the per-run analyzer.LLMTiering. Tests override this
// to inject stub clients (e.g., a counting ToolLLMClient for the drift
// investigator) without rebuilding the whole analyze harness.
var tieringFactory func(small, typical, large string) (analyzer.LLMTiering, error) = func(small, typical, large string) (analyzer.LLMTiering, error) {
	return newLLMTiering(small, typical, large)
}

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
		docsURL                      string
		repoPath                     string
		cacheDir                     string
		workers                      int
		noCache                      bool
		noSymbols                    bool
		llmSmall                     string
		llmTypical                   string
		llmLarge                     string
		experimentalCheckScreenshots bool
		siteMode                     string
		noSite                       bool
		noServe                      bool
		keepSiteSource               bool
		noPDF                        bool
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

			if langs := supportedLanguages(scan); len(langs) == 0 {
				supported := strings.Join(lang.Languages(), ", ")
				return fmt.Errorf( //nolint:staticcheck // ST1005: proper-noun lead-in
					"no supported programming languages detected in %s.\n\n"+
						"Find the Gaps walked %d files but found no %s source.\n\n"+
						"If your repo uses an unsupported language, please open an issue:\n"+
						"https://github.com/sandgardenhq/find-the-gaps/issues",
					repoPath, stats.Scanned, supported)
			}

			if os.Getenv("FIND_THE_GAPS_QUIET") != "1" {
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), formatScanSummary(stats))
			}

			// Resolve the docs URL up front so we can size the precheck list
			// to the work we will actually do: mdfetch is only needed when we
			// crawl the live site, not when on-disk mode synthesizes pages
			// from the repo.
			resolved, err := forge.Resolve(docsURL, repoPath)
			if err != nil {
				if errors.Is(err, forge.ErrForgeNotIngestable) {
					// Capitalized leading word is intentional: "Find the Gaps"
					// is the product name and must lead the user-facing
					// message. ST1005's lower-case-first rule does not apply
					// to proper nouns.
					return fmt.Errorf( //nolint:staticcheck // ST1005: proper-noun lead-in
						"Find the Gaps can't crawl source-control forges "+
							"(github.com, gitlab.com, bitbucket.org, codeberg.org, git.sr.ht). "+
							"To analyze these docs, clone the repo locally and pass --repo /path/to/it. "+
							"(%w)", err)
				}
				return err
			}

			// Build the precheck list from the resolution outcome: mdfetch is
			// only needed when we will actually crawl the docs site, and hugo
			// is only needed when we will render the report. Skipping the
			// precheck entirely when both are unneeded keeps on-disk + --no-site
			// runs free of environment-specific install requirements.
			required := make([]string, 0, 2)
			suffix := ""
			if !resolved.OnDisk {
				required = append(required, "mdfetch")
			}
			if !noSite {
				required = append(required, "hugo")
				suffix = "Pass --no-site to skip Hugo."
			}
			if len(required) > 0 {
				if err := requireExternalTools(ctx, doctor.Precheck{
					Command: "ftg analyze",
					Tools:   required,
					Suffix:  suffix,
				}); err != nil {
					return err
				}
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
			tiering, err := tieringFactory(llmSmall, llmTypical, llmLarge)
			if err != nil {
				// The setup-hint error already renders as a self-contained
				// user message — wrapping it would prepend "LLM client:" and
				// reintroduce noise we just removed.
				var she *llmSetupHintError
				if errors.As(err, &she) {
					return err
				}
				return fmt.Errorf("LLM client: %w", err)
			}
			if t, ok := tiering.(*llmTiering); ok {
				defer logLLMCallCounts(t)
			}

			docsDir := filepath.Join(projectDir, "docs")

			var pages map[string]string
			if resolved.OnDisk {
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), resolved.Notice)
				pages = resolved.Pages
			} else {
				log.Infof("crawling %s", docsURL)
				spiderOpts := spider.Options{
					CacheDir: docsDir,
					Workers:  workers,
				}
				pages, err = spider.Crawl(docsURL, spiderOpts, spider.MdfetchFetcher(spiderOpts))
				if err != nil {
					return fmt.Errorf("crawl failed: %w", err)
				}
				log.Debug("crawl complete", "pages", len(pages))
			}

			idx, err := spider.LoadIndex(docsDir)
			if err != nil {
				return fmt.Errorf("load index: %w", err)
			}

			// Analyze each page; skip cached results.
			log.Infof("analyzing %d pages...", len(pages))
			var analyses []analyzer.PageAnalysis
			type pageJob struct {
				url      string
				filePath string
			}
			jobs := make([]pageJob, 0, len(pages))
			for url, filePath := range pages {
				if summary, features, isDocs, role, ok := idx.Analysis(url); ok {
					log.Debug("page cache hit", "url", url)
					analyses = append(analyses, analyzer.PageAnalysis{
						URL:      url,
						Summary:  summary,
						Features: features,
						IsDocs:   isDocs,
						Role:     role,
					})
					continue
				}
				jobs = append(jobs, pageJob{url: url, filePath: filePath})
			}

			var analysesMu sync.Mutex
			cacheHitCount := len(analyses)
			err = parallel.Run(ctx, jobs, workers, func(ctx context.Context, j pageJob) error {
				content, readErr := os.ReadFile(j.filePath)
				if readErr != nil {
					return nil
				}
				log.Infof("  %s", j.url)
				pa, analyzeErr := analyzer.AnalyzePage(ctx, tiering, j.url, string(content))
				if analyzeErr != nil {
					log.Warnf("skipping %s: %v", j.url, analyzeErr)
					return nil
				}
				if recErr := idx.RecordAnalysis(j.url, pa.Summary, pa.Features, pa.IsDocs, pa.Role); recErr != nil {
					return fmt.Errorf("record analysis: %w", recErr)
				}
				analysesMu.Lock()
				analyses = append(analyses, pa)
				analysesMu.Unlock()
				return nil
			})
			if err != nil {
				return fmt.Errorf("analyze pages: %w", err)
			}
			freshCount := len(analyses) - cacheHitCount

			if len(analyses) == 0 {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "scanned %d files, fetched %d pages, 0 pages analyzed\n",
					len(scan.Files), len(pages))
				return nil
			}

			log.Infof("%s", classificationSummary(analyses))
			for _, a := range analyses {
				if !a.IsDocs {
					log.Debugf("  non-docs: %s — %s", a.URL, a.Summary)
				}
			}

			if err := allNotDocsGuard(analyses); err != nil {
				return err
			}

			// Build a per-run role resolver from the page-analysis cache so
			// both the drift judge prompt's "Page role hints:" block AND the
			// screenshot prompts' page_role hint reflect content-classified
			// roles instead of URL-segment heuristics. Hoisted above the
			// drift branch so warm-cache (drift-skipped) runs still populate
			// DocPage.Role for the screenshot pass.
			rolesByURL := make(map[string]string, len(analyses))
			for _, pa := range analyses {
				rolesByURL[pa.URL] = pa.Role
			}
			roleResolver := analyzer.NewRoleResolver(rolesByURL)

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

			// Filter docs-side input on IsDocs so blog/marketing/team pages
			// don't pollute the docs feature map. The code-side mapping (built
			// from `scan`) is independent and unfiltered.
			docsAnalyses := filterDocsAnalyses(analyses)
			docsPages := make(map[string]string, len(docsAnalyses))
			for _, a := range docsAnalyses {
				if filePath, ok := pages[a.URL]; ok {
					docsPages[a.URL] = filePath
				}
			}

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
					scan, docsPages, workers, analyzer.DocsMapperPageBudget,
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

			driftCachePath := filepath.Join(projectDir, "drift.json")
			gapsPath := filepath.Join(projectDir, "gaps.md")
			wantHash := computeDriftInputHash(featureMap, docsFeatureMap)

			driftSkipped := false
			var driftFindings []analyzer.DriftFinding

			if !noCache && codeMapCached && docsMapCached {
				if file, ok := loadDriftCacheFile(driftCachePath); ok && file.Complete != nil && file.Complete.Hash == wantHash {
					if _, statErr := os.Stat(gapsPath); statErr == nil {
						cachedMap := driftCacheEntriesToMap(file.Entries)
						driftFindings = driftFindingsFromCache(cachedMap, featureMap)
						driftSkipped = true
						log.Infof("drift: cache complete, skipping (hash %s…)", wantHash[:8])
					}
				}
			}

			if !driftSkipped {
				// Per-feature rationales for the Undocumented Features section
				// of gaps.md. We cache rationales by hash(name+description+layer)
				// so unchanged features skip the LLM call on rerun. Only newly
				// undocumented or content-changed features hit the small tier.
				// A failure degrades gracefully — the reporter falls back to
				// a generic blurb when a key is missing.
				whyCachePath := filepath.Join(projectDir, "why-document.json")
				whyCache := loadWhyDocumentCache(whyCachePath)
				undocFeatures := reporter.UndocumentedFeatures(featureMap, docCoveredFeatures)
				whyRationales := make(map[string]string, len(undocFeatures))
				var toFetch []analyzer.CodeFeature
				freshCache := make(map[string]whyDocumentCacheEntry, len(undocFeatures))
				for _, e := range undocFeatures {
					hash := whyDocumentInputHash(e.Feature)
					if cached, ok := whyCache[e.Feature.Name]; ok && cached.Hash == hash && cached.Rationale != "" {
						whyRationales[e.Feature.Name] = cached.Rationale
						freshCache[e.Feature.Name] = cached
						continue
					}
					toFetch = append(toFetch, e.Feature)
				}
				if len(toFetch) > 0 {
					fresh, whyErr := analyzer.WhyDocument(ctx, tiering, toFetch)
					if whyErr != nil {
						log.Warnf("WhyDocument failed; falling back to generic rationale: %v", whyErr)
					} else {
						for _, f := range toFetch {
							if r, ok := fresh[f.Name]; ok && r != "" {
								whyRationales[f.Name] = r
								freshCache[f.Name] = whyDocumentCacheEntry{
									Name:      f.Name,
									Hash:      whyDocumentInputHash(f),
									Rationale: r,
								}
							}
						}
					}
				}
				if len(undocFeatures) > 0 {
					if err := saveWhyDocumentCache(whyCachePath, freshCache); err != nil {
						log.Warnf("save why-document cache: %v", err)
					}
				}
				gapsPrefix := reporter.BuildGapsStaticPrefix(featureMap, docCoveredFeatures, whyRationales)
				gapsWriter := reporter.NewGapsWriter(projectDir, gapsPrefix, 500*time.Millisecond)
				// Seed the writer with an empty slice so a run that produces
				// zero findings still emits a gaps.md (matches the prior
				// unconditional WriteGaps behavior).
				gapsWriter.Push(nil)
				driftOnFinding := func(accumulated []analyzer.DriftFinding) error {
					gapsWriter.Push(accumulated)
					return nil
				}

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

				driftFindings, err = analyzer.DetectDrift(
					ctx, tiering, featureMap, docsFeatureMap,
					pageReader, roleResolver, repoPath,
					workers,
					cached, driftOnFinding, onFeatureDone,
				)
				// Always flush the writer before the function returns so a
				// detection error still produces the most-recent gaps.md state
				// on disk. Close is the source of truth for the live path; the
				// trailing WriteGaps call only fires on the cache-skipped path.
				if closeErr := gapsWriter.Close(); closeErr != nil && err == nil {
					return fmt.Errorf("close gaps writer: %w", closeErr)
				}
				if err != nil {
					if errors.Is(err, analyzer.ErrLLMRetriesExhausted) {
						printRestartHint(cmd.ErrOrStderr())
					}
					return fmt.Errorf("detect drift: %w", err)
				}
				log.Infof("drift cache: %d hits, %d fresh", hits, fresh)
				log.Debugf("drift detection complete: %d findings", len(driftFindings))

				// Stamp completion sentinel so the next no-op re-run can skip
				// the drift pass entirely. UTC for byte-stable JSON across
				// machines and timezones.
				if err := saveDriftCacheComplete(driftCachePath, liveCache, &driftComplete{
					Hash:        wantHash,
					CompletedAt: time.Now().UTC(),
				}); err != nil {
					return fmt.Errorf("save drift completion: %w", err)
				}
			}

			var screenshotResult analyzer.ScreenshotResult
			screenshotsSkipped := false
			if experimentalCheckScreenshots {
				// The cache lives at a separate path from the user-visible
				// screenshots.json artifact (which is rewritten by
				// reporter.WriteScreenshotsJSON in a flatter shape). Mixing the
				// two filenames would cause the reporter to clobber the cache
				// file's completion sentinel mid-run.
				screenshotsCachePath := filepath.Join(projectDir, "screenshots-cache.json")
				screenshotsMdPath := filepath.Join(projectDir, "screenshots.md")
				docPages := buildScreenshotDocPages(pages, analyses)
				// Stamp the content-classified role onto each DocPage so the
				// screenshot prompts' page_role hint reflects the same value
				// the drift judge sees. rolesByURL was built above, hoisted
				// out of the drift block so warm-cache (drift-skipped) runs
				// still populate it for this pass.
				for i := range docPages {
					docPages[i].Role = rolesByURL[docPages[i].URL]
				}
				wantScreenshotsHash := computeScreenshotsInputHash(docPages, llmSmall)

				if !noCache {
					if file, ok := loadScreenshotsCacheFile(screenshotsCachePath); ok &&
						file.Complete != nil && file.Complete.Hash == wantScreenshotsHash {
						if _, statErr := os.Stat(screenshotsMdPath); statErr == nil {
							cachedMap := screenshotsCacheEntriesToMap(file.Entries)
							screenshotResult = screenshotResultFromCache(cachedMap, docPages)
							screenshotsSkipped = true
							log.Infof("screenshots: cache complete, skipping (hash %s…)", wantScreenshotsHash[:8])
						}
					}
				}

				if !screenshotsSkipped {
					log.Infof("detecting missing screenshots...")
					progress := func(done, total int, page string) {
						log.Infof("  [%d/%d] %s", done, total, page)
					}

					var cachedScreenshots map[string]analyzer.ScreenshotsCachedPage
					var liveScreenshotsMap map[string]screenshotsCacheEntry
					if !noCache {
						if loaded, ok := loadScreenshotsCache(screenshotsCachePath); ok {
							liveScreenshotsMap = loaded
							cachedScreenshots = screenshotsCachedFromCli(loaded)
							log.Infof("using cached screenshot results (%d pages)", len(loaded))
						}
					}
					if liveScreenshotsMap == nil {
						liveScreenshotsMap = map[string]screenshotsCacheEntry{}
					}
					persist := newScreenshotsCachePersister(liveScreenshotsMap, screenshotsCachePath)
					onPageDone := func(_ string, entry analyzer.ScreenshotsCachedPage) error {
						return persist(screenshotsCacheEntryFromAnalyzer(entry))
					}

					// Stream snapshots to a debounced single-writer goroutine
					// so screenshots.md updates mid-run rather than only at
					// the end. The analyzer hands us deep-enough copies so
					// the writer can format without serializing workers.
					screenshotsWriter := reporter.NewScreenshotsWriter(projectDir, 500*time.Millisecond)
					// Seed the writer with an empty result so a run that
					// produces zero gaps still emits a screenshots.md
					// (matches the prior unconditional WriteScreenshots
					// behavior on the live path).
					screenshotsWriter.Push(analyzer.ScreenshotResult{})
					onResultUpdated := func(snap analyzer.ScreenshotResult) {
						screenshotsWriter.Push(snap)
					}

					screenshotResult, err = analyzer.DetectScreenshotGaps(
						ctx, tiering.Small(), docPages,
						workers,
						cachedScreenshots, onPageDone, onResultUpdated, progress,
					)
					// Always flush the writer before downstream readers
					// (site.Build) consume screenshots.md. Close is the
					// source of truth for the live path; the trailing
					// WriteScreenshots call is gone.
					if closeErr := screenshotsWriter.Close(); closeErr != nil && err == nil {
						return fmt.Errorf("close screenshots writer: %w", closeErr)
					}
					if err != nil {
						return fmt.Errorf("detect screenshots: %w", err)
					}
					log.Debugf("screenshot-gap detection complete: %d gaps", len(screenshotResult.MissingGaps))
					emitScreenshotAuditLog(screenshotResult.AuditStats)

					// Stamp the completion sentinel so the next no-op re-run
					// can short-circuit. UTC for byte-stable JSON.
					if err := saveScreenshotsCacheComplete(screenshotsCachePath, liveScreenshotsMap, &screenshotsComplete{
						Hash:        wantScreenshotsHash,
						CompletedAt: time.Now().UTC(),
					}); err != nil {
						return fmt.Errorf("save screenshots completion: %w", err)
					}
				}
			}

			if err := reporter.WriteMapping(projectDir, productSummary, featureMap, docsFeatureMap); err != nil {
				return fmt.Errorf("write mapping: %w", err)
			}
			// gaps.md was written by GapsWriter.Close() on the live drift
			// path above. On the cache-skipped path the existing gaps.md is
			// reused as-is — re-running WriteGaps with cache-rebuilt findings
			// would reorder the bytes (driftFindingsFromCache sorts by feature
			// name; the live path uses featureMap insertion order) and churn
			// the file for external tooling reading gaps.md (git diffs,
			// hash-based watchers). site.Build keys drift by feature name in
			// a map and is order-insensitive, so it does not need this gate —
			// gaps.md does.
			// screenshots.md was written by ScreenshotsWriter.Close() on the
			// live screenshot path above. On the cache-skipped path the
			// existing screenshots.md is reused as-is. screenshots.json (the
			// flat JSON artifact) is still written here because no
			// streaming-writer covers it — the cache file at
			// screenshots-cache.json is a different shape and serves a
			// different purpose (resume-from-crash).
			if experimentalCheckScreenshots && !screenshotsSkipped {
				if err := reporter.WriteScreenshotsJSON(projectDir, screenshotResult); err != nil {
					return fmt.Errorf("write screenshots.json: %w", err)
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
						Screenshots:    screenshotResult.MissingGaps,
						ImageIssues:    screenshotResult.ImageIssues,
						ScreenshotsRan: experimentalCheckScreenshots,
					},
					site.BuildOptions{
						ProjectDir:  projectDir,
						ProjectName: projectName,
						KeepSource:  keepSiteSource,
						Mode:        siteModeVal,
						GeneratedAt: time.Now(),
					})
				if err != nil {
					return fmt.Errorf("build site: %w", err)
				}
			}

			// Emit the PDF report unless --no-pdf.
			if !noPDF {
				if err := pdf.WriteReport(projectDir, pdf.Inputs{
					ProjectName:    projectName,
					RepoURL:        absRepo,
					DocsURL:        docsURL,
					GeneratedAt:    time.Now(),
					Summary:        productSummary,
					Mapping:        featureMap,
					DocsMap:        docsFeatureMap,
					Drift:          driftFindings,
					Screenshots:    screenshotResult,
					ScreenshotsRan: experimentalCheckScreenshots,
				}); err != nil {
					return fmt.Errorf("write pdf: %w", err)
				}
			}

			gapsLine := "  " + projectDir + "/gaps.md"
			if counts := driftPriorityCounts(driftFindings); counts != "" {
				gapsLine += " (" + counts + ")"
			}
			if driftSkipped {
				gapsLine += " (cached, drift unchanged)"
			}
			screenshotsLine := fmt.Sprintf("  %s/screenshots.md", projectDir)
			if !experimentalCheckScreenshots {
				screenshotsLine += " (skipped)"
			} else if counts := screenshotsPriorityCounts(screenshotResult); counts != "" {
				screenshotsLine += " (" + counts + ")"
			}
			siteLine := "  " + projectDir + "/site/"
			if noSite {
				siteLine += " (skipped)"
			}
			pdfLine := "  " + projectDir + "/report.pdf"
			if noPDF {
				pdfLine += " (skipped)"
			}
			extraLine := ""
			if keepSiteSource && !noSite {
				extraLine = "\n  " + projectDir + "/site-src/"
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(),
				"scanned %d files, fetched %d pages, %d features mapped\nreports:\n  %s/mapping.md\n%s\n%s\n%s\n%s%s\n",
				len(scan.Files), len(pages), len(featureMap),
				projectDir, gapsLine, screenshotsLine, siteLine, pdfLine, extraLine)

			decision := decideAutoServe(noSite, noServe, humanPresent(), os.Getenv)
			if decision.Serve {
				siteDir := filepath.Join(projectDir, "site")
				if err := runAutoServe(cmd.Context(), cmd.OutOrStdout(), siteDir); err != nil {
					return fmt.Errorf("preview server: %w", err)
				}
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&repoPath, "repo", ".", "path to the repository to analyze")
	cmd.Flags().StringVar(&cacheDir, "cache-dir", ".find-the-gaps", "base directory for all cached results")
	cmd.Flags().BoolVar(&noCache, "no-cache", false, "force full re-scan, ignoring any cached results")
	cmd.Flags().StringVar(&docsURL, "docs", "",
		"URL or local path of docs to analyze (default: scan --repo on disk)")
	cmd.Flags().IntVar(&workers, "workers", 5, "number of parallel mdfetch workers")
	cmd.Flags().StringVar(&llmSmall, "llm-small", "",
		"small-tier model as \"provider/model\" (default: anthropic/claude-haiku-4-5)")
	cmd.Flags().StringVar(&llmTypical, "llm-typical", "",
		"typical-tier model as \"provider/model\" (default: anthropic/claude-sonnet-4-6)")
	cmd.Flags().StringVar(&llmLarge, "llm-large", "",
		"large-tier model as \"provider/model\" (default: anthropic/claude-opus-4-7)")
	cmd.Flags().BoolVar(&noSymbols, "no-symbols", false, "map features to files only, skipping symbol-level analysis")
	cmd.Flags().BoolVar(&experimentalCheckScreenshots, "experimental-check-screenshots", false,
		"enable experimental missing-screenshot detection pass")
	cmd.Flags().StringVar(&siteMode, "site-mode", "mirror", "site content shape: \"mirror\" or \"expanded\"")
	cmd.Flags().BoolVar(&noSite, "no-site", false, "skip the Hugo site build; markdown reports still emitted")
	cmd.Flags().BoolVar(&noServe, "no-serve", false,
		"skip starting the local preview server after analyze completes "+
			"(default: false; --no-site, CI=*, FIND_THE_GAPS_QUIET=1, and a non-interactive stdin also skip)")
	cmd.Flags().BoolVar(&keepSiteSource, "keep-site-source", true,
		"preserve generated Hugo source at <projectDir>/site-src/ (default true; pass --keep-site-source=false to discard)")
	cmd.Flags().BoolVar(&noPDF, "no-pdf", false,
		"skip the report.pdf artifact; markdown reports and site still emitted")

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

// supportedLanguages returns the entries of scan.Languages with the
// "Generic" placeholder removed. An empty result means the codebase
// contained nothing that any dedicated extractor could parse.
func supportedLanguages(scan *scanner.ProjectScan) []string {
	out := make([]string, 0, len(scan.Languages))
	for _, l := range scan.Languages {
		if l == "Generic" {
			continue
		}
		out = append(out, l)
	}
	return out
}

// allNotDocsGuard returns an error when every page in analyses is classified
// as non-docs. It exists to make the "silent zero-output" failure mode noisy
// and recoverable: under inclusive-by-default classification, getting all
// non-docs almost certainly means the classifier was wrong, not that the site
// has no docs. The empty-input case is handled by the caller's existing
// "0 pages analyzed" branch and must not double-fire here.
func allNotDocsGuard(analyses []analyzer.PageAnalysis) error {
	if len(analyses) == 0 {
		return nil
	}
	for _, a := range analyses {
		if a.IsDocs {
			return nil
		}
	}
	return fmt.Errorf(
		"all %d pages classified as non-docs; refusing to produce a misleading report "+
			"(this is almost certainly a classifier mistake; "+
			"re-run with --no-cache, or file an issue with the docs URL)",
		len(analyses))
}

// classificationSummary returns the one-line audit log emitted after every
// analyze run. The (use -v to list) parenthetical points users at the verbose
// per-URL listing, which is the design's chosen audit signal under the
// no-overrides v1: console-only, no markdown report, no dedicated file.
func classificationSummary(analyses []analyzer.PageAnalysis) string {
	docs, notDocs := 0, 0
	for _, a := range analyses {
		if a.IsDocs {
			docs++
		} else {
			notDocs++
		}
	}
	return fmt.Sprintf("classified: %d docs, %d non-docs (use -v to list)", docs, notDocs)
}

// filterDocsAnalyses returns the subset of analyses whose IsDocs flag is true.
// Used to exclude non-docs pages (blogs, marketing, team, legal) from the docs
// feature map so drift detection cannot match code features against them.
func filterDocsAnalyses(in []analyzer.PageAnalysis) []analyzer.PageAnalysis {
	out := make([]analyzer.PageAnalysis, 0, len(in))
	for _, a := range in {
		if a.IsDocs {
			out = append(out, a)
		}
	}
	return out
}

// buildScreenshotDocPages assembles the []analyzer.DocPage slice fed to
// DetectScreenshotGaps, filtering out URLs whose analysis classified them as
// non-docs. URLs without a corresponding analysis entry are excluded (the
// realistic case is 1:1 with the analyze loop, so this defensive default
// matches the "if we don't know, don't bother screenshotting" instinct).
// Read errors on disk are logged and skipped, preserving prior behavior.
func buildScreenshotDocPages(pages map[string]string, analyses []analyzer.PageAnalysis) []analyzer.DocPage {
	isDocs := make(map[string]bool, len(analyses))
	for _, a := range analyses {
		isDocs[a.URL] = a.IsDocs
	}
	urls := make([]string, 0, len(pages))
	for url := range pages {
		if isDocs[url] {
			urls = append(urls, url)
		}
	}
	sort.Strings(urls)
	out := make([]analyzer.DocPage, 0, len(urls))
	for _, url := range urls {
		filePath := pages[url]
		data, err := os.ReadFile(filePath)
		if err != nil {
			log.Warnf("skip page %s: %v", url, err)
			continue
		}
		out = append(out, analyzer.DocPage{
			URL:     url,
			Path:    filePath,
			Content: string(data),
		})
	}
	return out
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

// printRestartHint writes a one-shot warning explaining that the run was
// stopped by a retried-and-exhausted LLM call (provider hiccup, malformed
// response, network blip) and that re-running analyze resumes from cached
// per-feature drift results.
func printRestartHint(w io.Writer) {
	_, _ = fmt.Fprintln(w)
	_, _ = fmt.Fprintln(w, "WARNING: analyze stopped because an LLM call failed after several retries.")
	_, _ = fmt.Fprintln(w, "         This usually means the LLM provider is temporarily unavailable.")
	_, _ = fmt.Fprintln(w, "         Re-run `ftg analyze` to resume — completed features are cached")
	_, _ = fmt.Fprintln(w, "         and will be skipped.")
	_, _ = fmt.Fprintln(w)
}
