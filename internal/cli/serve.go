package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"
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

// testInteractiveOverride lets tests force isInteractive() to a known value.
// nil means "use the real TTY check".
var testInteractiveOverride *bool

func isInteractive() bool {
	if testInteractiveOverride != nil {
		return *testInteractiveOverride
	}
	return term.IsTerminal(int(os.Stdout.Fd())) && term.IsTerminal(int(os.Stdin.Fd()))
}

func newServeCmd() *cobra.Command {
	var (
		repoPath    string
		cacheDir    string
		addr        string
		openFlag    bool
		projectFlag string
	)

	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Serve the find-the-gaps report site over HTTP.",
		RunE: func(cc *cobra.Command, _ []string) error {
			if projectFlag != "" && cc.Flags().Changed("repo") {
				return fmt.Errorf("--project and --repo are mutually exclusive")
			}

			siteDir, err := resolveServeSiteDir(cc, cacheDir, repoPath, projectFlag)
			if err != nil {
				return err
			}

			return runHTTPServer(cc.Context(), cc.OutOrStdout(), siteDir, addr, openFlag)
		},
	}

	cmd.Flags().StringVar(&repoPath, "repo", ".", "path to the repository whose report should be served")
	cmd.Flags().StringVar(&cacheDir, "cache-dir", ".find-the-gaps", "base directory containing analyze output")
	cmd.Flags().StringVar(&addr, "addr", "127.0.0.1:8080", "bind address for the local server (host:port; use 127.0.0.1:0 to pick a free port)")
	cmd.Flags().BoolVar(&openFlag, "open", false, "open the served URL in the default browser after the server is up")
	cmd.Flags().StringVar(&projectFlag, "project", "", "name of an analyzed project under <cache-dir>/; bypasses the picker")

	return cmd
}

// resolveServeSiteDir picks the site to serve based on flags + cache contents.
// Order of precedence:
//  1. --project NAME            → use <cacheDir>/NAME/site
//  2. --repo PATH (explicit)    → use <cacheDir>/base(PATH)/site
//  3. --repo not set            → scan, then auto-pick / prompt / error
func resolveServeSiteDir(cc *cobra.Command, cacheDir, repoPath, projectFlag string) (string, error) {
	if projectFlag != "" {
		siteDir := filepath.Join(cacheDir, projectFlag, "site")
		if info, err := os.Stat(siteDir); err != nil || !info.IsDir() {
			return "", fmt.Errorf("no rendered site at %s — check --project or run `ftg analyze` first", siteDir)
		}
		return siteDir, nil
	}

	if cc.Flags().Changed("repo") {
		absRepo, err := filepath.Abs(repoPath)
		if err != nil {
			return "", fmt.Errorf("resolve repo path: %w", err)
		}
		siteDir := filepath.Join(cacheDir, filepath.Base(absRepo), "site")
		if info, err := os.Stat(siteDir); err != nil || !info.IsDir() {
			return "", fmt.Errorf("no rendered site at %s — run `ftg analyze` first to generate it", siteDir)
		}
		return siteDir, nil
	}

	projects, err := ListAnalyzedProjects(cacheDir)
	if err != nil {
		return "", fmt.Errorf("scan cache dir: %w", err)
	}
	switch len(projects) {
	case 0:
		return "", fmt.Errorf("no analyzed projects found in %s — run `ftg analyze` first", cacheDir)
	case 1:
		_, _ = fmt.Fprintf(cc.OutOrStdout(), "found one project: %s\n", projects[0].Name)
		return projects[0].SiteDir, nil
	default:
		if !isInteractive() {
			names := make([]string, len(projects))
			for i, p := range projects {
				names[i] = p.Name
			}
			return "", fmt.Errorf("multiple analyzed projects found in %s; re-run with --project NAME (one of: %s)",
				cacheDir, strings.Join(names, ", "))
		}
		chosen, err := pickProject(projects)
		if err != nil {
			return "", err
		}
		return chosen.SiteDir, nil
	}
}
