package cli

import (
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"
)

var version = "dev"

func NewRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "find-the-gaps",
		Short: "Find outdated or missing documentation in a codebase.",
		Long: "find-the-gaps analyzes a codebase alongside its documentation site to " +
			"identify outdated or missing documentation.",
		Version:       version,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	cmd.AddCommand(newDoctorCmd(), newAnalyzeCmd())
	return cmd
}

func Execute() int {
	return run(os.Stderr, os.Args[1:])
}

func run(stderr io.Writer, args []string) int {
	root := NewRootCmd()
	root.SetArgs(args)
	root.SetErr(stderr)
	if err := root.Execute(); err != nil {
		_, _ = fmt.Fprintln(stderr, "Error:", err)
		return 1
	}
	return 0
}
