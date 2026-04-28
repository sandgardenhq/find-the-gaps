package cli

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"github.com/charmbracelet/log"
	"github.com/spf13/cobra"
)

// openInBrowser launches the user's default browser at url. Tests swap this out
// for a fake; the real implementation shells out per OS.
var openInBrowser = openURLInBrowser

func openURLInBrowser(url string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", url).Start()
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	default:
		return exec.Command("xdg-open", url).Start()
	}
}

func newServeCmd() *cobra.Command {
	var (
		repoPath string
		cacheDir string
		addr     string
		openFlag bool
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

			url := fmt.Sprintf("http://%s/", ln.Addr().String())
			_, _ = fmt.Fprintf(cc.OutOrStdout(), "serving %s at %s\n", siteDir, url)

			if openFlag {
				if err := openInBrowser(url); err != nil {
					log.Warnf("could not open browser: %v", err)
				}
			}

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
	cmd.Flags().StringVar(&addr, "addr", "127.0.0.1:8080", "bind address for the local server (host:port; use :0 to pick a free port)")
	cmd.Flags().BoolVar(&openFlag, "open", false, "open the served URL in the default browser after the server is up")

	return cmd
}
