package lang

import (
	"context"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/swift"

	"github.com/sandgardenhq/find-the-gaps/internal/scanner/types"
)

// SwiftExtractor extracts public / open declarations and imports from Swift source files.
//
// Swift exportedness: a declaration is emitted iff a modifier of `public` or
// `open` is present. Absence of a modifier is `internal` (Swift's default) —
// along with an explicit `internal`, `fileprivate`, or `private`, all skipped.
type SwiftExtractor struct{}

func (e *SwiftExtractor) Language() string     { return "Swift" }
func (e *SwiftExtractor) Extensions() []string { return []string{".swift"} }

// Extract parses Swift source and returns public/open declarations plus all
// top-level `import_declaration` entries.
//
// The tree-sitter-swift grammar exposes all class / struct / enum shapes as
// `class_declaration` (the keyword — `class`, `struct`, or `enum` — appears as
// a child token). Protocols are `protocol_declaration`. Functions are
// `function_declaration`. Name field lookup (`ChildByFieldName("name")`) works
// for functions and returns the `simple_identifier`; for class/struct/enum/
// protocol, name is a direct `type_identifier` child.
func (e *SwiftExtractor) Extract(_ string, content []byte) ([]types.Symbol, []types.Import, error) {
	syms := []types.Symbol{}
	imps := []types.Import{}

	parser := sitter.NewParser()
	defer parser.Close()
	parser.SetLanguage(swift.GetLanguage())

	tree, err := parser.ParseCtx(context.Background(), nil, content)
	if err != nil {
		return syms, imps, err
	}
	defer tree.Close()

	root := tree.RootNode()
	for i := 0; i < int(root.ChildCount()); i++ {
		node := root.Child(i)
		switch node.Type() {
		case "import_declaration":
			if imp, ok := swiftExtractImport(node, content); ok {
				imps = append(imps, imp)
			}
		case "class_declaration":
			// class/struct/enum all land here; all map to KindClass per plan.
			if s, ok := swiftExtractDecl(root, i, node, content, types.KindClass); ok {
				syms = append(syms, s)
			}
		case "protocol_declaration":
			if s, ok := swiftExtractDecl(root, i, node, content, types.KindInterface); ok {
				syms = append(syms, s)
			}
		case "function_declaration":
			if s, ok := swiftExtractDecl(root, i, node, content, types.KindFunc); ok {
				syms = append(syms, s)
			}
		case "property_declaration":
			if s, ok := swiftExtractDecl(root, i, node, content, types.KindVar); ok {
				syms = append(syms, s)
			}
		}
	}

	return syms, imps, nil
}

// swiftExtractDecl emits a symbol for a top-level declaration iff its
// modifiers include `public` or `open`.
func swiftExtractDecl(parent *sitter.Node, idx int, node *sitter.Node, content []byte, kind types.SymbolKind) (types.Symbol, bool) {
	if !swiftIsPublicOrOpen(node) {
		return types.Symbol{}, false
	}
	name, ok := swiftDeclName(node, content)
	if !ok {
		return types.Symbol{}, false
	}
	return types.Symbol{
		Name:       name,
		Kind:       kind,
		Signature:  swiftDeclSignature(node, content),
		DocComment: swiftPrecedingComment(parent, idx, content),
		Line:       int(node.StartPoint().Row) + 1,
	}, true
}

// swiftIsPublicOrOpen returns true iff the declaration has a `modifiers` child
// containing a `visibility_modifier` whose inner child's type is `public` or
// `open`. Absence of any modifier (Swift's default `internal`) returns false,
// as do explicit `internal`, `fileprivate`, and `private`.
func swiftIsPublicOrOpen(node *sitter.Node) bool {
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
				case "public", "open":
					return true
				}
			}
		}
	}
	return false
}

// swiftDeclName finds the declared name for function/class/struct/enum/
// protocol/property declarations. The tree-sitter-swift grammar exposes a
// `name` field for functions pointing to `simple_identifier`; for
// class/protocol declarations, the name is a direct `type_identifier` child;
// for properties, the name lives under a `pattern` child whose
// `bound_identifier` field holds a `simple_identifier`.
func swiftDeclName(node *sitter.Node, content []byte) (string, bool) {
	n := node.ChildByFieldName("name")
	if n == nil {
		return "", false
	}
	switch n.Type() {
	case "simple_identifier", "type_identifier":
		return n.Content(content), true
	case "pattern":
		if id := n.ChildByFieldName("bound_identifier"); id != nil {
			return id.Content(content), true
		}
	}
	return "", false
}

// swiftDeclSignature returns a one-line header for the declaration — the
// source sliced to the first `{`, `=`, or newline, trimmed.
func swiftDeclSignature(node *sitter.Node, content []byte) string {
	src := node.Content(content)
	if idx := strings.IndexAny(src, "{=\n"); idx >= 0 {
		return strings.TrimSpace(src[:idx])
	}
	return strings.TrimSpace(src)
}

// swiftPrecedingComment returns the doc comment immediately preceding a
// declaration. Swift supports two idiomatic forms:
//
//  1. Consecutive `///` lines (each is a separate `comment` sibling). Try this
//     form first: walk backward over consecutive `comment` siblings whose text
//     starts with `///`, strip the prefix from each, and join with newlines.
//  2. Single `/** */` block, which is a `multiline_comment` sibling. Use this
//     form only if the `///` scan found nothing.
//
// Returns "" when neither form matches the immediately-preceding sibling.
func swiftPrecedingComment(parent *sitter.Node, idx int, content []byte) string {
	if idx <= 0 {
		return ""
	}
	// First pass: consecutive `///` lines.
	var lines []string
	for j := idx - 1; j >= 0; j-- {
		prev := parent.Child(j)
		if prev.Type() != "comment" {
			break
		}
		c := prev.Content(content)
		if !strings.HasPrefix(c, "///") {
			break
		}
		stripped := strings.TrimSpace(strings.TrimPrefix(c, "///"))
		lines = append([]string{stripped}, lines...)
	}
	if len(lines) > 0 {
		return strings.Join(lines, "\n")
	}
	// Second pass: `/** */` block.
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

// swiftExtractImport parses an `import Foo` statement. Swift has no import
// alias concept. Path is the full `identifier` child's text (which for dotted
// forms like `import Foo.Bar` renders as `Foo.Bar`).
func swiftExtractImport(node *sitter.Node, content []byte) (types.Import, bool) {
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		if child.Type() == "identifier" {
			return types.Import{Path: child.Content(content)}, true
		}
	}
	return types.Import{}, false
}
