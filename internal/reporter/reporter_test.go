package reporter_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sandgardenhq/find-the-gaps/internal/analyzer"
	"github.com/sandgardenhq/find-the-gaps/internal/reporter"
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
	mapping := analyzer.FeatureMap{
		{Feature: "auth", Files: []string{"auth.go"}},
	}
	if err := reporter.WriteGaps(dir, mapping, []string{"search"}); err != nil {
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

// TestWriteGaps_NoneFound verifies "_None found._" when every feature has both
// a code implementation and a documentation page.
func TestWriteGaps_NoneFound(t *testing.T) {
	dir := t.TempDir()
	mapping := analyzer.FeatureMap{
		{Feature: "gap analysis", Files: []string{"internal/foo/bar.go"}},
	}
	allFeatures := []string{"gap analysis"} // documented AND implemented → no gaps

	if err := reporter.WriteGaps(dir, mapping, allFeatures); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "gaps.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "_None found._") {
		t.Errorf("expected '_None found._' when all features are covered, got:\n%s", string(data))
	}
}

// TestWriteGaps_UndocumentedCode verifies that a feature with code but no
// documentation page appears in the Undocumented Code section.
func TestWriteGaps_UndocumentedCode(t *testing.T) {
	dir := t.TempDir()
	mapping := analyzer.FeatureMap{
		{Feature: "auth", Files: []string{"auth.go"}},
	}
	// "auth" exists in code but is absent from docs
	if err := reporter.WriteGaps(dir, mapping, []string{}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "gaps.md"))
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	if !strings.Contains(content, "auth") {
		t.Error("gaps.md must list 'auth' in Undocumented Code")
	}
	if !strings.Contains(content, "no documentation page") {
		t.Error("gaps.md must describe the undocumented code gap")
	}
}

// TestWriteGaps_FeatureNoFiles_NotUndocumented verifies that a feature the LLM
// could not map to any file is not listed as undocumented code.
func TestWriteGaps_FeatureNoFiles_NotUndocumented(t *testing.T) {
	dir := t.TempDir()
	mapping := analyzer.FeatureMap{
		{Feature: "auth", Files: []string{}}, // no files — not "implemented"
	}
	if err := reporter.WriteGaps(dir, mapping, []string{}); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "gaps.md"))
	content := string(data)
	if strings.Contains(content, "no documentation page") {
		t.Error("features with no mapped files must not appear in Undocumented Code")
	}
}
