package cli

import (
	"github.com/sandgardenhq/find-the-gaps/internal/doctor"
	"github.com/spf13/cobra"
)

func newInstallDepsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "install-deps",
		Short: "Install required external tools (mdfetch, hugo).",
		Long:  "Install required external tools (mdfetch, hugo). mdfetch is always reinstalled to pull the latest published version; hugo is skipped if already on $PATH.",
		RunE: func(cc *cobra.Command, _ []string) error {
			if code := doctor.RunInstall(cc.Context(), cc.OutOrStdout(), cc.ErrOrStderr()); code != 0 {
				return &ExitCodeError{Code: code}
			}
			return nil
		},
	}
}
