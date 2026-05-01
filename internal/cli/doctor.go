package cli

import (
	"fmt"
	"io"
	"os"

	"github.com/sandgardenhq/find-the-gaps/internal/doctor"
	"github.com/spf13/cobra"
)

func newDoctorCmd() *cobra.Command {
	var llmSmall, llmTypical, llmLarge string

	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Check that the required external tools (mdfetch, hugo) are installed and report resolved LLM tier capabilities.",
		RunE: func(cc *cobra.Command, _ []string) error {
			code := doctor.Run(cc.Context(), cc.OutOrStdout(), cc.ErrOrStderr())

			// Tier capability lines print regardless of external-tool status:
			// they describe what the next analyze run would resolve, which is
			// useful even when mdfetch/hugo are missing.
			if llmSmall == "" {
				llmSmall = os.Getenv("FIND_THE_GAPS_LLM_SMALL")
			}
			if llmTypical == "" {
				llmTypical = os.Getenv("FIND_THE_GAPS_LLM_TYPICAL")
			}
			if llmLarge == "" {
				llmLarge = os.Getenv("FIND_THE_GAPS_LLM_LARGE")
			}
			printTierCapabilities(cc.OutOrStdout(), llmSmall, llmTypical, llmLarge)

			if code != 0 {
				return &ExitCodeError{Code: code}
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&llmSmall, "llm-small", "",
		"small-tier model as \"provider/model\" (default: anthropic/claude-haiku-4-5)")
	cmd.Flags().StringVar(&llmTypical, "llm-typical", "",
		"typical-tier model as \"provider/model\" (default: anthropic/claude-sonnet-4-6)")
	cmd.Flags().StringVar(&llmLarge, "llm-large", "",
		"large-tier model as \"provider/model\" (default: anthropic/claude-opus-4-7)")

	return cmd
}

// printTierCapabilities renders one line per tier showing the resolved
// (provider, model) pair and the boolean capability flags from the per-model
// registry. Empty inputs fall back to the same defaults the analyze command
// uses (defaultSmallTier / defaultTypicalTier / defaultLargeTier) so doctor
// shows what the next analyze run would actually use.
func printTierCapabilities(w io.Writer, small, typical, large string) {
	for _, tc := range []struct {
		name, raw string
		fallback  string
	}{
		{"small", small, defaultSmallTier},
		{"typical", typical, defaultTypicalTier},
		{"large", large, defaultLargeTier},
	} {
		s := tc.raw
		if s == "" {
			s = tc.fallback
		}
		provider, model, err := parseTierString(s)
		if err != nil {
			_, _ = fmt.Fprintf(w, "%s: %s (invalid: %v)\n", tc.name, s, err)
			continue
		}
		caps, ok := ResolveCapabilities(provider, model)
		if !ok {
			_, _ = fmt.Fprintf(w, "%s: %s/%s (unknown provider)\n", tc.name, provider, model)
			continue
		}
		_, _ = fmt.Fprintf(w, "%s: %s/%s (tool_use=%t vision=%t)\n",
			tc.name, provider, model, caps.ToolUse, caps.Vision)
	}
}
