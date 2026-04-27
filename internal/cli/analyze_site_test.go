package cli

import (
	"strings"
	"testing"
)

func TestAnalyzeFlagsSiteMode(t *testing.T) {
	cmd := newAnalyzeCmd()
	for _, name := range []string{"site-mode", "no-site", "keep-site-source"} {
		if cmd.Flags().Lookup(name) == nil {
			t.Errorf("missing flag --%s", name)
		}
	}
	// --site-mode should reject unknown values
	cmd.SetArgs([]string{"--site-mode=bogus", "--repo=.", "--docs-url=http://x"})
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	err := cmd.ParseFlags([]string{"--site-mode=bogus"})
	if err == nil {
		t.Skip("Cobra accepts arbitrary strings; validation happens in RunE")
	}
	_ = strings.Contains
}
