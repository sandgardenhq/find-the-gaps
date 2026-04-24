package lang

import (
	"context"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/csharp"

	"github.com/sandgardenhq/find-the-gaps/internal/scanner/types"
)

// CSharpExtractor extracts public declarations and using-directives from C# source files.
type CSharpExtractor struct{}

func (e *CSharpExtractor) Language() string     { return "C#" }
func (e *CSharpExtractor) Extensions() []string { return []string{".cs"} }

// Extract parses C# source and returns declarations whose modifiers include
// the `public` keyword, plus all top-level `using` directives. Namespace
// declarations are descended into so public types nested under a namespace
// are found.
func (e *CSharpExtractor) Extract(_ string, content []byte) ([]types.Symbol, []types.Import, error) {
	syms := []types.Symbol{}
	imps := []types.Import{}

	parser := sitter.NewParser()
	defer parser.Close()
	parser.SetLanguage(csharp.GetLanguage())

	tree, err := parser.ParseCtx(context.Background(), nil, content)
	if err != nil {
		return syms, imps, err
	}
	defer tree.Close()

	root := tree.RootNode()
	for i := 0; i < int(root.ChildCount()); i++ {
		node := root.Child(i)
		switch node.Type() {
		case "using_directive":
			if imp, ok := csharpExtractUsing(node, content); ok {
				imps = append(imps, imp)
			}
		case "namespace_declaration":
			syms = append(syms, csharpWalkNamespace(node, content)...)
		case "class_declaration", "record_declaration", "struct_declaration":
			syms = append(syms, csharpWalkType(root, i, node, content, types.KindClass)...)
		case "interface_declaration":
			syms = append(syms, csharpWalkType(root, i, node, content, types.KindInterface)...)
		case "enum_declaration":
			syms = append(syms, csharpWalkType(root, i, node, content, types.KindType)...)
		}
	}

	return syms, imps, nil
}

// csharpWalkNamespace descends into a namespace_declaration's declaration_list
// body, handling the same set of top-level kinds Extract does.
func csharpWalkNamespace(ns *sitter.Node, content []byte) []types.Symbol {
	var out []types.Symbol
	body := ns.ChildByFieldName("body")
	if body == nil {
		return out
	}
	for i := 0; i < int(body.ChildCount()); i++ {
		node := body.Child(i)
		switch node.Type() {
		case "namespace_declaration":
			out = append(out, csharpWalkNamespace(node, content)...)
		case "class_declaration", "record_declaration", "struct_declaration":
			out = append(out, csharpWalkType(body, i, node, content, types.KindClass)...)
		case "interface_declaration":
			out = append(out, csharpWalkType(body, i, node, content, types.KindInterface)...)
		case "enum_declaration":
			out = append(out, csharpWalkType(body, i, node, content, types.KindType)...)
		}
	}
	return out
}

// csharpWalkType emits a symbol for the type declaration itself (if `public`)
// and recurses into its body to emit public method members.
func csharpWalkType(parent *sitter.Node, idx int, node *sitter.Node, content []byte, kind types.SymbolKind) []types.Symbol {
	var out []types.Symbol

	if csharpHasPublic(node) {
		nameNode := node.ChildByFieldName("name")
		if nameNode != nil {
			out = append(out, types.Symbol{
				Name:       nameNode.Content(content),
				Kind:       kind,
				Signature:  csharpDeclSignature(node, content),
				DocComment: csharpPrecedingComment(parent, idx, content),
				Line:       int(node.StartPoint().Row) + 1,
			})
		}
	}

	body := node.ChildByFieldName("body")
	if body != nil {
		out = append(out, csharpWalkMembers(body, content)...)
	}
	return out
}

// csharpWalkMembers scans a class/struct/record/interface body for public
// method_declaration children.
func csharpWalkMembers(body *sitter.Node, content []byte) []types.Symbol {
	var out []types.Symbol
	for j := 0; j < int(body.ChildCount()); j++ {
		member := body.Child(j)
		if member.Type() != "method_declaration" {
			continue
		}
		if !csharpHasPublic(member) {
			continue
		}
		nameNode := member.ChildByFieldName("name")
		if nameNode == nil {
			continue
		}
		out = append(out, types.Symbol{
			Name:       nameNode.Content(content),
			Kind:       types.KindFunc,
			Signature:  csharpDeclSignature(member, content),
			DocComment: csharpPrecedingComment(body, j, content),
			Line:       int(member.StartPoint().Row) + 1,
		})
	}
	return out
}

// csharpHasPublic returns true iff the declaration has a `modifier` child
// whose text is `public`. C# defaults to `internal`, so absence of a modifier
// means not public.
func csharpHasPublic(node *sitter.Node) bool {
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		if child.Type() != "modifier" {
			continue
		}
		// Check named child: `public` / `private` / etc. appear as named children
		// of `modifier`.
		for j := 0; j < int(child.ChildCount()); j++ {
			if child.Child(j).Type() == "public" {
				return true
			}
		}
	}
	return false
}

// csharpDeclSignature returns a one-line header for the declaration — everything
// up to the opening `{` or `;`, trimmed.
func csharpDeclSignature(node *sitter.Node, content []byte) string {
	src := node.Content(content)
	if idx := strings.IndexAny(src, "{;"); idx >= 0 {
		return strings.TrimSpace(src[:idx])
	}
	return strings.TrimSpace(src)
}

// csharpPrecedingComment collects consecutive `///`-prefixed comment siblings
// immediately preceding a declaration, strips the `///` prefix from each, and
// joins them with newlines. Returns "" if no such block exists.
func csharpPrecedingComment(parent *sitter.Node, idx int, content []byte) string {
	var lines []string
	for j := idx - 1; j >= 0; j-- {
		prev := parent.Child(j)
		// Skip structural punctuation tokens that appear as anonymous siblings
		// inside declaration_list / enum_member_declaration_list bodies.
		if prev.Type() == "{" {
			continue
		}
		if prev.Type() != "comment" {
			break
		}
		c := prev.Content(content)
		if !strings.HasPrefix(c, "///") {
			break
		}
		// Collecting in reverse order; prepend so final slice is in source order.
		stripped := strings.TrimSpace(strings.TrimPrefix(c, "///"))
		lines = append([]string{stripped}, lines...)
	}
	return strings.Join(lines, "\n")
}

// csharpExtractUsing parses a `using X;` or `using A = X.Y;` directive.
// Plain form: one qualified_name (or identifier) child — path is its text.
// Aliased form: identifier (alias) + qualified_name (path), connected by `=`.
func csharpExtractUsing(node *sitter.Node, content []byte) (types.Import, bool) {
	// Detect aliased form by presence of the `name` field or a literal `=` token.
	var alias string
	aliasNode := node.ChildByFieldName("name")
	if aliasNode != nil {
		alias = aliasNode.Content(content)
	}

	// Find the path: prefer the `qualified_name` child; fall back to a lone
	// `identifier` child when no aliased name field is present.
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		switch child.Type() {
		case "qualified_name":
			return types.Import{Path: child.Content(content), Alias: alias}, true
		case "identifier":
			// Skip the alias identifier itself — it's reachable via the `name`
			// field. A bare identifier path (e.g. `using System;`) appears when
			// no `name` field is set.
			if aliasNode != nil && child == aliasNode {
				continue
			}
			return types.Import{Path: child.Content(content), Alias: alias}, true
		}
	}
	return types.Import{}, false
}
