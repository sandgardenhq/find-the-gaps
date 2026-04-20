package analyzer_test

import (
	"context"
	"errors"
	"testing"

	"github.com/sandgardenhq/find-the-gaps/internal/analyzer"
	"github.com/sandgardenhq/find-the-gaps/internal/scanner"
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

	features := []string{"gap analysis", "doctor command"}
	got, err := analyzer.MapFeaturesToCode(context.Background(), c, analyzer.NewTiktokenCounter(), features, scan, analyzer.MapperTokenBudget)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(got))
	}
	if got[0].Feature != "gap analysis" {
		t.Errorf("Feature[0]: got %q", got[0].Feature)
	}
	if len(got[0].Files) == 0 {
		t.Error("Files must not be empty for gap analysis")
	}
}

func TestMapFeaturesToCode_EmptyFeatures_ReturnsEmpty(t *testing.T) {
	c := &fakeClient{responses: []string{`[]`}}
	got, err := analyzer.MapFeaturesToCode(context.Background(), c, analyzer.NewTiktokenCounter(), []string{}, &scanner.ProjectScan{}, analyzer.MapperTokenBudget)
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
	_, err := analyzer.MapFeaturesToCode(context.Background(), c, analyzer.NewTiktokenCounter(), []string{"f1"}, &scanner.ProjectScan{
		Files: []scanner.ScannedFile{
			{Path: "a.go", Symbols: []scanner.Symbol{{Name: "Foo"}}},
		},
	}, analyzer.MapperTokenBudget)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestMapFeaturesToCode_InvalidJSON_ReturnsError(t *testing.T) {
	c := &fakeClient{responses: []string{"not json"}}
	_, err := analyzer.MapFeaturesToCode(context.Background(), c, analyzer.NewTiktokenCounter(), []string{"f1"}, &scanner.ProjectScan{
		Files: []scanner.ScannedFile{
			{Path: "a.go", Symbols: []scanner.Symbol{{Name: "Foo"}}},
		},
	}, analyzer.MapperTokenBudget)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestMapFeaturesToCode_NilFilesAndSymbols_NormalizedToEmpty(t *testing.T) {
	c := &fakeClient{responses: []string{
		`[{"feature":"f","files":null,"symbols":null}]`,
	}}
	got, err := analyzer.MapFeaturesToCode(context.Background(), c, analyzer.NewTiktokenCounter(), []string{"f"}, &scanner.ProjectScan{
		Files: []scanner.ScannedFile{
			{Path: "a.go", Symbols: []scanner.Symbol{{Name: "Foo"}}},
		},
	}, analyzer.MapperTokenBudget)
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

	got, err := analyzer.MapFeaturesToCode(context.Background(), c, counter, []string{"auth"}, scan, 1)
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

	got, err := analyzer.MapFeaturesToCode(context.Background(), c, counter, []string{"auth"}, scan, 80_000)
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
	got, err := analyzer.MapFeaturesToCode(context.Background(), c, analyzer.NewTiktokenCounter(), []string{"auth"}, scan, 80_000)
	if err != nil {
		t.Fatal(err)
	}
	// No sym lines → no batches → no LLM calls → empty result
	if c.callCount != 0 {
		t.Errorf("expected 0 LLM calls, got %d", c.callCount)
	}
	_ = got
}
