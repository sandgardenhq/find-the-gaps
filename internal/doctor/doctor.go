// Package doctor checks that the external tool find-the-gaps shells out to
// (mdfetch) is installed and reports a clear install hint if not.
package doctor

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"

	"github.com/charmbracelet/log"
)

type Tool struct {
	Name        string // display name, e.g. "mdfetch"
	Binary      string // executable name on PATH, e.g. "mdfetch"
	VersionArg  string // argument that prints the version, e.g. "--version"
	InstallHint string // human-readable install fallback shown when automated install is unavailable
	InstallCmds map[string][]string // GOOS → {cmd, arg1, ...} for automated install
}

// RequiredTools is the fixed list of external dependencies find-the-gaps needs.
var RequiredTools = []Tool{
	{
		Name:        "mdfetch",
		Binary:      "mdfetch",
		VersionArg:  "--version",
		InstallHint: "npm install -g @sandgarden/mdfetch",
		InstallCmds: map[string][]string{
			"darwin":  {"npm", "install", "-g", "@sandgarden/mdfetch"},
			"linux":   {"npm", "install", "-g", "@sandgarden/mdfetch"},
			"windows": {"npm", "install", "-g", "@sandgarden/mdfetch"},
		},
	},
}

type result struct {
	tool    Tool
	path    string
	version string
	err     error
}

// Run checks all RequiredTools, prints found tools to stdout, prints missing
// or broken tools to stderr with install hints, and returns an exit code:
// 0 if all tools are available, 1 otherwise.
func Run(ctx context.Context, stdout, stderr io.Writer) int {
	return runCheck(ctx, RequiredTools, stdout, stderr)
}

func runCheck(ctx context.Context, tools []Tool, stdout, stderr io.Writer) int {
	results := make([]result, 0, len(tools))
	allOK := true
	for _, t := range tools {
		r := check(ctx, t)
		if r.err != nil {
			allOK = false
		}
		results = append(results, r)
	}

	for _, r := range results {
		if r.err != nil {
			continue
		}
		_, _ = fmt.Fprintf(stdout, "%-8s OK       %s  (%s)\n", r.tool.Name, r.path, r.version)
	}

	if allOK {
		_, _ = fmt.Fprintln(stdout, "All required external tools are available.")
		return 0
	}

	for _, r := range results {
		if r.err == nil {
			continue
		}
		if errors.Is(r.err, exec.ErrNotFound) {
			_, _ = fmt.Fprintf(stderr,
				"%s (%s) is not installed or not on $PATH.\n  install: %s\n",
				r.tool.Name, r.tool.Binary, r.tool.InstallHint,
			)
			continue
		}
		_, _ = fmt.Fprintf(stderr,
			"%s (%s) was found at %s but `%s %s` failed: %v\n  install: %s\n",
			r.tool.Name, r.tool.Binary, r.path, r.tool.Binary, r.tool.VersionArg, r.err, r.tool.InstallHint,
		)
	}
	return 1
}

func check(ctx context.Context, t Tool) result {
	log.Debug("checking tool", "name", t.Name, "binary", t.Binary)
	path, err := exec.LookPath(t.Binary)
	if err != nil {
		log.Debug("tool not found", "binary", t.Binary, "err", err)
		return result{tool: t, err: err}
	}
	out, err := exec.CommandContext(ctx, path, t.VersionArg).Output()
	if err != nil {
		log.Debug("tool version check failed", "binary", t.Binary, "path", path, "err", err)
		return result{tool: t, path: path, err: err}
	}
	version := firstLine(string(out))
	log.Debug("tool found", "binary", t.Binary, "path", path, "version", version)
	return result{tool: t, path: path, version: version}
}

func firstLine(s string) string {
	first, _, _ := strings.Cut(s, "\n")
	return strings.TrimRight(first, "\r")
}
