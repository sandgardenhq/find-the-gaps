package lang

import (
	"strings"
	"testing"

	"github.com/sandgardenhq/find-the-gaps/internal/scanner/types"
)

// --- public-by-default function ---

func TestKotlinExtractor_publicDefault_emitted(t *testing.T) {
	src := []byte(`fun foo() {}
`)
	e := &KotlinExtractor{}
	syms, _, err := e.Extract("x.kt", src)
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
		t.Fatalf("expected a symbol named 'foo' (public by default), got: %v", syms)
	}
	if fn.Kind != types.KindFunc {
		t.Errorf("kind: got %q, want func", fn.Kind)
	}
	if fn.Line != 1 {
		t.Errorf("line: got %d, want 1", fn.Line)
	}
}

// --- private modifier skips ---

func TestKotlinExtractor_privateSkipped(t *testing.T) {
	src := []byte(`private fun hidden() {}
fun visible() {}
`)
	e := &KotlinExtractor{}
	syms, _, err := e.Extract("x.kt", src)
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

// --- internal modifier skips ---

func TestKotlinExtractor_internalSkipped(t *testing.T) {
	src := []byte(`internal fun hidden() {}
fun visible() {}
`)
	e := &KotlinExtractor{}
	syms, _, err := e.Extract("x.kt", src)
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

// --- class_declaration ---

func TestKotlinExtractor_class_extracted(t *testing.T) {
	src := []byte(`class Greeter { }
`)
	e := &KotlinExtractor{}
	syms, _, err := e.Extract("Greeter.kt", src)
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

// --- object_declaration ---

func TestKotlinExtractor_object_extracted(t *testing.T) {
	src := []byte(`object Singleton { }
`)
	e := &KotlinExtractor{}
	syms, _, err := e.Extract("Singleton.kt", src)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	var obj *types.Symbol
	for i := range syms {
		if syms[i].Name == "Singleton" {
			obj = &syms[i]
			break
		}
	}
	if obj == nil {
		t.Fatalf("expected a symbol named 'Singleton', got: %v", syms)
	}
	if obj.Kind != types.KindClass {
		t.Errorf("kind: got %q, want class", obj.Kind)
	}
}

// --- property_declaration ---

func TestKotlinExtractor_property_extracted(t *testing.T) {
	src := []byte(`val MaxRetries = 3
`)
	e := &KotlinExtractor{}
	syms, _, err := e.Extract("consts.kt", src)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	var prop *types.Symbol
	for i := range syms {
		if syms[i].Name == "MaxRetries" {
			prop = &syms[i]
			break
		}
	}
	if prop == nil {
		t.Fatalf("expected a symbol named 'MaxRetries', got: %v", syms)
	}
	if prop.Kind != types.KindVar {
		t.Errorf("kind: got %q, want var", prop.Kind)
	}
}

// --- imports with alias ---

func TestKotlinExtractor_imports_extracted(t *testing.T) {
	src := []byte(`import foo.Bar
import foo.Baz as Renamed

fun x() {}
`)
	e := &KotlinExtractor{}
	_, imps, err := e.Extract("x.kt", src)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(imps) != 2 {
		t.Fatalf("expected 2 imports, got %d: %v", len(imps), imps)
	}
	var plain, aliased *types.Import
	for i := range imps {
		if imps[i].Path == "foo.Bar" {
			plain = &imps[i]
		}
		if imps[i].Path == "foo.Baz" {
			aliased = &imps[i]
		}
	}
	if plain == nil {
		t.Errorf("expected import path 'foo.Bar', got: %v", imps)
	} else if plain.Alias != "" {
		t.Errorf("plain import should have no alias, got %q", plain.Alias)
	}
	if aliased == nil {
		t.Errorf("expected import path 'foo.Baz', got: %v", imps)
	} else if aliased.Alias != "Renamed" {
		t.Errorf("alias: got %q, want 'Renamed'", aliased.Alias)
	}
}

// --- KDoc ---

func TestKotlinExtractor_docComment_captured(t *testing.T) {
	src := []byte(`/** Adds two numbers. */
fun add(a: Int, b: Int): Int = a + b
`)
	e := &KotlinExtractor{}
	syms, _, err := e.Extract("add.kt", src)
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
		t.Fatalf("expected non-empty DocComment, got empty")
	}
	if strings.Contains(fn.DocComment, "/**") || strings.Contains(fn.DocComment, "*/") {
		t.Errorf("DocComment still contains /** */ markers: %q", fn.DocComment)
	}
	if !strings.Contains(fn.DocComment, "Adds two numbers.") {
		t.Errorf("DocComment missing expected text: %q", fn.DocComment)
	}
}
