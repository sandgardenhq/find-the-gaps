package lang

import (
	"strings"
	"testing"

	"github.com/sandgardenhq/find-the-gaps/internal/scanner/types"
)

// --- .h: public function prototype emitted ---

func TestCExtractor_header_publicFunc_emitted(t *testing.T) {
	src := []byte(`int add(int a, int b);
`)
	e := &CExtractor{}
	syms, _, err := e.Extract("api.h", src)
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

// --- .h: static function skipped, public one kept ---

func TestCExtractor_header_staticFunc_skipped(t *testing.T) {
	src := []byte(`static int helper(void);
int visible(void);
`)
	e := &CExtractor{}
	syms, _, err := e.Extract("api.h", src)
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
		t.Errorf("expected 'helper' to be skipped (static in .h), but it was emitted")
	}
}

// --- .c: non-static function emitted ---

func TestCExtractor_source_nonStaticFunc_emitted(t *testing.T) {
	src := []byte(`int add(int a, int b) {
    return a + b;
}
`)
	e := &CExtractor{}
	syms, _, err := e.Extract("impl.c", src)
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

// --- .c: static function skipped, public kept ---

func TestCExtractor_source_staticFunc_skipped(t *testing.T) {
	src := []byte(`static int helper(void) {
    return 0;
}
int visible(void) {
    return 1;
}
`)
	e := &CExtractor{}
	syms, _, err := e.Extract("impl.c", src)
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
		t.Errorf("expected 'helper' to be skipped (static in .c), but it was emitted")
	}
}

// --- struct emits KindClass ---

func TestCExtractor_struct_extracted(t *testing.T) {
	src := []byte(`struct Point {
    int x;
    int y;
};
`)
	e := &CExtractor{}
	syms, _, err := e.Extract("types.h", src)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	var st *types.Symbol
	for i := range syms {
		if syms[i].Name == "Point" {
			st = &syms[i]
			break
		}
	}
	if st == nil {
		t.Fatalf("expected a symbol named 'Point', got: %v", syms)
	}
	if st.Kind != types.KindClass {
		t.Errorf("kind: got %q, want class", st.Kind)
	}
}

// --- typedef emits KindType ---

func TestCExtractor_typedef_extracted(t *testing.T) {
	src := []byte(`typedef int MyInt;
`)
	e := &CExtractor{}
	syms, _, err := e.Extract("types.h", src)
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
		t.Fatalf("expected a symbol named 'MyInt', got: %v", syms)
	}
	if td.Kind != types.KindType {
		t.Errorf("kind: got %q, want type", td.Kind)
	}
}

// --- enum emits KindType ---

func TestCExtractor_enum_extracted(t *testing.T) {
	src := []byte(`enum Status {
    ACTIVE,
    INACTIVE
};
`)
	e := &CExtractor{}
	syms, _, err := e.Extract("types.h", src)
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
		t.Fatalf("expected a symbol named 'Status', got: %v", syms)
	}
	if en.Kind != types.KindType {
		t.Errorf("kind: got %q, want type", en.Kind)
	}
}

// --- #define macros not emitted; function alongside is emitted ---

func TestCExtractor_define_skipped(t *testing.T) {
	src := []byte(`#define MAX 100
int visible(void);
`)
	e := &CExtractor{}
	syms, _, err := e.Extract("api.h", src)
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
	if names["MAX"] {
		t.Errorf("expected #define macro 'MAX' NOT to be emitted, but it was")
	}
}

// --- #include imports ---

func TestCExtractor_imports_extracted(t *testing.T) {
	src := []byte(`#include <stdio.h>
#include "mylib.h"
`)
	e := &CExtractor{}
	_, imps, err := e.Extract("app.c", src)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(imps) != 2 {
		t.Fatalf("expected 2 imports, got %d: %v", len(imps), imps)
	}
	seen := map[string]bool{}
	for _, i := range imps {
		seen[i.Path] = true
	}
	if !seen["stdio.h"] {
		t.Errorf("expected import path 'stdio.h' (delimiters stripped), got: %v", imps)
	}
	if !seen["mylib.h"] {
		t.Errorf("expected import path 'mylib.h' (delimiters stripped), got: %v", imps)
	}
}

// --- global variable emits KindVar (init and bare forms) ---

func TestCExtractor_globalVar_extracted(t *testing.T) {
	src := []byte(`int global_count = 42;
int bare_flag;
static int private_var = 0;
`)
	e := &CExtractor{}
	syms, _, err := e.Extract("impl.c", src)
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
	if kinds["bare_flag"] != types.KindVar {
		t.Errorf("bare_flag: got kind %q, want var (syms=%v)", kinds["bare_flag"], syms)
	}
	if _, found := kinds["private_var"]; found {
		t.Errorf("expected static 'private_var' to be skipped, got: %v", kinds)
	}
}

// --- union emits KindClass ---

func TestCExtractor_union_extracted(t *testing.T) {
	src := []byte(`union Value {
    int i;
    float f;
};
`)
	e := &CExtractor{}
	syms, _, err := e.Extract("types.h", src)
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
		t.Fatalf("expected a symbol named 'Value', got: %v", syms)
	}
	if un.Kind != types.KindClass {
		t.Errorf("kind: got %q, want class", un.Kind)
	}
}

// --- /** */ doc comment captured, markers stripped ---

func TestCExtractor_docComment_captured(t *testing.T) {
	src := []byte(`/** Adds two numbers. */
int add(int a, int b);
`)
	e := &CExtractor{}
	syms, _, err := e.Extract("api.h", src)
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
