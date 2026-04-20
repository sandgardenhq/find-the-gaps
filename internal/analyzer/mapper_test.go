package analyzer_test

import (
	"context"
	"errors"
	"testing"

	"github.com/sandgardenhq/find-the-gaps/internal/analyzer"
	"github.com/sandgardenhq/find-the-gaps/internal/scanner"
)

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
	got, err := analyzer.MapFeaturesToCode(context.Background(), c, features, scan)
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
	got, err := analyzer.MapFeaturesToCode(context.Background(), c, []string{}, &scanner.ProjectScan{})
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
	_, err := analyzer.MapFeaturesToCode(context.Background(), c, []string{"f1"}, &scanner.ProjectScan{})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestMapFeaturesToCode_InvalidJSON_ReturnsError(t *testing.T) {
	c := &fakeClient{responses: []string{"not json"}}
	_, err := analyzer.MapFeaturesToCode(context.Background(), c, []string{"f1"}, &scanner.ProjectScan{})
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestMapFeaturesToCode_NilFilesAndSymbols_NormalizedToEmpty(t *testing.T) {
	c := &fakeClient{responses: []string{
		`[{"feature":"f","files":null,"symbols":null}]`,
	}}
	got, err := analyzer.MapFeaturesToCode(context.Background(), c, []string{"f"}, &scanner.ProjectScan{})
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
