package cli

import (
	"fmt"
	"path/filepath"

	"github.com/sandgardenhq/find-the-gaps/internal/scanner"
	"github.com/sandgardenhq/find-the-gaps/internal/spider"
	"github.com/spf13/cobra"
)

func newAnalyzeCmd() *cobra.Command {
	var (
		docsURL  string
		repoPath string
		cacheDir string
		workers  int
		noCache  bool
	)

	cmd := &cobra.Command{
		Use:   "analyze",
		Short: "Analyze a codebase against its documentation site for gaps.",
		RunE: func(cmd *cobra.Command, _ []string) error {
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

			if docsURL != "" {
				spiderOpts := spider.Options{
					CacheDir: filepath.Join(projectDir, "docs"),
					Workers:  workers,
				}
				pages, err := spider.Crawl(docsURL, spiderOpts, spider.MdfetchFetcher(spiderOpts))
				if err != nil {
					return fmt.Errorf("crawl failed: %w", err)
				}
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "scanned %d files, fetched %d pages\n",
					len(scan.Files), len(pages))
			} else {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "scanned %d files\n", len(scan.Files))
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&repoPath, "repo", ".", "path to the repository to analyze")
	cmd.Flags().StringVar(&cacheDir, "cache-dir", ".find-the-gaps", "base directory for all cached results")
	cmd.Flags().BoolVar(&noCache, "no-cache", false, "force full re-scan, ignoring any cached results")
	cmd.Flags().StringVar(&docsURL, "docs-url", "", "URL of the documentation site to analyze")
	cmd.Flags().IntVar(&workers, "workers", 5, "number of parallel mdfetch workers")

	return cmd
}
