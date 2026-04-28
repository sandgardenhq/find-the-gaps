package cli

import "github.com/spf13/cobra"

func newServeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "serve",
		Short: "Serve the find-the-gaps report site over HTTP.",
		RunE: func(cc *cobra.Command, _ []string) error {
			return nil
		},
	}
}
