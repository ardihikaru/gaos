package main

import (
	"context"
	"strings"
	"testing"
)

// T-168: the product list is the home page, and a product's page has a
// Timeline beside its Board and Blueprint (M14.7).

func TestTheHomePageIsTheProductListAndSignedOutItAsksForASignIn(t *testing.T) {
	srv, s := newTestServer(t)
	in := do(t, srv, "GET", "/", nil, boCookies()...).Body.String()
	ps, _ := s.Products(context.Background())
	if !strings.Contains(in, `id="boSheet"`) || strings.Contains(in, `id="boSheet" hidden`) || !strings.Contains(in, ps[0].Name) {
		t.Fatalf("signed in, the home page did not open on the product list naming %q; the product master is the home page (T-168)", ps[0].Name)
	}
	if strings.Contains(in, `id="carousel"`) || strings.Contains(in, `hx-get="ui/floor"`) {
		t.Fatal("the home page still carries the engineering floor or its ticker; the floor UI was retired in M14.7")
	}
	out := do(t, srv, "GET", "/", nil).Body.String()
	if strings.Contains(out, `id="loginSheet" hidden`) || !strings.Contains(out, `id="boSheet" hidden`) {
		t.Fatal("signed out, the home page did not show the sign-in sheet with the product master closed")
	}
}

func TestAProductsTimelineShowsItsOwnEventsAndNoOtherProducts(t *testing.T) {
	srv, s := newTestServer(t)
	ctx := context.Background()
	ms, _ := s.Masters(ctx)
	mine, other := ms[0], ms[1]
	before := len(s.rawEvents())
	if _, err := s.AddStory(ctx, mine.ID, Story{Role: "operator", Want: "show mine", So: "it shows"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddStory(ctx, other.ID, Story{Role: "operator", Want: "show theirs", So: "it does not"}); err != nil {
		t.Fatal(err)
	}
	var mineText, otherText string
	for _, e := range s.rawEvents()[before:] {
		switch e.DeskID {
		case mine.ID:
			mineText = e.Text
		case other.ID:
			otherText = e.Text
		}
	}
	body := do(t, srv, "GET", "/products/"+mine.ID+"?tab=timeline", nil, boCookies()...).Body.String()
	if mineText == "" || !strings.Contains(body, "ev-row") || !strings.Contains(body, mineText) {
		t.Fatalf("the Timeline tab of %s does not show %q, the event of the story just added to it", mine.ID, mineText)
	}
	if otherText == "" || strings.Contains(body, otherText) {
		t.Fatalf("the Timeline tab of %s shows %q, an event of %s; a product's timeline is its own", mine.ID, otherText, other.ID)
	}
}
