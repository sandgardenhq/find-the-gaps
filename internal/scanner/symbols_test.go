package scanner

import (
	"encoding/json"
	"testing"
	"time"
)

func TestProjectScan_JSONRoundTrip(t *testing.T) {
	scan := ProjectScan{
		RepoPath:  "/tmp/repo",
		ScannedAt: time.Now().Truncate(time.Second),
		Languages: []string{"Go"},
		Files: []ScannedFile{
			{
				Path:     "main.go",
				Language: "Go",
				Lines:    10,
				Symbols: []Symbol{
					{Name: "Run", Kind: KindFunc, Signature: "func Run() error", Line: 5},
				},
				Imports: []Import{{Path: "fmt"}},
			},
		},
		Graph: ImportGraph{
			Nodes: []GraphNode{{ID: "main.go", Label: "main", Language: "Go"}},
			Edges: []GraphEdge{},
		},
	}
	data, err := json.Marshal(scan)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got ProjectScan
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.RepoPath != scan.RepoPath {
		t.Errorf("RepoPath: got %q, want %q", got.RepoPath, scan.RepoPath)
	}
	if len(got.Files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(got.Files))
	}
	if got.Files[0].Symbols[0].Name != "Run" {
		t.Errorf("symbol name: got %q, want Run", got.Files[0].Symbols[0].Name)
	}
	if got.Files[0].Imports[0].Path != "fmt" {
		t.Errorf("import path: got %q, want fmt", got.Files[0].Imports[0].Path)
	}
}

func TestSymbolKind_constants(t *testing.T) {
	kinds := []SymbolKind{KindFunc, KindType, KindConst, KindVar, KindInterface, KindClass}
	for _, k := range kinds {
		if k == "" {
			t.Errorf("empty SymbolKind constant")
		}
	}
}
