package lang

import (
	"testing"

	"github.com/sandgardenhq/find-the-gaps/internal/scanner/types"
)

func TestRubyExtractor_topLevelMethod_emitted(t *testing.T) {
	src := []byte(`def foo
  "bar"
end
`)
	e := &RubyExtractor{}
	syms, _, err := e.Extract("top.rb", src)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(syms) != 1 {
		t.Fatalf("expected 1 symbol, got %d: %v", len(syms), syms)
	}
	if syms[0].Name != "foo" || syms[0].Kind != types.KindFunc {
		t.Errorf("got %+v", syms[0])
	}
}

func TestRubyExtractor_publicMethodInClass_emitted(t *testing.T) {
	src := []byte(`class Foo
  def public_method
  end
end
`)
	e := &RubyExtractor{}
	syms, _, err := e.Extract("foo.rb", src)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	// We expect both the class AND its public method.
	var names []string
	for _, s := range syms {
		names = append(names, s.Name)
	}
	foundClass := false
	foundMethod := false
	for _, s := range syms {
		if s.Name == "Foo" && s.Kind == types.KindClass {
			foundClass = true
		}
		if s.Name == "public_method" && s.Kind == types.KindFunc {
			foundMethod = true
		}
	}
	if !foundClass {
		t.Errorf("expected class Foo in %v", names)
	}
	if !foundMethod {
		t.Errorf("expected public_method in %v", names)
	}
}

func TestRubyExtractor_methodAfterPrivate_skipped(t *testing.T) {
	src := []byte(`class Foo
  def a
  end
  private
  def b
  end
end
`)
	e := &RubyExtractor{}
	syms, _, err := e.Extract("foo.rb", src)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	for _, s := range syms {
		if s.Name == "b" {
			t.Errorf("expected b to be skipped after bare private, got %+v", s)
		}
	}
	foundA := false
	for _, s := range syms {
		if s.Name == "a" {
			foundA = true
		}
	}
	if !foundA {
		t.Errorf("expected public method a to be emitted, got syms=%v", syms)
	}
}

func TestRubyExtractor_methodAfterProtected_skipped(t *testing.T) {
	src := []byte(`class Foo
  def a
  end
  protected
  def b
  end
end
`)
	e := &RubyExtractor{}
	syms, _, err := e.Extract("foo.rb", src)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	for _, s := range syms {
		if s.Name == "b" {
			t.Errorf("expected b to be skipped after bare protected, got %+v", s)
		}
	}
	foundA := false
	for _, s := range syms {
		if s.Name == "a" {
			foundA = true
		}
	}
	if !foundA {
		t.Errorf("expected public method a to be emitted, got syms=%v", syms)
	}
}

func TestRubyExtractor_privateScopeScopedToClass(t *testing.T) {
	// `private` inside class A must not affect methods in class B — each class
	// independently starts public.
	src := []byte(`class A
  def a1
  end
  private
  def a2
  end
end

class B
  def b1
  end
end
`)
	e := &RubyExtractor{}
	syms, _, err := e.Extract("both.rb", src)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}

	want := map[string]bool{"A": false, "B": false, "a1": false, "b1": false}
	for _, s := range syms {
		if _, ok := want[s.Name]; ok {
			want[s.Name] = true
		}
		if s.Name == "a2" {
			t.Errorf("a2 should be skipped (private in A)")
		}
	}
	for n, seen := range want {
		if !seen {
			t.Errorf("expected %q in output, got %v", n, syms)
		}
	}
}

func TestRubyExtractor_class_extracted(t *testing.T) {
	src := []byte(`class Foo
end
`)
	e := &RubyExtractor{}
	syms, _, err := e.Extract("foo.rb", src)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(syms) != 1 {
		t.Fatalf("expected 1 symbol, got %d: %v", len(syms), syms)
	}
	if syms[0].Name != "Foo" || syms[0].Kind != types.KindClass {
		t.Errorf("got %+v", syms[0])
	}
}

func TestRubyExtractor_module_extracted(t *testing.T) {
	src := []byte(`module Bar
end
`)
	e := &RubyExtractor{}
	syms, _, err := e.Extract("bar.rb", src)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(syms) != 1 {
		t.Fatalf("expected 1 symbol, got %d: %v", len(syms), syms)
	}
	if syms[0].Name != "Bar" || syms[0].Kind != types.KindClass {
		t.Errorf("got %+v", syms[0])
	}
}

func TestRubyExtractor_singletonMethod_extracted(t *testing.T) {
	src := []byte(`class Foo
  def self.baz
  end
end
`)
	e := &RubyExtractor{}
	syms, _, err := e.Extract("foo.rb", src)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	foundBaz := false
	for _, s := range syms {
		if s.Name == "baz" && s.Kind == types.KindFunc {
			foundBaz = true
		}
	}
	if !foundBaz {
		t.Errorf("expected singleton_method baz to be emitted, got %v", syms)
	}
}

func TestRubyExtractor_imports_extracted(t *testing.T) {
	src := []byte(`require 'foo'
require_relative 'bar'
`)
	e := &RubyExtractor{}
	_, imps, err := e.Extract("deps.rb", src)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(imps) != 2 {
		t.Fatalf("expected 2 imports, got %d: %v", len(imps), imps)
	}
	want := map[string]bool{"foo": false, "bar": false}
	for _, imp := range imps {
		if _, ok := want[imp.Path]; ok {
			want[imp.Path] = true
		}
	}
	for p, seen := range want {
		if !seen {
			t.Errorf("expected import path %q, got %v", p, imps)
		}
	}
}

func TestRubyExtractor_docComment_captured(t *testing.T) {
	src := []byte(`# greets the caller
# with enthusiasm
def greet
end
`)
	e := &RubyExtractor{}
	syms, _, err := e.Extract("greet.rb", src)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(syms) == 0 {
		t.Fatal("expected 1 symbol")
	}
	got := syms[0].DocComment
	if got != "greets the caller\nwith enthusiasm" {
		t.Errorf("doc comment: got %q", got)
	}
}
