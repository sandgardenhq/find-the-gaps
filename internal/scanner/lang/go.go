package lang

import (
	"context"
	"strings"
	"unicode"
	"unicode/utf8"

	sitter "github.com/smacker/go-tree-sitter"
	golang "github.com/smacker/go-tree-sitter/golang"

	"github.com/sandgardenhq/find-the-gaps/internal/scanner/types"
)

// GoExtractor extracts exported symbols and imports from Go source files
// using go-tree-sitter for accurate AST-based parsing.
type GoExtractor struct{}

func (e *GoExtractor) Language() string     { return "Go" }
func (e *GoExtractor) Extensions() []string { return []string{".go"} }

// Extract parses Go source and returns exported top-level declarations and all imports.
func (e *GoExtractor) Extract(_ string, content []byte) ([]types.Symbol, []types.Import, error) {
	parser := sitter.NewParser()
	parser.SetLanguage(golang.GetLanguage())
	tree, err := parser.ParseCtx(context.Background(), nil, content)
	if err != nil {
		return nil, nil, err
	}
	defer tree.Close()

	root := tree.RootNode()
	var symbols []types.Symbol
	var imports []types.Import

	n := int(root.ChildCount())
	for i := 0; i < n; i++ {
		node := root.Child(i)
		switch node.Type() {
		case "function_declaration", "method_declaration":
			nameNode := node.ChildByFieldName("name")
			if nameNode == nil || !goIsExported(nameNode.Content(content)) {
				continue
			}
			symbols = append(symbols, types.Symbol{
				Name:       nameNode.Content(content),
				Kind:       types.KindFunc,
				Signature:  goFuncSig(node, content),
				DocComment: goPrecedingComment(root, i, content),
				Line:       int(node.StartPoint().Row) + 1,
			})

		case "type_declaration":
			for j := 0; j < int(node.ChildCount()); j++ {
				spec := node.Child(j)
				if spec.Type() != "type_spec" {
					continue
				}
				nameNode := spec.ChildByFieldName("name")
				if nameNode == nil || !goIsExported(nameNode.Content(content)) {
					continue
				}
				typeNode := spec.ChildByFieldName("type")
				sig := "type " + nameNode.Content(content)
				if typeNode != nil {
					sig += " " + typeNode.Type()
				}
				symbols = append(symbols, types.Symbol{
					Name:       nameNode.Content(content),
					Kind:       types.KindType,
					Signature:  sig,
					DocComment: goPrecedingComment(root, i, content),
					Line:       int(spec.StartPoint().Row) + 1,
				})
			}

		case "const_declaration":
			for j := 0; j < int(node.ChildCount()); j++ {
				spec := node.Child(j)
				if spec.Type() != "const_spec" {
					continue
				}
				nameNode := spec.ChildByFieldName("name")
				if nameNode == nil || !goIsExported(nameNode.Content(content)) {
					continue
				}
				symbols = append(symbols, types.Symbol{
					Name:       nameNode.Content(content),
					Kind:       types.KindConst,
					Signature:  "const " + nameNode.Content(content),
					DocComment: goPrecedingComment(root, i, content),
					Line:       int(spec.StartPoint().Row) + 1,
				})
			}

		case "var_declaration":
			for j := 0; j < int(node.ChildCount()); j++ {
				child := node.Child(j)
				switch child.Type() {
				case "var_spec":
					// Single var declaration: var ErrFoo = ...
					nameNode := child.ChildByFieldName("name")
					if nameNode == nil || !goIsExported(nameNode.Content(content)) {
						continue
					}
					symbols = append(symbols, types.Symbol{
						Name:       nameNode.Content(content),
						Kind:       types.KindVar,
						Signature:  "var " + nameNode.Content(content),
						DocComment: goPrecedingComment(root, i, content),
						Line:       int(child.StartPoint().Row) + 1,
					})
				case "var_spec_list":
					// Grouped var block: var ( spec1; spec2; ... )
					for k := 0; k < int(child.ChildCount()); k++ {
						spec := child.Child(k)
						if spec.Type() != "var_spec" {
							continue
						}
						nameNode := spec.ChildByFieldName("name")
						if nameNode == nil || !goIsExported(nameNode.Content(content)) {
							continue
						}
						symbols = append(symbols, types.Symbol{
							Name:       nameNode.Content(content),
							Kind:       types.KindVar,
							Signature:  "var " + nameNode.Content(content),
							DocComment: goPrecedingComment(root, i, content),
							Line:       int(spec.StartPoint().Row) + 1,
						})
					}
				}
			}

		case "import_declaration":
			imports = append(imports, goExtractImports(node, content)...)
		}
	}

	return symbols, imports, nil
}

// goExtractImports handles both grouped import blocks and single-line imports.
// A grouped block has an import_spec_list child; a single-line import has an
// import_spec directly under the import_declaration.
func goExtractImports(importDecl *sitter.Node, content []byte) []types.Import {
	var result []types.Import
	for j := 0; j < int(importDecl.ChildCount()); j++ {
		child := importDecl.Child(j)
		switch child.Type() {
		case "import_spec":
			if imp, ok := goParseImportSpec(child, content); ok {
				result = append(result, imp)
			}
		case "import_spec_list":
			// Grouped import block: ( spec1; spec2; ... )
			for k := 0; k < int(child.ChildCount()); k++ {
				spec := child.Child(k)
				if spec.Type() == "import_spec" {
					if imp, ok := goParseImportSpec(spec, content); ok {
						result = append(result, imp)
					}
				}
			}
		}
	}
	return result
}

func goParseImportSpec(spec *sitter.Node, content []byte) (types.Import, bool) {
	pathNode := spec.ChildByFieldName("path")
	if pathNode == nil {
		return types.Import{}, false
	}
	imp := types.Import{
		Path: strings.Trim(pathNode.Content(content), `"`),
	}
	aliasNode := spec.ChildByFieldName("name")
	if aliasNode != nil {
		alias := aliasNode.Content(content)
		if alias != "." && alias != "_" {
			imp.Alias = alias
		}
	}
	return imp, true
}

func goIsExported(name string) bool {
	r, _ := utf8.DecodeRuneInString(name)
	return unicode.IsUpper(r)
}

func goPrecedingComment(parent *sitter.Node, childIdx int, content []byte) string {
	if childIdx == 0 {
		return ""
	}
	prev := parent.Child(childIdx - 1)
	if prev == nil || prev.Type() != "comment" {
		return ""
	}
	c := prev.Content(content)
	c = strings.TrimPrefix(c, "//")
	return strings.TrimSpace(c)
}

func goFuncSig(node *sitter.Node, content []byte) string {
	name := node.ChildByFieldName("name")
	params := node.ChildByFieldName("parameters")
	result := node.ChildByFieldName("result")

	recv := node.ChildByFieldName("receiver")
	sig := "func "
	if recv != nil {
		sig += recv.Content(content) + " "
	}
	if name != nil {
		sig += name.Content(content)
	}
	if params != nil {
		sig += params.Content(content)
	}
	if result != nil {
		sig += " " + result.Content(content)
	}
	return sig
}
