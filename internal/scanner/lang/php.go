package lang

import (
	"context"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/php"

	"github.com/sandgardenhq/find-the-gaps/internal/scanner/types"
)

// PHPExtractor extracts public declarations and namespace-use imports from PHP source files.
//
// PHP source starts with a `<?php` tag (rendered by tree-sitter as a `php_tag`
// node inside the top-level `program`). Top-level function/class/interface/
// trait/enum declarations are always considered public. For class members
// (method_declaration), emit iff the `visibility_modifier` child is absent
// (implicit public) or its text is `public`. Skip `private` and `protected`.
type PHPExtractor struct{}

func (e *PHPExtractor) Language() string     { return "PHP" }
func (e *PHPExtractor) Extensions() []string { return []string{".php"} }

// Extract parses PHP source and returns public declarations plus all
// `namespace_use_declaration` imports.
//
// Node types (verified against tree-sitter-php via a throwaway debug test):
//
//   - program                       — root
//   - php_tag                       — leading <?php marker (skipped)
//   - function_definition           — top-level function
//   - class_declaration             — `class Foo { ... }`
//   - interface_declaration         — `interface Foo { ... }`
//   - trait_declaration             — `trait Foo { ... }`
//   - enum_declaration              — `enum Foo { ... }`
//   - declaration_list              — body of class/interface/trait
//   - method_declaration            — class member method
//   - visibility_modifier           — `public` / `private` / `protected` child of method_declaration
//   - namespace_use_declaration     — `use Foo\Bar;` / `use Foo\Baz as Qux;`
//   - namespace_use_clause          — child of namespace_use_declaration
//   - qualified_name                — dotted path inside a use clause
//   - namespace_aliasing_clause     — `as Qux` portion (first `name` child = alias)
//   - comment                       — PHPDoc `/** ... */` block
func (e *PHPExtractor) Extract(_ string, content []byte) ([]types.Symbol, []types.Import, error) {
	syms := []types.Symbol{}
	imps := []types.Import{}

	parser := sitter.NewParser()
	defer parser.Close()
	parser.SetLanguage(php.GetLanguage())

	tree, err := parser.ParseCtx(context.Background(), nil, content)
	if err != nil {
		return syms, imps, err
	}
	defer tree.Close()

	root := tree.RootNode()
	for i := 0; i < int(root.ChildCount()); i++ {
		node := root.Child(i)
		switch node.Type() {
		case "namespace_use_declaration":
			imps = append(imps, phpExtractImports(node, content)...)
		case "function_definition":
			if s, ok := phpExtractDecl(root, i, node, content, types.KindFunc); ok {
				syms = append(syms, s)
			}
		case "class_declaration", "trait_declaration":
			if s, ok := phpExtractDecl(root, i, node, content, types.KindClass); ok {
				syms = append(syms, s)
			}
			syms = append(syms, phpWalkMembers(node, content)...)
		case "interface_declaration":
			if s, ok := phpExtractDecl(root, i, node, content, types.KindInterface); ok {
				syms = append(syms, s)
			}
			syms = append(syms, phpWalkMembers(node, content)...)
		case "enum_declaration":
			if s, ok := phpExtractDecl(root, i, node, content, types.KindClass); ok {
				syms = append(syms, s)
			}
			syms = append(syms, phpWalkMembers(node, content)...)
		}
	}

	return syms, imps, nil
}

// phpExtractDecl builds a Symbol for a top-level declaration. Top-level PHP
// declarations are always public, so no visibility check is needed here.
func phpExtractDecl(parent *sitter.Node, idx int, node *sitter.Node, content []byte, kind types.SymbolKind) (types.Symbol, bool) {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return types.Symbol{}, false
	}
	return types.Symbol{
		Name:       nameNode.Content(content),
		Kind:       kind,
		Signature:  phpDeclSignature(node, content),
		DocComment: phpPrecedingComment(parent, idx, content),
		Line:       int(node.StartPoint().Row) + 1,
	}, true
}

// phpWalkMembers walks the declaration_list body of a class/interface/trait/enum
// and emits method_declaration children whose visibility is public (explicit
// `public` keyword, or absent — PHP implicit public). Skips `private` and
// `protected` members.
func phpWalkMembers(typeDecl *sitter.Node, content []byte) []types.Symbol {
	var out []types.Symbol
	body := typeDecl.ChildByFieldName("body")
	if body == nil {
		return out
	}
	for j := 0; j < int(body.ChildCount()); j++ {
		member := body.Child(j)
		if member.Type() != "method_declaration" {
			continue
		}
		if !phpMemberIsPublic(member, content) {
			continue
		}
		nameNode := member.ChildByFieldName("name")
		if nameNode == nil {
			continue
		}
		out = append(out, types.Symbol{
			Name:       nameNode.Content(content),
			Kind:       types.KindFunc,
			Signature:  phpDeclSignature(member, content),
			DocComment: phpPrecedingComment(body, j, content),
			Line:       int(member.StartPoint().Row) + 1,
		})
	}
	return out
}

// phpMemberIsPublic returns true iff the method_declaration has no
// visibility_modifier (implicit public in PHP) or its visibility_modifier's
// text is `public`.
func phpMemberIsPublic(member *sitter.Node, content []byte) bool {
	for i := 0; i < int(member.ChildCount()); i++ {
		child := member.Child(i)
		if child.Type() != "visibility_modifier" {
			continue
		}
		return strings.Contains(child.Content(content), "public")
	}
	return true
}

// phpDeclSignature returns a one-line header for the declaration — the source
// sliced to the first `{` or `;`, trimmed.
func phpDeclSignature(node *sitter.Node, content []byte) string {
	src := node.Content(content)
	if idx := strings.IndexAny(src, "{;"); idx >= 0 {
		return strings.TrimSpace(src[:idx])
	}
	return strings.TrimSpace(src)
}

// phpPrecedingComment returns the PHPDoc `/** */` block immediately preceding
// a declaration (sibling idx in parent), with the `/**` and `*/` markers
// stripped. Returns "" if the previous sibling is not a `/**`-prefixed comment.
func phpPrecedingComment(parent *sitter.Node, idx int, content []byte) string {
	for j := idx - 1; j >= 0; j-- {
		prev := parent.Child(j)
		// Skip structural punctuation tokens that appear inside a
		// declaration_list body.
		if prev.Type() == "{" {
			continue
		}
		if prev.Type() != "comment" {
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
	return ""
}

// phpExtractImports walks a namespace_use_declaration and emits one Import per
// namespace_use_clause inside it. The clause holds a `qualified_name` (path)
// and optionally a `namespace_aliasing_clause` whose `name` child is the alias.
func phpExtractImports(node *sitter.Node, content []byte) []types.Import {
	var out []types.Import
	for i := 0; i < int(node.ChildCount()); i++ {
		clause := node.Child(i)
		if clause.Type() != "namespace_use_clause" {
			continue
		}
		var path, alias string
		for j := 0; j < int(clause.ChildCount()); j++ {
			inner := clause.Child(j)
			switch inner.Type() {
			case "qualified_name", "name":
				if path == "" {
					path = inner.Content(content)
				}
			case "namespace_aliasing_clause":
				// The alias is the `name` child of the aliasing clause.
				for k := 0; k < int(inner.ChildCount()); k++ {
					if inner.Child(k).Type() == "name" {
						alias = inner.Child(k).Content(content)
						break
					}
				}
			}
		}
		if path == "" {
			continue
		}
		out = append(out, types.Import{Path: path, Alias: alias})
	}
	return out
}
