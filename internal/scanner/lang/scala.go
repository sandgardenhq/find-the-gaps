package lang

import (
	"context"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/scala"

	"github.com/sandgardenhq/find-the-gaps/internal/scanner/types"
)

// ScalaExtractor extracts public declarations and imports from Scala source files.
//
// Scala is public-by-default: a top-level declaration is emitted unless its
// `modifiers` child contains an `access_modifier` whose keyword is `private`
// (with or without a `private[pkg]` qualifier) or `protected`. No-modifier and
// an explicit `public` (which Scala does not actually have as a keyword) are
// both treated as public.
type ScalaExtractor struct{}

func (e *ScalaExtractor) Language() string     { return "Scala" }
func (e *ScalaExtractor) Extensions() []string { return []string{".scala", ".sc"} }

// Extract parses Scala source and returns public (default) top-level
// declarations plus all `import_declaration` entries.
//
// Node types (verified against tree-sitter-scala via a throwaway debug test):
//
//   - import_declaration             — top-level imports
//   - class_definition               — `class Foo { ... }`
//   - object_definition              — `object Foo { ... }`
//   - trait_definition               — `trait Foo { ... }` → KindInterface
//   - function_definition            — `def foo() = ...` (with body)
//   - function_declaration           — `def foo(): Int` (abstract; inside traits)
//   - val_definition                 — `val X = ...`                → KindConst
//   - var_definition                 — `var X = ...`                → KindVar
//   - modifiers > access_modifier    — holds `private`/`protected` token and
//     an optional `access_qualifier` (`private[pkg]`)
//   - block_comment                  — Scaladoc `/** ... */`
func (e *ScalaExtractor) Extract(_ string, content []byte) ([]types.Symbol, []types.Import, error) {
	syms := []types.Symbol{}
	imps := []types.Import{}

	parser := sitter.NewParser()
	defer parser.Close()
	parser.SetLanguage(scala.GetLanguage())

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
			if imp, ok := scalaExtractImport(node, content); ok {
				imps = append(imps, imp)
			}
		case "class_definition", "object_definition":
			if s, ok := scalaExtractDecl(root, i, node, content, types.KindClass); ok {
				syms = append(syms, s)
			}
		case "trait_definition":
			if s, ok := scalaExtractDecl(root, i, node, content, types.KindInterface); ok {
				syms = append(syms, s)
			}
		case "function_definition", "function_declaration":
			if s, ok := scalaExtractDecl(root, i, node, content, types.KindFunc); ok {
				syms = append(syms, s)
			}
		case "val_definition":
			if s, ok := scalaExtractDecl(root, i, node, content, types.KindConst); ok {
				syms = append(syms, s)
			}
		case "var_definition":
			if s, ok := scalaExtractDecl(root, i, node, content, types.KindVar); ok {
				syms = append(syms, s)
			}
		}
	}

	return syms, imps, nil
}

// scalaExtractDecl emits a symbol for a top-level declaration iff its modifiers
// do NOT include `private` (with or without `[pkg]` qualifier) or `protected`.
func scalaExtractDecl(parent *sitter.Node, idx int, node *sitter.Node, content []byte, kind types.SymbolKind) (types.Symbol, bool) {
	if scalaIsNonPublic(node) {
		return types.Symbol{}, false
	}
	name, ok := scalaDeclName(node, content)
	if !ok {
		return types.Symbol{}, false
	}
	return types.Symbol{
		Name:       name,
		Kind:       kind,
		Signature:  scalaDeclSignature(node, content),
		DocComment: scalaPrecedingComment(parent, idx, content),
		Line:       int(node.StartPoint().Row) + 1,
	}, true
}

// scalaIsNonPublic returns true iff the declaration has a `modifiers` child
// containing an `access_modifier` whose keyword child is `private` or
// `protected`. `private[pkg]` is still private — the trailing `access_qualifier`
// is ignored for visibility purposes.
func scalaIsNonPublic(node *sitter.Node) bool {
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		if child.Type() != "modifiers" {
			continue
		}
		for j := 0; j < int(child.ChildCount()); j++ {
			mod := child.Child(j)
			if mod.Type() != "access_modifier" {
				continue
			}
			for k := 0; k < int(mod.ChildCount()); k++ {
				switch mod.Child(k).Type() {
				case "private", "protected":
					return true
				}
			}
		}
	}
	return false
}

// scalaDeclName finds the declared name. class/object/trait/function use a
// `name` field pointing to an `identifier`. val/var declarations use a
// `pattern` field whose node is the identifier itself.
func scalaDeclName(node *sitter.Node, content []byte) (string, bool) {
	if n := node.ChildByFieldName("name"); n != nil {
		return n.Content(content), true
	}
	if n := node.ChildByFieldName("pattern"); n != nil {
		return n.Content(content), true
	}
	return "", false
}

// scalaDeclSignature returns a one-line header for the declaration — the
// source sliced to the first `{`, `=`, or newline, trimmed.
func scalaDeclSignature(node *sitter.Node, content []byte) string {
	src := node.Content(content)
	if idx := strings.IndexAny(src, "{=\n"); idx >= 0 {
		return strings.TrimSpace(src[:idx])
	}
	return strings.TrimSpace(src)
}

// scalaPrecedingComment returns the Scaladoc `/** */` block immediately
// preceding a declaration (sibling idx in parent), with the `/**` and `*/`
// markers stripped. Returns "" if the previous sibling is not a block_comment
// starting with `/**`.
func scalaPrecedingComment(parent *sitter.Node, idx int, content []byte) string {
	if idx <= 0 {
		return ""
	}
	prev := parent.Child(idx - 1)
	if prev.Type() != "block_comment" {
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

// scalaExtractImport parses an `import foo.Bar` statement. Scala also supports
// selector/rename forms (`import foo.{Bar => Baz}`); per plan, rename details
// are not surfaced — we emit the source text after the leading `import `
// keyword as the path. For plain dotted imports this yields `foo.Bar`; for
// selector imports it yields `foo.{Bar => Baz}` verbatim.
func scalaExtractImport(node *sitter.Node, content []byte) (types.Import, bool) {
	src := strings.TrimSpace(node.Content(content))
	path := strings.TrimSpace(strings.TrimPrefix(src, "import "))
	if path == "" {
		return types.Import{}, false
	}
	return types.Import{Path: path}, true
}
