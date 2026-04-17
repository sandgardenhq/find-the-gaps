package cli

import (
	"fmt"

	"github.com/sandgardenhq/find-the-gaps/internal/spider"
	"github.com/spf13/cobra"
)

func newAnalyzeCmd() *cobra.Command {
	var (
		docsURL  string
		cacheDir string
		workers  int
	)

	cmd := &cobra.Command{
		Use:   "analyze",
		Short: "Analyze a codebase against its documentation site for gaps.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if docsURL == "" {
				return fmt.Errorf("--docs-url is required")
			}
			opts := spider.Options{
				CacheDir: cacheDir,
				Workers:  workers,
			}
			pages, err := spider.Crawl(docsURL, opts, spider.MdfetchFetcher(opts))
			if err != nil {
				return fmt.Errorf("crawl failed: %w", err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "fetched %d pages\n", len(pages))
			return nil
		},
	}

	cmd.Flags().StringVar(&docsURL, "docs-url", "", "URL of the documentation site to analyze (required)")
	cmd.Flags().StringVar(&cacheDir, "cache-dir", ".find-the-gaps/cache", "directory to cache fetched pages")
	cmd.Flags().IntVar(&workers, "workers", 5, "number of parallel mdfetch workers")

	return cmd
}
