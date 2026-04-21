package cli

import (
	"context"
	"errors"
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
		false,  // filesOnly
		nil,    // onCodeBatch
		nil,    // onDocsPage
	)
	require.NoError(t, err)
	require.Len(t, codeMap, 1)
	assert.Equal(t, "auth", codeMap[0].Feature)
	require.Len(t, docsMap, 1)
	assert.Equal(t, "auth", docsMap[0].Feature)
}

func TestRunBothMaps_CodeMapError(t *testing.T) {
	_, _, err := runBothMaps(
		context.Background(),
		&stubLLMClient{
			codeResp: `not json`, // forces MapFeaturesToCode to fail
			docsResp: `[]`,
		},
		analyzer.NewTiktokenCounter(),
		[]string{"auth"},
		stubScan(),
		map[string]string{
			"https://example.com/auth": writeTempFile(t, "content"),
		},
		1, 10_000, false, nil, nil,
	)
	require.Error(t, err)
}

func TestRunBothMaps_DocsMapError(t *testing.T) {
	_, _, err := runBothMaps(
		context.Background(),
		&stubLLMClient{
			codeResp: `[{"feature":"auth","files":[],"symbols":[]}]`,
			docsResp: `not json`, // forces MapFeaturesToDocs page call to fail
		},
		analyzer.NewTiktokenCounter(),
		[]string{"auth"},
		stubScan(),
		map[string]string{
			"https://example.com/auth": writeTempFile(t, "content"),
		},
		1, 10_000, false, nil, nil,
	)
	// docs page errors are logged and skipped, not propagated — result is empty pages
	require.NoError(t, err)
}

// TestRunBothMaps_DocsMapError_ViaOnPageCallback exercises the docsRes.err != nil
// path in runBothMaps. MapFeaturesToDocs only propagates an error when the onPage
// progress callback itself returns an error; individual page LLM errors are logged
// and skipped. We pass a callback that always returns an error so the docs goroutine
// returns a non-nil error, triggering the second error guard in runBothMaps.
func TestRunBothMaps_DocsMapError_ViaOnPageCallback(t *testing.T) {
	callbackErr := errors.New("onPage callback error")
	onDocsPage := func(_ analyzer.DocsFeatureMap) error {
		return callbackErr
	}

	_, _, err := runBothMaps(
		context.Background(),
		&stubLLMClient{
			codeResp: `[]`, // empty but valid JSON — code map succeeds immediately
			docsResp: `["auth"]`,
		},
		analyzer.NewTiktokenCounter(),
		[]string{"auth"},
		// Empty scan so MapFeaturesToCode returns immediately (no LLM call, no error).
		&scanner.ProjectScan{Files: []scanner.ScannedFile{}},
		map[string]string{
			"https://example.com/auth": writeTempFile(t, "auth content"),
		},
		1,
		10_000,
		false,
		nil,
		onDocsPage, // docs progress callback that errors → propagated to runBothMaps
	)
	require.Error(t, err, "expected error propagated from onDocsPage callback")
}

func TestRunBothMaps_FilesOnly_PassedThrough(t *testing.T) {
	client := &stubLLMClient{
		codeResp: `[{"feature":"auth","files":["auth.go"]}]`,
		docsResp: `["auth"]`,
	}
	codeMap, _, err := runBothMaps(
		context.Background(),
		client,
		analyzer.NewTiktokenCounter(),
		[]string{"auth"},
		stubScan(),
		map[string]string{
			"https://example.com/auth": writeTempFile(t, "auth content"),
		},
		2,
		10_000,
		true,  // filesOnly
		nil,
		nil,
	)
	require.NoError(t, err)
	require.Len(t, codeMap, 1)
	assert.Empty(t, codeMap[0].Symbols, "filesOnly mode must produce empty Symbols")
}

func TestAnalyzeCmd_NoSymbolsFlag_Registered(t *testing.T) {
	cmd := newAnalyzeCmd()
	flag := cmd.Flags().Lookup("no-symbols")
	if flag == nil {
		t.Fatal("--no-symbols flag not registered on analyze command")
	}
	if flag.DefValue != "false" {
		t.Errorf("--no-symbols default should be false, got %q", flag.DefValue)
	}
}

// --- stubs ---

type stubLLMClient struct {
	codeResp string
	docsResp string
}

func (s *stubLLMClient) Complete(_ context.Context, prompt string) (string, error) {
	if strings.Contains(prompt, "Code symbols (format:") || strings.Contains(prompt, "Code files:\n") {
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
