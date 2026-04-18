package scanner

import "testing"

func TestBuildGraph_noFiles_emptyGraph(t *testing.T) {
	g := BuildGraph(nil, "")
	if len(g.Nodes) != 0 || len(g.Edges) != 0 {
		t.Errorf("expected empty graph, got %+v", g)
	}
}

func TestBuildGraph_singleFile_oneNode(t *testing.T) {
	files := []ScannedFile{
		{Path: "main.go", Language: "Go"},
	}
	g := BuildGraph(files, "")
	if len(g.Nodes) != 1 {
		t.Errorf("expected 1 node, got %d", len(g.Nodes))
	}
	if len(g.Edges) != 0 {
		t.Errorf("expected 0 edges, got %d", len(g.Edges))
	}
}

func TestBuildGraph_goInternalImport_createsEdge(t *testing.T) {
	files := []ScannedFile{
		{
			Path:     "cmd/main.go",
			Language: "Go",
			Imports:  []Import{{Path: "github.com/org/repo/internal/spider"}},
		},
		{
			Path:     "internal/spider/spider.go",
			Language: "Go",
		},
	}
	g := BuildGraph(files, "github.com/org/repo")
	if len(g.Edges) != 1 {
		t.Fatalf("expected 1 edge, got %d: %+v", len(g.Edges), g.Edges)
	}
	if g.Edges[0].From != "cmd/main.go" {
		t.Errorf("edge from: got %q, want cmd/main.go", g.Edges[0].From)
	}
}

func TestBuildGraph_externalImport_noEdge(t *testing.T) {
	files := []ScannedFile{
		{
			Path:     "main.go",
			Language: "Go",
			Imports:  []Import{{Path: "github.com/spf13/cobra"}},
		},
	}
	g := BuildGraph(files, "github.com/myorg/myrepo")
	if len(g.Edges) != 0 {
		t.Errorf("expected 0 edges for external import, got %+v", g.Edges)
	}
}

func TestBuildGraph_relativeImport_createsEdge(t *testing.T) {
	files := []ScannedFile{
		{
			Path:     "src/app.ts",
			Language: "TypeScript",
			Imports:  []Import{{Path: "./utils"}},
		},
		{
			Path:     "src/utils.ts",
			Language: "TypeScript",
		},
	}
	g := BuildGraph(files, "")
	if len(g.Edges) != 1 {
		t.Fatalf("expected 1 edge for relative import, got %d: %+v", len(g.Edges), g.Edges)
	}
}

func TestBuildGraph_multipleFiles_allNodes(t *testing.T) {
	files := []ScannedFile{
		{Path: "a.go", Language: "Go"},
		{Path: "b.go", Language: "Go"},
		{Path: "c.go", Language: "Go"},
	}
	g := BuildGraph(files, "")
	if len(g.Nodes) != 3 {
		t.Errorf("expected 3 nodes, got %d", len(g.Nodes))
	}
}

func TestBuildGraph_relativeImport_withExtension(t *testing.T) {
	files := []ScannedFile{
		{
			Path:     "lib/main.py",
			Language: "Python",
			Imports:  []Import{{Path: "./helper"}},
		},
		{
			Path:     "lib/helper.py",
			Language: "Python",
		},
	}
	g := BuildGraph(files, "")
	if len(g.Edges) != 1 {
		t.Fatalf("expected 1 edge, got %d: %+v", len(g.Edges), g.Edges)
	}
	if g.Edges[0].To != "lib/helper.py" {
		t.Errorf("edge to: got %q, want lib/helper.py", g.Edges[0].To)
	}
}

func TestBuildGraph_relativeImport_indexFile(t *testing.T) {
	files := []ScannedFile{
		{
			Path:     "src/app.ts",
			Language: "TypeScript",
			Imports:  []Import{{Path: "./components"}},
		},
		{
			Path:     "src/components/index.ts",
			Language: "TypeScript",
		},
	}
	g := BuildGraph(files, "")
	if len(g.Edges) != 1 {
		t.Fatalf("expected 1 edge for index import, got %d: %+v", len(g.Edges), g.Edges)
	}
	if g.Edges[0].To != "src/components/index.ts" {
		t.Errorf("edge to: got %q, want src/components/index.ts", g.Edges[0].To)
	}
}

func TestBuildGraph_selfImport_noEdge(t *testing.T) {
	files := []ScannedFile{
		{
			Path:     "src/app.ts",
			Language: "TypeScript",
			Imports:  []Import{{Path: "./app"}},
		},
	}
	g := BuildGraph(files, "")
	if len(g.Edges) != 0 {
		t.Errorf("expected no self-edge, got %+v", g.Edges)
	}
}
