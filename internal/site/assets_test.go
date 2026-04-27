package site

import (
	"io/fs"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestThemeFSContainsHugoTomlSchema(t *testing.T) {
	t.Parallel()

	info, err := fs.Stat(themeFS, "assets/theme/hextra/theme.toml")
	require.NoError(t, err, "theme.toml must be embedded in themeFS")
	require.False(t, info.IsDir(), "theme.toml must be a file, not a directory")
	require.Greater(t, info.Size(), int64(0), "theme.toml must be non-empty")
}

func TestTemplatesFSExists(t *testing.T) {
	t.Parallel()

	entries, err := fs.ReadDir(templatesFS, "assets/templates")
	require.NoError(t, err, "templatesFS must contain assets/templates directory")
	// Directory exists; contents may be empty or contain only .gitkeep.
	_ = entries
}
