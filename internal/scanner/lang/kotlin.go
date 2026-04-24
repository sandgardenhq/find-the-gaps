package lang

import (
	"context"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/kotlin"

	"github.com/sandgardenhq/find-the-gaps/internal/scanner/types"
)

// KotlinExtractor extracts public declarations and imports from Kotlin source files.
//
// Kotlin is public-by-default: a declaration is emitted unless its `modifiers`
// child contains a `visibility_modifier` of `private`, `internal`, or
// `protected`. Absence of any modifier is public (opposite of Java / C#).
type KotlinExtractor struct{}

func (e *KotlinExtractor) Language() string     { return "Kotlin" }
func (e *KotlinExtractor) Extensions() []string { return []string{".kt", ".kts"} }

// Extract parses Kotlin source and returns public (default) declarations plus
// all top-level `import_header` entries (which live under an `import_list`
// parent in the tree-sitter-kotlin grammar).
func (e *KotlinExtractor) Extract(_ string, content []byte) ([]types.Symbol, []types.Import, error) {
	syms := []types.Symbol{}
	imps := []types.Import{}

	parser := sitter.NewParser()
	defer parser.Close()
	parser.SetLanguage(kotlin.GetLanguage())

	tree, err := parser.ParseCtx(context.Background(), nil, content)
	if err != nil {
		return syms, imps, err
	}
	defer tree.Close()

	root := tree.RootNode()
	for i := 0; i < int(root.ChildCount()); i++ {
		node := root.Child(i)
		switch node.Type() {
		case "import_list":
			for j := 0; j < int(node.ChildCount()); j++ {
				child := node.Child(j)
				if child.Type() != "import_header" {
					continue
				}
				if imp, ok := kotlinExtractImport(child, content); ok {
					imps = append(imps, imp)
				}
			}
		case "class_declaration", "object_declaration":
			if s, ok := kotlinExtractDecl(root, i, node, content, types.KindClass); ok {
				syms = append(syms, s)
			}
		case "function_declaration":
			if s, ok := kotlinExtractDecl(root, i, node, content, types.KindFunc); ok {
				syms = append(syms, s)
			}
		case "property_declaration":
			if s, ok := kotlinExtractDecl(root, i, node, content, types.KindVar); ok {
				syms = append(syms, s)
			}
		}
	}

	return syms, imps, nil
}

// kotlinExtractDecl emits a symbol for a top-level declaration iff its
// modifiers do NOT include `private`, `internal`, or `protected`.
func kotlinExtractDecl(parent *sitter.Node, idx int, node *sitter.Node, content []byte, kind types.SymbolKind) (types.Symbol, bool) {
	if kotlinIsNonPublic(node) {
		return types.Symbol{}, false
	}
	name, ok := kotlinDeclName(node, content)
	if !ok {
		return types.Symbol{}, false
	}
	return types.Symbol{
		Name:       name,
		Kind:       kind,
		Signature:  kotlinDeclSignature(node, content),
		DocComment: kotlinPrecedingComment(parent, idx, content),
		Line:       int(node.StartPoint().Row) + 1,
	}, true
}

// kotlinIsNonPublic returns true iff the declaration has a `modifiers` child
// containing a `visibility_modifier` whose text is `private`, `internal`, or
// `protected`. Explicit `public` and no-modifier-at-all are both public.
func kotlinIsNonPublic(node *sitter.Node) bool {
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		if child.Type() != "modifiers" {
			continue
		}
		for j := 0; j < int(child.ChildCount()); j++ {
			mod := child.Child(j)
			if mod.Type() != "visibility_modifier" {
				continue
			}
			for k := 0; k < int(mod.ChildCount()); k++ {
				switch mod.Child(k).Type() {
				case "private", "internal", "protected":
					return true
				}
			}
		}
	}
	return false
}

// kotlinDeclName finds the declared name for function/class/object/property
// declarations. The tree-sitter-kotlin grammar does not expose field names, so
// we scan children for the first identifier-bearing node. For
// property_declaration, the name is wrapped in a `variable_declaration`.
func kotlinDeclName(node *sitter.Node, content []byte) (string, bool) {
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		switch child.Type() {
		case "simple_identifier", "type_identifier":
			return child.Content(content), true
		case "variable_declaration":
			for j := 0; j < int(child.ChildCount()); j++ {
				inner := child.Child(j)
				if inner.Type() == "simple_identifier" {
					return inner.Content(content), true
				}
			}
		}
	}
	return "", false
}

// kotlinDeclSignature returns a one-line header for the declaration — the
// source sliced to the first `{`, `=`, or newline, trimmed.
func kotlinDeclSignature(node *sitter.Node, content []byte) string {
	src := node.Content(content)
	if idx := strings.IndexAny(src, "{=\n"); idx >= 0 {
		return strings.TrimSpace(src[:idx])
	}
	return strings.TrimSpace(src)
}

// kotlinPrecedingComment returns the KDoc `/** */` block immediately preceding
// a declaration (sibling idx in parent), with `/**` and `*/` markers stripped.
// Returns "" if the previous sibling is not a multiline_comment starting with
// `/**`.
func kotlinPrecedingComment(parent *sitter.Node, idx int, content []byte) string {
	if idx <= 0 {
		return ""
	}
	prev := parent.Child(idx - 1)
	if prev.Type() != "multiline_comment" {
		return ""
	}
	c := prev.Content(content)
	if !strings.HasPrefix(c, "/**") {
		return ""
	}
	c = strings.TrimPrefix(c, "/**")
	c = strings.TrimSuffix(c, "*/")
	return strings.TrimSpace(c)
}

// kotlinExtractImport parses an `import foo.Bar` or `import foo.Bar as Baz`
// header. The path is the text of the `identifier` child; when an
// `import_alias` child is present, its identifier text is the alias.
func kotlinExtractImport(node *sitter.Node, content []byte) (types.Import, bool) {
	var path, alias string
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		switch child.Type() {
		case "identifier":
			path = child.Content(content)
		case "import_alias":
			for j := 0; j < int(child.ChildCount()); j++ {
				inner := child.Child(j)
				switch inner.Type() {
				case "type_identifier", "simple_identifier", "identifier":
					alias = inner.Content(content)
				}
			}
		}
	}
	if path == "" {
		return types.Import{}, false
	}
	return types.Import{Path: path, Alias: alias}, true
}
