package lang

import (
	"context"
	"path/filepath"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/javascript"
	"github.com/smacker/go-tree-sitter/typescript/typescript"

	"github.com/sandgardenhq/find-the-gaps/internal/scanner/types"
)

// TypeScriptExtractor extracts exported symbols and imports from TypeScript and JavaScript source files.
type TypeScriptExtractor struct{}

func (e *TypeScriptExtractor) Language() string { return "TypeScript" }
func (e *TypeScriptExtractor) Extensions() []string {
	return []string{".ts", ".tsx", ".js", ".jsx", ".mjs"}
}

func (e *TypeScriptExtractor) Extract(path string, content []byte) ([]types.Symbol, []types.Import, error) {
	syms := []types.Symbol{}
	imps := []types.Import{}

	parser := sitter.NewParser()
	defer parser.Close()

	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".js", ".jsx", ".mjs":
		parser.SetLanguage(javascript.GetLanguage())
	default:
		parser.SetLanguage(typescript.GetLanguage())
	}

	tree, err := parser.ParseCtx(context.Background(), nil, content)
	if err != nil {
		return syms, imps, err
	}
	defer tree.Close()

	root := tree.RootNode()
	for i := 0; i < int(root.ChildCount()); i++ {
		node := root.Child(i)
		switch node.Type() {
		case "export_statement":
			if sym, ok := tsExtractExport(node, content); ok {
				syms = append(syms, sym)
			}
		case "import_statement":
			imps = append(imps, tsExtractImports(node, content)...)
		}
	}

	return syms, imps, nil
}

func tsExtractExport(node *sitter.Node, content []byte) (types.Symbol, bool) {
	decl := node.ChildByFieldName("declaration")
	if decl == nil {
		return types.Symbol{}, false
	}

	var name string
	var kind types.SymbolKind

	switch decl.Type() {
	case "function_declaration", "function":
		nameNode := decl.ChildByFieldName("name")
		if nameNode == nil {
			return types.Symbol{}, false
		}
		name = nameNode.Content(content)
		kind = types.KindFunc
	case "class_declaration", "class":
		nameNode := decl.ChildByFieldName("name")
		if nameNode == nil {
			return types.Symbol{}, false
		}
		name = nameNode.Content(content)
		kind = types.KindClass
	case "lexical_declaration":
		// const/let — grab first declarator's name
		for j := 0; j < int(decl.ChildCount()); j++ {
			child := decl.Child(j)
			if child.Type() == "variable_declarator" {
				nameNode := child.ChildByFieldName("name")
				if nameNode == nil {
					return types.Symbol{}, false
				}
				name = nameNode.Content(content)
				kind = types.KindConst
				break
			}
		}
		if name == "" {
			return types.Symbol{}, false
		}
	case "interface_declaration":
		nameNode := decl.ChildByFieldName("name")
		if nameNode == nil {
			return types.Symbol{}, false
		}
		name = nameNode.Content(content)
		kind = types.KindInterface
	case "type_alias_declaration":
		nameNode := decl.ChildByFieldName("name")
		if nameNode == nil {
			return types.Symbol{}, false
		}
		name = nameNode.Content(content)
		kind = types.KindType
	default:
		return types.Symbol{}, false
	}

	line := int(decl.StartPoint().Row) + 1
	sig := tsDeclSignature(decl, content)
	doc := tsPrecedingComment(node, content)

	return types.Symbol{
		Name:       name,
		Kind:       kind,
		Signature:  sig,
		DocComment: doc,
		Line:       line,
	}, true
}

// tsDeclSignature returns a one-line signature for a declaration node.
func tsDeclSignature(node *sitter.Node, content []byte) string {
	src := node.Content(content)
	// Trim at first newline or opening brace to get the header line.
	if idx := strings.IndexAny(src, "{\n"); idx >= 0 {
		return strings.TrimSpace(src[:idx])
	}
	return strings.TrimSpace(src)
}

// tsPrecedingComment returns the JSDoc comment immediately before an export_statement.
func tsPrecedingComment(node *sitter.Node, content []byte) string {
	parent := node.Parent()
	if parent == nil {
		return ""
	}
	// Find our index among siblings.
	for i := 0; i < int(parent.ChildCount()); i++ {
		if parent.Child(i) == node && i > 0 {
			prev := parent.Child(i - 1)
			if prev.Type() == "comment" {
				c := prev.Content(content)
				if strings.HasPrefix(c, "/**") {
					c = strings.TrimPrefix(c, "/**")
					c = strings.TrimSuffix(c, "*/")
					return strings.TrimSpace(c)
				}
			}
		}
	}
	return ""
}

// tsExtractImports extracts Import entries from an import_statement node.
func tsExtractImports(node *sitter.Node, content []byte) []types.Import {
	var imps []types.Import

	sourceNode := node.ChildByFieldName("source")
	if sourceNode == nil {
		return imps
	}
	// Source is a string node with quotes — strip them.
	path := strings.Trim(sourceNode.Content(content), `"'`)

	// Find import_clause child.
	var clause *sitter.Node
	for i := 0; i < int(node.ChildCount()); i++ {
		if node.Child(i).Type() == "import_clause" {
			clause = node.Child(i)
			break
		}
	}

	if clause == nil {
		// Side-effect import: import './polyfills'
		imps = append(imps, types.Import{Path: path})
		return imps
	}

	for i := 0; i < int(clause.ChildCount()); i++ {
		child := clause.Child(i)
		switch child.Type() {
		case "identifier":
			// Default import: import Foo from 'x'
			imps = append(imps, types.Import{Path: path})
		case "namespace_import":
			// import * as Foo from 'x'
			var alias string
			for j := 0; j < int(child.ChildCount()); j++ {
				c := child.Child(j)
				if c.Type() == "identifier" {
					alias = c.Content(content)
				}
			}
			imps = append(imps, types.Import{Path: path, Alias: alias})
		case "named_imports":
			// import { X, Y as Z } from 'x'
			for j := 0; j < int(child.ChildCount()); j++ {
				spec := child.Child(j)
				if spec.Type() != "import_specifier" {
					continue
				}
				nameNode := spec.ChildByFieldName("name")
				aliasNode := spec.ChildByFieldName("alias")
				if nameNode == nil {
					continue
				}
				imp := types.Import{Path: path}
				if aliasNode != nil {
					imp.Alias = aliasNode.Content(content)
				}
				imps = append(imps, imp)
			}
		}
	}

	return imps
}
