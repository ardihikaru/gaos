package main

import (
	"context"
	"testing"
)

// A product removed from the database must not free its number for the next
// product created. The ids used to be len(products)+1: delete p-1 from a floor
// that holds p-1..p-5 and the next product was p-5 a second time.
func TestAProductIdIsNeverReusedAfterADelete(t *testing.T) {
	ctx := context.Background()
	s := NewMemStore(1)
	// What a deletion in the graph leaves behind after the next boot: p-1 gone,
	// p-2..p-5 still there.
	s.products = s.products[1:]
	s.masters = s.masters[1:]

	seen := map[string]bool{}
	for _, p := range s.products {
		seen[p.ID] = true
	}
	for _, want := range []string{"p-6", "p-7"} {
		r, err := s.CreateProduct(ctx, ProductDraft{Name: "After the delete " + want})
		if err != nil {
			t.Fatalf("CreateProduct: %v", err)
		}
		if r.Master == nil || r.Master.ID != want {
			got := "<nil>"
			if r.Master != nil {
				got = r.Master.ID
			}
			t.Fatalf("new product id = %s, want %s: an id must be one past the highest in use, not the count plus one", got, want)
		}
		if seen[want] {
			t.Fatalf("new product id %s is already held by another product", want)
		}
		seen[want] = true
	}

	// An id of another shape is ignored, not read as a number.
	if got := nextProductID([]Product{{ID: "p-9"}, {ID: "p-x"}, {ID: "legacy"}, {ID: "p-"}}); got != "p-10" {
		t.Fatalf("nextProductID = %s, want p-10", got)
	}
	if got := nextProductID(nil); got != "p-1" {
		t.Fatalf("nextProductID(empty) = %s, want p-1", got)
	}
}
