package extract

import (
	"strings"

	sitter "github.com/tree-sitter/go-tree-sitter"
)

// simpleTypeName reduces a (possibly qualified, possibly generic) type
// expression to the bare type name that a declaration node is labeled
// with: `mixins.M` → `M`, `std::fmt::Display` → `Display`, `List<T>` →
// `List`. This lets inheritance resolution match base references against
// type/class node labels, which are themselves bare names.
func simpleTypeName(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexAny(s, "<(["); i >= 0 {
		s = s[:i]
	}
	if i := strings.LastIndex(s, "::"); i >= 0 {
		s = s[i+2:]
	}
	if i := strings.LastIndexByte(s, '.'); i >= 0 {
		s = s[i+1:]
	}
	return strings.TrimSpace(s)
}

// emitBasesOfKinds walks the direct children of parent and, for each child
// whose kind is in kinds, emits an inheritance edge from subtypeID using
// the child's text (normalized via simpleTypeName). Used by the languages
// whose base/interface references sit as flat children of a clause node.
func emitBasesOfKinds(s *extractState, subtypeID string, parent *sitter.Node, src []byte, relation string, kinds ...string) {
	if parent == nil {
		return
	}
	want := map[string]bool{}
	for _, k := range kinds {
		want[k] = true
	}
	for i := uint(0); i < parent.ChildCount(); i++ {
		c := parent.Child(i)
		if want[c.Kind()] {
			s.addInherits(subtypeID, simpleTypeName(c.Utf8Text(src)), relation)
		}
	}
}

// --- Python: class_definition `superclasses` = argument_list -----------

func goEmitPythonBases(s *extractState, declNode *sitter.Node, declID string, src []byte) {
	args := declNode.ChildByFieldName("superclasses")
	if args == nil {
		return
	}
	for i := uint(0); i < args.ChildCount(); i++ {
		c := args.Child(i)
		switch c.Kind() {
		case "identifier", "attribute", "subscript":
			s.addInherits(declID, simpleTypeName(c.Utf8Text(src)), "inherits")
		}
	}
}

// --- Java: superclass / super_interfaces; interface extends ------------

func goEmitJavaBases(s *extractState, declNode *sitter.Node, declID string, src []byte) {
	if sc := declNode.ChildByFieldName("superclass"); sc != nil {
		emitBasesOfKinds(s, declID, sc, src, "inherits", "type_identifier", "generic_type", "scoped_type_identifier")
	}
	if ifaces := declNode.ChildByFieldName("interfaces"); ifaces != nil {
		// super_interfaces → type_list → type_identifier(s)
		for i := uint(0); i < ifaces.ChildCount(); i++ {
			emitBasesOfKinds(s, declID, ifaces.Child(i), src, "implements", "type_identifier", "generic_type", "scoped_type_identifier")
		}
	}
	// interface_declaration: extends_interfaces → type_list
	for i := uint(0); i < declNode.ChildCount(); i++ {
		c := declNode.Child(i)
		if c.Kind() == "extends_interfaces" {
			for j := uint(0); j < c.ChildCount(); j++ {
				emitBasesOfKinds(s, declID, c.Child(j), src, "inherits", "type_identifier", "generic_type", "scoped_type_identifier")
			}
		}
	}
}

// --- JS: class_heritage = `extends <expr>` -----------------------------

func goEmitJSBases(s *extractState, declNode *sitter.Node, declID string, src []byte) {
	for i := uint(0); i < declNode.ChildCount(); i++ {
		c := declNode.Child(i)
		if c.Kind() != "class_heritage" {
			continue
		}
		for j := uint(0); j < c.ChildCount(); j++ {
			g := c.Child(j)
			switch g.Kind() {
			case "identifier", "member_expression", "call_expression":
				s.addInherits(declID, simpleTypeName(g.Utf8Text(src)), "inherits")
			}
		}
	}
}

// --- TS: class_heritage → extends_clause + implements_clause -----------

func goEmitTSBases(s *extractState, declNode *sitter.Node, declID string, src []byte) {
	for i := uint(0); i < declNode.ChildCount(); i++ {
		c := declNode.Child(i)
		switch c.Kind() {
		case "class_heritage":
			for j := uint(0); j < c.ChildCount(); j++ {
				clause := c.Child(j)
				switch clause.Kind() {
				case "extends_clause":
					emitBasesOfKinds(s, declID, clause, src, "inherits", "identifier", "member_expression", "generic_type", "type_identifier")
				case "implements_clause":
					emitBasesOfKinds(s, declID, clause, src, "implements", "type_identifier", "generic_type", "identifier")
				}
			}
		case "extends_type_clause": // interface_declaration extends
			emitBasesOfKinds(s, declID, c, src, "inherits", "type_identifier", "generic_type", "identifier")
		}
	}
}

// --- C#: base_list (cannot split class vs interface in the AST) --------

func goEmitCSharpBases(s *extractState, declNode *sitter.Node, declID string, src []byte) {
	for i := uint(0); i < declNode.ChildCount(); i++ {
		c := declNode.Child(i)
		if c.Kind() == "base_list" {
			emitBasesOfKinds(s, declID, c, src, "inherits", "identifier", "generic_name", "qualified_name")
		}
	}
}

// --- Kotlin: delegation_specifiers → user_type ------------------------

func goEmitKotlinBases(s *extractState, declNode *sitter.Node, declID string, src []byte) {
	for i := uint(0); i < declNode.ChildCount(); i++ {
		c := declNode.Child(i)
		if c.Kind() != "delegation_specifiers" {
			continue
		}
		for j := uint(0); j < c.ChildCount(); j++ {
			spec := c.Child(j)
			if spec.Kind() != "delegation_specifier" {
				continue
			}
			// constructor_invocation → user_type, or bare user_type
			ut := firstDescendantOfKind(spec, "user_type")
			if ut != nil {
				s.addInherits(declID, simpleTypeName(ut.Utf8Text(src)), "inherits")
			}
		}
	}
}

// --- Scala: extends_clause → type_identifier(s), `with` traits --------

func goEmitScalaBases(s *extractState, declNode *sitter.Node, declID string, src []byte) {
	for i := uint(0); i < declNode.ChildCount(); i++ {
		c := declNode.Child(i)
		if c.Kind() == "extends_clause" {
			emitBasesOfKinds(s, declID, c, src, "inherits", "type_identifier", "generic_type")
		}
	}
}

// --- Swift: inheritance_specifier → user_type -------------------------

func goEmitSwiftBases(s *extractState, declNode *sitter.Node, declID string, src []byte) {
	for i := uint(0); i < declNode.ChildCount(); i++ {
		c := declNode.Child(i)
		if c.Kind() != "inheritance_specifier" {
			continue
		}
		if ut := firstDescendantOfKind(c, "user_type", "type_identifier"); ut != nil {
			s.addInherits(declID, simpleTypeName(ut.Utf8Text(src)), "inherits")
		}
	}
}

// --- Dart: superclass + interfaces ------------------------------------

func goEmitDartBases(s *extractState, declNode *sitter.Node, declID string, src []byte) {
	if sc := declNode.ChildByFieldName("superclass"); sc != nil {
		emitBasesOfKinds(s, declID, sc, src, "inherits", "type_identifier", "type")
	}
	if ifaces := declNode.ChildByFieldName("interfaces"); ifaces != nil {
		emitBasesOfKinds(s, declID, ifaces, src, "implements", "type_identifier", "type")
	}
}

// --- Ruby: superclass → constant --------------------------------------

func goEmitRubyBases(s *extractState, declNode *sitter.Node, declID string, src []byte) {
	if sc := declNode.ChildByFieldName("superclass"); sc != nil {
		emitBasesOfKinds(s, declID, sc, src, "inherits", "constant", "scope_resolution")
	}
}

// --- PHP: base_clause (extends) + class_interface_clause --------------

func goEmitPHPBases(s *extractState, declNode *sitter.Node, declID string, src []byte) {
	for i := uint(0); i < declNode.ChildCount(); i++ {
		c := declNode.Child(i)
		switch c.Kind() {
		case "base_clause":
			emitBasesOfKinds(s, declID, c, src, "inherits", "name", "qualified_name")
		case "class_interface_clause":
			emitBasesOfKinds(s, declID, c, src, "implements", "name", "qualified_name")
		}
	}
}

// --- Go: struct/interface embedding -----------------------------------

// goEmitEmbeds emits `embeds` edges for Go struct embedding (a
// field_declaration with no `name` field) and interface embedding (a
// type_elem naming another interface). typeBody is the struct_type or
// interface_type node; declID is the enclosing type node.
func goEmitEmbeds(s *extractState, typeBody *sitter.Node, declID string, src []byte) {
	switch typeBody.Kind() {
	case "struct_type":
		list := firstChildOfKind(typeBody, "field_declaration_list")
		if list == nil {
			return
		}
		walkTypeBody(list, "field_declaration", func(decl *sitter.Node) {
			// Embedded field: a field_declaration with a `type` but no
			// `name` field (e.g. `Base` rather than `x Base`).
			if decl.ChildByFieldName("name") != nil {
				return
			}
			if t := decl.ChildByFieldName("type"); t != nil {
				s.addInherits(declID, simpleTypeName(t.Utf8Text(src)), "embeds")
			}
		})
	case "interface_type":
		walkTypeBody(typeBody, "type_elem", func(elem *sitter.Node) {
			s.addInherits(declID, simpleTypeName(elem.Utf8Text(src)), "embeds")
		})
	}
}

// firstDescendantOfKind returns the first descendant (BFS over direct
// children, then deeper) whose kind is in kinds, or nil.
func firstDescendantOfKind(n *sitter.Node, kinds ...string) *sitter.Node {
	want := map[string]bool{}
	for _, k := range kinds {
		want[k] = true
	}
	queue := []*sitter.Node{n}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for i := uint(0); i < cur.ChildCount(); i++ {
			c := cur.Child(i)
			if want[c.Kind()] {
				return c
			}
			queue = append(queue, c)
		}
	}
	return nil
}
