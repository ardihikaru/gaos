package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTheContactResolutionOrder(t *testing.T) {
	var b ContactBook
	_ = b.Add(Contact{GitHubLogin: "dev", DisplayName: "Dev Person", Destination: "6281234567890@s.whatsapp.net"})
	_ = b.Add(Contact{ProductID: "p-1", GitHubLogin: "dev", DisplayName: "Dev On P1", Destination: "111@g.us"})
	_ = b.Add(Contact{GitHubLogin: "quiet", Destination: "222@g.us", NotifyOn: []string{"e2e:failure"}})

	// The product's own contact wins over the every-product one.
	if d, a := resolveDestination(b, "p-1", "dev", "review:medium"); d != "111@g.us" || a != "dev-on-p1" {
		t.Errorf("got %q %q, want the product's own contact", d, a)
	}
	if d, _ := resolveDestination(b, "p-2", "DEV", "review:medium"); !strings.HasPrefix(d, "6281234567890") {
		t.Errorf("another product must fall back to the every-product contact, got %q", d)
	}
	// A contact that does not want this kind, an unknown account and no account
	// all go to the group (an empty destination).
	for name, login := range map[string]string{"unwanted kind": "quiet", "unknown": "stranger", "none": ""} {
		if d, a := resolveDestination(b, "p-1", login, "review:high"); d != "" || a != "group" {
			t.Errorf("%s: got %q %q, want the group", name, d, a)
		}
	}
	if d, _ := resolveDestination(b, "p-1", "quiet", "e2e:failure"); d != "222@g.us" {
		t.Errorf("a wanted kind must reach the contact, got %q", d)
	}
}

func TestAReviewKindIncludesTheWorseSeverities(t *testing.T) {
	c := Contact{NotifyOn: []string{"review:medium"}}
	for kind, want := range map[string]bool{"review:low": false, "review:medium": true, "review:high": true, "review:critical": true, "e2e:failure": false} {
		if got := notifyOnWants(c, kind); got != want {
			t.Errorf("review:medium wants %s = %v, want %v", kind, got, want)
		}
	}
	if !notifyOnWants(Contact{}, "unit:failure") {
		t.Error("a contact with no list wants everything")
	}
}

func TestADestinationIsMaskedWhenListedAndNeverInAnEvent(t *testing.T) {
	if got := maskDestination("120363000000000000@g.us"); got != "…@g.us" {
		t.Errorf("group mask = %q", got)
	}
	if got := maskDestination("6281234567890"); strings.Contains(got, "12345") {
		t.Errorf("number mask leaks: %q", got)
	}
	var b ContactBook
	_ = b.Add(Contact{GitHubLogin: "dev", Destination: "6281234567890@s.whatsapp.net"})
	if l := b.List(); strings.Contains(l[0].Destination, "6281234567890") {
		t.Errorf("List leaks the destination: %q", l[0].Destination)
	}
}

func TestAContactIsValidated(t *testing.T) {
	for name, c := range map[string]Contact{
		"no account":     {Destination: "1@g.us"},
		"no destination": {GitHubLogin: "dev"},
		"bad channel":    {GitHubLogin: "dev", Destination: "1@g.us", Channel: "email"},
		"bad notifyOn":   {GitHubLogin: "dev", Destination: "1@g.us", NotifyOn: []string{"everything"}},
	} {
		if err := ValidContact(c); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
}

func TestTheRegistryNeverReachesAnEventTheBlueprintOrAPacket(t *testing.T) {
	ctx := context.Background()
	st := NewMemStore(1)
	const secret = "6289999999999@s.whatsapp.net"
	res, err := st.UpsertContact(ctx, Contact{GitHubLogin: "dev", DisplayName: "Dev", Destination: secret}, "ops@example.com")
	if err != nil || len(res.Contacts) != 1 || len(res.NewEvents) != 0 {
		t.Fatalf("upsert: %+v %v; a contact change carries a registry op and no event", res, err)
	}
	if _, err := st.RecordNotification(ctx, "p-1", "dev", "review:high", "accepted"); err != nil {
		t.Fatal(err)
	}
	for _, e := range st.events {
		if strings.Contains(e.Text, secret) || strings.Contains(e.Text, "6289999") {
			t.Fatalf("event %d carries the destination: %q", e.Seq, e.Text)
		}
	}
	// The Master (which renders into the Blueprint) holds no contact.
	ms, _ := st.Masters(ctx)
	for _, m := range ms {
		blob, _ := jsonString(m)
		if strings.Contains(blob, secret) {
			t.Fatal("a Master carries the destination")
		}
	}
	// Delete.
	if _, err := st.DeleteContact(ctx, "", "dev", "ops@example.com"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := st.DeleteContact(ctx, "", "dev", "ops@example.com"); !errors.Is(err, ErrNotFound) {
		t.Errorf("a second delete answered %v, want not found", err)
	}
	if cs, _ := st.ListContacts(ctx); len(cs) != 0 {
		t.Errorf("%d contacts remain", len(cs))
	}
}

func jsonString(v any) (string, error) {
	b, err := json.Marshal(v)
	return string(b), err
}

func formPost(t *testing.T, srv *server, path, body string, withSession bool) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	if withSession {
		for _, c := range boCookies() {
			req.AddCookie(c)
		}
	}
	w := httptest.NewRecorder()
	srv.routes().ServeHTTP(w, req)
	return w
}

func TestTheContactRoutesNeedASessionAndNeverRenderTheDestination(t *testing.T) {
	srv, st := newTestServer(t)
	const dest = "6285550001111@s.whatsapp.net"
	form := "githubLogin=dev&displayName=Dev&destination=" + dest + "&notifyOn=review:medium"
	if w := formPost(t, srv, "/contacts", form, false); w.Code == http.StatusOK && len(mustContacts(t, st)) != 0 {
		t.Fatal("a contact was saved without a session")
	}
	w := formPost(t, srv, "/contacts", form, true)
	if w.Code != http.StatusOK {
		t.Fatalf("save answered %d: %s", w.Code, w.Body.String())
	}
	if len(mustContacts(t, st)) != 1 {
		t.Fatal("the contact was not saved")
	}
	if strings.Contains(w.Body.String(), "6285550001111") {
		t.Error("the page renders the destination whole")
	}
	if !strings.Contains(w.Body.String(), "…@s.whatsapp.net") {
		t.Error("the page does not show the masked destination")
	}
	// A refused value: a notifyOn outside the vocabulary.
	if w := formPost(t, srv, "/contacts", "githubLogin=x&destination=1@g.us&notifyOn=everything", true); len(mustContacts(t, st)) != 1 {
		t.Errorf("an invalid contact was saved (%d)", w.Code)
	}
	if w := formPost(t, srv, "/contacts/delete", "githubLogin=dev&productId=", true); w.Code != http.StatusOK || len(mustContacts(t, st)) != 0 {
		t.Errorf("delete answered %d, %d left", w.Code, len(mustContacts(t, st)))
	}
}

func mustContacts(t *testing.T, st Store) []Contact {
	t.Helper()
	cs, err := st.ListContacts(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return cs
}

func TestTheLabelRouteChangesALabelAndRefusesAnUnknownOne(t *testing.T) {
	srv, st := newTestServer(t)
	planned, _, _, err := st.PlanPullRequestCards(context.Background(), "p-1", prEvent(prTypeOpened, "laundry-api", 31))
	if err != nil {
		t.Fatal(err)
	}
	path := "/products/p-1/board/cards/" + planned[0] + "/label"
	if w := formPost(t, srv, path, "label="+LabelPRReviewManual, false); w.Code == http.StatusOK && hasLabel(st.taskByIDLocked(planned[0]).Labels, LabelPRReviewManual) {
		t.Fatal("a label changed without a session")
	}
	if w := formPost(t, srv, path, "label="+LabelPRReviewManual, true); w.Code != http.StatusOK {
		t.Fatalf("answered %d: %s", w.Code, w.Body.String())
	}
	if !hasLabel(prCards(t, st, "p-1")[planned[0]].Labels, LabelPRReviewManual) {
		t.Error("the label did not change")
	}
	before := len(st.events)
	formPost(t, srv, path, "label=invented", true)
	if len(st.events) != before {
		t.Error("a refused label wrote an event")
	}
}
