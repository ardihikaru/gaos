package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// D-082: the gateway deletes one session, and GAOS reports only what failed --
// never the session's id.
func TestTheGatewayDeletesASessionAndSaysOnlyWhatFailed(t *testing.T) {
	const id = "0f0e0d0c-0b0a-4909-8807-060504030201"
	var mu sync.Mutex
	var seen []string
	status := http.StatusOK
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		seen = append(seen, r.Method+" "+r.URL.RequestURI()+" "+r.Header.Get("Authorization"))
		w.WriteHeader(status)
	}))
	defer srv.Close()
	c := &AIMClient{Base: srv.URL, Creds: AIMCredentials{Login: "gaos", Secret: "s", AdminKey: "admin-KEY"}}
	ctx := context.Background()

	if err := c.DeleteSession(ctx, VendorClaude, id); err != nil {
		t.Fatalf("a 200 answered %v; want nil", err)
	}
	if err := c.DeleteSession(ctx, VendorAntigravity, id); err != nil {
		t.Fatalf("a 200 for antigravity answered %v; want nil", err)
	}
	want := []string{
		"DELETE /admin/sessions/" + id + " Bearer admin-KEY",
		"DELETE /admin/sessions/" + id + "?provider=antigravity Bearer admin-KEY",
	}
	if strings.Join(seen, "|") != strings.Join(want, "|") {
		t.Fatalf("the gateway saw %v; want %v -- the admin route, the vendor's worker, the admin bearer", seen, want)
	}
	for code, wantErr := range map[int]error{
		http.StatusNotFound: ErrSessionNotFound, http.StatusConflict: ErrSessionBusy,
		http.StatusInternalServerError: ErrGatewayTransient, http.StatusServiceUnavailable: ErrGatewayTransient,
	} {
		status = code
		err := c.DeleteSession(ctx, VendorClaude, id)
		if !errors.Is(err, wantErr) || strings.Contains(err.Error(), id) {
			t.Fatalf("a %d answered %v; want %v, with no session id in it", code, err, wantErr)
		}
	}
	srv.Close()
	if err := c.DeleteSession(ctx, VendorClaude, id); !errors.Is(err, ErrGatewayTransient) || strings.Contains(err.Error(), id) {
		t.Fatalf("an unreachable gateway answered %v; want ErrGatewayTransient with no session id", err)
	}
	noKey := &AIMClient{Base: srv.URL}
	if err := noKey.DeleteSession(ctx, VendorClaude, id); !errors.Is(err, ErrNoAdminKey) {
		t.Fatalf("no admin key answered %v; want ErrNoAdminKey", err)
	}

	// prune never sends a malformed id or an unknown vendor
	jan := &fakeJanitor{}
	s := &MemStore{adapter: jan}
	recs := s.deleteSessions(ctx, []pruneTarget{{Card: CardRef{"p", "t"}, Named: 3, Sessions: []sessionRef{
		{Vendor: VendorClaude, ID: "../workspaces/x"}, {Vendor: "codex", ID: id}, {Vendor: VendorClaude, ID: id},
	}}})
	if r := recs[CardRef{"p", "t"}]; len(jan.calls) != 1 || r.SessionsFailed != 2 || r.SessionsDeleted != 1 {
		t.Fatalf("prune sent %v and counted %+v; want only the well-formed claude id sent, two failed", jan.calls, r)
	}
}
