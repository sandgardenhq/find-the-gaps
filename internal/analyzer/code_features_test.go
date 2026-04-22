package analyzer_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/sandgardenhq/find-the-gaps/internal/analyzer"
	"github.com/sandgardenhq/find-the-gaps/internal/scanner"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExtractFeaturesFromCode_ReturnsFeatures(t *testing.T) {
	c := &fakeClient{responses: []string{`[{"name":"feature one","description":"Does X.","layer":"cli","user_facing":true},{"name":"feature two","description":"Does Y.","layer":"analysis engine","user_facing":false}]`}}
	scan := &scanner.ProjectScan{
		Files: []scanner.ScannedFile{
			{Path: "internal/auth/auth.go", Symbols: []scanner.Symbol{{Name: "Authenticate"}}},
			{Path: "internal/upload/upload.go", Symbols: []scanner.Symbol{{Name: "Upload"}}},
		},
	}
	got, err := analyzer.ExtractFeaturesFromCode(context.Background(), c, scan)
	if err != nil {
		t.Fatal(err)
	}
	require.Len(t, got, 2)
	assert.Equal(t, "feature one", got[0].Name)
	assert.Equal(t, "Does X.", got[0].Description)
	assert.Equal(t, "cli", got[0].Layer)
	assert.True(t, got[0].UserFacing)
	assert.Equal(t, "feature two", got[1].Name)
	assert.False(t, got[1].UserFacing)
	assert.Equal(t, "Does Y.", got[1].Description)
	assert.Equal(t, "analysis engine", got[1].Layer)
}

func TestExtractFeaturesFromCode_EmptyScan_ReturnsEmpty(t *testing.T) {
	c := &fakeClient{}
	scan := &scanner.ProjectScan{Files: []scanner.ScannedFile{}}
	got, err := analyzer.ExtractFeaturesFromCode(context.Background(), c, scan)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty, got %v", got)
	}
	if c.callCount != 0 {
		t.Error("expected no LLM call for empty scan")
	}
}

func TestExtractFeaturesFromCode_NoSymbols_ReturnsEmpty(t *testing.T) {
	c := &fakeClient{}
	scan := &scanner.ProjectScan{
		Files: []scanner.ScannedFile{
			{Path: "README.md", Symbols: nil},
			{Path: "internal/foo.go", Symbols: []scanner.Symbol{}},
		},
	}
	got, err := analyzer.ExtractFeaturesFromCode(context.Background(), c, scan)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty, got %v", got)
	}
	if c.callCount != 0 {
		t.Error("expected no LLM call when no files have symbols")
	}
}

func TestExtractFeaturesFromCode_ClientError_Propagates(t *testing.T) {
	c := &fakeClient{forcedErr: errors.New("network down")}
	scan := &scanner.ProjectScan{
		Files: []scanner.ScannedFile{
			{Path: "auth.go", Symbols: []scanner.Symbol{{Name: "Auth"}}},
		},
	}
	_, err := analyzer.ExtractFeaturesFromCode(context.Background(), c, scan)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestExtractFeaturesFromCode_InvalidJSON_ReturnsError(t *testing.T) {
	c := &fakeClient{responses: []string{"not valid json"}}
	scan := &scanner.ProjectScan{
		Files: []scanner.ScannedFile{
			{Path: "auth.go", Symbols: []scanner.Symbol{{Name: "Auth"}}},
		},
	}
	_, err := analyzer.ExtractFeaturesFromCode(context.Background(), c, scan)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestExtractFeaturesFromCode_NilResponse_NormalizedToEmpty(t *testing.T) {
	// LLM returns explicit null
	c := &fakeClient{responses: []string{`null`}}
	scan := &scanner.ProjectScan{
		Files: []scanner.ScannedFile{
			{Path: "auth.go", Symbols: []scanner.Symbol{{Name: "Auth"}}},
		},
	}
	got, err := analyzer.ExtractFeaturesFromCode(context.Background(), c, scan)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Error("nil features must be normalized to empty slice")
	}
}

func TestExtractFeaturesFromCode_DeduplicatesWithinBatch(t *testing.T) {
	// LLM returns a response with a duplicate name — result must deduplicate.
	c := &fakeClient{responses: []string{`[{"name":"authentication","description":"Auth.","layer":"cli","user_facing":true},{"name":"authentication","description":"Auth again.","layer":"cli","user_facing":true},{"name":"file upload","description":"Upload.","layer":"cli","user_facing":true}]`}}
	scan := &scanner.ProjectScan{
		Files: []scanner.ScannedFile{
			{Path: "auth.go", Symbols: []scanner.Symbol{{Name: "Auth"}}},
		},
	}
	got, err := analyzer.ExtractFeaturesFromCode(context.Background(), c, scan)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Errorf("got %d features, want 2 (duplicates must be removed)", len(got))
	}
	seen := make(map[string]int)
	for _, f := range got {
		seen[f.Name]++
	}
	for feat, count := range seen {
		if count > 1 {
			t.Errorf("feature %q appears %d times; want exactly 1", feat, count)
		}
	}
}

func TestExtractFeaturesFromCode_PromptContainsSymbols(t *testing.T) {
	// Verify the prompt sent to the LLM includes the file path and symbol name.
	c := &fakeClient{responses: []string{`[{"name":"some feature","description":"Does something.","layer":"cli","user_facing":true}]`}}
	scan := &scanner.ProjectScan{
		Files: []scanner.ScannedFile{
			{Path: "internal/auth/handler.go", Symbols: []scanner.Symbol{{Name: "HandleLogin"}}},
		},
	}
	_, err := analyzer.ExtractFeaturesFromCode(context.Background(), c, scan)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.receivedPrompts) == 0 {
		t.Fatal("expected at least one prompt")
	}
	prompt := c.receivedPrompts[0]
	if !strings.Contains(prompt, "internal/auth/handler.go") {
		t.Error("prompt must include file path")
	}
	if !strings.Contains(prompt, "HandleLogin") {
		t.Error("prompt must include symbol name")
	}
}
