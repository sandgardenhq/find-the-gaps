package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/charmbracelet/log"
	"github.com/sandgardenhq/find-the-gaps/internal/analyzer"
	"github.com/sandgardenhq/find-the-gaps/internal/reporter"
	"github.com/sandgardenhq/find-the-gaps/internal/scanner"
	"github.com/sandgardenhq/find-the-gaps/internal/spider"
	"github.com/spf13/cobra"
)

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

	// MapFeaturesToDocs still accepts a raw client and []string feature names.
	// Task 12 will migrate it onto the tiering interface.
	client := tiering.Large()
	featureNames := make([]string, len(features))
	for i, f := range features {
		featureNames[i] = f.Name
	}

	go func() {
		fm, err := analyzer.MapFeaturesToCode(ctx, tiering, features, scan, analyzer.MapperTokenBudget, filesOnly, onCodeBatch)
		codeCh <- bothMapsResult{fm, err}
	}()

	go func() {
		fm, err := analyzer.MapFeaturesToDocs(ctx, client, featureNames, pages, workers, docsTokenBudget, onDocsPage)
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
		docsURL    string
		repoPath   string
		cacheDir   string
		workers    int
		noCache    bool
		noSymbols  bool
		llmSmall   string
		llmTypical string
		llmLarge   string
	)

	cmd := &cobra.Command{
		Use:   "analyze",
		Short: "Analyze a codebase against its documentation site for gaps.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := context.Background()

			projectName := filepath.Base(filepath.Clean(repoPath))
			projectDir := filepath.Join(cacheDir, projectName)

			log.Info("scanning repository", "path", repoPath)
			scanOpts := scanner.Options{
				CacheDir: filepath.Join(projectDir, "scan"),
				NoCache:  noCache,
			}
			scan, err := scanner.Scan(repoPath, scanOpts)
			if err != nil {
				return fmt.Errorf("scan failed: %w", err)
			}
			log.Debug("scan complete", "files", len(scan.Files))

			if docsURL == "" {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "scanned %d files\n", len(scan.Files))
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
			llmClient := tiering.Large() // temp: Phase 4 will route each call site to its proper tier

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
			if freshCount == 0 && idx.ProductSummary != "" {
				productSummary = analyzer.ProductSummary{
					Description: idx.ProductSummary,
					Features:    idx.AllFeatures,
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

			tokenCounter := tiering.LargeCounter()
			_ = tokenCounter // Task 14 will drop this local; kept here to minimize this commit's diff.

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
			toolClient, ok := llmClient.(analyzer.ToolLLMClient)
			if !ok {
				return fmt.Errorf("LLM client does not support tool use (required for drift detection); configure a tool-use-capable provider in --llm-large (anthropic or openai)")
			}
			pageReader := func(url string) (string, error) {
				path, ok := idx.FilePath(url)
				if !ok {
					return "", fmt.Errorf("page not in cache: %s", url)
				}
				data, err := os.ReadFile(path)
				return string(data), err
			}
			// Build the list of code features that have at least one documentation page.
			docCoveredFeatures := make([]string, 0, len(docsFeatureMap))
			for _, entry := range docsFeatureMap {
				if len(entry.Pages) > 0 {
					docCoveredFeatures = append(docCoveredFeatures, entry.Feature)
				}
			}
			driftOnFinding := func(accumulated []analyzer.DriftFinding) error {
				return reporter.WriteGaps(projectDir, featureMap, docCoveredFeatures, accumulated)
			}
			driftFindings, err := analyzer.DetectDrift(ctx, toolClient, featureMap, docsFeatureMap, pageReader, repoPath, driftOnFinding)
			if err != nil {
				return fmt.Errorf("detect drift: %w", err)
			}
			log.Debugf("drift detection complete: %d findings", len(driftFindings))
			if err := reporter.WriteMapping(projectDir, productSummary, featureMap, docsFeatureMap); err != nil {
				return fmt.Errorf("write mapping: %w", err)
			}
			if err := reporter.WriteGaps(projectDir, featureMap, docCoveredFeatures, driftFindings); err != nil {
				return fmt.Errorf("write gaps: %w", err)
			}

			_, _ = fmt.Fprintf(cmd.OutOrStdout(),
				"scanned %d files, fetched %d pages, %d features mapped\nreports: %s/mapping.md, %s/gaps.md\n",
				len(scan.Files), len(pages), len(featureMap), projectDir, projectDir)

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

	return cmd
}
