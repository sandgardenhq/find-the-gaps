package reporter_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sandgardenhq/find-the-gaps/internal/analyzer"
	"github.com/sandgardenhq/find-the-gaps/internal/reporter"
	"github.com/sandgardenhq/find-the-gaps/internal/scanner"
)

func TestWriteMapping_CreatesFile(t *testing.T) {
	dir := t.TempDir()

	summary := analyzer.ProductSummary{
		Description: "A CLI tool for finding doc gaps.",
		Features:    []string{"gap analysis", "doctor command"},
	}
	mapping := analyzer.FeatureMap{
		{Feature: "gap analysis", Files: []string{"internal/analyzer/analyzer.go"}, Symbols: []string{"AnalyzePage"}},
		{Feature: "doctor command", Files: []string{"internal/cli/doctor.go"}, Symbols: []string{}},
	}
	pages := []analyzer.PageAnalysis{
		{URL: "https://docs.example.com/gap", Summary: "Covers gap analysis.", Features: []string{"gap analysis"}},
	}

	if err := reporter.WriteMapping(dir, summary, mapping, pages); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "mapping.md"))
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	if !strings.Contains(content, "gap analysis") {
		t.Error("mapping.md must mention 'gap analysis'")
	}
	if !strings.Contains(content, "A CLI tool") {
		t.Error("mapping.md must include product summary")
	}
	if !strings.Contains(content, "internal/analyzer/analyzer.go") {
		t.Error("mapping.md must include file paths")
	}
}

func TestWriteGaps_CreatesFile(t *testing.T) {
	dir := t.TempDir()

	scan := &scanner.ProjectScan{
		Files: []scanner.ScannedFile{
			{Path: "internal/foo/bar.go", Symbols: []scanner.Symbol{{Name: "Undocumented", Kind: scanner.KindFunc}}},
		},
	}
	mapping := analyzer.FeatureMap{} // no features map to Undocumented
	allFeatures := []string{"gap analysis"}

	if err := reporter.WriteGaps(dir, scan, mapping, allFeatures, false); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "gaps.md"))
	if err != nil {
		t.Fatal(err)
	}
	if len(data) == 0 {
		t.Error("gaps.md must not be empty")
	}
}

func TestWriteMapping_EmptyMapping_Succeeds(t *testing.T) {
	dir := t.TempDir()
	err := reporter.WriteMapping(dir,
		analyzer.ProductSummary{Description: "Product.", Features: []string{}},
		analyzer.FeatureMap{},
		[]analyzer.PageAnalysis{},
	)
	if err != nil {
		t.Fatal(err)
	}
}

// TestWriteGaps_NoneFound covers the "_None found._" branches when all symbols
// are mapped and all doc features have a code match.
func TestWriteGaps_NoneFound(t *testing.T) {
	dir := t.TempDir()

	// Exported func that IS already in the feature mapping — no gap.
	scan := &scanner.ProjectScan{
		Files: []scanner.ScannedFile{
			{Path: "internal/foo/bar.go", Symbols: []scanner.Symbol{
				{Name: "Documented", Kind: scanner.KindFunc},
			}},
		},
	}
	mapping := analyzer.FeatureMap{
		{Feature: "gap analysis", Files: []string{"internal/foo/bar.go"}, Symbols: []string{"Documented"}},
	}
	allFeatures := []string{"gap analysis"} // mapped feature → no unmapped features

	if err := reporter.WriteGaps(dir, scan, mapping, allFeatures, false); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "gaps.md"))
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	if !strings.Contains(content, "_None found._") {
		t.Errorf("expected '_None found._' in gaps.md when all symbols are mapped, got:\n%s", content)
	}
}

// TestWriteGaps_SkipsNonFuncTypeInterface verifies that symbols of kind Const/Var/Class
// are not listed in the undocumented code section even if they are exported and unmapped.
func TestWriteGaps_SkipsNonFuncTypeInterface(t *testing.T) {
	dir := t.TempDir()

	scan := &scanner.ProjectScan{
		Files: []scanner.ScannedFile{
			{Path: "internal/foo/bar.go", Symbols: []scanner.Symbol{
				{Name: "MyConst", Kind: scanner.KindConst}, // should be skipped
				{Name: "MyVar", Kind: scanner.KindVar},     // should be skipped
			}},
		},
	}

	if err := reporter.WriteGaps(dir, scan, analyzer.FeatureMap{}, []string{}, false); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "gaps.md"))
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	if strings.Contains(content, "MyConst") || strings.Contains(content, "MyVar") {
		t.Error("gaps.md must not list KindConst or KindVar symbols")
	}
}

// TestWriteGaps_UnexportedSymbolSkipped verifies that unexported symbols are ignored,
// including the isExported("") empty-name edge case.
func TestWriteGaps_UnexportedSymbolSkipped(t *testing.T) {
	dir := t.TempDir()

	scan := &scanner.ProjectScan{
		Files: []scanner.ScannedFile{
			{Path: "internal/foo/bar.go", Symbols: []scanner.Symbol{
				{Name: "unexported", Kind: scanner.KindFunc},
				{Name: "", Kind: scanner.KindFunc}, // empty name → isExported returns false
			}},
		},
	}

	if err := reporter.WriteGaps(dir, scan, analyzer.FeatureMap{}, []string{}, false); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "gaps.md"))
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	if strings.Contains(content, "unexported") {
		t.Error("gaps.md must not list unexported symbols")
	}
}

func TestWriteGaps_FilesOnly_ReplacesSymbolSectionWithNote(t *testing.T) {
	dir := t.TempDir()
	scan := &scanner.ProjectScan{
		Files: []scanner.ScannedFile{{
			Path:    "auth.go",
			Symbols: []scanner.Symbol{{Name: "Login", Kind: scanner.KindFunc}},
		}},
	}
	err := reporter.WriteGaps(dir, scan, analyzer.FeatureMap{}, []string{}, true)
	if err != nil {
		t.Fatal(err)
	}
	content, readErr := os.ReadFile(filepath.Join(dir, "gaps.md"))
	if readErr != nil {
		t.Fatal(readErr)
	}
	text := string(content)
	if strings.Contains(text, "Login") {
		t.Error("filesOnly mode must not list symbol names in gaps.md")
	}
	if !strings.Contains(text, "not available") {
		t.Error("filesOnly mode must include a 'not available' note in the Undocumented Code section")
	}
}
