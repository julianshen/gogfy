package extract

import "testing"

// TestCalleeShapesExhaustive locks the invariant that every shape in
// calleeKinds maps to a non-zero, valid extraction strategy. The
// previous two-list (firstCallee + callTargetName) structure could
// silently drop call edges when a kind was added to one list and not
// the other; the unified map prevents that, but only if every entry
// is reachable from callTargetName's switch.
func TestCalleeShapesExhaustive(t *testing.T) {
	for kind, shape := range calleeKinds {
		switch shape {
		case shapeBare, shapeMemberAccess, shapeNavigation:
			// reachable
		default:
			t.Errorf("calleeKinds[%q] has unknown shape %d — callTargetName will return \"\" silently", kind, shape)
		}
	}
}

// TestMemberLeafKindsSupersetOfBareIdent locks that every bare-ident
// kind that can plausibly appear as a member-access leaf is in
// memberLeafKinds. If `name` were a member-access leaf in some grammar
// (Ruby?) and missing here, member-access dispatch on Ruby calls would
// silently return "" — we'd miss the call edge entirely.
func TestMemberLeafKindsSupersetOfBareIdent(t *testing.T) {
	leaves := map[string]bool{}
	for _, k := range memberLeafKinds {
		leaves[k] = true
	}
	// `name` is a Ruby/Bash bare-ident kind; we deliberately exclude it
	// because no current grammar puts `name` at the trailing position of
	// a member-access wrapper. If a future grammar does, this test plus
	// a real-source assertion will catch the gap.
	for kind, shape := range calleeKinds {
		if shape != shapeBare {
			continue
		}
		switch kind {
		case "name":
			// see comment above
			continue
		}
		if !leaves[kind] {
			t.Errorf("bare-ident kind %q is not in memberLeafKinds — member-access wrappers ending in this kind would drop call edges", kind)
		}
	}
}
