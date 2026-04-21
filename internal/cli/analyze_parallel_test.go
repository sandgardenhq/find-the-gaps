package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sandgardenhq/find-the-gaps/internal/analyzer"
	"github.com/sandgardenhq/find-the-gaps/internal/scanner"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunBothMapsInParallel(t *testing.T) {
	codeMap, docsMap, err := runBothMaps(
		context.Background(),
		&stubLLMClient{
			codeResp: `[{"feature":"auth","files":["auth.go"],"symbols":["Login"]}]`,
			docsResp: `["auth"]`,
		},
		analyzer.NewTiktokenCounter(),
		[]string{"auth"},
		stubScan(),
		map[string]string{
			"https://example.com/auth": writeTempFile(t, "auth content"),
		},
		2,      // workers
		10_000, // docsTokenBudget
		nil,    // onCodeBatch
		nil,    // onDocsPage
	)
	require.NoError(t, err)
	require.Len(t, codeMap, 1)
	assert.Equal(t, "auth", codeMap[0].Feature)
	require.Len(t, docsMap, 1)
	assert.Equal(t, "auth", docsMap[0].Feature)
}

// --- stubs ---

type stubLLMClient struct {
	codeResp string
	docsResp string
}

func (s *stubLLMClient) Complete(_ context.Context, prompt string) (string, error) {
	if strings.Contains(prompt, "Code symbols") {
		return s.codeResp, nil
	}
	return s.docsResp, nil
}

func stubScan() *scanner.ProjectScan {
	return &scanner.ProjectScan{
		RepoPath:  "/fake",
		ScannedAt: time.Now(),
		Files: []scanner.ScannedFile{
			{
				Path:     "auth.go",
				Language: "go",
				Symbols:  []scanner.Symbol{{Name: "Login", Kind: scanner.KindFunc}},
			},
		},
	}
}

func writeTempFile(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "page.md")
	require.NoError(t, os.WriteFile(p, []byte(content), 0o644))
	return p
}
