package graph_test

import (
	"slices"
	"testing"

	"github.com/shanghe000/ontology-workbench/examples"
	"github.com/shanghe000/ontology-workbench/internal/graph"
)

func TestEvidenceBudgetAndAnchors(t *testing.T) {
	snap, _, err := examples.Parcel()
	if err != nil || graph.Validate(snap) != nil {
		t.Fatalf("invalid example: %v", err)
	}
	narrow, err := graph.Explore(snap, "on-time", 1, 40)
	if err != nil || len(narrow.Steps) != 2 || narrow.Steps[1].Node.ID != "events" || narrow.Steps[1].Via != "on-time-scan" {
		t.Fatalf("direct evidence should outrank business relations: %+v, %v", narrow, err)
	}
	wide, err := graph.Explore(snap, "on-time", 6, 40)
	if err != nil || !slices.ContainsFunc(wide.Edges, func(e graph.Edge) bool {
		return e.To == "events" && e.Anchor == "carrier-accepted"
	}) || !slices.ContainsFunc(wide.Edges, func(e graph.Edge) bool {
		return e.To == "events" && e.Anchor == "delivery-confirmed"
	}) {
		t.Fatalf("shared source should preserve both edge anchors: %+v, %v", wide, err)
	}
	if _, err := graph.Explore(snap, "missing", 4, 40); err == nil {
		t.Fatal("traversal accepted a missing root")
	}
}

func TestCandidateRejectsCyclesAndInvalidSources(t *testing.T) {
	snap, _, err := examples.Parcel()
	if err != nil {
		t.Fatal(err)
	}
	for _, patch := range []graph.Patch{
		{Edges: []graph.Edge{{ID: "loop", From: "handoff-time", To: "on-time", Kind: "depends_on"}}},
		{Nodes: []graph.Node{{ID: "bad-source", Kind: "source", Label: "Invalid", Parent: "dispatch",
			Reference: &graph.Reference{Repository: "parcel-demo", Path: "../summary.md"}}}},
		{RemoveNodes: []string{"events"}},
	} {
		if _, err := graph.Apply(snap, patch); err == nil {
			t.Fatalf("invalid candidate accepted: %+v", patch)
		}
	}
}
