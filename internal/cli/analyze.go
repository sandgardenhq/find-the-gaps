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

func newAnalyzeCmd() *cobra.Command {
	var (
		docsURL     string
		repoPath    string
		cacheDir    string
		workers     int
		noCache     bool
		llmProvider string
		llmModel    string
		llmBaseURL  string
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

			cfg := &LLMConfig{
				Provider: llmProvider,
				Model:    llmModel,
				BaseURL:  llmBaseURL,
			}
			llmClient, err := newLLMClient(cfg)
			if err != nil {
				return fmt.Errorf("LLM client: %w", err)
			}

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
				pa, analyzeErr := analyzer.AnalyzePage(ctx, llmClient, url, string(content))
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
				productSummary, err = analyzer.SynthesizeProduct(ctx, llmClient, analyses)
				if err != nil {
					return fmt.Errorf("synthesize: %w", err)
				}
				if err := idx.SetProductSummary(productSummary.Description, productSummary.Features); err != nil {
					return fmt.Errorf("save product summary: %w", err)
				}
			}

			var tokenCounter analyzer.TokenCounter
			switch cfg.Provider {
			case "anthropic":
				tokenCounter = analyzer.NewAnthropicCounter(os.Getenv("ANTHROPIC_API_KEY"), cfg.Model, os.Getenv("ANTHROPIC_BASE_URL"))
			default:
				tokenCounter = analyzer.NewTiktokenCounter()
			}

			featureMapCachePath := filepath.Join(projectDir, "featuremap.json")
			var featureMap analyzer.FeatureMap
			if !noCache {
				if cached, ok := loadFeatureMapCache(featureMapCachePath, productSummary.Features); ok {
					log.Infof("using cached feature map (%d features)", len(cached))
					featureMap = cached
				}
			}

			if featureMap == nil {
				log.Infof("mapping %d features across %d code files...", len(productSummary.Features), len(scan.Files))
				featureMap, err = analyzer.MapFeaturesToCode(ctx, llmClient, tokenCounter, productSummary.Features, scan, analyzer.MapperTokenBudget,
					func(partial analyzer.FeatureMap) error {
						return saveFeatureMapCache(featureMapCachePath, productSummary.Features, partial)
					})
				if err != nil {
					return fmt.Errorf("map features: %w", err)
				}
				if err := saveFeatureMapCache(featureMapCachePath, productSummary.Features, featureMap); err != nil {
					return fmt.Errorf("save feature map cache: %w", err)
				}
			}
			log.Debug("feature mapping complete", "mapped", len(featureMap))

			if err := reporter.WriteMapping(projectDir, productSummary, featureMap, analyses); err != nil {
				return fmt.Errorf("write mapping: %w", err)
			}
			if err := reporter.WriteGaps(projectDir, scan, featureMap, productSummary.Features); err != nil {
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
	cmd.Flags().StringVar(&llmProvider, "llm-provider", "anthropic",
		"LLM provider: anthropic | openai | ollama | lmstudio | openai-compatible")
	cmd.Flags().StringVar(&llmModel, "llm-model", "",
		"model name (default varies by provider; e.g. llama3 for ollama)")
	cmd.Flags().StringVar(&llmBaseURL, "llm-base-url", "",
		"base URL for local providers (required for openai-compatible; default: provider-specific)")

	return cmd
}
