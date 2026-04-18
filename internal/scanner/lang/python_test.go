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

func TestPythonExtractor_language(t *testing.T) {
	e := &PythonExtractor{}
	if e.Language() != "Python" {
		t.Errorf("got %q, want Python", e.Language())
	}
}

func TestPythonExtractor_extensions(t *testing.T) {
	e := &PythonExtractor{}
	exts := e.Extensions()
	if len(exts) == 0 {
		t.Fatal("expected non-empty extensions list")
	}
	found := false
	for _, x := range exts {
		if x == ".py" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected .py in extensions, got %v", exts)
	}
}

func TestPythonExtractor_lineNumber_recorded(t *testing.T) {
	src := []byte(`def first():
    pass

def second():
    pass
`)
	e := &PythonExtractor{}
	syms, _, err := e.Extract("mod.py", src)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(syms) != 2 {
		t.Fatalf("expected 2 symbols, got %d", len(syms))
	}
	if syms[0].Line != 1 {
		t.Errorf("first func line: got %d, want 1", syms[0].Line)
	}
	if syms[1].Line != 4 {
		t.Errorf("second func line: got %d, want 4", syms[1].Line)
	}
}

func TestPythonExtractor_signature_recorded(t *testing.T) {
	src := []byte(`def fetch(url, timeout=30):
    pass
`)
	e := &PythonExtractor{}
	syms, _, err := e.Extract("mod.py", src)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(syms) == 0 {
		t.Fatal("expected 1 symbol")
	}
	if syms[0].Signature == "" {
		t.Errorf("expected non-empty signature, got empty")
	}
}

func TestPythonExtractor_emptyFile_noError(t *testing.T) {
	e := &PythonExtractor{}
	syms, imps, err := e.Extract("empty.py", []byte{})
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

func TestPythonExtractor_nestedFunc_skipped(t *testing.T) {
	src := []byte(`def outer():
    def inner():
        pass
    return inner
`)
	e := &PythonExtractor{}
	syms, _, err := e.Extract("mod.py", src)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	// Only outer should be extracted; inner is nested, not module-level
	if len(syms) != 1 {
		t.Fatalf("expected 1 symbol (outer only), got %d: %v", len(syms), syms)
	}
	if syms[0].Name != "outer" {
		t.Errorf("expected outer, got %q", syms[0].Name)
	}
}

func TestPythonExtractor_classDocstring_extracted(t *testing.T) {
	src := []byte(`class Config:
    """Holds configuration values."""
    pass
`)
	e := &PythonExtractor{}
	syms, _, err := e.Extract("config.py", src)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(syms) == 0 {
		t.Fatal("expected 1 symbol")
	}
	if syms[0].DocComment != "Holds configuration values." {
		t.Errorf("doc: got %q", syms[0].DocComment)
	}
}

func TestPythonExtractor_importAlias_extracted(t *testing.T) {
	src := []byte(`import numpy as np
`)
	e := &PythonExtractor{}
	_, imps, err := e.Extract("mod.py", src)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(imps) != 1 {
		t.Fatalf("expected 1 import, got %d: %v", len(imps), imps)
	}
	if imps[0].Path != "numpy" {
		t.Errorf("import path: got %q, want numpy", imps[0].Path)
	}
}
