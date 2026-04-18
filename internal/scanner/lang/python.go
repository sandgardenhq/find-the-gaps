package lang

import (
	"context"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
	python "github.com/smacker/go-tree-sitter/python"

	"github.com/sandgardenhq/find-the-gaps/internal/scanner"
)

// PythonExtractor extracts public symbols and imports from Python source files
// using go-tree-sitter for accurate AST-based parsing.
type PythonExtractor struct{}

func (e *PythonExtractor) Language() string     { return "Python" }
func (e *PythonExtractor) Extensions() []string { return []string{".py", ".pyw"} }

// Extract parses Python source and returns public module-level declarations and all imports.
// Public means the name does NOT start with '_'.
func (e *PythonExtractor) Extract(_ string, content []byte) ([]scanner.Symbol, []scanner.Import, error) {
	parser := sitter.NewParser()
	parser.SetLanguage(python.GetLanguage())
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
		case "function_definition":
			nameNode := node.ChildByFieldName("name")
			if nameNode == nil {
				continue
			}
			name := nameNode.Content(content)
			if strings.HasPrefix(name, "_") {
				continue
			}
			params := node.ChildByFieldName("parameters")
			sig := "def " + name
			if params != nil {
				sig += params.Content(content)
			}
			symbols = append(symbols, scanner.Symbol{
				Name:       name,
				Kind:       scanner.KindFunc,
				Signature:  sig,
				DocComment: pyDocstring(node, content),
				Line:       int(node.StartPoint().Row) + 1,
			})

		case "class_definition":
			nameNode := node.ChildByFieldName("name")
			if nameNode == nil {
				continue
			}
			name := nameNode.Content(content)
			if strings.HasPrefix(name, "_") {
				continue
			}
			symbols = append(symbols, scanner.Symbol{
				Name:       name,
				Kind:       scanner.KindClass,
				Signature:  "class " + name,
				DocComment: pyDocstring(node, content),
				Line:       int(node.StartPoint().Row) + 1,
			})

		case "import_statement":
			// import os  /  import sys as system  /  import a, b
			for j := 0; j < int(node.ChildCount()); j++ {
				child := node.Child(j)
				switch child.Type() {
				case "dotted_name":
					imports = append(imports, scanner.Import{Path: child.Content(content)})
				case "aliased_import":
					nameN := child.ChildByFieldName("name")
					aliasN := child.ChildByFieldName("alias")
					if nameN != nil {
						imp := scanner.Import{Path: nameN.Content(content)}
						if aliasN != nil {
							imp.Alias = aliasN.Content(content)
						}
						imports = append(imports, imp)
					}
				}
			}

		case "import_from_statement":
			// from pathlib import Path  /  from os.path import join, dirname
			moduleNode := node.ChildByFieldName("module_name")
			if moduleNode != nil {
				imports = append(imports, scanner.Import{Path: moduleNode.Content(content)})
			}
		}
	}

	return symbols, imports, nil
}

// pyDocstring extracts the docstring from the first statement of a function or class body.
// Python docstrings are string literals that appear as the very first statement.
func pyDocstring(node *sitter.Node, content []byte) string {
	body := node.ChildByFieldName("body")
	if body == nil || body.ChildCount() == 0 {
		return ""
	}
	// Only inspect the first child — docstrings must be the very first statement.
	first := body.Child(0)
	if first.Type() != "expression_statement" {
		return ""
	}
	for j := 0; j < int(first.ChildCount()); j++ {
		s := first.Child(j)
		if s.Type() == "string" {
			raw := s.Content(content)
			// Strip triple-quote delimiters first, then single-quote ones.
			raw = strings.TrimPrefix(raw, `"""`)
			raw = strings.TrimSuffix(raw, `"""`)
			raw = strings.TrimPrefix(raw, `'''`)
			raw = strings.TrimSuffix(raw, `'''`)
			raw = strings.TrimPrefix(raw, `"`)
			raw = strings.TrimSuffix(raw, `"`)
			raw = strings.TrimPrefix(raw, `'`)
			raw = strings.TrimSuffix(raw, `'`)
			return strings.TrimSpace(raw)
		}
	}
	return ""
}
