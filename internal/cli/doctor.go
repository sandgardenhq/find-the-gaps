package cli

import (
	"github.com/sandgardenhq/find-the-gaps/internal/doctor"
	"github.com/spf13/cobra"
)

func newDoctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Check that the required external tool (mdfetch) is installed.",
		RunE: func(cc *cobra.Command, _ []string) error {
			if code := doctor.Run(cc.Context(), cc.OutOrStdout(), cc.ErrOrStderr()); code != 0 {
				return &ExitCodeError{Code: code}
			}
			return nil
		},
	}
}
