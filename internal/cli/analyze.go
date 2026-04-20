package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

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

			scanOpts := scanner.Options{
				CacheDir: filepath.Join(projectDir, "scan"),
				NoCache:  noCache,
			}
			scan, err := scanner.Scan(repoPath, scanOpts)
			if err != nil {
				return fmt.Errorf("scan failed: %w", err)
			}

			if docsURL == "" {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "scanned %d files\n", len(scan.Files))
				return nil
			}

			llmClient, err := newLLMClient(LLMConfig{
				Provider: llmProvider,
				Model:    llmModel,
				BaseURL:  llmBaseURL,
			})
			if err != nil {
				return fmt.Errorf("LLM client: %w", err)
			}

			docsDir := filepath.Join(projectDir, "docs")
			spiderOpts := spider.Options{
				CacheDir: docsDir,
				Workers:  workers,
			}
			pages, err := spider.Crawl(docsURL, spiderOpts, spider.MdfetchFetcher(spiderOpts))
			if err != nil {
				return fmt.Errorf("crawl failed: %w", err)
			}

			idx, err := spider.LoadIndex(docsDir)
			if err != nil {
				return fmt.Errorf("load index: %w", err)
			}

			// Analyze each page; skip cached results.
			var analyses []analyzer.PageAnalysis
			freshCount := 0
			for url, filePath := range pages {
				if summary, features, ok := idx.Analysis(url); ok {
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
				pa, analyzeErr := analyzer.AnalyzePage(ctx, llmClient, url, string(content))
				if analyzeErr != nil {
					_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "warning: AnalyzePage %s: %v\n", url, analyzeErr)
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

			featureMap, err := analyzer.MapFeaturesToCode(ctx, llmClient, productSummary.Features, scan)
			if err != nil {
				return fmt.Errorf("map features: %w", err)
			}

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
