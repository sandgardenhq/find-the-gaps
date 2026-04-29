package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/spf13/cobra"
)

// openInBrowser launches the user's default browser at url. Tests swap this out
// for a fake; the real implementation shells out per OS.
var openInBrowser = openURLInBrowser

// browserOpenerArgs returns the command name and args used to open a URL in
// the default browser for the given GOOS. Pure function so tests can exercise
// every OS branch without controlling runtime.GOOS.
func browserOpenerArgs(goos, url string) (string, []string) {
	switch goos {
	case "darwin":
		return "open", []string{url}
	case "windows":
		return "rundll32", []string{"url.dll,FileProtocolHandler", url}
	default:
		return "xdg-open", []string{url}
	}
}

func openURLInBrowser(url string) error {
	name, args := browserOpenerArgs(runtime.GOOS, url)
	return exec.Command(name, args...).Start()
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
			absRepo, err := filepath.Abs(repoPath)
			if err != nil {
				return fmt.Errorf("resolve repo path: %w", err)
			}
			projectName := filepath.Base(absRepo)
			siteDir := filepath.Join(cacheDir, projectName, "site")

			info, err := os.Stat(siteDir)
			if err != nil || !info.IsDir() {
				return fmt.Errorf("no rendered site at %s — run `ftg analyze` first to generate it", siteDir)
			}

			return runHTTPServer(cc.Context(), cc.OutOrStdout(), siteDir, addr, openFlag)
		},
	}

	cmd.Flags().StringVar(&repoPath, "repo", ".", "path to the repository whose report should be served")
	cmd.Flags().StringVar(&cacheDir, "cache-dir", ".find-the-gaps", "base directory containing analyze output")
	cmd.Flags().StringVar(&addr, "addr", "127.0.0.1:8080", "bind address for the local server (host:port; use 127.0.0.1:0 to pick a free port)")
	cmd.Flags().BoolVar(&openFlag, "open", false, "open the served URL in the default browser after the server is up")

	return cmd
}
