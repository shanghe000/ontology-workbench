package catalog_test

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"

	"github.com/shanghe000/ontology-workbench/examples"
	"github.com/shanghe000/ontology-workbench/internal/catalog"
	"github.com/shanghe000/ontology-workbench/internal/graph"
)

func TestConcurrentReviewPublishesExactlyOneSnapshot(t *testing.T) {
	ctx := context.Background()
	filename := filepath.Join(t.TempDir(), "data.sqlite")
	first, err := catalog.Open(filename)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := catalog.Open(filename)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	snapshot, patch, err := examples.Parcel()
	if err != nil {
		t.Fatal(err)
	}
	if err := first.EnsureSpace(ctx, "parcel", "Parcel", snapshot); err != nil {
		t.Fatal(err)
	}
	if err := first.EnsureSpace(ctx, "empty", "Empty", graph.Empty()); err != nil {
		t.Fatal(err)
	}
	input := catalog.Submission{RequestID: "candidate-a", BaseRevision: &snapshot.Revision, Title: "Review A", Patch: patch}
	a, err := first.Submit(ctx, "parcel", input)
	if err != nil {
		t.Fatal(err)
	}
	retry, err := second.Submit(ctx, "parcel", input)
	if err != nil || retry.ID != a.ID {
		t.Fatalf("idempotent submit: %+v, %v", retry, err)
	}
	input.Title = "A different request"
	if _, err := first.Submit(ctx, "parcel", input); !errors.Is(err, catalog.ErrConflict) {
		t.Fatalf("changed content reused idempotency ID: %v", err)
	}
	input.RequestID = "candidate-b"
	b, err := second.Submit(ctx, "parcel", input)
	if err != nil {
		t.Fatal(err)
	}
	before, err := second.Graph(ctx, "parcel", nil)
	if err != nil || before.Revision != 1 || len(before.Nodes) != len(snapshot.Nodes) {
		t.Fatalf("draft escaped to head: %+v, %v", before, err)
	}
	if _, _, err := first.Preview(ctx, "empty", a.ID); !errors.Is(err, catalog.ErrMissing) {
		t.Fatalf("proposal escaped its space: %v", err)
	}
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for i, p := range []catalog.Proposal{a, b} {
		wg.Add(1)
		go func(s *catalog.Store, id string) {
			defer wg.Done()
			_, err := s.Review(ctx, "parcel", id, "approve")
			results <- err
		}([]*catalog.Store{first, second}[i], p.ID)
	}
	wg.Wait()
	close(results)
	published, conflicts := 0, 0
	for err := range results {
		switch {
		case err == nil:
			published++
		case errors.Is(err, catalog.ErrConflict):
			conflicts++
		default:
			t.Fatal(err)
		}
	}
	if published != 1 || conflicts != 1 {
		t.Fatalf("unexpected outcome: published=%d conflicts=%d", published, conflicts)
	}
	head, err := second.Graph(ctx, "parcel", nil)
	if err != nil || head.Revision != 2 || len(head.Nodes) != len(snapshot.Nodes)+1 {
		t.Fatalf("incorrect new head: %+v, %v", head, err)
	}
	old, err := first.Graph(ctx, "parcel", &snapshot.Revision)
	if err != nil || old.Revision != 1 || len(old.Nodes) != len(before.Nodes) {
		t.Fatalf("prior revision was modified: %+v, %v", old, err)
	}
}
