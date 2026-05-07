package updatecheck

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRenderNotice_HeaderShowsBothVersions(t *testing.T) {
	out := RenderNotice("v1.3.0", "v1.4.2", "darwin", true)

	assert.Contains(t, out, "v1.4.2")
	assert.Contains(t, out, "v1.3.0")
	assert.Contains(t, out,
		"https://github.com/sandgardenhq/find-the-gaps/releases/tag/v1.4.2")
}

func TestRenderNotice_DarwinShowsBrewFirst(t *testing.T) {
	out := RenderNotice("v1.3.0", "v1.4.2", "darwin", false)

	brewIdx := strings.Index(out, "brew upgrade sandgardenhq/tap/find-the-gaps")
	goIdx := strings.Index(out, "go install github.com/sandgardenhq/find-the-gaps/cmd/find-the-gaps@latest")
	require.NotEqual(t, -1, brewIdx, "brew line missing")
	require.NotEqual(t, -1, goIdx, "go install line missing")
	assert.Less(t, brewIdx, goIdx, "brew should appear before go install on darwin")
}

func TestRenderNotice_LinuxWithBrewShowsBrewFirst(t *testing.T) {
	out := RenderNotice("v1.3.0", "v1.4.2", "linux", true)

	brewIdx := strings.Index(out, "brew upgrade sandgardenhq/tap/find-the-gaps")
	goIdx := strings.Index(out, "go install github.com/sandgardenhq/find-the-gaps/cmd/find-the-gaps@latest")
	require.NotEqual(t, -1, brewIdx)
	require.NotEqual(t, -1, goIdx)
	assert.Less(t, brewIdx, goIdx, "linux+brew should still show brew first")
}

func TestRenderNotice_LinuxWithoutBrewShowsGoInstallFirst(t *testing.T) {
	out := RenderNotice("v1.3.0", "v1.4.2", "linux", false)

	brewIdx := strings.Index(out, "brew upgrade sandgardenhq/tap/find-the-gaps")
	goIdx := strings.Index(out, "go install github.com/sandgardenhq/find-the-gaps/cmd/find-the-gaps@latest")
	require.NotEqual(t, -1, brewIdx, "linux without brew should still mention brew as a fallback")
	require.NotEqual(t, -1, goIdx)
	assert.Less(t, goIdx, brewIdx, "linux without brew should show go install first")
}

func TestRenderNotice_WindowsShowsGoInstallOnly(t *testing.T) {
	out := RenderNotice("v1.3.0", "v1.4.2", "windows", false)

	assert.Contains(t, out, "go install github.com/sandgardenhq/find-the-gaps/cmd/find-the-gaps@latest")
	assert.NotContains(t, out, "brew upgrade",
		"windows should not mention brew")
}

func TestRenderNotice_TrailingNewlineForCleanStderr(t *testing.T) {
	out := RenderNotice("v1.3.0", "v1.4.2", "darwin", true)
	assert.True(t, strings.HasSuffix(out, "\n"),
		"notice must end with a newline so it doesn't run into the next prompt")
}
