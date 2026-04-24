package lang

import (
	"context"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/java"

	"github.com/sandgardenhq/find-the-gaps/internal/scanner/types"
)

// JavaExtractor extracts public declarations and imports from Java source files.
type JavaExtractor struct{}

func (e *JavaExtractor) Language() string     { return "Java" }
func (e *JavaExtractor) Extensions() []string { return []string{".java"} }

// Extract parses Java source and returns declarations whose modifiers include
// the `public` keyword, plus all top-level `import` statements.
func (e *JavaExtractor) Extract(_ string, content []byte) ([]types.Symbol, []types.Import, error) {
	syms := []types.Symbol{}
	imps := []types.Import{}

	parser := sitter.NewParser()
	defer parser.Close()
	parser.SetLanguage(java.GetLanguage())

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
			if imp, ok := javaExtractImport(node, content); ok {
				imps = append(imps, imp)
			}
		case "class_declaration", "record_declaration":
			syms = append(syms, javaWalkType(root, i, node, content, types.KindClass)...)
		case "interface_declaration":
			syms = append(syms, javaWalkType(root, i, node, content, types.KindInterface)...)
		case "enum_declaration":
			syms = append(syms, javaWalkType(root, i, node, content, types.KindType)...)
		}
	}

	return syms, imps, nil
}

// javaWalkType emits a symbol for the type declaration itself (if `public`) and
// recurses into its body to emit public method members.
func javaWalkType(parent *sitter.Node, idx int, node *sitter.Node, content []byte, kind types.SymbolKind) []types.Symbol {
	var out []types.Symbol

	if javaHasModifier(node, "public") {
		nameNode := node.ChildByFieldName("name")
		if nameNode != nil {
			out = append(out, types.Symbol{
				Name:       nameNode.Content(content),
				Kind:       kind,
				Signature:  javaDeclSignature(node, content),
				DocComment: javaPrecedingComment(parent, idx, content),
				Line:       int(node.StartPoint().Row) + 1,
			})
		}
	}

	body := node.ChildByFieldName("body")
	if body != nil {
		out = append(out, javaWalkMembers(body, content)...)
	}
	return out
}

// javaWalkMembers scans a class/interface/enum body for public method_declaration children.
func javaWalkMembers(body *sitter.Node, content []byte) []types.Symbol {
	var out []types.Symbol
	for j := 0; j < int(body.ChildCount()); j++ {
		member := body.Child(j)
		if member.Type() != "method_declaration" {
			continue
		}
		if !javaHasModifier(member, "public") {
			continue
		}
		nameNode := member.ChildByFieldName("name")
		if nameNode == nil {
			continue
		}
		out = append(out, types.Symbol{
			Name:       nameNode.Content(content),
			Kind:       types.KindFunc,
			Signature:  javaDeclSignature(member, content),
			DocComment: javaPrecedingComment(body, j, content),
			Line:       int(member.StartPoint().Row) + 1,
		})
	}
	return out
}

// javaHasModifier returns true iff the declaration has a `modifiers` child
// that contains a token whose content equals `keyword`.
func javaHasModifier(node *sitter.Node, keyword string) bool {
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		if child.Type() != "modifiers" {
			continue
		}
		for j := 0; j < int(child.ChildCount()); j++ {
			if child.Child(j).Type() == keyword {
				return true
			}
		}
	}
	return false
}

// javaDeclSignature returns a one-line header for the declaration — everything
// up to the opening `{` or `;`, trimmed.
func javaDeclSignature(node *sitter.Node, content []byte) string {
	src := node.Content(content)
	if idx := strings.IndexAny(src, "{;"); idx >= 0 {
		return strings.TrimSpace(src[:idx])
	}
	return strings.TrimSpace(src)
}

// javaPrecedingComment returns the Javadoc comment immediately preceding a
// declaration (sibling index idx in parent), with /** and */ markers stripped.
// Returns "" if the previous sibling is not a block_comment starting with /**.
func javaPrecedingComment(parent *sitter.Node, idx int, content []byte) string {
	for j := idx - 1; j >= 0; j-- {
		prev := parent.Child(j)
		// Skip structural punctuation tokens (e.g. `{` inside a class_body).
		if prev.Type() == "{" {
			continue
		}
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
	return ""
}

// javaExtractImport parses an `import ...;` declaration. Java has no alias
// syntax, so Alias is always empty; Path is the full dotted name.
func javaExtractImport(node *sitter.Node, content []byte) (types.Import, bool) {
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		switch child.Type() {
		case "scoped_identifier", "identifier", "asterisk":
			return types.Import{Path: child.Content(content)}, true
		}
	}
	return types.Import{}, false
}
