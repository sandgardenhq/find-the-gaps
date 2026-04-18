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

func TestGoExtractor_exportedVar_extracted(t *testing.T) {
	src := []byte(`package foo

var ErrNotFound = errors.New("not found")
var privateVar = "secret"
`)
	e := &GoExtractor{}
	syms, _, err := e.Extract("foo.go", src)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	found := false
	for _, s := range syms {
		if s.Name == "ErrNotFound" && s.Kind == scanner.KindVar {
			found = true
		}
		if s.Name == "privateVar" {
			t.Errorf("unexported var should not be extracted")
		}
	}
	if !found {
		t.Errorf("ErrNotFound not found in %v", syms)
	}
}

func TestGoExtractor_methodOnExportedReceiver_extracted(t *testing.T) {
	src := []byte(`package foo

type Client struct{}

// Do sends the request.
func (c *Client) Do() error {
	return nil
}

func (c *Client) private() {}
`)
	e := &GoExtractor{}
	syms, _, err := e.Extract("foo.go", src)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	// Should have: Client (type) + Do (method)
	foundType := false
	foundMethod := false
	for _, s := range syms {
		if s.Name == "Client" && s.Kind == scanner.KindType {
			foundType = true
		}
		if s.Name == "Do" && s.Kind == scanner.KindFunc {
			foundMethod = true
			if s.DocComment != "Do sends the request." {
				t.Errorf("method doc: got %q", s.DocComment)
			}
		}
		if s.Name == "private" {
			t.Errorf("unexported method should not be extracted")
		}
	}
	if !foundType {
		t.Errorf("Client type not found")
	}
	if !foundMethod {
		t.Errorf("Do method not found")
	}
}

func TestGoExtractor_language_isGo(t *testing.T) {
	e := &GoExtractor{}
	if e.Language() != "Go" {
		t.Errorf("got %q, want Go", e.Language())
	}
}

func TestGoExtractor_extensions_includesDotGo(t *testing.T) {
	e := &GoExtractor{}
	exts := e.Extensions()
	found := false
	for _, ext := range exts {
		if ext == ".go" {
			found = true
		}
	}
	if !found {
		t.Errorf("Extensions() = %v, want to include .go", exts)
	}
}

// TestGoExtractor_firstTopLevelDecl_noDocComment verifies that when a declaration
// is the very first child of the source file (index 0, no preceding sibling),
// the doc comment is empty — exercises the childIdx==0 early-return in goPrecedingComment.
func TestGoExtractor_firstTopLevelDecl_noDocComment(t *testing.T) {
	// package_clause is always child 0; function_declaration is child 1 here.
	// There's no comment between package and func, so DocComment should be empty.
	src := []byte(`package main
func Exported() {}
`)
	e := &GoExtractor{}
	syms, _, err := e.Extract("main.go", src)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(syms) != 1 {
		t.Fatalf("expected 1 symbol, got %d", len(syms))
	}
	if syms[0].DocComment != "" {
		t.Errorf("expected empty doc comment, got %q", syms[0].DocComment)
	}
}

// TestGoExtractor_blankImportAlias_notRecorded verifies that blank-import aliases (_)
// are not stored in Import.Alias.
func TestGoExtractor_blankImportAlias_notRecorded(t *testing.T) {
	src := []byte(`package main

import _ "net/http"
`)
	e := &GoExtractor{}
	_, imps, err := e.Extract("main.go", src)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(imps) != 1 {
		t.Fatalf("expected 1 import, got %d: %v", len(imps), imps)
	}
	if imps[0].Alias != "" {
		t.Errorf("blank import alias should not be stored, got %q", imps[0].Alias)
	}
	if imps[0].Path != "net/http" {
		t.Errorf("import path: got %q, want net/http", imps[0].Path)
	}
}
