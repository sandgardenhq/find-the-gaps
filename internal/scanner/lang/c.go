package lang

import (
	"context"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
	cgrammar "github.com/smacker/go-tree-sitter/c"

	"github.com/sandgardenhq/find-the-gaps/internal/scanner/types"
)

// CExtractor extracts public declarations and #include imports from C source
// files. C has no `public` keyword — visibility is determined by the
// combination of file extension and the `static` storage-class specifier:
//
//   - In .h headers, top-level declarations are public unless they are marked
//     `static` (a rare but supported case).
//   - In .c sources, top-level declarations are public unless they are marked
//     `static` (a common case for file-local helpers).
//
// #define macros are never emitted — they are text substitution, not symbols.
//
// Tree-sitter node types (verified against tree-sitter-c via a throwaway debug
// test before coding, then deleted):
//
//   - translation_unit         — root
//   - preproc_include          — `#include <x>` or `#include "x"`; contains a
//                                `system_lib_string` or `string_literal` child
//                                whose content is `<x>` / `"x"` (delimiters
//                                must be stripped)
//   - preproc_def              — `#define NAME ...`; skipped entirely
//   - declaration              — a function prototype (contains a
//                                `function_declarator`) OR a global variable
//                                (contains an `init_declarator` or bare
//                                identifier declarator)
//   - function_definition      — a function with a body (`compound_statement`)
//   - storage_class_specifier  — wraps a `static` / `extern` / etc. keyword;
//                                `.Content()` yields e.g. "static"
//   - struct_specifier         — `struct X { ... }`; fields: name=type_identifier,
//                                body=field_declaration_list
//   - union_specifier          — `union X { ... }`; same shape as struct
//   - type_definition          — `typedef ... Name;`; the last `type_identifier`
//                                child is the new type name. A
//                                `typedef struct X { ... } Y;` wraps a
//                                struct_specifier.
//   - enum_specifier           — `enum X { ... }`; name=type_identifier
//   - function_declarator      — contains an `identifier` name child
//   - init_declarator          — a global variable initializer; first child is
//                                an `identifier`
type CExtractor struct{}

func (e *CExtractor) Language() string     { return "C" }
func (e *CExtractor) Extensions() []string { return []string{".c", ".h"} }

// Extract parses C source and returns top-level declarations and #include
// imports. Visibility is determined by path extension (.h vs .c) plus the
// absence of a `static` storage-class specifier. #define macros are skipped.
func (e *CExtractor) Extract(_ string, content []byte) ([]types.Symbol, []types.Import, error) {
	syms := []types.Symbol{}
	imps := []types.Import{}

	parser := sitter.NewParser()
	defer parser.Close()
	parser.SetLanguage(cgrammar.GetLanguage())

	tree, err := parser.ParseCtx(context.Background(), nil, content)
	if err != nil {
		return syms, imps, err
	}
	defer tree.Close()

	// C visibility is determined by file extension plus the `static` storage
	// class:
	//   - .h (header): top-level declarations are public unless marked
	//     `static` (rare in headers but legal; e.g. `static inline` helpers).
	//   - .c (source): top-level declarations are public unless marked
	//     `static` (file-local helpers use this extensively).
	// Both variants reduce to the same test — skip iff a
	// `storage_class_specifier` child is `static` — so the implementation does
	// not actually branch on the extension. Detect() ensures only .c/.h files
	// reach this extractor, and both follow the same rule.

	root := tree.RootNode()
	for i := 0; i < int(root.ChildCount()); i++ {
		node := root.Child(i)
		switch node.Type() {
		case "preproc_include":
			if imp, ok := cExtractInclude(node, content); ok {
				imps = append(imps, imp)
			}
		case "preproc_def":
			// #define macros are text substitution, not symbols. Skip.
			continue
		case "declaration", "function_definition":
			// In both .h and .c, a `static` storage class means file-local.
			// Skip unconditionally.
			if cHasStatic(node) {
				continue
			}
			if s, ok := cFuncSymbol(root, i, node, content); ok {
				syms = append(syms, s)
				continue
			}
			if s, ok := cVarSymbol(root, i, node, content); ok {
				syms = append(syms, s)
			}
		case "struct_specifier", "union_specifier":
			if s, ok := cTypeSymbol(root, i, node, content, types.KindClass); ok {
				syms = append(syms, s)
			}
		case "type_definition":
			if s, ok := cTypedefSymbol(root, i, node, content); ok {
				syms = append(syms, s)
			}
		case "enum_specifier":
			if s, ok := cTypeSymbol(root, i, node, content, types.KindType); ok {
				syms = append(syms, s)
			}
		}
	}

	return syms, imps, nil
}

// cHasStatic reports whether a `declaration` or `function_definition` node has
// a `storage_class_specifier` child whose keyword is `static`. In the
// tree-sitter-c grammar, a `storage_class_specifier` wraps a single token
// whose Type() is the keyword itself — `static`, `extern`, `auto`, or
// `register`.
func cHasStatic(node *sitter.Node) bool {
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		if child.Type() != "storage_class_specifier" {
			continue
		}
		for j := 0; j < int(child.ChildCount()); j++ {
			if child.Child(j).Type() == "static" {
				return true
			}
		}
	}
	return false
}

// cFuncSymbol builds a Symbol for a function prototype (`declaration` with a
// `function_declarator` descendant) or a function definition. Returns false if
// the node is not function-shaped.
func cFuncSymbol(parent *sitter.Node, idx int, node *sitter.Node, content []byte) (types.Symbol, bool) {
	decl := cFindChildByType(node, "function_declarator")
	if decl == nil {
		return types.Symbol{}, false
	}
	nameNode := cFindChildByType(decl, "identifier")
	if nameNode == nil {
		return types.Symbol{}, false
	}
	return types.Symbol{
		Name:       nameNode.Content(content),
		Kind:       types.KindFunc,
		Signature:  cFuncSignature(node, content),
		DocComment: cPrecedingComment(parent, idx, content),
		Line:       int(node.StartPoint().Row) + 1,
	}, true
}

// cVarSymbol builds a Symbol for a top-level global variable declaration. A
// declaration like `int global_var = 42;` or `int flag;` parses as a
// `declaration` with an `init_declarator` (initialized) or `identifier`
// (bare) declarator.
func cVarSymbol(parent *sitter.Node, idx int, node *sitter.Node, content []byte) (types.Symbol, bool) {
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		switch child.Type() {
		case "init_declarator":
			nameNode := cFindChildByType(child, "identifier")
			if nameNode == nil {
				continue
			}
			return types.Symbol{
				Name:       nameNode.Content(content),
				Kind:       types.KindVar,
				Signature:  cDeclSignature(node, content),
				DocComment: cPrecedingComment(parent, idx, content),
				Line:       int(node.StartPoint().Row) + 1,
			}, true
		case "identifier":
			return types.Symbol{
				Name:       child.Content(content),
				Kind:       types.KindVar,
				Signature:  cDeclSignature(node, content),
				DocComment: cPrecedingComment(parent, idx, content),
				Line:       int(node.StartPoint().Row) + 1,
			}, true
		}
	}
	return types.Symbol{}, false
}

// cTypeSymbol builds a Symbol for a `struct_specifier`, `union_specifier`, or
// `enum_specifier`. The name comes from the `type_identifier` child. Returns
// false for anonymous declarations.
func cTypeSymbol(parent *sitter.Node, idx int, node *sitter.Node, content []byte, kind types.SymbolKind) (types.Symbol, bool) {
	nameNode := cFindChildByType(node, "type_identifier")
	if nameNode == nil {
		return types.Symbol{}, false
	}
	return types.Symbol{
		Name:       nameNode.Content(content),
		Kind:       kind,
		Signature:  cDeclSignature(node, content),
		DocComment: cPrecedingComment(parent, idx, content),
		Line:       int(node.StartPoint().Row) + 1,
	}, true
}

// cTypedefSymbol builds a Symbol for a `type_definition` node. The new type
// name is the trailing `type_identifier` child (for `typedef struct X { ... }
// Y;`, the trailing ID is `Y`; for `typedef int MyInt;`, the trailing ID is
// `MyInt`).
func cTypedefSymbol(parent *sitter.Node, idx int, node *sitter.Node, content []byte) (types.Symbol, bool) {
	var nameNode *sitter.Node
	// Walk children in reverse and pick the last `type_identifier` — this is
	// the new type alias name. An earlier `type_identifier` (e.g. the inner
	// struct tag for `typedef struct X { ... } Y;`) is NOT the alias name.
	for i := int(node.ChildCount()) - 1; i >= 0; i-- {
		child := node.Child(i)
		if child.Type() == "type_identifier" {
			nameNode = child
			break
		}
	}
	if nameNode == nil {
		return types.Symbol{}, false
	}
	return types.Symbol{
		Name:       nameNode.Content(content),
		Kind:       types.KindType,
		Signature:  cDeclSignature(node, content),
		DocComment: cPrecedingComment(parent, idx, content),
		Line:       int(node.StartPoint().Row) + 1,
	}, true
}

// cFindChildByType returns the first direct child of the given node whose
// Type() equals the target string, or nil.
func cFindChildByType(node *sitter.Node, target string) *sitter.Node {
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		if child.Type() == target {
			return child
		}
	}
	return nil
}

// cFuncSignature returns the one-line function signature: the node's source
// sliced to the opening `{` or `;`, trimmed.
func cFuncSignature(node *sitter.Node, content []byte) string {
	src := node.Content(content)
	if idx := strings.IndexAny(src, "{;"); idx >= 0 {
		return strings.TrimSpace(src[:idx])
	}
	return strings.TrimSpace(src)
}

// cDeclSignature returns the declaration's source text, trimmed. Used for
// struct/union/enum/typedef/variable declarations where the entire declaration
// text is reasonable as the signature.
func cDeclSignature(node *sitter.Node, content []byte) string {
	return strings.TrimSpace(node.Content(content))
}

// cPrecedingComment returns the `/** ... */` or `/* ... */` block comment
// immediately preceding a declaration (at sibling index idx in parent) with
// its markers stripped. Returns "" if the previous non-trivial sibling is not
// a comment node.
func cPrecedingComment(parent *sitter.Node, idx int, content []byte) string {
	for j := idx - 1; j >= 0; j-- {
		prev := parent.Child(j)
		// Skip stray semicolon tokens that can appear as root children after
		// `struct X { ... };` etc.
		if prev.Type() == ";" {
			continue
		}
		if prev.Type() != "comment" {
			return ""
		}
		c := prev.Content(content)
		if !strings.HasPrefix(c, "/*") {
			return ""
		}
		c = strings.TrimPrefix(c, "/**")
		c = strings.TrimPrefix(c, "/*")
		c = strings.TrimSuffix(c, "*/")
		return strings.TrimSpace(c)
	}
	return ""
}

// cExtractInclude parses a `#include` directive. The `path` field child is
// either a `system_lib_string` (content `<stdio.h>`) or a `string_literal`
// (content `"mylib.h"`). The enclosing delimiters are stripped so downstream
// code sees just the header name.
func cExtractInclude(node *sitter.Node, content []byte) (types.Import, bool) {
	pathNode := node.ChildByFieldName("path")
	if pathNode == nil {
		return types.Import{}, false
	}
	raw := pathNode.Content(content)
	raw = strings.TrimPrefix(raw, "<")
	raw = strings.TrimSuffix(raw, ">")
	raw = strings.TrimPrefix(raw, `"`)
	raw = strings.TrimSuffix(raw, `"`)
	return types.Import{Path: raw}, true
}
