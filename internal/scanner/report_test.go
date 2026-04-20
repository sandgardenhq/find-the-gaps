package scanner

import (
	"strings"
	"testing"
	"time"
)

func minimalScan() *ProjectScan {
	return &ProjectScan{
		RepoPath:  "/repo/myproject",
		ScannedAt: time.Date(2026, 4, 17, 0, 0, 0, 0, time.UTC),
		Languages: []string{"Go"},
		Files: []ScannedFile{
			{
				Path:     "internal/spider/spider.go",
				Language: "Go",
				Lines:    120,
				Symbols: []Symbol{
					{Name: "Crawl", Kind: KindFunc, Signature: "func Crawl(...) (map[string]string, error)", Line: 42},
				},
				Imports: []Import{{Path: "fmt"}, {Path: "github.com/org/repo/internal/cache"}},
			},
		},
		Graph: ImportGraph{
			Nodes: []GraphNode{{ID: "internal/spider/spider.go", Label: "spider", Language: "Go"}},
			Edges: []GraphEdge{},
		},
	}
}

func TestGenerateReport_containsRepoName(t *testing.T) {
	md := GenerateReport(minimalScan())
	if !strings.Contains(md, "myproject") {
		t.Errorf("report should contain repo name 'myproject':\n%s", md)
	}
}

func TestGenerateReport_containsLanguage(t *testing.T) {
	md := GenerateReport(minimalScan())
	if !strings.Contains(md, "Go") {
		t.Errorf("report should contain language 'Go':\n%s", md)
	}
}

func TestGenerateReport_containsSymbolName(t *testing.T) {
	md := GenerateReport(minimalScan())
	if !strings.Contains(md, "Crawl") {
		t.Errorf("report should contain symbol 'Crawl':\n%s", md)
	}
}

func TestGenerateReport_containsMermaid(t *testing.T) {
	scan := minimalScan()
	scan.Graph.Edges = []GraphEdge{{From: "a.go", To: "b.go"}}
	md := GenerateReport(scan)
	if !strings.Contains(md, "mermaid") {
		t.Errorf("report should contain mermaid diagram:\n%s", md)
	}
}

func TestGenerateReport_emptyFiles_noError(t *testing.T) {
	scan := &ProjectScan{
		RepoPath:  "/empty",
		Languages: []string{},
		Graph:     ImportGraph{Nodes: []GraphNode{}, Edges: []GraphEdge{}},
	}
	md := GenerateReport(scan)
	if md == "" {
		t.Error("expected non-empty report even for empty scan")
	}
}

func TestGenerateReport_noEdges_noMermaid(t *testing.T) {
	md := GenerateReport(minimalScan())
	if strings.Contains(md, "graph TD") {
		t.Errorf("report with no edges should not contain mermaid graph TD")
	}
	if !strings.Contains(md, "No internal dependencies") {
		t.Errorf("report should note no internal dependencies")
	}
}

func TestGenerateReport_docComment_included(t *testing.T) {
	scan := minimalScan()
	scan.Files[0].Symbols[0].DocComment = "Crawl fetches all pages."
	md := GenerateReport(scan)
	if !strings.Contains(md, "Crawl fetches all pages.") {
		t.Errorf("doc comment should appear in report:\n%s", md)
	}
}
