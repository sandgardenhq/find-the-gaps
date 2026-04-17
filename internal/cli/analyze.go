package cli

import (
	"errors"

	"github.com/spf13/cobra"
)

func newAnalyzeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "analyze",
		Short: "Analyze a codebase against its documentation site for gaps.",
		RunE: func(_ *cobra.Command, _ []string) error {
			return errors.New("analyze: not yet implemented")
		},
	}
}
