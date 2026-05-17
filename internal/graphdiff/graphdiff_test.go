package graphdiff

import (
	"testing"

	"github.com/julianshen/gogfy/internal/schema"
)

func TestComputeIdentifiesAddedNodes(t *testing.T) {
	oldN := []schema.Node{{ID: "a", Label: "A"}}
	newN := []schema.Node{{ID: "a", Label: "A"}, {ID: "b", Label: "B"}}
	d := Compute(oldN, newN, nil, nil)
	if len(d.NodesAdded) != 1 || d.NodesAdded[0].ID != "b" {
		t.Fatalf("expected node 'b' added, got %+v", d.NodesAdded)
	}
	if len(d.NodesRemoved) != 0 {
		t.Errorf("nothing should be marked removed, got %+v", d.NodesRemoved)
	}
}

func TestComputeIdentifiesRemovedNodes(t *testing.T) {
	oldN := []schema.Node{{ID: "a"}, {ID: "b"}}
	newN := []schema.Node{{ID: "a"}}
	d := Compute(oldN, newN, nil, nil)
	if len(d.NodesRemoved) != 1 || d.NodesRemoved[0].ID != "b" {
		t.Fatalf("expected node 'b' removed, got %+v", d.NodesRemoved)
	}
}

func TestComputeDetectsLabelChange(t *testing.T) {
	oldN := []schema.Node{{ID: "a", Label: "Auth"}}
	newN := []schema.Node{{ID: "a", Label: "Authentication"}}
	d := Compute(oldN, newN, nil, nil)
	if len(d.NodesChanged) != 1 {
		t.Fatalf("expected 1 changed node, got %d", len(d.NodesChanged))
	}
	if d.NodesChanged[0].Old.Label != "Auth" || d.NodesChanged[0].New.Label != "Authentication" {
		t.Errorf("change tuple wrong: %+v", d.NodesChanged[0])
	}
}

func TestComputeUnchangedNodeNotInChangedList(t *testing.T) {
	n := schema.Node{ID: "a", Label: "A", Community: "1", FileType: schema.FileTypeCode}
	d := Compute([]schema.Node{n}, []schema.Node{n}, nil, nil)
	if len(d.NodesChanged) != 0 {
		t.Fatalf("identical nodes shouldn't appear in NodesChanged: %+v", d.NodesChanged)
	}
}

func TestComputeIdentifiesAddedAndRemovedEdges(t *testing.T) {
	oldE := []schema.Edge{{Source: "a", Target: "b", Relation: "calls"}}
	newE := []schema.Edge{
		{Source: "a", Target: "b", Relation: "calls"},
		{Source: "a", Target: "c", Relation: "imports"},
	}
	d := Compute(nil, nil, oldE, newE)
	if len(d.EdgesAdded) != 1 || d.EdgesAdded[0].Target != "c" {
		t.Errorf("expected a→c added, got %+v", d.EdgesAdded)
	}
	if len(d.EdgesRemoved) != 0 {
		t.Errorf("nothing should be removed, got %+v", d.EdgesRemoved)
	}
}

func TestComputeEdgeIdentityIgnoresConfidence(t *testing.T) {
	// (source, target, relation) is the edge identity. A confidence
	// flip on the same edge is treated as no-change — there's still
	// "this connection" in both graphs, just with different metadata.
	// Documented as a v1 limitation in the package comment.
	oldE := []schema.Edge{{Source: "a", Target: "b", Relation: "calls", Confidence: schema.Extracted}}
	newE := []schema.Edge{{Source: "a", Target: "b", Relation: "calls", Confidence: schema.Inferred}}
	d := Compute(nil, nil, oldE, newE)
	if len(d.EdgesAdded) != 0 || len(d.EdgesRemoved) != 0 {
		t.Fatalf("confidence flip should not appear as add/remove: added=%v removed=%v", d.EdgesAdded, d.EdgesRemoved)
	}
}

func TestComputeDeterministicOrderingAcrossRuns(t *testing.T) {
	// Inputs deliberately in non-sorted order to flush out ordering
	// bugs in the diff output.
	oldN := []schema.Node{{ID: "z"}, {ID: "a"}, {ID: "m"}}
	newN := []schema.Node{{ID: "a"}, {ID: "n"}, {ID: "b"}, {ID: "y"}}
	d1 := Compute(oldN, newN, nil, nil)
	d2 := Compute(oldN, newN, nil, nil)
	if len(d1.NodesAdded) != len(d2.NodesAdded) {
		t.Fatal("length drift")
	}
	for i := range d1.NodesAdded {
		if d1.NodesAdded[i].ID != d2.NodesAdded[i].ID {
			t.Fatalf("ordering drift at %d: %q vs %q", i, d1.NodesAdded[i].ID, d2.NodesAdded[i].ID)
		}
	}
	// Added nodes should be sorted alphabetically.
	for i := 1; i < len(d1.NodesAdded); i++ {
		if d1.NodesAdded[i-1].ID > d1.NodesAdded[i].ID {
			t.Fatalf("NodesAdded not sorted: %+v", d1.NodesAdded)
		}
	}
}

func TestComputeEmptySnapshotsProduceEmptyDiff(t *testing.T) {
	d := Compute(nil, nil, nil, nil)
	if len(d.NodesAdded)+len(d.NodesRemoved)+len(d.NodesChanged)+len(d.EdgesAdded)+len(d.EdgesRemoved) != 0 {
		t.Fatalf("empty snapshots should produce empty diff, got %+v", d)
	}
}
