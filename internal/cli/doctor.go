package cli

import (
	"errors"

	"github.com/spf13/cobra"
)

func newDoctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Check that required external tools (ripgrep, mdfetch) are installed.",
		RunE: func(_ *cobra.Command, _ []string) error {
			return errors.New("doctor: not yet implemented")
		},
	}
}
