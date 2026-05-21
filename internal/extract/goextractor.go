// Package extract implements source-code extraction using tree-sitter.
package extract

import (
	"strings"

	"github.com/julianshen/gogfy/internal/schema"
	sitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_go "github.com/tree-sitter/tree-sitter-go/bindings/go"
)

// GoExtractor uses tree-sitter-go to extract module/function/method nodes,
// import edges, and call edges from Go sources.
type GoExtractor struct{}

func (GoExtractor) Extract(path string) (Result, error) {
	return runExtraction(path, tree_sitter_go.Language(), "go", walkGo)
}

// walkGo emits one module node per file (label is the Go package name as
// declared by the file's package_clause), function/method nodes for each
// declaration, import edges from the module to each imported path, and
// call edges from the innermost enclosing function (or the module, for
// top-level init expressions) to a synthetic `go:call:<callee>` target.
func walkGo(cursor *sitter.TreeCursor, src []byte, state *extractState) {
	node := cursor.Node()
	switch node.Kind() {
	case "package_clause":
		nameNode := node.ChildByFieldName("name")
		if nameNode == nil {
			nameNode = firstChildOfKind(node, "package_identifier")
		}
		if nameNode != nil {
			// Override the runExtraction-emitted module node's label so
			// `go:module:<file>` carries the package name (more meaningful
			// than the bare filename for Go's per-package model).
			rewriteModuleLabel(state, nameNode.Utf8Text(src))
		}
	case "type_declaration":
		// `type Foo struct{}` / `type Bar interface{}` / `type Baz =
		// Qux`. A single declaration can group multiple specs
		// (`type ( A int; B string )`), so walk every type_spec child.
		// Each becomes a `go:type:` node, registered for later
		// method-ownership linking.
		for i := uint(0); i < node.ChildCount(); i++ {
			ch := node.Child(i)
			if ch.Kind() != "type_spec" && ch.Kind() != "type_alias" {
				continue
			}
			nameNode := ch.ChildByFieldName("name")
			state.emitDecl("type", ch, nameNode, src)
			if nameNode != nil {
				typeName := nameNode.Utf8Text(src)
				typeID := declID(state.lang, "type", state.filePath, nameNode, src)
				// Extract the type's members so the type subgraph is
				// rich: struct fields and interface method specs become
				// nodes contained by the type. Mirrors graphify, which
				// extracts these as finer-grained entities.
				goEmitTypeMembers(state, ch, typeName, typeID, src)
			}
		}
	case "const_declaration", "var_declaration":
		// Package- or block-level const/var declarations. Each spec's
		// name becomes a `go:const:` / `go:var:` node contained by the
		// module (emitDecl wires the module→decl edge). Multi-name
		// specs (`var a, b = 1, 2`) emit one node per name.
		kind := "const"
		if node.Kind() == "var_declaration" {
			kind = "var"
		}
		specKind := kind + "_spec"
		for i := uint(0); i < node.ChildCount(); i++ {
			spec := node.Child(i)
			if spec.Kind() != specKind {
				continue
			}
			for j := uint(0); j < spec.ChildCount(); j++ {
				nameNode := spec.Child(j)
				if nameNode.Kind() != "identifier" {
					continue
				}
				// Skip the blank identifier — `var _ = …` / `const _ = …`
				// is a deliberate discard, not a named entity.
				if nameNode.Utf8Text(src) == "_" {
					continue
				}
				state.emitDecl(kind, spec, nameNode, src)
			}
		}
	case "function_declaration", "method_declaration":
		kind := "function"
		if node.Kind() == "method_declaration" {
			kind = "method"
		}
		nameNode := node.ChildByFieldName("name")
		state.emitDecl(kind, node, nameNode, src)
		// Method ownership: emit a `method_of` edge to a synthetic
		// receiver-type-ref. resolve.MethodOwnership rewrites it to a
		// `type contains method` edge against the real type node in
		// the same package directory — which works whether the type
		// is declared in this file or a sibling file of the package.
		// (emitDecl already linked the method to its module; the
		// ownership edge adds the type axis.)
		if kind == "method" && nameNode != nil {
			if recv := goReceiverTypeName(node, src); recv != "" {
				state.addMethodOf(
					declID(state.lang, "method", state.filePath, nameNode, src), recv)
			}
		}
		state.walkFnScope(kind, nameNode, src, cursor, walkGo)
		return
	case "call_expression":
		state.addCall(callTargetName(node.ChildByFieldName("function"), src))
	case "import_spec":
		pathNode := node.ChildByFieldName("path")
		if pathNode != nil {
			state.addImport(strings.Trim(pathNode.Utf8Text(src), `"`))
		}
	}
	walkChildren(cursor, func() { walkGo(cursor, src, state) })
}

// goReceiverTypeName extracts the receiver type name from a Go
// method_declaration, stripping a leading pointer. For
// `func (r *Repo) Save()` and `func (r Repo) Load()` it returns
// "Repo". Returns "" when the receiver or its type can't be found
// (defensive — a malformed AST shouldn't panic the walker).
//
// AST shape: method_declaration has a `receiver` field = parameter_list
// → parameter_declaration whose `type` field is either a
// `pointer_type` (→ type_identifier) or a bare `type_identifier`.
func goReceiverTypeName(method *sitter.Node, src []byte) string {
	recv := method.ChildByFieldName("receiver")
	if recv == nil {
		return ""
	}
	param := firstChildOfKind(recv, "parameter_declaration")
	if param == nil {
		return ""
	}
	typeNode := param.ChildByFieldName("type")
	if typeNode == nil {
		return ""
	}
	// Unwrap a pointer receiver (*Repo → Repo).
	if typeNode.Kind() == "pointer_type" {
		if id := firstChildOfKind(typeNode, "type_identifier"); id != nil {
			return id.Utf8Text(src)
		}
		return ""
	}
	if typeNode.Kind() == "type_identifier" {
		return typeNode.Utf8Text(src)
	}
	// Generic receiver (`func (r *Repo[T]) ...`) → generic_type wraps
	// the pointer/identifier; dig for the first type_identifier.
	if id := firstChildOfKind(typeNode, "type_identifier"); id != nil {
		return id.Utf8Text(src)
	}
	return ""
}

// goEmitTypeMembers extracts the members of a type_spec — struct
// fields and interface method specs — as nodes contained by the
// type. Field IDs are qualified with the type name (`Type.field`) so
// two structs with a same-named field don't collide. Interface
// methods are emitted as `method` nodes (same kind as concrete
// methods) so "what methods does this type have" queries see both.
func goEmitTypeMembers(state *extractState, typeSpec *sitter.Node, typeName, typeID string, src []byte) {
	body := typeSpec.ChildByFieldName("type")
	if body == nil {
		return
	}
	switch body.Kind() {
	case "struct_type":
		// struct_type → field_declaration_list → field_declaration.
		list := firstChildOfKind(body, "field_declaration_list")
		if list == nil {
			return
		}
		walkTypeBody(list, "field_declaration", func(decl *sitter.Node) {
			// A field_declaration can name several fields
			// (`x, y int`); emit one node per field_identifier.
			for i := uint(0); i < decl.ChildCount(); i++ {
				id := decl.Child(i)
				if id.Kind() != "field_identifier" {
					continue
				}
				goEmitMember(state, "field", typeName, typeID, id, decl, src)
			}
		})
	case "interface_type":
		walkTypeBody(body, "method_elem", func(elem *sitter.Node) {
			if id := firstChildOfKind(elem, "field_identifier"); id != nil {
				goEmitMember(state, "method", typeName, typeID, id, elem, src)
			}
		})
	}
}

// walkTypeBody invokes fn for each direct child of body whose kind
// matches. Kept tiny so the struct/interface cases above stay
// declarative.
func walkTypeBody(body *sitter.Node, kind string, fn func(*sitter.Node)) {
	for i := uint(0); i < body.ChildCount(); i++ {
		if ch := body.Child(i); ch.Kind() == kind {
			fn(ch)
		}
	}
}

// goEmitMember appends a member node (struct field or interface
// method) and a `contains` edge from its owning type. The member ID
// is `go:<kind>:<file>:<TypeName>.<memberName>` — type-qualified so
// members of different types don't collide.
func goEmitMember(state *extractState, kind, typeName, typeID string, nameNode, declNode *sitter.Node, src []byte) {
	name := nameNode.Utf8Text(src)
	if name == "" {
		return
	}
	memberID := schema.LangID(state.lang, kind, state.filePath+":"+typeName+"."+name)
	state.nodes = append(state.nodes, schema.Node{
		ID:             memberID,
		Label:          name,
		SourceFile:     state.filePath,
		SourceLocation: nodeLocation(declNode),
	})
	state.edges = append(state.edges, schema.Edge{
		Source:     typeID,
		Target:     memberID,
		Relation:   "contains",
		Confidence: schema.Extracted,
	})
}
