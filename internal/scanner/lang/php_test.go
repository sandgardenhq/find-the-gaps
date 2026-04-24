package lang

import (
	"strings"
	"testing"

	"github.com/sandgardenhq/find-the-gaps/internal/scanner/types"
)

// --- top-level function ---

func TestPHPExtractor_topLevelFunc_emitted(t *testing.T) {
	src := []byte(`<?php
function foo() {
  return 1;
}
`)
	e := &PHPExtractor{}
	syms, _, err := e.Extract("foo.php", src)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	var fn *types.Symbol
	for i := range syms {
		if syms[i].Name == "foo" {
			fn = &syms[i]
			break
		}
	}
	if fn == nil {
		t.Fatalf("expected a symbol named 'foo', got: %v", syms)
	}
	if fn.Kind != types.KindFunc {
		t.Errorf("kind: got %q, want func", fn.Kind)
	}
	if fn.Line != 2 {
		t.Errorf("line: got %d, want 2", fn.Line)
	}
}

// --- public method emitted ---

func TestPHPExtractor_publicMethod_extracted(t *testing.T) {
	src := []byte(`<?php
class Svc {
  public function bar() {}
}
`)
	e := &PHPExtractor{}
	syms, _, err := e.Extract("Svc.php", src)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	names := map[string]bool{}
	for _, s := range syms {
		names[s.Name] = true
	}
	if !names["bar"] {
		t.Errorf("expected 'bar' to be emitted, got: %v", names)
	}
}

// --- private method skipped ---

func TestPHPExtractor_privateMethod_skipped(t *testing.T) {
	src := []byte(`<?php
class Svc {
  public function visible() {}
  private function hidden() {}
}
`)
	e := &PHPExtractor{}
	syms, _, err := e.Extract("Svc.php", src)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	names := map[string]bool{}
	for _, s := range syms {
		names[s.Name] = true
	}
	if !names["visible"] {
		t.Errorf("expected 'visible' to be emitted, got: %v", names)
	}
	if names["hidden"] {
		t.Errorf("expected 'hidden' to be skipped, but it was emitted")
	}
}

// --- protected method skipped ---

func TestPHPExtractor_protectedMethod_skipped(t *testing.T) {
	src := []byte(`<?php
class Svc {
  public function visible() {}
  protected function shielded() {}
}
`)
	e := &PHPExtractor{}
	syms, _, err := e.Extract("Svc.php", src)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	names := map[string]bool{}
	for _, s := range syms {
		names[s.Name] = true
	}
	if !names["visible"] {
		t.Errorf("expected 'visible' to be emitted, got: %v", names)
	}
	if names["shielded"] {
		t.Errorf("expected 'shielded' to be skipped, but it was emitted")
	}
}

// --- no-modifier (implicit public) method emitted ---

func TestPHPExtractor_noModifierMethod_emitted(t *testing.T) {
	src := []byte(`<?php
class Svc {
  function implicitPublic() {}
}
`)
	e := &PHPExtractor{}
	syms, _, err := e.Extract("Svc.php", src)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	names := map[string]bool{}
	for _, s := range syms {
		names[s.Name] = true
	}
	if !names["implicitPublic"] {
		t.Errorf("expected 'implicitPublic' to be emitted (PHP implicit public), got: %v", names)
	}
}

// --- class emitted as KindClass ---

func TestPHPExtractor_class_extracted(t *testing.T) {
	src := []byte(`<?php
class Greeter {
}
`)
	e := &PHPExtractor{}
	syms, _, err := e.Extract("Greeter.php", src)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	var cls *types.Symbol
	for i := range syms {
		if syms[i].Name == "Greeter" {
			cls = &syms[i]
			break
		}
	}
	if cls == nil {
		t.Fatalf("expected a symbol named 'Greeter', got: %v", syms)
	}
	if cls.Kind != types.KindClass {
		t.Errorf("kind: got %q, want class", cls.Kind)
	}
}

// --- interface emitted as KindInterface ---

func TestPHPExtractor_interface_extracted(t *testing.T) {
	src := []byte(`<?php
interface Repo {
}
`)
	e := &PHPExtractor{}
	syms, _, err := e.Extract("Repo.php", src)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	var iface *types.Symbol
	for i := range syms {
		if syms[i].Name == "Repo" {
			iface = &syms[i]
			break
		}
	}
	if iface == nil {
		t.Fatalf("expected a symbol named 'Repo', got: %v", syms)
	}
	if iface.Kind != types.KindInterface {
		t.Errorf("kind: got %q, want interface", iface.Kind)
	}
}

// --- trait emitted as KindClass ---

func TestPHPExtractor_trait_extracted(t *testing.T) {
	src := []byte(`<?php
trait Helper {
}
`)
	e := &PHPExtractor{}
	syms, _, err := e.Extract("Helper.php", src)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	var tr *types.Symbol
	for i := range syms {
		if syms[i].Name == "Helper" {
			tr = &syms[i]
			break
		}
	}
	if tr == nil {
		t.Fatalf("expected a symbol named 'Helper', got: %v", syms)
	}
	if tr.Kind != types.KindClass {
		t.Errorf("kind: got %q, want class", tr.Kind)
	}
}

// --- imports (plain + aliased) ---

func TestPHPExtractor_imports_extracted(t *testing.T) {
	src := []byte(`<?php
use Foo\Bar;
use Foo\Baz as Qux;
`)
	e := &PHPExtractor{}
	_, imps, err := e.Extract("x.php", src)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(imps) != 2 {
		t.Fatalf("expected 2 imports, got %d: %v", len(imps), imps)
	}
	var plain, aliased *types.Import
	for i := range imps {
		switch imps[i].Path {
		case "Foo\\Bar":
			plain = &imps[i]
		case "Foo\\Baz":
			aliased = &imps[i]
		}
	}
	if plain == nil {
		t.Errorf("expected import path 'Foo\\Bar', got: %v", imps)
	} else if plain.Alias != "" {
		t.Errorf("plain import should have empty alias, got %q", plain.Alias)
	}
	if aliased == nil {
		t.Errorf("expected import path 'Foo\\Baz', got: %v", imps)
	} else if aliased.Alias != "Qux" {
		t.Errorf("aliased import Alias: got %q, want %q", aliased.Alias, "Qux")
	}
}

// --- PHPDoc captured, markers stripped ---

func TestPHPExtractor_docComment_captured(t *testing.T) {
	src := []byte(`<?php
/** Adds two numbers. */
function add($a, $b) {
  return $a + $b;
}
`)
	e := &PHPExtractor{}
	syms, _, err := e.Extract("add.php", src)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	var fn *types.Symbol
	for i := range syms {
		if syms[i].Name == "add" {
			fn = &syms[i]
			break
		}
	}
	if fn == nil {
		t.Fatalf("expected a symbol named 'add', got: %v", syms)
	}
	if fn.DocComment == "" {
		t.Errorf("expected non-empty DocComment")
	}
	if fn.DocComment == "/** Adds two numbers. */" {
		t.Errorf("DocComment still includes /** */ markers: %q", fn.DocComment)
	}
	if !strings.Contains(fn.DocComment, "Adds two numbers.") {
		t.Errorf("DocComment missing expected text: %q", fn.DocComment)
	}
}
