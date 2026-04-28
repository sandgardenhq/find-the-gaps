package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"runtime/debug"

	"github.com/charmbracelet/log"
	"github.com/spf13/cobra"
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
	cmd.AddCommand(newDoctorCmd(), newAnalyzeCmd(), newInstallDepsCmd(), newServeCmd())
	return cmd
}

func Execute() int {
	return run(os.Stdout, os.Stderr, os.Args[1:])
}

func run(stdout, stderr io.Writer, args []string) int {
	root := NewRootCmd()
	root.SetArgs(args)
	root.SetOut(stdout)
	root.SetErr(stderr)
	return errorToExitCode(root.Execute(), stderr)
}

func errorToExitCode(err error, stderr io.Writer) int {
	if err == nil {
		return 0
	}
	var ec *ExitCodeError
	if errors.As(err, &ec) {
		return ec.Code
	}
	_, _ = fmt.Fprintln(stderr, "Error:", err)
	return 1
}
