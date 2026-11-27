package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

// TC-824: the binding (D-104 §3): its checks, one enabled product per kind, the event, the admin
// gate, the cache cleared on a change, and the Product document property under ArcadeDB.
func TestThirdPartyBinding(t *testing.T) {
	ctx := context.Background()
	st := NewMemStore(1)
	ps, _ := st.Products(ctx)
	if len(ps) < 2 {
		t.Fatalf("fixture: %d products, want two", len(ps))
	}
	a, b := ps[0].ID, ps[1].ID
	on := func(code string) ThirdPartyLink {
		return ThirdPartyLink{Kind: ThirdPartyTaskMgr, Enabled: true, ProductCode: code}
	}
	if _, err := st.SetThirdParty(ctx, a, ThirdPartyLink{Kind: "jira", Enabled: true, ProductCode: "DEMO"}, testEmail); !errors.Is(err, ErrRejected) {
		t.Errorf("an unknown kind gave %v, want a rejection", err)
	}
	for _, code := range []string{"", "-DEMO", "DE MO", "DEMO<b>", strings.Repeat("A", 33)} {
		if _, err := st.SetThirdParty(ctx, a, on(code), testEmail); !errors.Is(err, ErrRejected) {
			t.Errorf("the product code %q gave %v, want a rejection", code, err)
		}
	}
	if _, err := st.SetThirdParty(ctx, "p-404", on("DEMO"), testEmail); !errors.Is(err, ErrNotFound) {
		t.Errorf("an unknown product gave %v, want ErrNotFound", err)
	}
	res, err := st.SetThirdParty(ctx, a, on("DEMO"), testEmail)
	if err != nil || res.Master == nil || len(res.Master.ThirdParty) != 1 || len(res.NewEvents) != 1 {
		t.Fatalf("binding %s answered %+v %v", a, res, err)
	}
	ev := res.NewEvents[0]
	if ev.Type != ThirdPartyConfigured || ev.Payload.Rule != "third-party-config" || ev.Payload.Reason != "kind=taskmgr enabled=true code=DEMO" ||
		strings.Contains(ev.Payload.Actor, "@") {
		t.Errorf("the event is %s %+v; want ThirdPartyConfigured naming the values and the actor's alias", ev.Type, ev.Payload)
	}
	if _, err := st.SetThirdParty(ctx, b, on("OTHER"), testEmail); !errors.Is(err, ErrConflict) || !strings.Contains(err.Error(), a) {
		t.Errorf("a second enabled binding gave %v, want ErrConflict naming %s", err, a)
	}
	if _, err := st.SetThirdParty(ctx, b, ThirdPartyLink{Kind: ThirdPartyTaskMgr, ProductCode: "OTHER"}, testEmail); err != nil {
		t.Errorf("a disabled binding on a second product must be allowed: %v", err)
	}
	if _, err := st.SetThirdParty(ctx, a, ThirdPartyLink{Kind: ThirdPartyTaskMgr, ProductCode: "DEMO"}, testEmail); err != nil {
		t.Fatal(err)
	}
	if m, _ := st.Master(ctx, a); len(m.ThirdParty) != 1 || m.ThirdParty[0].Enabled || m.ThirdParty[0].ProductCode != "DEMO" {
		t.Errorf("disabling must keep the entry with enabled=false, got %+v", m.ThirdParty)
	}
	if _, err := st.SetThirdParty(ctx, b, on("OTHER"), testEmail); err != nil {
		t.Errorf("once the first is disabled the second may be enabled: %v", err)
	}
	if _, err := (readOnly{st}).SetThirdParty(ctx, a, on("DEMO"), testEmail); !errors.Is(err, ErrRejected) {
		t.Errorf("a read-only store gave %v, want a rejection", err)
	}

	// The route.
	srv, sst := newTestServer(t)
	srv.tm = newTaskMgrLink(sst, taskmgrConfig{})
	srv.tm.checks.put(taskCheckKey("DEMO", "DEMO-FEATURE-00001"), taskCheck{Result: "ok", At: time.Now()})
	form := url.Values{"kind": {"taskmgr"}, "enabled": {"on"}, "productCode": {"DEMO"}}
	path := "/products/" + a + "/third-party"
	t.Setenv("GAOS_AUTOFIX_ADMINS", "")
	if rec := do(t, srv, "POST", path, form); rec.Code != http.StatusUnauthorized && rec.Code != http.StatusSeeOther && rec.Code != http.StatusFound {
		t.Errorf("no session answered %d, want it refused", rec.Code)
	}
	if rec := do(t, srv, "POST", path, form, boCookies()...); rec.Code != http.StatusForbidden {
		t.Errorf("a signed-in account that is not an auto-fix admin answered %d, want 403", rec.Code)
	}
	autoFixAdminsFile(t, 0o600, testEmail)
	if rec := do(t, srv, "POST", path, form, boCookies()...); rec.Code != http.StatusOK {
		t.Fatalf("an admin's binding answered %d %s", rec.Code, rec.Body)
	}
	if m, _ := sst.Master(ctx, a); len(m.ThirdParty) != 1 || !m.ThirdParty[0].Enabled {
		t.Errorf("the route did not save the binding: %+v", m.ThirdParty)
	}
	if n := srv.tm.checks.len(); n != 0 {
		t.Errorf("the validation cache kept %d entr(ies) across a binding change", n)
	}
	if rec := do(t, srv, "POST", "/products/"+b+"/third-party", form, boCookies()...); rec.Code != http.StatusConflict {
		t.Errorf("a second enabled binding through the route answered %d, want 409", rec.Code)
	}
	bad := url.Values{"kind": {"taskmgr"}, "enabled": {"on"}, "productCode": {"DE MO"}}
	if rec := do(t, srv, "POST", "/products/"+b+"/third-party", bad, boCookies()...); rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("a bad code through the route answered %d, want 422", rec.Code)
	}

	// ArcadeDB: the binding is a schema-less Product document property, and the load reads it.
	var mu sync.Mutex
	var bodies []string
	g := &fakeGraph{}
	s, _, pid := arcadeOver(t, func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		mu.Lock()
		bodies = append(bodies, string(raw))
		mu.Unlock()
		r.Body = io.NopCloser(bytes.NewReader(raw))
		g.handler(w, r)
	})
	if _, err := s.SetThirdParty(ctx, pid, on("DEMO"), testEmail); err != nil {
		t.Fatalf("SetThirdParty under ArcadeDB: %v", err)
	}
	mu.Lock()
	all := strings.Join(bodies, "\n")
	mu.Unlock()
	if !strings.Contains(all, "thirdParty = :p") || !strings.Contains(all, `"productCode":"DEMO"`) || !strings.Contains(all, "INSERT INTO Event") {
		t.Errorf("the graph write carries no thirdParty property or no event: %.600s", all)
	}
	if !strings.Contains(arcadeProductSelect(), "thirdParty") {
		t.Error("the Product load does not select thirdParty")
	}
}
