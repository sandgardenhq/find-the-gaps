package pdf_test

import (
	"os"
	"path/filepath"
	"testing"

	pdfreader "github.com/ledongthuc/pdf"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sandgardenhq/find-the-gaps/internal/pdf"
)

func TestWriteReport_EmitsFile(t *testing.T) {
	dir := t.TempDir()

	err := pdf.WriteReport(dir, pdf.Inputs{ProjectName: "fixture"})
	require.NoError(t, err)

	path := filepath.Join(dir, "report.pdf")
	info, err := os.Stat(path)
	require.NoError(t, err, "report.pdf must be created")
	require.Greater(t, info.Size(), int64(0), "report.pdf must be non-empty")

	f, r, err := pdfreader.Open(path)
	require.NoError(t, err, "report.pdf must parse as a valid PDF")
	defer f.Close()

	assert.GreaterOrEqual(t, r.NumPage(), 1, "report.pdf must contain at least one page")
}

func TestWriteReport_ReturnsErrorWhenDirMissing(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "does-not-exist")

	err := pdf.WriteReport(missing, pdf.Inputs{ProjectName: "fixture"})
	require.Error(t, err)
}
