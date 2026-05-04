package lang

import (
	"context"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
	cppgrammar "github.com/smacker/go-tree-sitter/cpp"

	"github.com/sandgardenhq/find-the-gaps/internal/scanner/types"
)

// CPPExtractor extracts public declarations and imports from C++ source files.
// It extends the C rules with two C++-specific wrinkles:
//
//   - Anonymous namespaces (`namespace { ... }`) are file-private — every
//     declaration inside is skipped regardless of other modifiers. Named
//     namespaces (`namespace foo { ... }`) do NOT hide their contents.
//   - Class/struct member declarations are gated by C++ access specifiers
//     (`public:` / `private:` / `protected:`). The extractor walks class/struct
//     body children in order, maintaining a `currentAccess` flag defaulting to
//     `private` for `class` and `public` for `struct`, and only emits members
//     while the flag is `public`.
//
// Tree-sitter node types (verified against tree-sitter-cpp via a throwaway
// debug test before coding, then deleted):
//
//   - translation_unit            — root
//   - preproc_include             — same shape as C; `path` field child is a
//     `system_lib_string` or `string_literal`
//   - preproc_def                 — #define; skipped
//   - using_declaration           — TWO shapes:
//     (a) `using std::string;` — children:
//     `using` keyword, `qualified_identifier`
//     (→ `namespace_identifier(std)` ::
//     `identifier(string)`), `;`.
//     (b) `using namespace X;` — children:
//     `using` keyword, `namespace` keyword,
//     then `identifier(X)` OR
//     `qualified_identifier(foo::bar)`, `;`.
//     The presence of the `namespace` keyword
//     child distinguishes (b) from (a).
//   - namespace_definition        — optional `name` field (absent = anonymous);
//     `body` field = `declaration_list`.
//   - class_specifier             — `class X { ... }`; first keyword child's
//     Type() is `"class"`. `name` field =
//     `type_identifier`; `body` field =
//     `field_declaration_list`.
//   - struct_specifier            — `struct X { ... }`; first keyword child's
//     Type() is `"struct"`. Same layout as
//     class_specifier. (union_specifier also
//     exists and is treated like a C union →
//     KindClass; no members extracted.)
//   - access_specifier            — inside a class/struct body. Contains a
//     single keyword token whose Type() is
//     `"public"` / `"private"` / `"protected"`.
//     The trailing colon is a separate `:` token
//     sibling.
//   - field_declaration           — a class/struct member (function prototype
//     or variable). Member function's name lives
//     in `function_declarator > field_identifier`.
//   - function_definition         — a function body. Out-of-class member
//     definitions (`int A::m(int) { ... }`)
//     reach here with a
//     `function_declarator > qualified_identifier`
//     whose `scope` = class, `name` = method.
type CPPExtractor struct{}

func (e *CPPExtractor) Language() string { return "C++" }
func (e *CPPExtractor) Extensions() []string {
	return []string{".cc", ".cpp", ".cxx", ".hh", ".hpp", ".hxx"}
}

// Extract parses C++ source and returns top-level declarations and imports.
// Top-level here means "reachable at file scope or inside a named namespace";
// anonymous namespaces hide everything inside them. Class/struct members are
// gated by the C++ access-specifier state machine.
func (e *CPPExtractor) Extract(_ string, content []byte) ([]types.Symbol, []types.Import, error) {
	syms := []types.Symbol{}
	imps := []types.Import{}

	parser := sitter.NewParser()
	defer parser.Close()
	parser.SetLanguage(cppgrammar.GetLanguage())

	tree, err := parser.ParseCtx(context.Background(), nil, content)
	if err != nil {
		return syms, imps, err
	}
	defer tree.Close()

	root := tree.RootNode()
	s, i := cppWalkContainer(root, content)
	syms = append(syms, s...)
	imps = append(imps, i...)

	return syms, imps, nil
}

// cppWalkContainer walks the children of a container node (translation_unit or
// a namespace body's declaration_list) and returns the public symbols and
// imports reachable at that level, recursing into named namespaces.
func cppWalkContainer(container *sitter.Node, content []byte) ([]types.Symbol, []types.Import) {
	var syms []types.Symbol
	var imps []types.Import

	for i := 0; i < int(container.ChildCount()); i++ {
		node := container.Child(i)
		switch node.Type() {
		case "preproc_include":
			if imp, ok := cExtractInclude(node, content); ok {
				imps = append(imps, imp)
			}
		case "preproc_def":
			// #define macros are text substitution, not symbols. Skip.
			continue
		case "using_declaration":
			if imp, ok := cppExtractUsing(node, content); ok {
				imps = append(imps, imp)
			}
		case "namespace_definition":
			// Anonymous namespaces hide their contents from other translation
			// units — skip everything inside. Named namespaces don't hide;
			// descend into the body.
			if node.ChildByFieldName("name") == nil {
				continue
			}
			body := node.ChildByFieldName("body")
			if body == nil {
				continue
			}
			s, ip := cppWalkContainer(body, content)
			syms = append(syms, s...)
			imps = append(imps, ip...)
		case "declaration", "function_definition":
			if cHasStatic(node) {
				continue
			}
			if s, ok := cFuncSymbol(container, i, node, content); ok {
				syms = append(syms, s)
				continue
			}
			if s, ok := cVarSymbol(container, i, node, content); ok {
				syms = append(syms, s)
			}
		case "class_specifier":
			syms = append(syms, cppWalkClassLike(container, i, node, content, "private")...)
		case "struct_specifier":
			syms = append(syms, cppWalkClassLike(container, i, node, content, "public")...)
		case "union_specifier":
			if s, ok := cTypeSymbol(container, i, node, content, types.KindClass); ok {
				syms = append(syms, s)
			}
		case "type_definition":
			if s, ok := cTypedefSymbol(container, i, node, content); ok {
				syms = append(syms, s)
			}
		case "enum_specifier":
			if s, ok := cTypeSymbol(container, i, node, content, types.KindType); ok {
				syms = append(syms, s)
			}
		}
	}

	return syms, imps
}

// cppWalkClassLike emits a symbol for the class/struct itself (if named) and
// then walks its body's `field_declaration_list`, honoring `access_specifier`
// transitions. The initial access level is set by the caller (`private` for
// class, `public` for struct).
func cppWalkClassLike(parent *sitter.Node, idx int, node *sitter.Node, content []byte, initialAccess string) []types.Symbol {
	var out []types.Symbol

	// Emit the class/struct itself as KindClass.
	nameNode := node.ChildByFieldName("name")
	if nameNode != nil {
		out = append(out, types.Symbol{
			Name:       nameNode.Content(content),
			Kind:       types.KindClass,
			Signature:  cDeclSignature(node, content),
			DocComment: cPrecedingComment(parent, idx, content),
			Line:       int(node.StartPoint().Row) + 1,
		})
	}

	body := node.ChildByFieldName("body")
	if body == nil {
		return out
	}

	currentAccess := initialAccess
	for j := 0; j < int(body.ChildCount()); j++ {
		child := body.Child(j)
		switch child.Type() {
		case "access_specifier":
			// The keyword (public/private/protected) is the first child node
			// of access_specifier; the trailing `:` is a separate sibling in
			// the parent body.
			for k := 0; k < int(child.ChildCount()); k++ {
				kw := child.Child(k).Type()
				if kw == "public" || kw == "private" || kw == "protected" {
					currentAccess = kw
					break
				}
			}
		case "field_declaration":
			if currentAccess != "public" {
				continue
			}
			if s, ok := cppFieldSymbol(body, j, child, content); ok {
				out = append(out, s)
			}
		}
	}
	return out
}

// cppFieldSymbol builds a Symbol for a member declaration (function prototype
// or variable) inside a class/struct body. Only function members are emitted
// at this time — data members are skipped because they rarely appear in API
// documentation and the design doc's YAGNI list matches the C extractor's
// behavior (member-variable doc extraction is a future addition if needed).
func cppFieldSymbol(parent *sitter.Node, idx int, node *sitter.Node, content []byte) (types.Symbol, bool) {
	decl := cFindChildByType(node, "function_declarator")
	if decl == nil {
		return types.Symbol{}, false
	}
	// Member function name lives in function_declarator > field_identifier.
	// Fall back to identifier (defensive; not seen in practice for members).
	nameNode := cFindChildByType(decl, "field_identifier")
	if nameNode == nil {
		nameNode = cFindChildByType(decl, "identifier")
	}
	if nameNode == nil {
		return types.Symbol{}, false
	}
	return types.Symbol{
		Name:       nameNode.Content(content),
		Kind:       types.KindFunc,
		Signature:  cFuncSignature(node, content),
		DocComment: cPrecedingComment(parent, idx, content),
		Line:       int(node.StartPoint().Row) + 1,
	}, true
}

// cppExtractUsing parses a `using` declaration. Two forms:
//
//  1. `using std::string;` — children are `using`, `qualified_identifier`
//     (e.g. "std::string"), `;`. Emit Path = "std::string", no alias.
//  2. `using namespace X;` — children are `using`, `namespace`, then an
//     `identifier` or `qualified_identifier` for the namespace name.
//     Emit Path = namespace name, no alias.
//
// The two forms are distinguished by the presence of a `namespace` keyword
// child.
func cppExtractUsing(node *sitter.Node, content []byte) (types.Import, bool) {
	isNamespaceForm := false
	for i := 0; i < int(node.ChildCount()); i++ {
		if node.Child(i).Type() == "namespace" {
			isNamespaceForm = true
			break
		}
	}

	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		switch child.Type() {
		case "qualified_identifier":
			path := strings.TrimSpace(child.Content(content))
			return types.Import{Path: path}, path != ""
		case "identifier":
			// Only treat `identifier` as the import target in the
			// `using namespace X` form. For `using X::Y`, the target is the
			// `qualified_identifier` child, not a bare identifier.
			if !isNamespaceForm {
				continue
			}
			path := strings.TrimSpace(child.Content(content))
			return types.Import{Path: path}, path != ""
		}
	}
	return types.Import{}, false
}
