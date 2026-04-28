package cli

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"
)

func newServeCmd() *cobra.Command {
	var (
		repoPath string
		cacheDir string
		addr     string
	)

	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Serve the find-the-gaps report site over HTTP.",
		RunE: func(cc *cobra.Command, _ []string) error {
			projectName := filepath.Base(filepath.Clean(repoPath))
			siteDir := filepath.Join(cacheDir, projectName, "site")

			info, err := os.Stat(siteDir)
			if err != nil || !info.IsDir() {
				return fmt.Errorf("no rendered site at %s — run `ftg analyze` first to generate it", siteDir)
			}

			ln, err := net.Listen("tcp", addr)
			if err != nil {
				return fmt.Errorf("listen on %s: %w", addr, err)
			}

			srv := &http.Server{
				Handler:           http.FileServer(http.Dir(siteDir)),
				ReadHeaderTimeout: 5 * time.Second,
			}

			_, _ = fmt.Fprintf(cc.OutOrStdout(), "serving %s at http://%s/\n", siteDir, ln.Addr().String())

			errCh := make(chan error, 1)
			go func() {
				errCh <- srv.Serve(ln)
			}()

			select {
			case <-cc.Context().Done():
				shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				_ = srv.Shutdown(shutdownCtx)
				return nil
			case err := <-errCh:
				if errors.Is(err, http.ErrServerClosed) {
					return nil
				}
				return fmt.Errorf("serve: %w", err)
			}
		},
	}

	cmd.Flags().StringVar(&repoPath, "repo", ".", "path to the repository whose report should be served")
	cmd.Flags().StringVar(&cacheDir, "cache-dir", ".find-the-gaps", "base directory containing analyze output")
	cmd.Flags().StringVar(&addr, "addr", "127.0.0.1:0", "bind address for the local server (host:port; :0 picks a free port)")

	return cmd
}
