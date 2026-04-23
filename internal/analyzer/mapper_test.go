package analyzer_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/charmbracelet/log"
	"github.com/sandgardenhq/find-the-gaps/internal/analyzer"
	"github.com/sandgardenhq/find-the-gaps/internal/scanner"
	"github.com/stretchr/testify/assert"
)

// fakeCounter returns a fixed token count for every input, regardless of content.
type fakeCounter struct{ n int }

func (f *fakeCounter) CountTokens(_ context.Context, _ string) (int, error) {
	return f.n, nil
}

// splitForcingCounter returns over-budget when the input has more than ~50 chars,
// simulating a provider that reports a large batch as too long.
type splitForcingCounter struct{ budget int }

func (s *splitForcingCounter) CountTokens(_ context.Context, text string) (int, error) {
	if len(text) > 50 {
		return s.budget + 1, nil // over budget → triggers split
	}
	return 1, nil // single-line prompt always fits
}

func TestMapFeaturesToCode_ReturnsMappings(t *testing.T) {
	c := &fakeClient{responses: []string{
		`[{"feature":"gap analysis","files":["internal/analyzer/analyzer.go"],"symbols":["AnalyzePage"]},{"feature":"doctor command","files":["internal/cli/doctor.go"],"symbols":["RunDoctor"]}]`,
	}}

	scan := &scanner.ProjectScan{
		Files: []scanner.ScannedFile{
			{Path: "internal/analyzer/analyzer.go", Language: "go", Symbols: []scanner.Symbol{{Name: "AnalyzePage"}}},
			{Path: "internal/cli/doctor.go", Language: "go", Symbols: []scanner.Symbol{{Name: "RunDoctor"}}},
		},
	}

	features := []analyzer.CodeFeature{
		{Name: "gap analysis", Description: "Identifies doc gaps.", Layer: "analysis engine", UserFacing: false},
		{Name: "doctor command", Description: "Checks external deps.", Layer: "cli", UserFacing: true},
	}
	got, err := analyzer.MapFeaturesToCode(context.Background(), c, analyzer.NewTiktokenCounter(), features, scan, analyzer.MapperTokenBudget, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(got))
	}
	if got[0].Feature.Name != "gap analysis" {
		t.Errorf("Feature[0].Name: got %q", got[0].Feature.Name)
	}
	assert.Equal(t, "Identifies doc gaps.", got[0].Feature.Description)
	assert.Equal(t, "analysis engine", got[0].Feature.Layer)
	assert.Equal(t, false, got[0].Feature.UserFacing)
	if len(got[0].Files) == 0 {
		t.Error("Files must not be empty for gap analysis")
	}
}

func TestMapFeaturesToCode_EmptyFeatures_ReturnsEmpty(t *testing.T) {
	c := &fakeClient{responses: []string{`[]`}}
	got, err := analyzer.MapFeaturesToCode(context.Background(), c, analyzer.NewTiktokenCounter(), []analyzer.CodeFeature{}, &scanner.ProjectScan{}, analyzer.MapperTokenBudget, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty map, got %v", got)
	}
	// LLM must NOT be called for an empty feature list
	if c.callCount != 0 {
		t.Errorf("expected 0 LLM calls for empty features, got %d", c.callCount)
	}
}

func TestMapFeaturesToCode_ClientError_Propagates(t *testing.T) {
	c := &fakeClient{forcedErr: errors.New("llm down")}
	_, err := analyzer.MapFeaturesToCode(context.Background(), c, analyzer.NewTiktokenCounter(), []analyzer.CodeFeature{{Name: "f1"}}, &scanner.ProjectScan{
		Files: []scanner.ScannedFile{
			{Path: "a.go", Symbols: []scanner.Symbol{{Name: "Foo"}}},
		},
	}, analyzer.MapperTokenBudget, false, nil)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestMapFeaturesToCode_InvalidJSON_ReturnsError(t *testing.T) {
	c := &fakeClient{responses: []string{"not json"}}
	_, err := analyzer.MapFeaturesToCode(context.Background(), c, analyzer.NewTiktokenCounter(), []analyzer.CodeFeature{{Name: "f1"}}, &scanner.ProjectScan{
		Files: []scanner.ScannedFile{
			{Path: "a.go", Symbols: []scanner.Symbol{{Name: "Foo"}}},
		},
	}, analyzer.MapperTokenBudget, false, nil)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestMapFeaturesToCode_NilFilesAndSymbols_NormalizedToEmpty(t *testing.T) {
	c := &fakeClient{responses: []string{
		`[{"feature":"f","files":null,"symbols":null}]`,
	}}
	got, err := analyzer.MapFeaturesToCode(context.Background(), c, analyzer.NewTiktokenCounter(), []analyzer.CodeFeature{{Name: "f"}}, &scanner.ProjectScan{
		Files: []scanner.ScannedFile{
			{Path: "a.go", Symbols: []scanner.Symbol{{Name: "Foo"}}},
		},
	}, analyzer.MapperTokenBudget, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(got))
	}
	if got[0].Files == nil {
		t.Error("Files must be normalized to empty slice, not nil")
	}
	if got[0].Symbols == nil {
		t.Error("Symbols must be normalized to empty slice, not nil")
	}
}

func TestMapFeaturesToCode_MultipleBatches_MergesResults(t *testing.T) {
	// budget=1 forces one sym line per batch (batchSymLines), and fakeCounter always
	// returns 0 so no split-and-retry occurs. Result: 2 batches, 2 LLM calls.
	c := &fakeClient{responses: []string{
		`[{"feature":"auth","files":["auth.go"],"symbols":["Login"]}]`,
		`[{"feature":"auth","files":["session.go"],"symbols":["NewSession"]}]`,
	}}
	counter := &fakeCounter{n: 0} // always fits, no retry

	scan := &scanner.ProjectScan{
		Files: []scanner.ScannedFile{
			{Path: "auth.go", Symbols: []scanner.Symbol{{Name: "Login"}}},
			{Path: "session.go", Symbols: []scanner.Symbol{{Name: "NewSession"}}},
		},
	}

	got, err := analyzer.MapFeaturesToCode(context.Background(), c, counter, []analyzer.CodeFeature{{Name: "auth"}}, scan, 1, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 feature entry, got %d", len(got))
	}
	if len(got[0].Files) != 2 {
		t.Errorf("expected 2 files merged, got %v", got[0].Files)
	}
	if c.callCount != 2 {
		t.Errorf("expected 2 LLM calls, got %d", c.callCount)
	}
}

func TestMapFeaturesToCode_CounterOverBudget_SplitsBatch(t *testing.T) {
	// fakeCounter returns a count over budget, forcing the mapper to split every
	// 2-line batch into 1-line batches. Verifies split-and-retry logic.
	c := &fakeClient{responses: []string{
		`[{"feature":"auth","files":["auth.go"],"symbols":["Login"]}]`,
		`[{"feature":"auth","files":["session.go"],"symbols":["NewSession"]}]`,
	}}
	// Counter always says "over budget" — but the batcher already put 2 lines per batch.
	// The mapper must split them into 1-line batches and retry.
	// We use a counter that returns 999999 (always over) until the batch is 1 line.
	counter := &splitForcingCounter{budget: 80_000}

	scan := &scanner.ProjectScan{
		Files: []scanner.ScannedFile{
			{Path: "auth.go", Symbols: []scanner.Symbol{{Name: "Login"}}},
			{Path: "session.go", Symbols: []scanner.Symbol{{Name: "NewSession"}}},
		},
	}

	got, err := analyzer.MapFeaturesToCode(context.Background(), c, counter, []analyzer.CodeFeature{{Name: "auth"}}, scan, 80_000, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got[0].Files) != 2 {
		t.Errorf("expected 2 files merged, got %v", got[0].Files)
	}
	if c.callCount != 2 {
		t.Errorf("expected 2 LLM calls after forced split, got %d", c.callCount)
	}
}

func TestMapFeaturesToCode_FilesWithNoSymbols_Skipped(t *testing.T) {
	// Files with no symbols contribute no sym lines and produce no batches.
	c := &fakeClient{responses: []string{`[]`}}
	scan := &scanner.ProjectScan{
		Files: []scanner.ScannedFile{
			{Path: "empty.go", Symbols: nil},
		},
	}
	got, err := analyzer.MapFeaturesToCode(context.Background(), c, analyzer.NewTiktokenCounter(), []analyzer.CodeFeature{{Name: "auth"}}, scan, 80_000, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	// No sym lines → no batches → no LLM calls → empty result
	if c.callCount != 0 {
		t.Errorf("expected 0 LLM calls, got %d", c.callCount)
	}
	if len(got) != 0 {
		t.Errorf("expected empty FeatureMap, got %v", got)
	}
}

func TestMapFeaturesToCode_AllFilesProcessed_NoneSkipped(t *testing.T) {
	// budget=1 is below any real featuresTokens, so remaining is negative and every
	// sym line lands in its own batch → 5 files = 5 LLM calls.
	// This verifies the batcher processes every file regardless of budget pressure.
	responses := []string{
		`[{"feature":"f","files":["a.go"],"symbols":[]}]`,
		`[{"feature":"f","files":["b.go"],"symbols":[]}]`,
		`[{"feature":"f","files":["c.go"],"symbols":[]}]`,
		`[{"feature":"f","files":["d.go"],"symbols":[]}]`,
		`[{"feature":"f","files":["e.go"],"symbols":[]}]`,
	}
	c := &fakeClient{responses: responses}
	counter := &fakeCounter{n: 0} // always fits, no split-and-retry

	files := make([]scanner.ScannedFile, 5)
	names := []string{"a.go", "b.go", "c.go", "d.go", "e.go"}
	for i, name := range names {
		files[i] = scanner.ScannedFile{Path: name, Symbols: []scanner.Symbol{{Name: "Sym"}}}
	}
	scan := &scanner.ProjectScan{Files: files}
	_, err := analyzer.MapFeaturesToCode(context.Background(), c, counter, []analyzer.CodeFeature{{Name: "f"}}, scan, 1, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if c.callCount != 5 {
		t.Errorf("expected 5 LLM calls (one per file), got %d — some files may have been skipped", c.callCount)
	}
}

func TestMapFeaturesToCode_TinyBudget_AllFilesStillCovered(t *testing.T) {
	// With budget=0, remaining goes negative and every line lands in its own batch.
	// Verifies that even with extreme fragmentation, all files appear in the merged result.
	c := &fakeClient{responses: []string{
		`[{"feature":"f","files":["a.go"],"symbols":["A"]}]`,
		`[{"feature":"f","files":["b.go"],"symbols":["B"]}]`,
	}}
	scan := &scanner.ProjectScan{
		Files: []scanner.ScannedFile{
			{Path: "a.go", Symbols: []scanner.Symbol{{Name: "A"}}},
			{Path: "b.go", Symbols: []scanner.Symbol{{Name: "B"}}},
		},
	}
	counter := &fakeCounter{n: 0} // always fits
	got, err := analyzer.MapFeaturesToCode(context.Background(), c, counter, []analyzer.CodeFeature{{Name: "f"}}, scan, 0, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got[0].Files) != 2 {
		t.Errorf("expected both files in result, got %v", got[0].Files)
	}
}

func TestMapFeaturesToCode_MixedScan_SymbollessFilesSkippedInCoverageCheck(t *testing.T) {
	// A scan with both files-with-symbols and files-without-symbols exercises the
	// len(f.Symbols)==0 continue branch in the post-batch coverage check loop.
	c := &fakeClient{responses: []string{
		`[{"feature":"f","files":["a.go"],"symbols":["A"]}]`,
	}}
	counter := &fakeCounter{n: 0}
	scan := &scanner.ProjectScan{
		Files: []scanner.ScannedFile{
			{Path: "a.go", Symbols: []scanner.Symbol{{Name: "A"}}}, // has symbols
			{Path: "b.go", Symbols: nil},                           // no symbols — skipped in coverage check
		},
	}
	_, err := analyzer.MapFeaturesToCode(context.Background(), c, counter, []analyzer.CodeFeature{{Name: "f"}}, scan, 80_000, false, nil)
	if err != nil {
		t.Fatal(err)
	}
}

// errorCounter is a TokenCounter that always returns an error.
type errorCounter struct{}

func (e *errorCounter) CountTokens(_ context.Context, _ string) (int, error) {
	return 0, errors.New("counter failed")
}

func TestMapFeaturesToCode_CounterError_Propagates(t *testing.T) {
	c := &fakeClient{responses: []string{}}
	scan := &scanner.ProjectScan{
		Files: []scanner.ScannedFile{
			{Path: "a.go", Symbols: []scanner.Symbol{{Name: "A"}}},
		},
	}
	_, err := analyzer.MapFeaturesToCode(context.Background(), c, &errorCounter{}, []analyzer.CodeFeature{{Name: "f"}}, scan, 80_000, false, nil)
	if err == nil {
		t.Fatal("expected counter error, got nil")
	}
}

func TestMapFeaturesToCode_PathWithColonSpace_Works(t *testing.T) {
	// Paths containing ": " must not trigger a false coverage-check error.
	// Previously, strings.SplitN re-parsing of batch lines extracted only the
	// prefix before ": ", so "a: b.go" was stored as "a" in the batched set,
	// causing a spurious "was not included in any batch" error.
	c := &fakeClient{responses: []string{
		`[{"feature":"f","files":["a: b.go"],"symbols":["Sym"]}]`,
	}}
	counter := &fakeCounter{n: 0}
	scan := &scanner.ProjectScan{
		Files: []scanner.ScannedFile{
			{Path: "a: b.go", Symbols: []scanner.Symbol{{Name: "Sym"}}},
		},
	}
	got, err := analyzer.MapFeaturesToCode(context.Background(), c, counter, []analyzer.CodeFeature{{Name: "f"}}, scan, 80_000, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(got))
	}
}

func TestMapFeaturesToCode_UnknownFeatureInResponse_Ignored(t *testing.T) {
	// LLM returns "unknown-feature" which was not in the input list.
	// It should be silently skipped; only the known feature appears in output.
	c := &fakeClient{responses: []string{
		`[{"feature":"f","files":["a.go"],"symbols":["A"]},{"feature":"unknown-feature","files":["x.go"],"symbols":[]}]`,
	}}
	counter := &fakeCounter{n: 0}
	scan := &scanner.ProjectScan{
		Files: []scanner.ScannedFile{
			{Path: "a.go", Symbols: []scanner.Symbol{{Name: "A"}}},
		},
	}
	got, err := analyzer.MapFeaturesToCode(context.Background(), c, counter, []analyzer.CodeFeature{{Name: "f"}}, scan, 80_000, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 feature entry, got %d", len(got))
	}
	if got[0].Feature.Name != "f" {
		t.Errorf("expected feature name 'f', got %q", got[0].Feature.Name)
	}
}

func TestMapFeaturesToCode_LogsBatchProgress(t *testing.T) {
	var buf bytes.Buffer
	log.SetOutput(&buf)
	log.SetLevel(log.InfoLevel)
	t.Cleanup(func() {
		log.SetOutput(os.Stderr)
		log.SetLevel(log.InfoLevel)
	})

	c := &fakeClient{responses: []string{
		`[{"feature":"f","files":["a.go"],"symbols":["A"]}]`,
	}}
	scan := &scanner.ProjectScan{
		Files: []scanner.ScannedFile{
			{Path: "a.go", Symbols: []scanner.Symbol{{Name: "A"}}},
		},
	}

	_, err := analyzer.MapFeaturesToCode(context.Background(), c, &fakeCounter{n: 0}, []analyzer.CodeFeature{{Name: "f"}}, scan, 80_000, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "batch") {
		t.Errorf("expected batch progress in log output; got: %q", buf.String())
	}
}

func TestMapFeaturesToCode_OnBatchCalled_PerLLMCall(t *testing.T) {
	callCount := 0
	c := &fakeClient{responses: []string{
		`[{"feature":"f","files":["a.go"],"symbols":["A"]}]`,
		`[{"feature":"f","files":["b.go"],"symbols":["B"]}]`,
	}}
	counter := &fakeCounter{n: 0}
	scan := &scanner.ProjectScan{
		Files: []scanner.ScannedFile{
			{Path: "a.go", Symbols: []scanner.Symbol{{Name: "A"}}},
			{Path: "b.go", Symbols: []scanner.Symbol{{Name: "B"}}},
		},
	}

	_, err := analyzer.MapFeaturesToCode(context.Background(), c, counter, []analyzer.CodeFeature{{Name: "f"}}, scan, 1, false,
		func(partial analyzer.FeatureMap) error {
			callCount++
			return nil
		})
	if err != nil {
		t.Fatal(err)
	}
	if callCount != 2 {
		t.Errorf("expected onBatch called 2 times, got %d", callCount)
	}
}

func TestMapFeaturesToCode_OnBatchError_Aborts(t *testing.T) {
	c := &fakeClient{responses: []string{
		`[{"feature":"f","files":["a.go"],"symbols":["A"]}]`,
		`[{"feature":"f","files":["b.go"],"symbols":["B"]}]`,
	}}
	counter := &fakeCounter{n: 0}
	scan := &scanner.ProjectScan{
		Files: []scanner.ScannedFile{
			{Path: "a.go", Symbols: []scanner.Symbol{{Name: "A"}}},
			{Path: "b.go", Symbols: []scanner.Symbol{{Name: "B"}}},
		},
	}

	callCount := 0
	_, err := analyzer.MapFeaturesToCode(context.Background(), c, counter, []analyzer.CodeFeature{{Name: "f"}}, scan, 1, false,
		func(partial analyzer.FeatureMap) error {
			callCount++
			return errors.New("disk full")
		})
	if err == nil {
		t.Fatal("expected error from onBatch")
	}
	if callCount != 1 {
		t.Errorf("expected onBatch called once before abort, got %d", callCount)
	}
}

func TestMapFeaturesToCode_FilesOnly_PromptOmitsSymbolNames(t *testing.T) {
	client := &fakeClient{responses: []string{`[{"feature":"auth","files":["auth.go"]}]`}}
	scan := &scanner.ProjectScan{
		Files: []scanner.ScannedFile{
			{Path: "auth.go", Symbols: []scanner.Symbol{{Name: "Login", Kind: scanner.KindFunc}}},
		},
	}
	_, err := analyzer.MapFeaturesToCode(context.Background(), client, &fakeCounter{n: 0}, []analyzer.CodeFeature{{Name: "auth"}}, scan, 80_000, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(client.receivedPrompts) == 0 {
		t.Fatal("expected LLM to be called")
	}
	prompt := client.receivedPrompts[0]
	if strings.Contains(prompt, ": Login") {
		t.Errorf("filesOnly prompt must not contain symbol names, but found ': Login' in:\n%s", prompt)
	}
	if !strings.Contains(prompt, "auth.go") {
		t.Error("filesOnly prompt must still contain file paths")
	}
}

func TestMapFeaturesToCode_FilesOnly_SymbolsAlwaysEmpty(t *testing.T) {
	// Even if the LLM incorrectly returns symbols, they must be stripped in filesOnly mode.
	client := &fakeClient{responses: []string{`[{"feature":"auth","files":["auth.go"],"symbols":["Login"]}]`}}
	scan := &scanner.ProjectScan{
		Files: []scanner.ScannedFile{
			{Path: "auth.go", Symbols: []scanner.Symbol{{Name: "Login", Kind: scanner.KindFunc}}},
		},
	}
	got, err := analyzer.MapFeaturesToCode(context.Background(), client, &fakeCounter{n: 0}, []analyzer.CodeFeature{{Name: "auth"}}, scan, 80_000, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) == 0 {
		t.Fatal("expected one entry")
	}
	if len(got[0].Symbols) != 0 {
		t.Errorf("filesOnly mode must produce empty Symbols, got %v", got[0].Symbols)
	}
	if len(got[0].Files) == 0 {
		t.Error("filesOnly mode must still return matched files")
	}
}
