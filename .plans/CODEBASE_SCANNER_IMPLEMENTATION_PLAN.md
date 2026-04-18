# Codebase Scanner Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Implement a static codebase scanner that extracts exported symbols and import graphs from Go, Python, TypeScript, and Rust source files using tree-sitter, caches results by file mtime, and emits a `project.md` report.

**Architecture:** A `Scan()` orchestrator walks the repo (respecting .gitignore), dispatches each file to the correct language extractor (via tree-sitter), builds an import graph in a second pass, and writes both `scan.json` (cache) and `project.md` (report) to `.find-the-gaps/scan-cache/`. No LLM calls — pure static analysis. The architect phase (future) consumes scanner output alongside docs crawl.

**Tech Stack:** Go stdlib, `github.com/smacker/go-tree-sitter` (CGo, 40+ language grammars), `github.com/sabhiram/go-gitignore` (gitignore parsing), Cobra flags on `analyze`.

**Working directory:** `.worktrees/feat-codebase-scanner` (branch `feat/codebase-scanner`).

**Commands:**
```bash
go test ./...                                                    # run all tests
go test ./internal/scanner/... -v -run TestName                  # single test
go test -race ./internal/scanner/...                             # race detector
go test -coverprofile=coverage.out ./... && go tool cover -func=coverage.out
go build ./...                                                   # build check
```

---

### Task 1: Add dependencies + `internal/scanner/symbols.go` data types

**Files:**
- Modify: `go.mod` / `go.sum` (via `go get`)
- Create: `internal/scanner/symbols.go`
- Create: `internal/scanner/symbols_test.go`

**Step 1: Add go-tree-sitter and go-gitignore**

```bash
cd .worktrees/feat-codebase-scanner
go get github.com/smacker/go-tree-sitter@latest
go get github.com/sabhiram/go-gitignore@latest
```

Expected: go.mod and go.sum updated, `go build ./...` still succeeds.

**Step 2: Write the failing test**

`internal/scanner/symbols_test.go`:
```go
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
```

**Step 3: Run test to verify it fails**

```bash
go test ./internal/scanner/... -v -run TestProjectScan
```
Expected: compile error — package `scanner` does not exist.

**Step 4: Write minimal implementation**

`internal/scanner/symbols.go`:
```go
package scanner

import "time"

// SymbolKind classifies an exported declaration.
type SymbolKind string

const (
	KindFunc      SymbolKind = "func"
	KindType      SymbolKind = "type"
	KindConst     SymbolKind = "const"
	KindVar       SymbolKind = "var"
	KindInterface SymbolKind = "interface"
	KindClass     SymbolKind = "class"
)

// Symbol is a single exported declaration in a source file.
type Symbol struct {
	Name       string     `json:"name"`
	Kind       SymbolKind `json:"kind"`
	Signature  string     `json:"signature"`
	DocComment string     `json:"doc_comment,omitempty"`
	Line       int        `json:"line"`
}

// Import is a single import statement.
type Import struct {
	Path  string `json:"path"`
	Alias string `json:"alias,omitempty"`
}

// ScannedFile holds everything extracted from one source file.
type ScannedFile struct {
	Path     string    `json:"path"`
	Language string    `json:"language"`
	Size     int64     `json:"size"`
	Lines    int       `json:"lines"`
	ModTime  time.Time `json:"mod_time"`
	Symbols  []Symbol  `json:"symbols"`
	Imports  []Import  `json:"imports"`
}

// GraphNode is a file vertex in the import graph.
type GraphNode struct {
	ID       string `json:"id"`
	Label    string `json:"label"`
	Language string `json:"language"`
}

// GraphEdge is a directed import relationship between two files.
type GraphEdge struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// ImportGraph is the directed graph of internal file dependencies.
type ImportGraph struct {
	Nodes []GraphNode `json:"nodes"`
	Edges []GraphEdge `json:"edges"`
}

// ProjectScan is the complete output of a Scan run.
type ProjectScan struct {
	RepoPath  string        `json:"repo_path"`
	ScannedAt time.Time     `json:"scanned_at"`
	Languages []string      `json:"languages"`
	Files     []ScannedFile `json:"files"`
	Graph     ImportGraph   `json:"graph"`
}
```

**Step 5: Run test to verify it passes**

```bash
go test ./internal/scanner/... -v -run "TestProjectScan|TestSymbolKind"
```
Expected: PASS.

**Step 6: Commit**

```bash
git add go.mod go.sum internal/scanner/symbols.go internal/scanner/symbols_test.go
git commit -m "feat(scanner): data types + go-tree-sitter dependency"
```

---

### Task 2: `lang/extractor.go` — Extractor interface + `lang/detect.go` — extension map

**Files:**
- Create: `internal/scanner/lang/extractor.go`
- Create: `internal/scanner/lang/detect.go`
- Create: `internal/scanner/lang/detect_test.go`

**Step 1: Write the failing tests**

`internal/scanner/lang/detect_test.go`:
```go
package lang

import "testing"

func TestDetect_goFile_returnsGoExtractor(t *testing.T) {
	e := Detect("internal/foo/bar.go")
	if e == nil {
		t.Fatal("expected non-nil extractor for .go")
	}
	if e.Language() != "Go" {
		t.Errorf("got %q, want Go", e.Language())
	}
}

func TestDetect_tsFile_returnsTypeScriptExtractor(t *testing.T) {
	e := Detect("src/index.ts")
	if e == nil || e.Language() != "TypeScript" {
		t.Errorf("got %v", e)
	}
}

func TestDetect_jsFile_returnsTypeScriptExtractor(t *testing.T) {
	e := Detect("src/util.js")
	if e == nil || e.Language() != "TypeScript" {
		t.Errorf("expected TypeScript extractor for .js, got %v", e)
	}
}

func TestDetect_pyFile_returnsPythonExtractor(t *testing.T) {
	e := Detect("app/main.py")
	if e == nil || e.Language() != "Python" {
		t.Errorf("got %v", e)
	}
}

func TestDetect_rsFile_returnsRustExtractor(t *testing.T) {
	e := Detect("src/lib.rs")
	if e == nil || e.Language() != "Rust" {
		t.Errorf("got %v", e)
	}
}

func TestDetect_unknownExtension_returnsGeneric(t *testing.T) {
	e := Detect("Makefile")
	if e == nil {
		t.Fatal("expected non-nil extractor for unknown file")
	}
	if e.Language() != "Generic" {
		t.Errorf("got %q, want Generic", e.Language())
	}
}

func TestDetect_binaryExtension_returnsNil(t *testing.T) {
	for _, name := range []string{"image.png", "data.zip", "font.ttf"} {
		if e := Detect(name); e != nil {
			t.Errorf("Detect(%q): expected nil for binary, got %v", name, e.Language())
		}
	}
}
```

**Step 2: Run to verify it fails**

```bash
go test ./internal/scanner/lang/... -v -run TestDetect
```
Expected: compile error — package `lang` does not exist.

**Step 3: Write minimal implementation**

`internal/scanner/lang/extractor.go`:
```go
package lang

import "github.com/sandgardenhq/find-the-gaps/internal/scanner"

// Extractor extracts exported symbols and imports from a source file.
type Extractor interface {
	Language() string
	Extensions() []string
	Extract(path string, content []byte) ([]scanner.Symbol, []scanner.Import, error)
}
```

`internal/scanner/lang/detect.go`:
```go
package lang

import "path/filepath"

// binary extensions — return nil (skip entirely)
var binaryExts = map[string]bool{
	".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".svg": true,
	".ico": true, ".webp": true, ".bmp": true, ".tiff": true,
	".zip": true, ".tar": true, ".gz": true, ".bz2": true, ".xz": true,
	".exe": true, ".dll": true, ".so": true, ".dylib": true, ".a": true,
	".pdf": true, ".ttf": true, ".woff": true, ".woff2": true, ".eot": true,
	".mp3": true, ".mp4": true, ".wav": true, ".ogg": true,
	".db": true, ".sqlite": true,
}

var registry []Extractor

func init() {
	registry = []Extractor{
		&GoExtractor{},
		&PythonExtractor{},
		&TypeScriptExtractor{},
		&RustExtractor{},
	}
}

// Detect returns the appropriate Extractor for a file path.
// Returns nil for binary files (should be skipped).
// Returns GenericExtractor for unknown text file types.
func Detect(path string) Extractor {
	ext := filepath.Ext(path)
	if binaryExts[ext] {
		return nil
	}
	for _, e := range registry {
		for _, x := range e.Extensions() {
			if x == ext {
				return e
			}
		}
	}
	return &GenericExtractor{}
}
```

Note: `GoExtractor`, `PythonExtractor`, `TypeScriptExtractor`, `RustExtractor`, and
`GenericExtractor` are forward-declared here — they will be implemented in Tasks 3–7.
To make the package compile now, create stub files for each:

`internal/scanner/lang/generic.go` (stub):
```go
package lang

type GenericExtractor struct{}
func (e *GenericExtractor) Language() string        { return "Generic" }
func (e *GenericExtractor) Extensions() []string    { return nil }
func (e *GenericExtractor) Extract(path string, content []byte) ([]scanner.Symbol, []scanner.Import, error) {
	return nil, nil, nil
}
```

`internal/scanner/lang/go.go` (stub):
```go
package lang

type GoExtractor struct{}
func (e *GoExtractor) Language() string        { return "Go" }
func (e *GoExtractor) Extensions() []string    { return []string{".go"} }
func (e *GoExtractor) Extract(path string, content []byte) ([]scanner.Symbol, []scanner.Import, error) {
	return nil, nil, nil
}
```

`internal/scanner/lang/python.go` (stub):
```go
package lang

type PythonExtractor struct{}
func (e *PythonExtractor) Language() string        { return "Python" }
func (e *PythonExtractor) Extensions() []string    { return []string{".py", ".pyw"} }
func (e *PythonExtractor) Extract(path string, content []byte) ([]scanner.Symbol, []scanner.Import, error) {
	return nil, nil, nil
}
```

`internal/scanner/lang/typescript.go` (stub):
```go
package lang

type TypeScriptExtractor struct{}
func (e *TypeScriptExtractor) Language() string        { return "TypeScript" }
func (e *TypeScriptExtractor) Extensions() []string    { return []string{".ts", ".tsx", ".js", ".jsx", ".mjs"} }
func (e *TypeScriptExtractor) Extract(path string, content []byte) ([]scanner.Symbol, []scanner.Import, error) {
	return nil, nil, nil
}
```

`internal/scanner/lang/rust.go` (stub):
```go
package lang

type RustExtractor struct{}
func (e *RustExtractor) Language() string        { return "Rust" }
func (e *RustExtractor) Extensions() []string    { return []string{".rs"} }
func (e *RustExtractor) Extract(path string, content []byte) ([]scanner.Symbol, []scanner.Import, error) {
	return nil, nil, nil
}
```

**Step 4: Run tests to verify they pass**

```bash
go test ./internal/scanner/lang/... -v -run TestDetect
```
Expected: all 7 tests PASS.

**Step 5: Commit**

```bash
git add internal/scanner/lang/
git commit -m "feat(scanner): Extractor interface + Detect + language stubs"
```

---

### Task 3: `lang/generic.go` — full implementation

**Files:**
- Modify: `internal/scanner/lang/generic.go`
- Create: `internal/scanner/lang/generic_test.go`

**Step 1: Write the failing tests**

`internal/scanner/lang/generic_test.go`:
```go
package lang

import "testing"

func TestGenericExtractor_returnsEmptySymbolsAndImports(t *testing.T) {
	e := &GenericExtractor{}
	syms, imps, err := e.Extract("file.xyz", []byte("some content here"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(syms) != 0 {
		t.Errorf("expected 0 symbols, got %d", len(syms))
	}
	if len(imps) != 0 {
		t.Errorf("expected 0 imports, got %d", len(imps))
	}
}

func TestGenericExtractor_languageIsGeneric(t *testing.T) {
	e := &GenericExtractor{}
	if e.Language() != "Generic" {
		t.Errorf("got %q, want Generic", e.Language())
	}
}

func TestGenericExtractor_emptyContent_noError(t *testing.T) {
	e := &GenericExtractor{}
	_, _, err := e.Extract("Makefile", []byte{})
	if err != nil {
		t.Fatalf("unexpected error on empty content: %v", err)
	}
}
```

**Step 2: Run to verify it fails**

```bash
go test ./internal/scanner/lang/... -v -run TestGenericExtractor
```
Expected: PASS already (stub returns nil, nil, nil). If so, treat this task as confirmation — run and commit. The tests document the contract explicitly.

**Step 3: Replace stub with explicit implementation**

`internal/scanner/lang/generic.go`:
```go
package lang

import "github.com/sandgardenhq/find-the-gaps/internal/scanner"

// GenericExtractor handles file types with no dedicated extractor.
// It returns no symbols or imports — only file metadata is preserved.
type GenericExtractor struct{}

func (e *GenericExtractor) Language() string     { return "Generic" }
func (e *GenericExtractor) Extensions() []string { return nil }

func (e *GenericExtractor) Extract(_ string, _ []byte) ([]scanner.Symbol, []scanner.Import, error) {
	return []scanner.Symbol{}, []scanner.Import{}, nil
}
```

**Step 4: Run all lang tests**

```bash
go test ./internal/scanner/lang/... -v
```
Expected: all tests PASS.

**Step 5: Commit**

```bash
git add internal/scanner/lang/generic.go internal/scanner/lang/generic_test.go
git commit -m "feat(scanner): GenericExtractor — file metadata only"
```

---

### Task 4: `lang/go.go` — Go extractor

**Files:**
- Modify: `internal/scanner/lang/go.go`
- Create: `internal/scanner/lang/go_test.go`

The Go extractor uses tree-sitter to parse Go source and extract exported top-level
declarations (functions, methods, types, consts, vars) with doc comments, plus all
import paths.

**Step 1: Write the failing tests**

`internal/scanner/lang/go_test.go`:
```go
package lang

import (
	"testing"

	"github.com/sandgardenhq/find-the-gaps/internal/scanner"
)

func TestGoExtractor_exportedFunc_extracted(t *testing.T) {
	src := []byte(`package main

// Run executes the program.
func Run() error {
	return nil
}

func unexported() {}
`)
	e := &GoExtractor{}
	syms, _, err := e.Extract("main.go", src)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(syms) != 1 {
		t.Fatalf("expected 1 symbol, got %d: %v", len(syms), syms)
	}
	if syms[0].Name != "Run" {
		t.Errorf("name: got %q, want Run", syms[0].Name)
	}
	if syms[0].Kind != scanner.KindFunc {
		t.Errorf("kind: got %q, want func", syms[0].Kind)
	}
	if syms[0].DocComment != "Run executes the program." {
		t.Errorf("doc: got %q", syms[0].DocComment)
	}
	if syms[0].Line != 4 {
		t.Errorf("line: got %d, want 4", syms[0].Line)
	}
}

func TestGoExtractor_unexportedFunc_skipped(t *testing.T) {
	src := []byte(`package main

func run() error { return nil }
`)
	e := &GoExtractor{}
	syms, _, err := e.Extract("main.go", src)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(syms) != 0 {
		t.Errorf("expected 0 symbols, got %d: %v", len(syms), syms)
	}
}

func TestGoExtractor_exportedType_extracted(t *testing.T) {
	src := []byte(`package spider

// Options configures the spider.
type Options struct {
	Workers int
}
`)
	e := &GoExtractor{}
	syms, _, err := e.Extract("spider.go", src)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(syms) != 1 || syms[0].Name != "Options" || syms[0].Kind != scanner.KindType {
		t.Errorf("got %v", syms)
	}
}

func TestGoExtractor_exportedConst_extracted(t *testing.T) {
	src := []byte(`package foo

const MaxSize = 100
const unexportedConst = 5
`)
	e := &GoExtractor{}
	syms, _, err := e.Extract("foo.go", src)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	found := false
	for _, s := range syms {
		if s.Name == "MaxSize" && s.Kind == scanner.KindConst {
			found = true
		}
		if s.Name == "unexportedConst" {
			t.Errorf("unexported const should not be extracted")
		}
	}
	if !found {
		t.Errorf("MaxSize not found in %v", syms)
	}
}

func TestGoExtractor_imports_extracted(t *testing.T) {
	src := []byte(`package main

import (
	"fmt"
	myfmt "fmt"
	"github.com/spf13/cobra"
)
`)
	e := &GoExtractor{}
	_, imps, err := e.Extract("main.go", src)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(imps) != 3 {
		t.Fatalf("expected 3 imports, got %d: %v", len(imps), imps)
	}
	var foundAlias bool
	for _, imp := range imps {
		if imp.Alias == "myfmt" {
			foundAlias = true
		}
	}
	if !foundAlias {
		t.Errorf("aliased import myfmt not found in %v", imps)
	}
}

func TestGoExtractor_singleImport_extracted(t *testing.T) {
	src := []byte(`package main

import "os"
`)
	e := &GoExtractor{}
	_, imps, err := e.Extract("main.go", src)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(imps) != 1 || imps[0].Path != "os" {
		t.Errorf("got %v", imps)
	}
}

func TestGoExtractor_emptyFile_noError(t *testing.T) {
	src := []byte(`package main
`)
	e := &GoExtractor{}
	syms, imps, err := e.Extract("main.go", src)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(syms) != 0 || len(imps) != 0 {
		t.Errorf("expected empty results for package-only file")
	}
}
```

**Step 2: Run to verify it fails**

```bash
go test ./internal/scanner/lang/... -v -run TestGoExtractor
```
Expected: all tests FAIL (stub returns empty slices).

**Step 3: Write the implementation**

`internal/scanner/lang/go.go`:
```go
package lang

import (
	"context"
	"strings"
	"unicode"
	"unicode/utf8"

	sitter "github.com/smacker/go-tree-sitter"
	golang "github.com/smacker/go-tree-sitter/golang"

	"github.com/sandgardenhq/find-the-gaps/internal/scanner"
)

type GoExtractor struct{}

func (e *GoExtractor) Language() string     { return "Go" }
func (e *GoExtractor) Extensions() []string { return []string{".go"} }

func (e *GoExtractor) Extract(path string, content []byte) ([]scanner.Symbol, []scanner.Import, error) {
	parser := sitter.NewParser()
	parser.SetLanguage(golang.GetLanguage())
	tree, err := parser.ParseCtx(context.Background(), nil, content)
	if err != nil {
		return nil, nil, err
	}
	defer tree.Close()

	root := tree.RootNode()
	var symbols []scanner.Symbol
	var imports []scanner.Import

	n := int(root.ChildCount())
	for i := 0; i < n; i++ {
		node := root.Child(i)
		switch node.Type() {
		case "function_declaration", "method_declaration":
			nameNode := node.ChildByFieldName("name")
			if nameNode == nil || !goIsExported(nameNode.Content(content)) {
				continue
			}
			symbols = append(symbols, scanner.Symbol{
				Name:       nameNode.Content(content),
				Kind:       scanner.KindFunc,
				Signature:  goFuncSig(node, content),
				DocComment: goPrecedingComment(root, i, content),
				Line:       int(node.StartPoint().Row) + 1,
			})

		case "type_declaration":
			for j := 0; j < int(node.ChildCount()); j++ {
				spec := node.Child(j)
				if spec.Type() != "type_spec" {
					continue
				}
				nameNode := spec.ChildByFieldName("name")
				if nameNode == nil || !goIsExported(nameNode.Content(content)) {
					continue
				}
				typeNode := spec.ChildByFieldName("type")
				sig := "type " + nameNode.Content(content)
				if typeNode != nil {
					sig += " " + typeNode.Type()
				}
				symbols = append(symbols, scanner.Symbol{
					Name:       nameNode.Content(content),
					Kind:       scanner.KindType,
					Signature:  sig,
					DocComment: goPrecedingComment(root, i, content),
					Line:       int(spec.StartPoint().Row) + 1,
				})
			}

		case "const_declaration":
			for j := 0; j < int(node.ChildCount()); j++ {
				spec := node.Child(j)
				if spec.Type() != "const_spec" {
					continue
				}
				nameNode := spec.ChildByFieldName("name")
				if nameNode == nil || !goIsExported(nameNode.Content(content)) {
					continue
				}
				symbols = append(symbols, scanner.Symbol{
					Name:       nameNode.Content(content),
					Kind:       scanner.KindConst,
					Signature:  "const " + nameNode.Content(content),
					DocComment: goPrecedingComment(root, i, content),
					Line:       int(spec.StartPoint().Row) + 1,
				})
			}

		case "var_declaration":
			for j := 0; j < int(node.ChildCount()); j++ {
				spec := node.Child(j)
				if spec.Type() != "var_spec" {
					continue
				}
				nameNode := spec.ChildByFieldName("name")
				if nameNode == nil || !goIsExported(nameNode.Content(content)) {
					continue
				}
				symbols = append(symbols, scanner.Symbol{
					Name:       nameNode.Content(content),
					Kind:       scanner.KindVar,
					Signature:  "var " + nameNode.Content(content),
					DocComment: goPrecedingComment(root, i, content),
					Line:       int(spec.StartPoint().Row) + 1,
				})
			}

		case "import_declaration":
			for j := 0; j < int(node.ChildCount()); j++ {
				spec := node.Child(j)
				if spec.Type() != "import_spec" {
					continue
				}
				pathNode := spec.ChildByFieldName("path")
				if pathNode == nil {
					continue
				}
				imp := scanner.Import{
					Path: strings.Trim(pathNode.Content(content), `"`),
				}
				aliasNode := spec.ChildByFieldName("name")
				if aliasNode != nil {
					alias := aliasNode.Content(content)
					if alias != "." && alias != "_" {
						imp.Alias = alias
					}
				}
				imports = append(imports, imp)
			}
		}
	}

	return symbols, imports, nil
}

func goIsExported(name string) bool {
	r, _ := utf8.DecodeRuneInString(name)
	return unicode.IsUpper(r)
}

func goPrecedingComment(parent *sitter.Node, childIdx int, content []byte) string {
	if childIdx == 0 {
		return ""
	}
	prev := parent.Child(childIdx - 1)
	if prev == nil || prev.Type() != "comment" {
		return ""
	}
	c := prev.Content(content)
	c = strings.TrimPrefix(c, "//")
	return strings.TrimSpace(c)
}

func goFuncSig(node *sitter.Node, content []byte) string {
	name := node.ChildByFieldName("name")
	params := node.ChildByFieldName("parameters")
	result := node.ChildByFieldName("result")

	recv := node.ChildByFieldName("receiver")
	sig := "func "
	if recv != nil {
		sig += recv.Content(content) + " "
	}
	if name != nil {
		sig += name.Content(content)
	}
	if params != nil {
		sig += params.Content(content)
	}
	if result != nil {
		sig += " " + result.Content(content)
	}
	return sig
}
```

**Step 4: Run tests**

```bash
go test ./internal/scanner/lang/... -v -run TestGoExtractor
```
Expected: all 7 tests PASS.

**Troubleshooting tree-sitter node types:** If a test fails because a node type name is wrong, add a temporary debug loop to print all child node types:
```go
for i := 0; i < int(root.ChildCount()); i++ {
    t.Logf("child %d: type=%q", i, root.Child(i).Type())
}
```
Common issues:
- `const_spec` children may be under an `import_spec_list` or `const_spec_list` wrapper node — check by printing child types
- Single `import "os"` produces `import_spec` directly under `import_declaration`, not inside a list — handle both

**Step 5: Run all tests**

```bash
go test ./... && go build ./...
```
Expected: all PASS, build clean.

**Step 6: Commit**

```bash
git add internal/scanner/lang/go.go internal/scanner/lang/go_test.go
git commit -m "feat(scanner): Go extractor — exported symbols + imports via tree-sitter"
```

---

### Task 5: `lang/python.go` — Python extractor

**Files:**
- Modify: `internal/scanner/lang/python.go`
- Create: `internal/scanner/lang/python_test.go`

Extract module-level `def` and `class` that don't start with `_`, plus `import` and
`from ... import` statements.

**Step 1: Write the failing tests**

`internal/scanner/lang/python_test.go`:
```go
package lang

import (
	"testing"

	"github.com/sandgardenhq/find-the-gaps/internal/scanner"
)

func TestPythonExtractor_publicFunc_extracted(t *testing.T) {
	src := []byte(`def process_data(x, y):
    """Process the data."""
    return x + y

def _private(x):
    pass
`)
	e := &PythonExtractor{}
	syms, _, err := e.Extract("mod.py", src)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(syms) != 1 {
		t.Fatalf("expected 1 symbol, got %d: %v", len(syms), syms)
	}
	if syms[0].Name != "process_data" || syms[0].Kind != scanner.KindFunc {
		t.Errorf("got %+v", syms[0])
	}
}

func TestPythonExtractor_publicClass_extracted(t *testing.T) {
	src := []byte(`class MyClient:
    """A client."""
    pass

class _Internal:
    pass
`)
	e := &PythonExtractor{}
	syms, _, err := e.Extract("client.py", src)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(syms) != 1 || syms[0].Name != "MyClient" || syms[0].Kind != scanner.KindClass {
		t.Errorf("got %v", syms)
	}
}

func TestPythonExtractor_imports_extracted(t *testing.T) {
	src := []byte(`import os
import sys as system
from pathlib import Path
from os.path import join, dirname
`)
	e := &PythonExtractor{}
	_, imps, err := e.Extract("mod.py", src)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(imps) < 2 {
		t.Fatalf("expected at least 2 imports, got %d: %v", len(imps), imps)
	}
}

func TestPythonExtractor_docstring_extracted(t *testing.T) {
	src := []byte(`def greet(name):
    """Say hello to name."""
    print(f"Hello, {name}")
`)
	e := &PythonExtractor{}
	syms, _, err := e.Extract("greet.py", src)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(syms) == 0 {
		t.Fatal("expected 1 symbol")
	}
	if syms[0].DocComment != "Say hello to name." {
		t.Errorf("doc: got %q", syms[0].DocComment)
	}
}
```

**Step 2: Run to verify it fails**

```bash
go test ./internal/scanner/lang/... -v -run TestPythonExtractor
```
Expected: all FAIL (stub returns empty).

**Step 3: Write the implementation**

`internal/scanner/lang/python.go`:
```go
package lang

import (
	"context"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
	python "github.com/smacker/go-tree-sitter/python"

	"github.com/sandgardenhq/find-the-gaps/internal/scanner"
)

type PythonExtractor struct{}

func (e *PythonExtractor) Language() string     { return "Python" }
func (e *PythonExtractor) Extensions() []string { return []string{".py", ".pyw"} }

func (e *PythonExtractor) Extract(_ string, content []byte) ([]scanner.Symbol, []scanner.Import, error) {
	parser := sitter.NewParser()
	parser.SetLanguage(python.GetLanguage())
	tree, err := parser.ParseCtx(context.Background(), nil, content)
	if err != nil {
		return nil, nil, err
	}
	defer tree.Close()

	root := tree.RootNode()
	var symbols []scanner.Symbol
	var imports []scanner.Import

	for i := 0; i < int(root.ChildCount()); i++ {
		node := root.Child(i)
		switch node.Type() {
		case "function_definition":
			nameNode := node.ChildByFieldName("name")
			if nameNode == nil {
				continue
			}
			name := nameNode.Content(content)
			if strings.HasPrefix(name, "_") {
				continue
			}
			params := node.ChildByFieldName("parameters")
			sig := "def " + name
			if params != nil {
				sig += params.Content(content)
			}
			symbols = append(symbols, scanner.Symbol{
				Name:       name,
				Kind:       scanner.KindFunc,
				Signature:  sig,
				DocComment: pyDocstring(node, content),
				Line:       int(node.StartPoint().Row) + 1,
			})

		case "class_definition":
			nameNode := node.ChildByFieldName("name")
			if nameNode == nil {
				continue
			}
			name := nameNode.Content(content)
			if strings.HasPrefix(name, "_") {
				continue
			}
			symbols = append(symbols, scanner.Symbol{
				Name:       name,
				Kind:       scanner.KindClass,
				Signature:  "class " + name,
				DocComment: pyDocstring(node, content),
				Line:       int(node.StartPoint().Row) + 1,
			})

		case "import_statement":
			// import os / import sys as system
			for j := 0; j < int(node.ChildCount()); j++ {
				child := node.Child(j)
				if child.Type() == "dotted_name" {
					imports = append(imports, scanner.Import{Path: child.Content(content)})
				} else if child.Type() == "aliased_import" {
					nameN := child.ChildByFieldName("name")
					if nameN != nil {
						imports = append(imports, scanner.Import{Path: nameN.Content(content)})
					}
				}
			}

		case "import_from_statement":
			// from pathlib import Path
			moduleNode := node.ChildByFieldName("module_name")
			if moduleNode != nil {
				imports = append(imports, scanner.Import{Path: moduleNode.Content(content)})
			}
		}
	}

	return symbols, imports, nil
}

// pyDocstring extracts the first string literal from a function/class body.
func pyDocstring(node *sitter.Node, content []byte) string {
	body := node.ChildByFieldName("body")
	if body == nil {
		return ""
	}
	for i := 0; i < int(body.ChildCount()); i++ {
		child := body.Child(i)
		if child.Type() == "expression_statement" {
			for j := 0; j < int(child.ChildCount()); j++ {
				s := child.Child(j)
				if s.Type() == "string" {
					raw := s.Content(content)
					raw = strings.Trim(raw, `"'`)
					raw = strings.Trim(raw, `"""`)
					return strings.TrimSpace(raw)
				}
			}
		}
		break
	}
	return ""
}
```

**Step 4: Run tests**

```bash
go test ./internal/scanner/lang/... -v -run TestPythonExtractor
```
Expected: all PASS.

**Step 5: Commit**

```bash
git add internal/scanner/lang/python.go internal/scanner/lang/python_test.go
git commit -m "feat(scanner): Python extractor — public defs/classes + imports"
```

---

### Task 6: `lang/typescript.go` — TypeScript/JavaScript extractor

**Files:**
- Modify: `internal/scanner/lang/typescript.go`
- Create: `internal/scanner/lang/typescript_test.go`

Extract `export function`, `export class`, `export const`, `export interface`,
`export type` declarations plus `import` statements.

**Step 1: Write the failing tests**

`internal/scanner/lang/typescript_test.go`:
```go
package lang

import (
	"testing"

	"github.com/sandgardenhq/find-the-gaps/internal/scanner"
)

func TestTypeScriptExtractor_exportedFunction(t *testing.T) {
	src := []byte(`// Fetch user data.
export function fetchUser(id: string): Promise<User> {
  return api.get(id);
}

function internalHelper() {}
`)
	e := &TypeScriptExtractor{}
	syms, _, err := e.Extract("api.ts", src)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(syms) != 1 || syms[0].Name != "fetchUser" || syms[0].Kind != scanner.KindFunc {
		t.Errorf("got %v", syms)
	}
}

func TestTypeScriptExtractor_exportedInterface(t *testing.T) {
	src := []byte(`export interface User {
  id: string;
  name: string;
}
`)
	e := &TypeScriptExtractor{}
	syms, _, err := e.Extract("types.ts", src)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(syms) != 1 || syms[0].Name != "User" || syms[0].Kind != scanner.KindInterface {
		t.Errorf("got %v", syms)
	}
}

func TestTypeScriptExtractor_exportedConst(t *testing.T) {
	src := []byte(`export const MAX_RETRIES = 3;
const internalMax = 10;
`)
	e := &TypeScriptExtractor{}
	syms, _, err := e.Extract("config.ts", src)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(syms) != 1 || syms[0].Name != "MAX_RETRIES" {
		t.Errorf("got %v", syms)
	}
}

func TestTypeScriptExtractor_imports_extracted(t *testing.T) {
	src := []byte(`import { useState } from 'react';
import type { FC } from 'react';
import * as path from 'path';
`)
	e := &TypeScriptExtractor{}
	_, imps, err := e.Extract("app.tsx", src)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(imps) < 2 {
		t.Fatalf("expected at least 2 imports, got %d: %v", len(imps), imps)
	}
	var foundReact bool
	for _, imp := range imps {
		if imp.Path == "react" {
			foundReact = true
		}
	}
	if !foundReact {
		t.Errorf("react import not found in %v", imps)
	}
}
```

**Step 2: Run to verify it fails**

```bash
go test ./internal/scanner/lang/... -v -run TestTypeScriptExtractor
```
Expected: all FAIL.

**Step 3: Write the implementation**

`internal/scanner/lang/typescript.go`:
```go
package lang

import (
	"context"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
	typescript "github.com/smacker/go-tree-sitter/typescript/typescript"

	"github.com/sandgardenhq/find-the-gaps/internal/scanner"
)

type TypeScriptExtractor struct{}

func (e *TypeScriptExtractor) Language() string { return "TypeScript" }
func (e *TypeScriptExtractor) Extensions() []string {
	return []string{".ts", ".tsx", ".js", ".jsx", ".mjs"}
}

func (e *TypeScriptExtractor) Extract(_ string, content []byte) ([]scanner.Symbol, []scanner.Import, error) {
	parser := sitter.NewParser()
	parser.SetLanguage(typescript.GetLanguage())
	tree, err := parser.ParseCtx(context.Background(), nil, content)
	if err != nil {
		return nil, nil, err
	}
	defer tree.Close()

	root := tree.RootNode()
	var symbols []scanner.Symbol
	var imports []scanner.Import

	for i := 0; i < int(root.ChildCount()); i++ {
		node := root.Child(i)
		switch node.Type() {
		case "export_statement":
			sym, ok := tsExportedSymbol(node, content, root, i)
			if ok {
				symbols = append(symbols, sym)
			}
		case "import_statement":
			src := node.ChildByFieldName("source")
			if src != nil {
				imports = append(imports, scanner.Import{
					Path: strings.Trim(src.Content(content), `"'`),
				})
			}
		}
	}

	return symbols, imports, nil
}

func tsExportedSymbol(node *sitter.Node, content []byte, parent *sitter.Node, idx int) (scanner.Symbol, bool) {
	// Walk export_statement children to find the declaration
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		switch child.Type() {
		case "function_declaration", "function":
			nameNode := child.ChildByFieldName("name")
			if nameNode == nil {
				continue
			}
			return scanner.Symbol{
				Name:       nameNode.Content(content),
				Kind:       scanner.KindFunc,
				Signature:  "export function " + nameNode.Content(content),
				DocComment: tsPrecedingComment(parent, idx, content),
				Line:       int(child.StartPoint().Row) + 1,
			}, true
		case "class_declaration", "class":
			nameNode := child.ChildByFieldName("name")
			if nameNode == nil {
				continue
			}
			return scanner.Symbol{
				Name:      nameNode.Content(content),
				Kind:      scanner.KindClass,
				Signature: "export class " + nameNode.Content(content),
				Line:      int(child.StartPoint().Row) + 1,
			}, true
		case "interface_declaration":
			nameNode := child.ChildByFieldName("name")
			if nameNode == nil {
				continue
			}
			return scanner.Symbol{
				Name:      nameNode.Content(content),
				Kind:      scanner.KindInterface,
				Signature: "export interface " + nameNode.Content(content),
				Line:      int(child.StartPoint().Row) + 1,
			}, true
		case "lexical_declaration":
			// export const / export let
			for j := 0; j < int(child.ChildCount()); j++ {
				decl := child.Child(j)
				if decl.Type() == "variable_declarator" {
					nameNode := decl.ChildByFieldName("name")
					if nameNode != nil {
						return scanner.Symbol{
							Name:      nameNode.Content(content),
							Kind:      scanner.KindConst,
							Signature: "export const " + nameNode.Content(content),
							Line:      int(decl.StartPoint().Row) + 1,
						}, true
					}
				}
			}
		case "type_alias_declaration":
			nameNode := child.ChildByFieldName("name")
			if nameNode == nil {
				continue
			}
			return scanner.Symbol{
				Name:      nameNode.Content(content),
				Kind:      scanner.KindType,
				Signature: "export type " + nameNode.Content(content),
				Line:      int(child.StartPoint().Row) + 1,
			}, true
		}
	}
	return scanner.Symbol{}, false
}

func tsPrecedingComment(parent *sitter.Node, idx int, content []byte) string {
	if idx == 0 {
		return ""
	}
	prev := parent.Child(idx - 1)
	if prev == nil {
		return ""
	}
	if prev.Type() == "comment" {
		c := prev.Content(content)
		c = strings.TrimPrefix(c, "//")
		return strings.TrimSpace(c)
	}
	return ""
}
```

**Step 4: Run tests**

```bash
go test ./internal/scanner/lang/... -v -run TestTypeScriptExtractor
```
Expected: all PASS.

**Step 5: Commit**

```bash
git add internal/scanner/lang/typescript.go internal/scanner/lang/typescript_test.go
git commit -m "feat(scanner): TypeScript extractor — exported symbols + imports"
```

---

### Task 7: `lang/rust.go` — Rust extractor

**Files:**
- Modify: `internal/scanner/lang/rust.go`
- Create: `internal/scanner/lang/rust_test.go`

Extract `pub fn`, `pub struct`, `pub enum`, `pub trait`, `pub const` plus `use` statements.

**Step 1: Write the failing tests**

`internal/scanner/lang/rust_test.go`:
```go
package lang

import (
	"testing"

	"github.com/sandgardenhq/find-the-gaps/internal/scanner"
)

func TestRustExtractor_pubFn_extracted(t *testing.T) {
	src := []byte(`/// Parse the input string.
pub fn parse(input: &str) -> Result<(), Error> {
    Ok(())
}

fn internal_helper() {}
`)
	e := &RustExtractor{}
	syms, _, err := e.Extract("lib.rs", src)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(syms) != 1 || syms[0].Name != "parse" || syms[0].Kind != scanner.KindFunc {
		t.Errorf("got %v", syms)
	}
	if syms[0].DocComment != "Parse the input string." {
		t.Errorf("doc: got %q", syms[0].DocComment)
	}
}

func TestRustExtractor_pubStruct_extracted(t *testing.T) {
	src := []byte(`pub struct Config {
    pub timeout: u32,
}

struct Internal {}
`)
	e := &RustExtractor{}
	syms, _, err := e.Extract("config.rs", src)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(syms) != 1 || syms[0].Name != "Config" || syms[0].Kind != scanner.KindType {
		t.Errorf("got %v", syms)
	}
}

func TestRustExtractor_pubEnum_extracted(t *testing.T) {
	src := []byte(`pub enum Status {
    Active,
    Inactive,
}
`)
	e := &RustExtractor{}
	syms, _, err := e.Extract("status.rs", src)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(syms) != 1 || syms[0].Name != "Status" {
		t.Errorf("got %v", syms)
	}
}

func TestRustExtractor_useStatements_extracted(t *testing.T) {
	src := []byte(`use std::collections::HashMap;
use crate::config::Config;
`)
	e := &RustExtractor{}
	_, imps, err := e.Extract("main.rs", src)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(imps) < 1 {
		t.Fatalf("expected at least 1 import, got %d", len(imps))
	}
}
```

**Step 2: Run to verify it fails**

```bash
go test ./internal/scanner/lang/... -v -run TestRustExtractor
```
Expected: all FAIL.

**Step 3: Write the implementation**

`internal/scanner/lang/rust.go`:
```go
package lang

import (
	"context"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
	rust "github.com/smacker/go-tree-sitter/rust"

	"github.com/sandgardenhq/find-the-gaps/internal/scanner"
)

type RustExtractor struct{}

func (e *RustExtractor) Language() string     { return "Rust" }
func (e *RustExtractor) Extensions() []string { return []string{".rs"} }

func (e *RustExtractor) Extract(_ string, content []byte) ([]scanner.Symbol, []scanner.Import, error) {
	parser := sitter.NewParser()
	parser.SetLanguage(rust.GetLanguage())
	tree, err := parser.ParseCtx(context.Background(), nil, content)
	if err != nil {
		return nil, nil, err
	}
	defer tree.Close()

	root := tree.RootNode()
	var symbols []scanner.Symbol
	var imports []scanner.Import

	for i := 0; i < int(root.ChildCount()); i++ {
		node := root.Child(i)
		switch node.Type() {
		case "function_item":
			if !rustIsPublic(node, content) {
				continue
			}
			nameNode := node.ChildByFieldName("name")
			if nameNode == nil {
				continue
			}
			symbols = append(symbols, scanner.Symbol{
				Name:       nameNode.Content(content),
				Kind:       scanner.KindFunc,
				Signature:  "pub fn " + nameNode.Content(content),
				DocComment: rustDocComment(root, i, content),
				Line:       int(node.StartPoint().Row) + 1,
			})

		case "struct_item":
			if !rustIsPublic(node, content) {
				continue
			}
			nameNode := node.ChildByFieldName("name")
			if nameNode == nil {
				continue
			}
			symbols = append(symbols, scanner.Symbol{
				Name:      nameNode.Content(content),
				Kind:      scanner.KindType,
				Signature: "pub struct " + nameNode.Content(content),
				Line:      int(node.StartPoint().Row) + 1,
			})

		case "enum_item":
			if !rustIsPublic(node, content) {
				continue
			}
			nameNode := node.ChildByFieldName("name")
			if nameNode == nil {
				continue
			}
			symbols = append(symbols, scanner.Symbol{
				Name:      nameNode.Content(content),
				Kind:      scanner.KindType,
				Signature: "pub enum " + nameNode.Content(content),
				Line:      int(node.StartPoint().Row) + 1,
			})

		case "trait_item":
			if !rustIsPublic(node, content) {
				continue
			}
			nameNode := node.ChildByFieldName("name")
			if nameNode == nil {
				continue
			}
			symbols = append(symbols, scanner.Symbol{
				Name:      nameNode.Content(content),
				Kind:      scanner.KindInterface,
				Signature: "pub trait " + nameNode.Content(content),
				Line:      int(node.StartPoint().Row) + 1,
			})

		case "const_item":
			if !rustIsPublic(node, content) {
				continue
			}
			nameNode := node.ChildByFieldName("name")
			if nameNode == nil {
				continue
			}
			symbols = append(symbols, scanner.Symbol{
				Name:      nameNode.Content(content),
				Kind:      scanner.KindConst,
				Signature: "pub const " + nameNode.Content(content),
				Line:      int(node.StartPoint().Row) + 1,
			})

		case "use_declaration":
			arg := node.ChildByFieldName("argument")
			if arg != nil {
				imports = append(imports, scanner.Import{Path: arg.Content(content)})
			}
		}
	}

	return symbols, imports, nil
}

// rustIsPublic returns true if the node starts with a `pub` visibility modifier.
func rustIsPublic(node *sitter.Node, content []byte) bool {
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		if child.Type() == "visibility_modifier" {
			return strings.HasPrefix(child.Content(content), "pub")
		}
	}
	return false
}

// rustDocComment collects `///` doc comments immediately preceding a node.
func rustDocComment(parent *sitter.Node, idx int, content []byte) string {
	var lines []string
	for i := idx - 1; i >= 0; i-- {
		prev := parent.Child(i)
		if prev == nil || prev.Type() != "line_comment" {
			break
		}
		c := strings.TrimPrefix(prev.Content(content), "///")
		lines = append([]string{strings.TrimSpace(c)}, lines...)
	}
	return strings.Join(lines, " ")
}
```

**Step 4: Run tests**

```bash
go test ./internal/scanner/lang/... -v -run TestRustExtractor
```
Expected: all PASS.

**Step 5: Run all lang tests**

```bash
go test ./internal/scanner/lang/... -v
```
Expected: all tests PASS.

**Step 6: Commit**

```bash
git add internal/scanner/lang/rust.go internal/scanner/lang/rust_test.go
git commit -m "feat(scanner): Rust extractor — pub items + use statements"
```

---

### Task 8: `walker.go` — gitignore-aware file walker

**Files:**
- Create: `internal/scanner/walker.go`
- Create: `internal/scanner/walker_test.go`

Recursively walks a directory, skipping binary files (via `Detect` returning nil) and
paths matched by `.gitignore` or default ignore patterns. Returns a channel of
`WalkedFile` (path + os.FileInfo).

**Step 1: Write the failing tests**

`internal/scanner/walker_test.go`:
```go
package scanner

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
)

func TestWalk_returnsGoFiles(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "main.go"), "package main")
	writeFile(t, filepath.Join(dir, "sub", "util.go"), "package sub")

	files, err := Walk(dir)
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	var paths []string
	for _, f := range files {
		paths = append(paths, f.Path)
	}
	sort.Strings(paths)
	if len(paths) != 2 {
		t.Errorf("expected 2 files, got %v", paths)
	}
}

func TestWalk_respectsGitignore(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".gitignore"), "ignored/\n")
	writeFile(t, filepath.Join(dir, "main.go"), "package main")
	writeFile(t, filepath.Join(dir, "ignored", "secret.go"), "package ignored")

	files, err := Walk(dir)
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	for _, f := range files {
		if filepath.Base(filepath.Dir(f.Path)) == "ignored" {
			t.Errorf("ignored/ dir should be excluded, got %q", f.Path)
		}
	}
}

func TestWalk_skipsBinaryFiles(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "main.go"), "package main")
	writeFile(t, filepath.Join(dir, "image.png"), "\x89PNG\r\n")

	files, err := Walk(dir)
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	for _, f := range files {
		if filepath.Ext(f.Path) == ".png" {
			t.Errorf("binary file should be excluded: %q", f.Path)
		}
	}
}

func TestWalk_skipsDefaultIgnorePatterns(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "main.go"), "package main")
	writeFile(t, filepath.Join(dir, ".git", "config"), "git config")
	writeFile(t, filepath.Join(dir, "node_modules", "pkg", "index.js"), "module")
	writeFile(t, filepath.Join(dir, "vendor", "lib.go"), "package lib")

	files, err := Walk(dir)
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	for _, f := range files {
		for _, bad := range []string{".git", "node_modules", "vendor"} {
			if strings.Contains(f.Path, bad+string(filepath.Separator)) ||
				strings.HasPrefix(f.Path, bad+string(filepath.Separator)) {
				t.Errorf("default-ignored path included: %q", f.Path)
			}
		}
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
```

Add `"strings"` to the import block.

**Step 2: Run to verify it fails**

```bash
go test ./internal/scanner/... -v -run TestWalk
```
Expected: compile error — `Walk` undefined.

**Step 3: Write the implementation**

`internal/scanner/walker.go`:
```go
package scanner

import (
	"os"
	"path/filepath"

	gitignore "github.com/sabhiram/go-gitignore"

	"github.com/sandgardenhq/find-the-gaps/internal/scanner/lang"
)

// defaultIgnore patterns are always excluded regardless of .gitignore.
var defaultIgnore = []string{
	".git", ".svn", ".hg",
	"node_modules", "bower_components",
	"vendor", "venv", ".venv", "__pycache__",
	"dist", "build", "out", "target",
	".next", ".nuxt", ".cache", ".turbo",
	"coverage", ".nyc_output",
	".idea", ".vscode",
}

// WalkedFile is a source file discovered during a walk.
type WalkedFile struct {
	Path    string
	Info    os.FileInfo
}

// Walk recursively walks root, returning all non-binary, non-ignored source files.
func Walk(root string) ([]WalkedFile, error) {
	// Build ignore matcher from root .gitignore (best-effort).
	var matcher *gitignore.GitIgnore
	if gi, err := gitignore.CompileIgnoreFile(filepath.Join(root, ".gitignore")); err == nil {
		matcher = gi
	}

	defaultSet := make(map[string]bool, len(defaultIgnore))
	for _, d := range defaultIgnore {
		defaultSet[d] = true
	}

	var files []WalkedFile
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // skip unreadable entries
		}

		rel, _ := filepath.Rel(root, path)

		// Skip default-ignored directories.
		if d.IsDir() {
			if defaultSet[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}

		// Skip gitignore matches.
		if matcher != nil && matcher.MatchesPath(rel) {
			return nil
		}

		// Skip binary files.
		if lang.Detect(path) == nil {
			return nil
		}

		info, err := d.Info()
		if err != nil {
			return nil
		}
		files = append(files, WalkedFile{Path: rel, Info: info})
		return nil
	})
	return files, err
}
```

**Step 4: Run tests**

```bash
go test ./internal/scanner/... -v -run TestWalk
```
Expected: all PASS.

**Step 5: Commit**

```bash
git add internal/scanner/walker.go internal/scanner/walker_test.go
git commit -m "feat(scanner): gitignore-aware file walker"
```

---

### Task 9: `cache.go` — scan cache

**Files:**
- Create: `internal/scanner/cache.go`
- Create: `internal/scanner/cache_test.go`

**Step 1: Write the failing tests**

`internal/scanner/cache_test.go`:
```go
package scanner

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestScanCache_saveAndLoad(t *testing.T) {
	dir := t.TempDir()
	scan := &ProjectScan{
		RepoPath:  "/repo",
		ScannedAt: time.Now().Truncate(time.Second),
		Languages: []string{"Go"},
		Files:     []ScannedFile{{Path: "main.go", Language: "Go"}},
		Graph:     ImportGraph{Nodes: []GraphNode{}, Edges: []GraphEdge{}},
	}
	c := NewScanCache(dir)
	if err := c.Save(scan); err != nil {
		t.Fatalf("Save: %v", err)
	}
	loaded, err := c.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.RepoPath != scan.RepoPath {
		t.Errorf("RepoPath: got %q, want %q", loaded.RepoPath, scan.RepoPath)
	}
	if len(loaded.Files) != 1 {
		t.Errorf("expected 1 file, got %d", len(loaded.Files))
	}
}

func TestScanCache_load_missingFile_returnsNil(t *testing.T) {
	c := NewScanCache(t.TempDir())
	loaded, err := c.Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if loaded != nil {
		t.Errorf("expected nil for missing cache, got %+v", loaded)
	}
}

func TestScanCache_fileMap_byPath(t *testing.T) {
	scan := &ProjectScan{
		Files: []ScannedFile{
			{Path: "a.go", ModTime: time.Now()},
			{Path: "b.go", ModTime: time.Now()},
		},
	}
	m := scan.FileMap()
	if _, ok := m["a.go"]; !ok {
		t.Error("expected a.go in file map")
	}
	if _, ok := m["b.go"]; !ok {
		t.Error("expected b.go in file map")
	}
}

func TestScanCache_save_createsDirIfMissing(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "new", "nested", "dir")
	c := NewScanCache(dir)
	scan := &ProjectScan{Graph: ImportGraph{Nodes: []GraphNode{}, Edges: []GraphEdge{}}}
	if err := c.Save(scan); err != nil {
		t.Fatalf("Save into non-existent dir: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "scan.json")); err != nil {
		t.Errorf("scan.json not created: %v", err)
	}
}
```

**Step 2: Run to verify it fails**

```bash
go test ./internal/scanner/... -v -run TestScanCache
```
Expected: compile error — `NewScanCache`, `FileMap` undefined.

**Step 3: Write the implementation**

`internal/scanner/cache.go`:
```go
package scanner

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

// ScanCache reads and writes scan.json in a cache directory.
type ScanCache struct {
	dir string
}

// NewScanCache returns a ScanCache backed by dir.
func NewScanCache(dir string) *ScanCache {
	return &ScanCache{dir: dir}
}

// Load reads scan.json from the cache directory.
// Returns nil, nil if the file does not exist.
func (c *ScanCache) Load() (*ProjectScan, error) {
	data, err := os.ReadFile(filepath.Join(c.dir, "scan.json"))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var scan ProjectScan
	if err := json.Unmarshal(data, &scan); err != nil {
		return nil, err
	}
	return &scan, nil
}

// Save writes scan.json to the cache directory, creating it if needed.
func (c *ScanCache) Save(scan *ProjectScan) error {
	if err := os.MkdirAll(c.dir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(scan, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(c.dir, "scan.json"), data, 0o644)
}

// FileMap returns a map of relative path → ScannedFile for cache lookups.
func (s *ProjectScan) FileMap() map[string]ScannedFile {
	m := make(map[string]ScannedFile, len(s.Files))
	for _, f := range s.Files {
		m[f.Path] = f
	}
	return m
}
```

**Step 4: Run tests**

```bash
go test ./internal/scanner/... -v -run TestScanCache
```
Expected: all PASS.

**Step 5: Commit**

```bash
git add internal/scanner/cache.go internal/scanner/cache_test.go
git commit -m "feat(scanner): ScanCache — save/load scan.json with mtime-based file map"
```

---

### Task 10: `graph.go` — import graph builder

**Files:**
- Create: `internal/scanner/graph.go`
- Create: `internal/scanner/graph_test.go`

Builds `ImportGraph` from `[]ScannedFile`. Edges connect files within the same repo
only. For Go, resolves module-path imports using a provided module prefix.

**Step 1: Write the failing tests**

`internal/scanner/graph_test.go`:
```go
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
	// TypeScript relative imports: ./utils resolves against the importing file's dir
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
```

**Step 2: Run to verify it fails**

```bash
go test ./internal/scanner/... -v -run TestBuildGraph
```
Expected: compile error — `BuildGraph` undefined.

**Step 3: Write the implementation**

`internal/scanner/graph.go`:
```go
package scanner

import (
	"path/filepath"
	"strings"
)

// BuildGraph constructs an ImportGraph from the scanned files.
// modulePrefix is the Go module path (e.g. "github.com/org/repo") used to
// resolve internal Go imports. Pass "" for non-Go repos.
func BuildGraph(files []ScannedFile, modulePrefix string) ImportGraph {
	if len(files) == 0 {
		return ImportGraph{Nodes: []GraphNode{}, Edges: []GraphEdge{}}
	}

	// Index files by path for O(1) lookup.
	byPath := make(map[string]bool, len(files))
	for _, f := range files {
		byPath[f.Path] = true
	}

	// Also index Go packages: package dir → file paths that are in that dir.
	// "github.com/org/repo/internal/spider" → files in "internal/spider/"
	goPkgIndex := make(map[string]string) // package import path → representative file path
	if modulePrefix != "" {
		for _, f := range files {
			if f.Language != "Go" {
				continue
			}
			dir := filepath.Dir(f.Path)
			importPath := modulePrefix + "/" + filepath.ToSlash(dir)
			if _, exists := goPkgIndex[importPath]; !exists {
				goPkgIndex[importPath] = f.Path
			}
		}
	}

	nodes := make([]GraphNode, 0, len(files))
	for _, f := range files {
		nodes = append(nodes, GraphNode{
			ID:       f.Path,
			Label:    filepath.Base(filepath.Dir(f.Path)),
			Language: f.Language,
		})
	}

	var edges []GraphEdge
	for _, f := range files {
		for _, imp := range f.Imports {
			target := resolveImport(f.Path, imp.Path, modulePrefix, goPkgIndex, byPath)
			if target != "" && target != f.Path {
				edges = append(edges, GraphEdge{From: f.Path, To: target})
			}
		}
	}

	if edges == nil {
		edges = []GraphEdge{}
	}

	return ImportGraph{Nodes: nodes, Edges: edges}
}

func resolveImport(fromPath, importPath, modulePrefix string, goPkgIndex map[string]string, byPath map[string]bool) string {
	// Go internal import: starts with module prefix.
	if modulePrefix != "" && strings.HasPrefix(importPath, modulePrefix+"/") {
		if target, ok := goPkgIndex[importPath]; ok {
			return target
		}
		return ""
	}

	// Relative import (TypeScript, Python, Rust): starts with ./ or ../
	if strings.HasPrefix(importPath, "./") || strings.HasPrefix(importPath, "../") {
		base := filepath.Dir(fromPath)
		resolved := filepath.Clean(filepath.Join(base, importPath))
		// Try with common extensions.
		for _, ext := range []string{"", ".ts", ".tsx", ".js", ".py", ".rs"} {
			candidate := resolved + ext
			if byPath[candidate] {
				return candidate
			}
		}
		// Try as directory index.
		for _, idx := range []string{"/index.ts", "/index.js", "/mod.rs", "/__init__.py"} {
			candidate := resolved + idx
			if byPath[candidate] {
				return candidate
			}
		}
	}

	return ""
}
```

**Step 4: Run tests**

```bash
go test ./internal/scanner/... -v -run TestBuildGraph
```
Expected: all PASS.

**Step 5: Commit**

```bash
git add internal/scanner/graph.go internal/scanner/graph_test.go
git commit -m "feat(scanner): BuildGraph — import graph with Go module + relative resolution"
```

---

### Task 11: `report.go` — project.md generator

**Files:**
- Create: `internal/scanner/report.go`
- Create: `internal/scanner/report_test.go`

**Step 1: Write the failing tests**

`internal/scanner/report_test.go`:
```go
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
```

**Step 2: Run to verify it fails**

```bash
go test ./internal/scanner/... -v -run TestGenerateReport
```
Expected: compile error — `GenerateReport` undefined.

**Step 3: Write the implementation**

`internal/scanner/report.go`:
```go
package scanner

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

// GenerateReport produces a project.md string from a ProjectScan.
func GenerateReport(scan *ProjectScan) string {
	var b strings.Builder

	repoName := filepath.Base(scan.RepoPath)
	fmt.Fprintf(&b, "# %s\n\n", repoName)
	fmt.Fprintf(&b, "**Path:** `%s`\n", scan.RepoPath)
	if !scan.ScannedAt.IsZero() {
		fmt.Fprintf(&b, "**Scanned:** %s\n", scan.ScannedAt.Format("2006-01-02 15:04:05 UTC"))
	}
	if len(scan.Languages) > 0 {
		fmt.Fprintf(&b, "**Languages:** %s\n", strings.Join(scan.Languages, ", "))
	}
	b.WriteString("\n")

	// Summary
	totalSymbols := 0
	for _, f := range scan.Files {
		totalSymbols += len(f.Symbols)
	}
	b.WriteString("## Summary\n\n")
	b.WriteString("| Metric | Count |\n|--------|-------|\n")
	fmt.Fprintf(&b, "| Files | %d |\n", len(scan.Files))
	fmt.Fprintf(&b, "| Exported Symbols | %d |\n", totalSymbols)
	fmt.Fprintf(&b, "| Internal Dependencies | %d |\n\n", len(scan.Graph.Edges))

	// Directory structure
	if len(scan.Files) > 0 {
		b.WriteString("## Directory Structure\n\n")
		b.WriteString("| Path | Language | Lines | Key Exports |\n|------|----------|-------|-------------|\n")
		// Group by directory
		dirs := make(map[string][]ScannedFile)
		for _, f := range scan.Files {
			dir := filepath.Dir(f.Path)
			dirs[dir] = append(dirs[dir], f)
		}
		dirKeys := make([]string, 0, len(dirs))
		for k := range dirs {
			dirKeys = append(dirKeys, k)
		}
		sort.Strings(dirKeys)
		for _, dir := range dirKeys {
			files := dirs[dir]
			var lang string
			var lines int
			var exports []string
			for _, f := range files {
				lang = f.Language
				lines += f.Lines
				for _, s := range f.Symbols {
					exports = append(exports, fmt.Sprintf("`%s`", s.Name))
				}
			}
			exportStr := strings.Join(exports, ", ")
			if exportStr == "" {
				exportStr = "—"
			}
			fmt.Fprintf(&b, "| `%s` | %s | %d | %s |\n", dir, lang, lines, exportStr)
		}
		b.WriteString("\n")
	}

	// Public API
	if totalSymbols > 0 {
		b.WriteString("## Public API\n\n")
		for _, f := range scan.Files {
			if len(f.Symbols) == 0 {
				continue
			}
			fmt.Fprintf(&b, "### `%s` (%s)\n\n", filepath.Dir(f.Path), f.Language)
			for _, s := range f.Symbols {
				fmt.Fprintf(&b, "#### `%s`\n\n", s.Signature)
				if s.DocComment != "" {
					fmt.Fprintf(&b, "> %s\n\n", s.DocComment)
				}
			}
		}
	}

	// Import graph
	b.WriteString("## Import Graph\n\n")
	if len(scan.Graph.Edges) > 0 {
		b.WriteString("```mermaid\ngraph TD\n")
		for _, e := range scan.Graph.Edges {
			fmt.Fprintf(&b, "  %q --> %q\n", e.From, e.To)
		}
		b.WriteString("```\n\n")
	} else {
		b.WriteString("_No internal dependencies detected._\n\n")
	}

	// Files table
	if len(scan.Files) > 0 {
		b.WriteString("## Files\n\n")
		b.WriteString("| File | Language | Lines | Exports | Imports |\n|------|----------|-------|---------|--------|\n")
		for _, f := range scan.Files {
			fmt.Fprintf(&b, "| `%s` | %s | %d | %d | %d |\n",
				f.Path, f.Language, f.Lines, len(f.Symbols), len(f.Imports))
		}
		b.WriteString("\n")
	}

	return b.String()
}
```

**Step 4: Run tests**

```bash
go test ./internal/scanner/... -v -run TestGenerateReport
```
Expected: all PASS.

**Step 5: Commit**

```bash
git add internal/scanner/report.go internal/scanner/report_test.go
git commit -m "feat(scanner): GenerateReport — project.md with symbols, graph, file table"
```

---

### Task 12: `scanner.go` — `Scan()` orchestrator

**Files:**
- Create: `internal/scanner/scanner.go`
- Create: `internal/scanner/scanner_test.go`

`Scan()` wires together: Walk → per-file Extract (reusing cache where mtime matches) → BuildGraph → GenerateReport → save scan.json + project.md.

**Step 1: Write the failing tests**

`internal/scanner/scanner_test.go`:
```go
package scanner

import (
	"os"
	"path/filepath"
	"testing"
)

func TestScan_emptyDir_returnsEmptyScan(t *testing.T) {
	dir := t.TempDir()
	cacheDir := t.TempDir()

	scan, err := Scan(dir, Options{CacheDir: cacheDir})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if scan.RepoPath != dir {
		t.Errorf("RepoPath: got %q, want %q", scan.RepoPath, dir)
	}
	if len(scan.Files) != 0 {
		t.Errorf("expected 0 files, got %d", len(scan.Files))
	}
}

func TestScan_goFile_extractsSymbols(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "main.go"), `package main

// Run starts the app.
func Run() error { return nil }
`)
	scan, err := Scan(dir, Options{CacheDir: t.TempDir()})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(scan.Files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(scan.Files))
	}
	if len(scan.Files[0].Symbols) != 1 {
		t.Fatalf("expected 1 symbol, got %d", len(scan.Files[0].Symbols))
	}
	if scan.Files[0].Symbols[0].Name != "Run" {
		t.Errorf("symbol name: got %q, want Run", scan.Files[0].Symbols[0].Name)
	}
}

func TestScan_cacheReusedOnSecondRun(t *testing.T) {
	dir := t.TempDir()
	cacheDir := t.TempDir()
	writeFile(t, filepath.Join(dir, "main.go"), `package main
func Run() error { return nil }
`)

	// First scan — parses file.
	_, err := Scan(dir, Options{CacheDir: cacheDir})
	if err != nil {
		t.Fatalf("first Scan: %v", err)
	}

	// Verify scan.json written.
	if _, err := os.Stat(filepath.Join(cacheDir, "scan.json")); err != nil {
		t.Fatalf("scan.json not written: %v", err)
	}

	// Second scan — should reuse cache (file not modified).
	scan2, err := Scan(dir, Options{CacheDir: cacheDir})
	if err != nil {
		t.Fatalf("second Scan: %v", err)
	}
	if len(scan2.Files) != 1 || scan2.Files[0].Symbols[0].Name != "Run" {
		t.Errorf("cache not reused correctly: %+v", scan2.Files)
	}
}

func TestScan_writesProjectMd(t *testing.T) {
	dir := t.TempDir()
	cacheDir := t.TempDir()
	writeFile(t, filepath.Join(dir, "main.go"), "package main\n")

	_, err := Scan(dir, Options{CacheDir: cacheDir})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if _, err := os.Stat(filepath.Join(cacheDir, "project.md")); err != nil {
		t.Fatalf("project.md not written: %v", err)
	}
}

func TestScan_noCache_forcesReparse(t *testing.T) {
	dir := t.TempDir()
	cacheDir := t.TempDir()
	writeFile(t, filepath.Join(dir, "main.go"), "package main\nfunc Run() error { return nil }\n")

	_, err := Scan(dir, Options{CacheDir: cacheDir})
	if err != nil {
		t.Fatalf("first Scan: %v", err)
	}

	// Force re-scan even though file is unchanged.
	scan2, err := Scan(dir, Options{CacheDir: cacheDir, NoCache: true})
	if err != nil {
		t.Fatalf("second Scan: %v", err)
	}
	if len(scan2.Files) != 1 {
		t.Errorf("expected 1 file on no-cache rescan, got %d", len(scan2.Files))
	}
}
```

**Step 2: Run to verify it fails**

```bash
go test ./internal/scanner/... -v -run TestScan
```
Expected: compile error — `Scan`, `Options` undefined.

**Step 3: Write the implementation**

`internal/scanner/scanner.go`:
```go
package scanner

import (
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/sandgardenhq/find-the-gaps/internal/scanner/lang"
)

// Options configures a Scan run.
type Options struct {
	CacheDir     string // defaults to .find-the-gaps/scan-cache
	NoCache      bool   // force full re-parse
	ModulePrefix string // Go module path for import graph resolution
}

// Scan walks root, extracts symbols from all source files, builds an import
// graph, and writes scan.json + project.md to opts.CacheDir.
func Scan(root string, opts Options) (*ProjectScan, error) {
	if opts.CacheDir == "" {
		opts.CacheDir = filepath.Join(root, ".find-the-gaps", "scan-cache")
	}

	// Load existing cache.
	cache := NewScanCache(opts.CacheDir)
	var cached map[string]ScannedFile
	if !opts.NoCache {
		if prev, err := cache.Load(); err == nil && prev != nil {
			cached = prev.FileMap()
		}
	}
	if cached == nil {
		cached = make(map[string]ScannedFile)
	}

	// Walk the repo.
	walked, err := Walk(root)
	if err != nil {
		return nil, err
	}

	// Parse each file (or reuse cache).
	langSet := make(map[string]bool)
	files := make([]ScannedFile, 0, len(walked))

	for _, wf := range walked {
		extractor := lang.Detect(filepath.Join(root, wf.Path))
		if extractor == nil {
			continue
		}

		language := extractor.Language()
		langSet[language] = true

		// Cache hit: same mtime → reuse.
		if cached, ok := cached[wf.Path]; ok && cached.ModTime.Equal(wf.Info.ModTime()) {
			files = append(files, cached)
			continue
		}

		// Parse the file.
		content, err := os.ReadFile(filepath.Join(root, wf.Path))
		if err != nil {
			continue // skip unreadable files
		}

		lines := countLines(content)
		symbols, imports, err := extractor.Extract(wf.Path, content)
		if err != nil {
			symbols = []Symbol{}
			imports = []Import{}
		}
		if symbols == nil {
			symbols = []Symbol{}
		}
		if imports == nil {
			imports = []Import{}
		}

		files = append(files, ScannedFile{
			Path:     wf.Path,
			Language: language,
			Size:     wf.Info.Size(),
			Lines:    lines,
			ModTime:  wf.Info.ModTime(),
			Symbols:  symbols,
			Imports:  imports,
		})
	}

	// Collect languages.
	languages := make([]string, 0, len(langSet))
	for l := range langSet {
		languages = append(languages, l)
	}
	sort.Strings(languages)

	// Build import graph.
	graph := BuildGraph(files, opts.ModulePrefix)

	scan := &ProjectScan{
		RepoPath:  root,
		ScannedAt: time.Now(),
		Languages: languages,
		Files:     files,
		Graph:     graph,
	}

	// Save cache + report.
	if err := cache.Save(scan); err != nil {
		return nil, err
	}
	report := GenerateReport(scan)
	if err := os.WriteFile(filepath.Join(opts.CacheDir, "project.md"), []byte(report), 0o644); err != nil {
		return nil, err
	}

	return scan, nil
}

func countLines(content []byte) int {
	if len(content) == 0 {
		return 0
	}
	n := 1
	for _, b := range content {
		if b == '\n' {
			n++
		}
	}
	return n
}
```

**Step 4: Run all tests**

```bash
go test ./... && go build ./...
```
Expected: all PASS, build clean.

**Step 5: Check coverage**

```bash
go test -coverprofile=coverage.out ./... && go tool cover -func=coverage.out
```
Expected: `internal/scanner` packages at ≥90%.

**Step 6: Commit**

```bash
git add internal/scanner/scanner.go internal/scanner/scanner_test.go
git commit -m "feat(scanner): Scan() orchestrator — walk, parse, cache, report"
```

---

### Task 13: `cli/analyze.go` — wire `--repo`, `--scan-cache-dir`, `--no-cache`

**Files:**
- Modify: `internal/cli/analyze.go`
- Modify: `internal/cli/analyze_test.go`

Add `--repo` (default `.`), `--scan-cache-dir` (default `.find-the-gaps/scan-cache`),
and `--no-cache` flags. The command runs Scan then Crawl in sequence and prints a
combined summary.

**Step 1: Write the failing tests**

Append to `internal/cli/analyze_test.go`:
```go
func TestAnalyze_repoFlag_appearsInHelp(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run(&stdout, &stderr, []string{"analyze", "--help"})
	if code != 0 {
		t.Fatalf("--help failed: %q", stderr.String())
	}
	if !strings.Contains(stdout.String(), "--repo") {
		t.Errorf("--repo flag not in help output:\n%s", stdout.String())
	}
}

func TestAnalyze_noCacheFlag_appearsInHelp(t *testing.T) {
	var stdout, stderr bytes.Buffer
	run(&stdout, &stderr, []string{"analyze", "--help"})
	if !strings.Contains(stdout.String(), "--no-cache") {
		t.Errorf("--no-cache flag not in help output:\n%s", stdout.String())
	}
}
```

Add `"strings"` to the test file imports if not present.

**Step 2: Run to verify it fails**

```bash
go test ./internal/cli/... -v -run "TestAnalyze_repoFlag|TestAnalyze_noCacheFlag"
```
Expected: FAIL — flags not present in help.

**Step 3: Update analyze.go**

Replace `internal/cli/analyze.go`:
```go
package cli

import (
	"fmt"

	"github.com/sandgardenhq/find-the-gaps/internal/scanner"
	"github.com/sandgardenhq/find-the-gaps/internal/spider"
	"github.com/spf13/cobra"
)

func newAnalyzeCmd() *cobra.Command {
	var (
		docsURL      string
		repoPath     string
		cacheDir     string
		scanCacheDir string
		workers      int
		noCache      bool
	)

	cmd := &cobra.Command{
		Use:   "analyze",
		Short: "Analyze a codebase against its documentation site for gaps.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if docsURL == "" {
				return fmt.Errorf("--docs-url is required")
			}

			// Scan the codebase.
			scanOpts := scanner.Options{
				CacheDir: scanCacheDir,
				NoCache:  noCache,
			}
			scan, err := scanner.Scan(repoPath, scanOpts)
			if err != nil {
				return fmt.Errorf("scan failed: %w", err)
			}

			// Crawl the docs site.
			spiderOpts := spider.Options{
				CacheDir: cacheDir,
				Workers:  workers,
			}
			pages, err := spider.Crawl(docsURL, spiderOpts, spider.MdfetchFetcher(spiderOpts))
			if err != nil {
				return fmt.Errorf("crawl failed: %w", err)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "scanned %d files, fetched %d pages\n",
				len(scan.Files), len(pages))
			return nil
		},
	}

	cmd.Flags().StringVar(&docsURL, "docs-url", "", "URL of the documentation site (required)")
	cmd.Flags().StringVar(&repoPath, "repo", ".", "path to the repository to analyze")
	cmd.Flags().StringVar(&cacheDir, "cache-dir", ".find-the-gaps/cache", "directory to cache fetched doc pages")
	cmd.Flags().StringVar(&scanCacheDir, "scan-cache-dir", ".find-the-gaps/scan-cache", "directory to cache code scan results")
	cmd.Flags().IntVar(&workers, "workers", 5, "number of parallel mdfetch workers")
	cmd.Flags().BoolVar(&noCache, "no-cache", false, "force full re-scan of the codebase")

	return cmd
}
```

Note: this imports `internal/spider` which is on the `feat/mdfetch-spider` branch and
not yet merged. If the spider package is unavailable, stub the crawl section and remove
the spider import temporarily until the branch is merged.

**Step 4: Run all tests**

```bash
go test ./... && go build ./...
```
Expected: all PASS, build succeeds.

**Step 5: Coverage check**

```bash
go test -coverprofile=coverage.out ./... && go tool cover -func=coverage.out
```
Expected: all packages ≥90%.

**Step 6: Commit**

```bash
git add internal/cli/analyze.go internal/cli/analyze_test.go
git commit -m "feat(cli): --repo, --scan-cache-dir, --no-cache flags wired to Scan()"
```

---

## Done

All tasks complete when:
- `go test -race ./...` is fully green
- `go build ./...` succeeds
- All `internal/scanner` packages at ≥90% statement coverage
- `find-the-gaps analyze --repo . --docs-url https://example.com` triggers both a real scan and crawl
