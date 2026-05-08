package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"time"

	"github.com/charmbracelet/log"
	"github.com/sandgardenhq/find-the-gaps/internal/updatecheck"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// version is overwritten at release time via:
//   go build -ldflags "-X github.com/sandgardenhq/find-the-gaps/internal/cli.version=v1.2.3"
// When unset, currentVersion() falls back to the module version reported by
// runtime/debug.BuildInfo (populated by `go install`), and finally to "dev".
var version = "dev"

// resolveVersion picks the best version string given an ldflags-injected value
// and the module version reported by runtime/debug.BuildInfo. Precedence:
// ldflags override > BuildInfo module version > "dev".
func resolveVersion(ldflagsVersion, buildInfoVersion string) string {
	if ldflagsVersion != "" && ldflagsVersion != "dev" {
		return ldflagsVersion
	}
	if buildInfoVersion != "" && buildInfoVersion != "(devel)" {
		return buildInfoVersion
	}
	return "dev"
}

func currentVersion() string {
	var biVersion string
	if info, ok := debug.ReadBuildInfo(); ok {
		biVersion = info.Main.Version
	}
	return resolveVersion(version, biVersion)
}

// ExitCodeError signals to Execute that the CLI should exit with the given
// non-zero code without printing any additional error text. The subcommand
// that returns this error is responsible for having already written any
// user-facing output.
type ExitCodeError struct {
	Code int
}

func (e *ExitCodeError) Error() string { return fmt.Sprintf("exit code %d", e.Code) }

func NewRootCmd() *cobra.Command {
	var verbose bool

	cmd := &cobra.Command{
		Use:   "ftg",
		Short: "Find outdated or missing documentation in a codebase.",
		Long: "ftg analyzes a codebase alongside its documentation site to " +
			"identify outdated or missing documentation.",
		Version:       currentVersion(),
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			log.SetOutput(cmd.ErrOrStderr())
			if verbose {
				log.SetLevel(log.DebugLevel)
			} else {
				log.SetLevel(log.InfoLevel)
			}
			return nil
		},
	}

	cmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "show debug logs")
	cmd.AddCommand(newDoctorCmd(), newAnalyzeCmd(), newRenderCmd(), newServeCmd())
	return cmd
}

func Execute() int {
	return run(os.Stdout, os.Stderr, os.Args[1:])
}

func run(stdout, stderr io.Writer, args []string) int {
	root := NewRootCmd()

	// Capture which subcommand was actually invoked so the post-run update
	// check can decide whether to skip (e.g. for `--help`, `__complete`, or
	// trivial commands that never reach PersistentPreRunE).
	var executed *cobra.Command
	prevPreRun := root.PersistentPreRunE
	root.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
		executed = cmd
		if prevPreRun != nil {
			return prevPreRun(cmd, args)
		}
		return nil
	}

	root.SetArgs(args)
	root.SetOut(stdout)
	root.SetErr(stderr)

	code := errorToExitCode(root.Execute(), stderr)

	// Update check runs after the command's output has been written.
	// Best-effort: never affects the exit code, never panics out.
	cmdName := ""
	if executed != nil {
		cmdName = executed.Name()
	}
	runUpdateCheck(stderr, cmdName)

	return code
}

// runUpdateCheck calls into the updatecheck package and writes any returned
// notice to stderr. Wired so test hooks (FIND_THE_GAPS_UPDATE_*) can redirect
// the GitHub base URL, cache path, and pretend-TTY status without touching
// production defaults.
func runUpdateCheck(stderr io.Writer, cmdName string) {
	version := os.Getenv("FIND_THE_GAPS_UPDATE_VERSION")
	if version == "" {
		version = currentVersion()
	}

	stderrIsTTY := false
	if f, ok := stderr.(*os.File); ok {
		stderrIsTTY = term.IsTerminal(int(f.Fd()))
	}
	if os.Getenv("FIND_THE_GAPS_UPDATE_FORCE_TTY") == "1" {
		stderrIsTTY = true
	}

	cachePath := os.Getenv("FIND_THE_GAPS_UPDATE_CACHE_PATH")
	if cachePath == "" {
		cachePath = defaultUpdateCheckCachePath()
		if cachePath == "" {
			return // No home directory — silently skip.
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	notice, _ := updatecheck.Run(ctx, updatecheck.RunOptions{
		CurrentVersion: version,
		Command:        cmdName,
		StderrIsTTY:    stderrIsTTY,
		Env:            os.Getenv,
		GOOS:           runtime.GOOS,
		BrewOnPath:     brewOnPath(),
		CachePath:      cachePath,
		BaseURL:        os.Getenv("FIND_THE_GAPS_UPDATE_BASE_URL"),
		Timeout:        2 * time.Second,
	})
	if notice != "" {
		_, _ = fmt.Fprint(stderr, "\n", notice)
	}
}

// defaultUpdateCheckCachePath returns the per-user cache file location, or ""
// if the home directory cannot be resolved.
func defaultUpdateCheckCachePath() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, ".find-the-gaps", "update-check.json")
}

func brewOnPath() bool {
	_, err := exec.LookPath("brew")
	return err == nil
}

func errorToExitCode(err error, stderr io.Writer) int {
	if err == nil {
		return 0
	}
	var ec *ExitCodeError
	if errors.As(err, &ec) {
		return ec.Code
	}
	var she *llmSetupHintError
	if errors.As(err, &she) {
		_, _ = fmt.Fprintln(stderr, she.Error())
		return 1
	}
	_, _ = fmt.Fprintln(stderr, "Error:", err)
	return 1
}
