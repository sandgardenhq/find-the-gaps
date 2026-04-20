package cli

import (
	"fmt"

	"github.com/sandgardenhq/find-the-gaps/internal/scanner"
	"github.com/spf13/cobra"
)

func newAnalyzeCmd() *cobra.Command {
	var (
		repoPath     string
		scanCacheDir string
		noCache      bool
	)

	cmd := &cobra.Command{
		Use:   "analyze",
		Short: "Analyze a codebase against its documentation site for gaps.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			scanOpts := scanner.Options{
				CacheDir: scanCacheDir,
				NoCache:  noCache,
			}
			scan, err := scanner.Scan(repoPath, scanOpts)
			if err != nil {
				return fmt.Errorf("scan failed: %w", err)
			}

			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "scanned %d files\n", len(scan.Files))
			return nil
		},
	}

	cmd.Flags().StringVar(&repoPath, "repo", ".", "path to the repository to analyze")
	cmd.Flags().StringVar(&scanCacheDir, "scan-cache-dir", ".find-the-gaps/scan-cache", "directory to cache scan results")
	cmd.Flags().BoolVar(&noCache, "no-cache", false, "force full re-scan, ignoring any cached results")

	return cmd
}
