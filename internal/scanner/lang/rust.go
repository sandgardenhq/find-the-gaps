package lang

import (
	"context"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/rust"

	"github.com/sandgardenhq/find-the-gaps/internal/scanner"
)

// RustExtractor extracts exported symbols and imports from Rust source files
// using go-tree-sitter for accurate AST-based parsing.
type RustExtractor struct{}

func (e *RustExtractor) Language() string     { return "Rust" }
func (e *RustExtractor) Extensions() []string { return []string{".rs"} }

// Extract parses Rust source and returns pub top-level declarations and all use declarations.
// Only items with a visibility_modifier of "pub" at the top level are returned.
// Methods inside impl blocks are excluded.
func (e *RustExtractor) Extract(_ string, content []byte) ([]scanner.Symbol, []scanner.Import, error) {
	parser := sitter.NewParser()
	parser.SetLanguage(rust.GetLanguage())
	tree, err := parser.ParseCtx(context.Background(), nil, content)
	if err != nil {
		return nil, nil, err
	}
	defer tree.Close()

	root := tree.RootNode()
	symbols := []scanner.Symbol{}
	imports := []scanner.Import{}

	n := int(root.ChildCount())
	for i := 0; i < n; i++ {
		node := root.Child(i)
		switch node.Type() {
		case "function_item":
			if !rustIsPub(node, content) {
				continue
			}
			nameNode := node.ChildByFieldName("name")
			if nameNode == nil {
				continue
			}
			symbols = append(symbols, scanner.Symbol{
				Name:       nameNode.Content(content),
				Kind:       scanner.KindFunc,
				Signature:  rustFuncSig(node, content),
				DocComment: rustPrecedingDocComment(root, i, content),
				Line:       int(node.StartPoint().Row) + 1,
			})

		case "struct_item":
			if !rustIsPub(node, content) {
				continue
			}
			nameNode := node.ChildByFieldName("name")
			if nameNode == nil {
				continue
			}
			symbols = append(symbols, scanner.Symbol{
				Name:       nameNode.Content(content),
				Kind:       scanner.KindType,
				Signature:  "pub struct " + nameNode.Content(content),
				DocComment: rustPrecedingDocComment(root, i, content),
				Line:       int(node.StartPoint().Row) + 1,
			})

		case "enum_item":
			if !rustIsPub(node, content) {
				continue
			}
			nameNode := node.ChildByFieldName("name")
			if nameNode == nil {
				continue
			}
			symbols = append(symbols, scanner.Symbol{
				Name:       nameNode.Content(content),
				Kind:       scanner.KindType,
				Signature:  "pub enum " + nameNode.Content(content),
				DocComment: rustPrecedingDocComment(root, i, content),
				Line:       int(node.StartPoint().Row) + 1,
			})

		case "trait_item":
			if !rustIsPub(node, content) {
				continue
			}
			nameNode := node.ChildByFieldName("name")
			if nameNode == nil {
				continue
			}
			symbols = append(symbols, scanner.Symbol{
				Name:       nameNode.Content(content),
				Kind:       scanner.KindInterface,
				Signature:  "pub trait " + nameNode.Content(content),
				DocComment: rustPrecedingDocComment(root, i, content),
				Line:       int(node.StartPoint().Row) + 1,
			})

		case "const_item":
			if !rustIsPub(node, content) {
				continue
			}
			nameNode := node.ChildByFieldName("name")
			if nameNode == nil {
				continue
			}
			symbols = append(symbols, scanner.Symbol{
				Name:       nameNode.Content(content),
				Kind:       scanner.KindConst,
				Signature:  "pub const " + nameNode.Content(content),
				DocComment: rustPrecedingDocComment(root, i, content),
				Line:       int(node.StartPoint().Row) + 1,
			})

		case "use_declaration":
			if imp, ok := rustParseUseDecl(node, content); ok {
				imports = append(imports, imp)
			}
		}
	}

	return symbols, imports, nil
}

// rustIsPub reports whether a node has a direct visibility_modifier child with text "pub".
func rustIsPub(node *sitter.Node, content []byte) bool {
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		if child.Type() == "visibility_modifier" && child.Content(content) == "pub" {
			return true
		}
	}
	return false
}

// rustFuncSig returns the one-line signature for a function_item.
func rustFuncSig(node *sitter.Node, content []byte) string {
	src := node.Content(content)
	// Trim at the first '{' or newline to get just the header.
	if idx := strings.IndexAny(src, "{\n"); idx >= 0 {
		return strings.TrimSpace(src[:idx])
	}
	return strings.TrimSpace(src)
}

// rustPrecedingDocComment looks for /// line_comment nodes immediately before the declaration
// at childIdx in parent, and concatenates them.
func rustPrecedingDocComment(parent *sitter.Node, childIdx int, content []byte) string {
	if childIdx == 0 {
		return ""
	}
	// Collect consecutive /// comments immediately before childIdx.
	var lines []string
	for j := childIdx - 1; j >= 0; j-- {
		prev := parent.Child(j)
		if prev == nil {
			break
		}
		if prev.Type() != "line_comment" {
			break
		}
		text := prev.Content(content)
		if !strings.HasPrefix(text, "///") {
			break
		}
		// Strip the /// prefix and optional leading space.
		text = strings.TrimPrefix(text, "///")
		text = strings.TrimPrefix(text, " ")
		lines = append([]string{text}, lines...) // prepend to preserve order
	}
	return strings.TrimSpace(strings.Join(lines, " "))
}

// rustParseUseDecl extracts a path and optional alias from a use_declaration node.
//
// The Rust tree-sitter grammar produces two shapes for use declarations:
//
//	use std::io;                           → use_declaration > scoped_identifier
//	use std::collections::HashMap as Map; → use_declaration > use_as_clause
//	                                              > scoped_identifier + "as" + identifier
func rustParseUseDecl(node *sitter.Node, content []byte) (scanner.Import, bool) {
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		switch child.Type() {
		case "scoped_identifier", "identifier":
			// Plain use: `use std::io;`
			path := child.Content(content)
			return scanner.Import{Path: path}, true

		case "use_as_clause":
			// Aliased use: `use std::collections::HashMap as Map;`
			// Children: scoped_identifier, "as", identifier
			var path, alias string
			for j := 0; j < int(child.ChildCount()); j++ {
				gc := child.Child(j)
				switch gc.Type() {
				case "scoped_identifier", "identifier":
					if path == "" {
						path = gc.Content(content)
					} else {
						alias = gc.Content(content)
					}
				}
			}
			if path == "" {
				return scanner.Import{}, false
			}
			return scanner.Import{Path: path, Alias: alias}, true
		}
	}
	return scanner.Import{}, false
}
