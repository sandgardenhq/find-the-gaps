package lang

import (
	"context"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
	ruby "github.com/smacker/go-tree-sitter/ruby"

	"github.com/sandgardenhq/find-the-gaps/internal/scanner/types"
)

// RubyExtractor extracts public methods, classes, modules, and `require`
// imports from Ruby source files.
//
// Ruby's visibility model is different from the modifier-keyword languages:
// inside a class or module, a bare `private` or `protected` *directive* (an
// identifier with no arguments) flips the visibility of every following method
// in that scope. A `private :foo` call with arguments is NOT a scope directive
// — it selectively marks only `:foo` as private and is ignored here.
//
// Tree-sitter node types (verified against tree-sitter-ruby via a throwaway
// debug test before coding, then deleted):
//
//   - program                — root
//   - class                  — `class Foo … end`; fields: name=constant, body=body_statement
//   - module                 — `module Bar … end`; same shape as class
//   - method                 — `def name …`; field name=identifier
//   - singleton_method       — `def self.foo …`; fields object=self, name=identifier
//   - identifier             — appears standalone in a class/module body when
//     a bare `private` or `protected` directive is
//     written. This is the unique Ruby "directive"
//     shape — it is NOT a `call`.
//   - call                   — `require 'x'`, `require_relative 'x'`, or
//     `private :foo`; fields method=identifier,
//     arguments=argument_list
//   - string / string_content — string literal and its inner text
//   - comment                — a `#`-prefixed line
//   - body_statement         — wraps statements inside a class/module body
type RubyExtractor struct{}

func (e *RubyExtractor) Language() string     { return "Ruby" }
func (e *RubyExtractor) Extensions() []string { return []string{".rb"} }

// Extract parses Ruby source and returns public top-level declarations, public
// methods inside top-level classes/modules, and `require` / `require_relative`
// imports.
func (e *RubyExtractor) Extract(_ string, content []byte) ([]types.Symbol, []types.Import, error) {
	syms := []types.Symbol{}
	imps := []types.Import{}

	parser := sitter.NewParser()
	defer parser.Close()
	parser.SetLanguage(ruby.GetLanguage())

	tree, err := parser.ParseCtx(context.Background(), nil, content)
	if err != nil {
		return syms, imps, err
	}
	defer tree.Close()

	root := tree.RootNode()
	for i := 0; i < int(root.ChildCount()); i++ {
		node := root.Child(i)
		switch node.Type() {
		case "method":
			// Top-level method — always public, always emit.
			if s, ok := rubyMethodSymbol(root, i, node, content); ok {
				syms = append(syms, s)
			}
		case "class", "module":
			if s, ok := rubyTypeSymbol(root, i, node, content); ok {
				syms = append(syms, s)
			}
			syms = append(syms, rubyWalkBody(node, content)...)
		case "call":
			if imp, ok := rubyExtractRequire(node, content); ok {
				imps = append(imps, imp)
			}
		}
	}

	return syms, imps, nil
}

// rubyTypeSymbol builds the Symbol for a `class` or `module` declaration.
func rubyTypeSymbol(parent *sitter.Node, idx int, node *sitter.Node, content []byte) (types.Symbol, bool) {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return types.Symbol{}, false
	}
	name := nameNode.Content(content)
	keyword := "class"
	if node.Type() == "module" {
		keyword = "module"
	}
	return types.Symbol{
		Name:       name,
		Kind:       types.KindClass,
		Signature:  keyword + " " + name,
		DocComment: rubyPrecedingComment(parent, idx, content),
		Line:       int(node.StartPoint().Row) + 1,
	}, true
}

// rubyMethodSymbol builds the Symbol for a `method` or `singleton_method`.
func rubyMethodSymbol(parent *sitter.Node, idx int, node *sitter.Node, content []byte) (types.Symbol, bool) {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return types.Symbol{}, false
	}
	name := nameNode.Content(content)
	return types.Symbol{
		Name:       name,
		Kind:       types.KindFunc,
		Signature:  rubyMethodSignature(node, content),
		DocComment: rubyPrecedingComment(parent, idx, content),
		Line:       int(node.StartPoint().Row) + 1,
	}, true
}

// rubyWalkBody walks the `body_statement` of a class or module, tracking the
// current visibility scope. A bare `identifier` child whose text is exactly
// `private` or `protected` flips the scope for subsequent methods. Methods
// emitted while the scope is public (the default) become symbols.
func rubyWalkBody(typeDecl *sitter.Node, content []byte) []types.Symbol {
	var out []types.Symbol
	body := typeDecl.ChildByFieldName("body")
	if body == nil {
		return out
	}

	// currentVisibility starts public (Ruby's default) and is local to this
	// class/module's body — nested classes get their own fresh scope.
	currentVisibility := "public"

	for j := 0; j < int(body.ChildCount()); j++ {
		child := body.Child(j)
		switch child.Type() {
		case "identifier":
			// A bare identifier in statement position is a visibility directive
			// like `private` or `protected` with no arguments.
			text := child.Content(content)
			if text == "private" || text == "protected" {
				currentVisibility = text
			}
		case "method", "singleton_method":
			if currentVisibility != "public" {
				continue
			}
			if s, ok := rubyMethodSymbol(body, j, child, content); ok {
				out = append(out, s)
			}
		}
	}
	return out
}

// rubyMethodSignature returns the `def name(params)` header — the node's
// source sliced to the first newline, trimmed.
func rubyMethodSignature(node *sitter.Node, content []byte) string {
	src := node.Content(content)
	if idx := strings.IndexByte(src, '\n'); idx >= 0 {
		return strings.TrimSpace(src[:idx])
	}
	return strings.TrimSpace(src)
}

// rubyPrecedingComment collects consecutive `#`-prefixed comment siblings
// immediately preceding a declaration in its parent, strips the leading `# `
// (or `#`) from each, and joins them with newlines in source order. Returns ""
// if the previous non-structural sibling is not a comment.
func rubyPrecedingComment(parent *sitter.Node, idx int, content []byte) string {
	var lines []string
	for j := idx - 1; j >= 0; j-- {
		prev := parent.Child(j)
		if prev.Type() != "comment" {
			break
		}
		c := prev.Content(content)
		if !strings.HasPrefix(c, "#") {
			break
		}
		stripped := strings.TrimPrefix(c, "#")
		stripped = strings.TrimPrefix(stripped, " ")
		// Collecting in reverse; prepend so output is in source order.
		lines = append([]string{stripped}, lines...)
	}
	return strings.Join(lines, "\n")
}

// rubyExtractRequire returns an Import if the call node is `require 'x'` or
// `require_relative 'x'` with a single string argument.
func rubyExtractRequire(node *sitter.Node, content []byte) (types.Import, bool) {
	methodNode := node.ChildByFieldName("method")
	if methodNode == nil {
		return types.Import{}, false
	}
	name := methodNode.Content(content)
	if name != "require" && name != "require_relative" {
		return types.Import{}, false
	}
	argsNode := node.ChildByFieldName("arguments")
	if argsNode == nil {
		return types.Import{}, false
	}
	for i := 0; i < int(argsNode.ChildCount()); i++ {
		arg := argsNode.Child(i)
		if arg.Type() != "string" {
			continue
		}
		// Strip the delimiting quotes — the string literal's text includes them.
		raw := arg.Content(content)
		raw = strings.TrimPrefix(raw, `"`)
		raw = strings.TrimSuffix(raw, `"`)
		raw = strings.TrimPrefix(raw, `'`)
		raw = strings.TrimSuffix(raw, `'`)
		return types.Import{Path: raw}, true
	}
	return types.Import{}, false
}
