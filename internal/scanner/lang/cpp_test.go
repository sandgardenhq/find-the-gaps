package lang

import (
	"strings"
	"testing"

	"github.com/sandgardenhq/find-the-gaps/internal/scanner/types"
)

// --- .hpp: public function prototype emitted ---

func TestCPPExtractor_header_publicFunc_emitted(t *testing.T) {
	src := []byte(`int add(int a, int b);
`)
	e := &CPPExtractor{}
	syms, _, err := e.Extract("api.hpp", src)
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
	if fn.Kind != types.KindFunc {
		t.Errorf("kind: got %q, want func", fn.Kind)
	}
	if fn.Line != 1 {
		t.Errorf("line: got %d, want 1", fn.Line)
	}
}

// --- .cpp: static function skipped, visible kept ---

func TestCPPExtractor_source_staticFunc_skipped(t *testing.T) {
	src := []byte(`static int helper() { return 0; }
int visible() { return 1; }
`)
	e := &CPPExtractor{}
	syms, _, err := e.Extract("impl.cpp", src)
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
	if names["helper"] {
		t.Errorf("expected static 'helper' to be skipped, but it was emitted")
	}
}

// --- class itself emitted as KindClass ---

func TestCPPExtractor_class_extracted(t *testing.T) {
	src := []byte(`class MyClass {
public:
    void foo();
};
`)
	e := &CPPExtractor{}
	syms, _, err := e.Extract("api.hpp", src)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	var cl *types.Symbol
	for i := range syms {
		if syms[i].Name == "MyClass" {
			cl = &syms[i]
			break
		}
	}
	if cl == nil {
		t.Fatalf("expected a symbol named 'MyClass', got: %v", syms)
	}
	if cl.Kind != types.KindClass {
		t.Errorf("kind: got %q, want class", cl.Kind)
	}
}

// --- private member skipped, public member kept ---

func TestCPPExtractor_privateMember_skipped(t *testing.T) {
	src := []byte(`class C {
private:
    void hidden();
public:
    void visible();
};
`)
	e := &CPPExtractor{}
	syms, _, err := e.Extract("api.hpp", src)
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
		t.Errorf("expected 'hidden' (private member) to be skipped, but it was emitted")
	}
	if !names["C"] {
		t.Errorf("expected class 'C' itself to be emitted, got: %v", names)
	}
}

// --- class defaults to private before any access specifier ---

func TestCPPExtractor_classDefaultPrivate(t *testing.T) {
	src := []byte(`class C {
    void hidden();
public:
    void visible();
};
`)
	e := &CPPExtractor{}
	syms, _, err := e.Extract("api.hpp", src)
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
		t.Errorf("expected 'hidden' (private by default in class) to be skipped, but it was emitted")
	}
	if !names["C"] {
		t.Errorf("expected class 'C' itself to be emitted, got: %v", names)
	}
}

// --- struct defaults to public ---

func TestCPPExtractor_structDefaultPublic(t *testing.T) {
	src := []byte(`struct S {
    void visible();
};
`)
	e := &CPPExtractor{}
	syms, _, err := e.Extract("api.hpp", src)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	names := map[string]bool{}
	for _, s := range syms {
		names[s.Name] = true
	}
	if !names["visible"] {
		t.Errorf("expected 'visible' to be emitted (struct defaults public), got: %v", names)
	}
	if !names["S"] {
		t.Errorf("expected struct 'S' itself to be emitted, got: %v", names)
	}
}

// --- anonymous namespace hides its contents ---

func TestCPPExtractor_anonymousNamespace_skipped(t *testing.T) {
	src := []byte(`namespace {
int hidden() { return 0; }
}
int visible() { return 1; }
`)
	e := &CPPExtractor{}
	syms, _, err := e.Extract("impl.cpp", src)
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
		t.Errorf("expected 'hidden' (inside anonymous namespace) to be skipped, but it was emitted")
	}
}

// --- named namespaces DO emit their contents ---

func TestCPPExtractor_namedNamespace_emitted(t *testing.T) {
	src := []byte(`namespace foo {
int bar() { return 0; }
}
`)
	e := &CPPExtractor{}
	syms, _, err := e.Extract("impl.cpp", src)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	names := map[string]bool{}
	for _, s := range syms {
		names[s.Name] = true
	}
	if !names["bar"] {
		t.Errorf("expected 'bar' inside named namespace 'foo' to be emitted, got: %v", names)
	}
}

// --- using std::string; imports with path std::string ---

func TestCPPExtractor_usingDeclaration_extracted(t *testing.T) {
	src := []byte(`using std::string;
`)
	e := &CPPExtractor{}
	_, imps, err := e.Extract("api.hpp", src)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(imps) != 1 {
		t.Fatalf("expected 1 import, got %d: %v", len(imps), imps)
	}
	if imps[0].Path != "std::string" {
		t.Errorf("expected path 'std::string', got %q", imps[0].Path)
	}
	if imps[0].Alias != "" {
		t.Errorf("expected empty alias, got %q", imps[0].Alias)
	}
}

// --- using namespace std; imports with path std (no alias) ---

func TestCPPExtractor_usingNamespace_extracted(t *testing.T) {
	src := []byte(`using namespace std;
`)
	e := &CPPExtractor{}
	_, imps, err := e.Extract("api.hpp", src)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(imps) != 1 {
		t.Fatalf("expected 1 import, got %d: %v", len(imps), imps)
	}
	if imps[0].Path != "std" {
		t.Errorf("expected path 'std', got %q", imps[0].Path)
	}
	if imps[0].Alias != "" {
		t.Errorf("expected empty alias, got %q", imps[0].Alias)
	}
}

// --- #include <vector> imports with path vector ---

func TestCPPExtractor_include_extracted(t *testing.T) {
	src := []byte(`#include <vector>
`)
	e := &CPPExtractor{}
	_, imps, err := e.Extract("api.hpp", src)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(imps) != 1 {
		t.Fatalf("expected 1 import, got %d: %v", len(imps), imps)
	}
	if imps[0].Path != "vector" {
		t.Errorf("expected path 'vector' (delimiters stripped), got %q", imps[0].Path)
	}
}

// --- enum, typedef, union emit correct kinds (shared C behavior) ---

func TestCPPExtractor_enum_extracted(t *testing.T) {
	src := []byte(`enum Status { ACTIVE, INACTIVE };
`)
	e := &CPPExtractor{}
	syms, _, err := e.Extract("api.hpp", src)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	var en *types.Symbol
	for i := range syms {
		if syms[i].Name == "Status" {
			en = &syms[i]
			break
		}
	}
	if en == nil {
		t.Fatalf("expected 'Status' enum, got: %v", syms)
	}
	if en.Kind != types.KindType {
		t.Errorf("kind: got %q, want type", en.Kind)
	}
}

func TestCPPExtractor_typedef_extracted(t *testing.T) {
	src := []byte(`typedef int MyInt;
`)
	e := &CPPExtractor{}
	syms, _, err := e.Extract("api.hpp", src)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	var td *types.Symbol
	for i := range syms {
		if syms[i].Name == "MyInt" {
			td = &syms[i]
			break
		}
	}
	if td == nil {
		t.Fatalf("expected 'MyInt' typedef, got: %v", syms)
	}
	if td.Kind != types.KindType {
		t.Errorf("kind: got %q, want type", td.Kind)
	}
}

func TestCPPExtractor_union_extracted(t *testing.T) {
	src := []byte(`union Value { int i; float f; };
`)
	e := &CPPExtractor{}
	syms, _, err := e.Extract("api.hpp", src)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	var un *types.Symbol
	for i := range syms {
		if syms[i].Name == "Value" {
			un = &syms[i]
			break
		}
	}
	if un == nil {
		t.Fatalf("expected 'Value' union, got: %v", syms)
	}
	if un.Kind != types.KindClass {
		t.Errorf("kind: got %q, want class", un.Kind)
	}
}

// --- #define macro skipped ---

func TestCPPExtractor_define_skipped(t *testing.T) {
	src := []byte(`#define MAX 100
int visible();
`)
	e := &CPPExtractor{}
	syms, _, err := e.Extract("api.hpp", src)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	names := map[string]bool{}
	for _, s := range syms {
		names[s.Name] = true
	}
	if names["MAX"] {
		t.Errorf("expected #define 'MAX' to be skipped, got: %v", names)
	}
	if !names["visible"] {
		t.Errorf("expected 'visible' to be emitted alongside #define, got: %v", names)
	}
}

// --- top-level global variable emits KindVar ---

func TestCPPExtractor_globalVar_extracted(t *testing.T) {
	src := []byte(`int global_count = 42;
`)
	e := &CPPExtractor{}
	syms, _, err := e.Extract("impl.cpp", src)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	kinds := map[string]types.SymbolKind{}
	for _, s := range syms {
		kinds[s.Name] = s.Kind
	}
	if kinds["global_count"] != types.KindVar {
		t.Errorf("global_count: got kind %q, want var (syms=%v)", kinds["global_count"], syms)
	}
}

// --- using namespace foo::bar; import with dotted path ---

func TestCPPExtractor_usingNamespaceQualified_extracted(t *testing.T) {
	src := []byte(`using namespace foo::bar;
`)
	e := &CPPExtractor{}
	_, imps, err := e.Extract("api.hpp", src)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(imps) != 1 {
		t.Fatalf("expected 1 import, got %d: %v", len(imps), imps)
	}
	if imps[0].Path != "foo::bar" {
		t.Errorf("expected path 'foo::bar', got %q", imps[0].Path)
	}
}

// --- /** ... */ doc comment captured, markers stripped ---

func TestCPPExtractor_docComment_captured(t *testing.T) {
	src := []byte(`/** Adds two numbers. */
int add(int a, int b);
`)
	e := &CPPExtractor{}
	syms, _, err := e.Extract("api.hpp", src)
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
		t.Errorf("expected non-empty DocComment, got empty")
	}
	if fn.DocComment == "/** Adds two numbers. */" {
		t.Errorf("DocComment still includes /** */ markers: %q", fn.DocComment)
	}
	if !strings.Contains(fn.DocComment, "Adds two numbers.") {
		t.Errorf("DocComment missing expected text: %q", fn.DocComment)
	}
}
