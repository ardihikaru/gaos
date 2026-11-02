package main

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"testing"
	"time"
)

// M16: a vendor policy for each product (D-067, BR-45). Against the fake
// ai-manager in aimanager_test.go, which keeps each vendor's copy of a
// workspace apart and lists an antigravity provider with three models.

func mixedBackend() VendorPolicy {
	return VendorPolicy{Mode: VendorModeMixed, Roles: map[string]string{"backend": VendorAntigravity, "architect": VendorClaude}}
}

func TestAProductRunsOnClaudeUntilItsPolicySaysOtherwise(t *testing.T) {
	var p VendorPolicy
	for _, r := range roleOrder {
		if v := p.VendorFor(r); v != VendorClaude {
			t.Fatalf("a product with no vendor policy sends its %s tasks to %q; want claude, what every product ran on before M16", r, v)
		}
	}
	if got := p.Vendors(); len(got) != 1 || got[0] != VendorClaude {
		t.Fatalf("a product with no vendor policy dispatches to %v; want only claude", got)
	}
	f, ad := newFakeAIM(t)
	completeTurn(f, "T-1", 1, "0.1")
	if _, err := ad.Dispatch(context.Background(), gatewayCleared(t)); err != nil {
		t.Fatal(err)
	}
	if b := f.bodies[0]; b.Provider != "" || b.Model != "claude-sonnet-5" {
		t.Fatalf("a Claude turn was sent with provider %q and model %q; want no provider and GAOS_MODEL, the body it had before M16", b.Provider, b.Model)
	}
}

func TestMixedModeResolvesTheVendorByRole(t *testing.T) {
	p := mixedBackend()
	for role, want := range map[string]string{"backend": VendorAntigravity, "architect": VendorClaude, "qa": VendorClaude} {
		if got := p.VendorFor(role); got != want {
			t.Fatalf("in mixed mode the %s role resolves to %q; want %q (a role the map does not name runs on Claude)", role, got, want)
		}
	}
	if got := p.Vendors(); strings.Join(got, ",") != "claude,antigravity" {
		t.Fatalf("mixed mode with one role on Antigravity dispatches to %v; want [claude antigravity]", got)
	}
	s := NewMemStore(1)
	ctx := context.Background()
	id := s.desks[0].ID
	if _, err := s.SaveVendorPolicy(ctx, id, p); err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	s.tasks = append(s.tasks, TaskRecord{ID: "t-be", DeskID: id, Role: "backend", Status: StatusPending},
		TaskRecord{ID: "t-sa", DeskID: id, Role: "architect", Status: StatusPending})
	s.mu.Unlock()
	b, err := s.Board(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range b.Cards {
		if want := p.VendorFor(c.Role); (c.ID == "t-be" || c.ID == "t-sa") && c.Vendor != want {
			t.Fatalf("the board shows card %s (%s) on %q; want %q, where its next turn goes", c.ID, c.Role, c.Vendor, want)
		}
	}
}

func TestAnUnknownModeRoleOrVendorIsRefused(t *testing.T) {
	s := NewMemStore(1)
	ctx := context.Background()
	id := s.desks[0].ID
	for _, bad := range []VendorPolicy{
		{Mode: "openai"},
		{Mode: VendorModeMixed, Roles: map[string]string{"janitor": VendorClaude}},
		{Mode: VendorModeMixed, Roles: map[string]string{"backend": "minimax"}},
	} {
		if _, err := s.SaveVendorPolicy(ctx, id, bad); !errors.Is(err, ErrRejected) {
			t.Fatalf("the policy %+v was accepted (%v); want a rejection naming what is wrong", bad, err)
		}
	}
	if m, _ := s.Master(ctx, id); m.Vendors.ModeOrDefault() != VendorModeClaude {
		t.Fatalf("a refused policy still changed the product to %+v", m.Vendors)
	}
}

func TestAnAntigravityModelTheGatewayDoesNotOfferIsRefused(t *testing.T) {
	s, _, _, id := turnStore(t)
	ctx := context.Background()
	_, err := s.SaveVendorPolicy(ctx, id, VendorPolicy{Mode: VendorModeAntigravity, AgyModel: "gpt-9"})
	if err == nil || !strings.Contains(err.Error(), `Antigravity does not offer the model "gpt-9"`) {
		t.Fatalf("a model the pool does not list was saved (%v); want the gateway's list to refuse it", err)
	}
	if _, err := s.SaveVendorPolicy(ctx, id, VendorPolicy{Mode: VendorModeAntigravity, AgyModel: "gemini-3.8-flash-low"}); err != nil {
		t.Fatalf("a model the pool lists was refused: %v", err)
	}
	if m, _ := s.Master(ctx, id); m.Vendors.AgyModel != "gemini-3.8-flash-low" {
		t.Fatalf("the saved policy is %+v; want the model the operator picked", m.Vendors)
	}
}

func TestAnAntigravityTurnNamesTheVendorAndItsModel(t *testing.T) {
	s, f, ad, id := turnStore(t)
	ctx := context.Background()
	if _, err := s.SaveVendorPolicy(ctx, id, VendorPolicy{Mode: VendorModeAntigravity, AgyModel: "gemini-3.8-flash-low"}); err != nil {
		t.Fatal(err)
	}
	completeTurn(f, "t-1", 1, "0.1")
	if _, err := s.superviseRun(ctx, ad, id, clearedForRole(t, s, id, "t-1", "backend", 1), 1); err != nil {
		t.Fatal(err)
	}
	if b := f.bodies[0]; b.Provider != VendorAntigravity || b.Model != "gemini-3.8-flash-low" || !b.NewSession {
		t.Fatalf("an Antigravity turn was sent as provider %q model %q new_session %v; want antigravity, the product's model, a new session", b.Provider, b.Model, b.NewSession)
	}
	if r := waitForRun(t, s, runIDFor("t-1", 1), RunStatusExited); r.Provider != VendorAntigravity {
		t.Fatalf("the run recorded provider %q; want antigravity, so the next attempt knows where its session lives", r.Provider)
	}
	// The resumed turn names the vendor too: a session lives with the
	// provider that opened it, and ai-manager defaults to claude.
	if _, err := ad.turn(ctx, clearedForRole(t, s, id, "t-1", "backend", 2), "3f0c0000-0000-0000-0000-000000000001"); err != nil {
		t.Fatal(err)
	}
	if b := f.bodies[len(f.bodies)-1]; b.Provider != VendorAntigravity || b.SessionID == "" {
		t.Fatalf("a resumed Antigravity turn was sent as provider %q session %q; want antigravity and the session", b.Provider, b.SessionID)
	}
}

func TestAVendorChangeOpensANewSession(t *testing.T) {
	s, _, ad, id := turnStore(t)
	ctx := context.Background()
	s.mu.Lock()
	s.runs = append(s.runs, TaskRunRecord{ID: runIDFor("t-v", 1), TaskID: "t-v", DeskID: id, Attempt: 1, Status: RunStatusExited,
		Model: "claude-sonnet-5", Provider: VendorClaude, SessionID: "s-claude", StartedAt: time.Now().Unix()})
	s.mu.Unlock()
	if _, err := s.SaveVendorPolicy(ctx, id, VendorPolicy{Mode: VendorModeAntigravity}); err != nil {
		t.Fatal(err)
	}
	s.mu.RLock()
	v, m := s.vendorForLocked(id, "backend")
	sid, why := s.sessionForLocked("t-v", ad, v, m, time.Now().Unix())
	s.mu.RUnlock()
	if sid != "" || why != SessionVendor {
		t.Fatalf("after the product moved to Antigravity the retry resumes %q (reason %q); want a new session, reason %q (BR-45)", sid, why, SessionVendor)
	}
}

func TestASessionResumesOnTheVendorThatOpenedIt(t *testing.T) {
	s, _, ad, id := turnStore(t)
	now := time.Now().Unix()
	s.mu.Lock()
	// t-old ran before M16: no provider, and it went to Claude.
	s.runs = append(s.runs,
		TaskRunRecord{ID: runIDFor("t-old", 1), TaskID: "t-old", DeskID: id, Attempt: 1, Status: RunStatusExited,
			Model: "claude-sonnet-5", SessionID: "s-old", StartedAt: now},
		TaskRunRecord{ID: runIDFor("t-agy", 1), TaskID: "t-agy", DeskID: id, Attempt: 1, Status: RunStatusExited,
			Model: "gemini-3.8-flash-high", Provider: VendorAntigravity, SessionID: "s-agy", StartedAt: now})
	sid, why := s.sessionForLocked("t-old", ad, VendorClaude, "", now)
	agy, agyWhy := s.sessionForLocked("t-agy", ad, VendorAntigravity, "", now)
	s.mu.Unlock()
	if sid != "s-old" {
		t.Fatalf("a run recorded before M16 under a Claude policy opens a new session (reason %q); want it resumed, it went to Claude", why)
	}
	if agy != "s-agy" {
		t.Fatalf("an Antigravity retry with no model chosen opens a new session (reason %q); want it resumed -- GAOS_MODEL is not Antigravity's to escalate from", agyWhy)
	}
}

func TestTheBlueprintGateHoldsAProductWhoseAntigravityCopyIsStale(t *testing.T) {
	s, _, id := publishedStore(t)
	ctx := context.Background()
	if _, err := s.SyncBlueprint(ctx, id); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveVendorPolicy(ctx, id, mixedBackend()); err != nil {
		t.Fatal(err)
	}
	r, err := s.DispatchTask(ctx, s.deskIdxByID(id))
	refused := eventsOf(r, DispatchRefused)
	if err != nil || len(refused) != 1 || refused[0].Payload.Rule != RuleBlueprintDrift ||
		!strings.Contains(refused[0].Payload.Reason, "the workspace on Antigravity holds no Blueprint") {
		t.Fatalf("dispatching with Antigravity's copy unsynced gave %v %v; want one DispatchRefused rule %s naming Antigravity (BR-39)", r.NewEvents, err, RuleBlueprintDrift)
	}
	if _, err := s.SyncBlueprint(ctx, id); err != nil {
		t.Fatal(err)
	}
	s.mu.RLock()
	pin, drift := s.blueprintGate(id)
	s.mu.RUnlock()
	if drift != nil || pin != 1 {
		t.Fatalf("after a sync to both vendors the gate says %v (pin %d); want it open at version 1", drift, pin)
	}
}

func TestASyncWritesEveryVendorThePolicyUses(t *testing.T) {
	s, f, id := publishedStore(t)
	ctx := context.Background()
	if _, err := s.SyncBlueprint(ctx, id); err != nil {
		t.Fatal(err)
	}
	if len(f.vendorCalls) != 0 {
		t.Fatalf("a Claude-only product's sync named a vendor: %v; want the calls it made before M16", f.vendorCalls)
	}
	if _, err := s.SaveVendorPolicy(ctx, id, mixedBackend()); err != nil {
		t.Fatal(err)
	}
	r, err := s.SyncBlueprint(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	ws := "gaos-t1-" + id
	if f.spaces[ws] == nil || f.spaces[vendorSpace(VendorAntigravity, ws)] == nil {
		t.Fatalf("after the sync the gateway holds Claude's copy %v and Antigravity's %v; want both", f.spaces[ws] != nil, f.spaces[vendorSpace(VendorAntigravity, ws)] != nil)
	}
	if !strings.Contains(r.Toast, "on Claude CLI and Antigravity") {
		t.Fatalf("the sync said %q; want it to name the vendors it wrote", r.Toast)
	}
	m, _ := s.Master(ctx, id)
	if h := m.Workspace.Holds(VendorAntigravity); h.Version != 1 {
		t.Fatalf("the record says Antigravity's copy holds version %d; want 1", h.Version)
	}
}

func TestAPoolThatFailsTheSyncLeavesTheProductHeldAndNeverFallsBack(t *testing.T) {
	s, f, id := publishedStore(t)
	ctx := context.Background()
	if _, err := s.SaveVendorPolicy(ctx, id, mixedBackend()); err != nil {
		t.Fatal(err)
	}
	f.agyDown = true
	r, err := s.SyncBlueprint(ctx, id)
	if err != nil || !strings.Contains(r.Toast, "Not synced — Antigravity:") {
		t.Fatalf("a sync whose Antigravity pool failed gave %q %v; want Claude synced and the failure named", r.Toast, err)
	}
	m, _ := s.Master(ctx, id)
	if m.Workspace.Holds(VendorClaude).Version != 1 || m.Workspace.Holds(VendorAntigravity).Version != 0 {
		t.Fatalf("the record says Claude holds v%d and Antigravity v%d; want 1 and 0", m.Workspace.Holds(VendorClaude).Version, m.Workspace.Holds(VendorAntigravity).Version)
	}
	s.mu.RLock()
	_, drift := s.blueprintGate(id)
	s.mu.RUnlock()
	if drift == nil {
		t.Fatal("the gate is open while Antigravity's copy holds nothing; a backend task would run on rules it never saw, or on Claude instead")
	}
}

func TestTheVendorPolicyAndEachRunsVendorSurviveAReopen(t *testing.T) {
	arc := arcadeTestTarget(t)
	ctx := t.Context()
	tenant := "conf-vendor-restart"
	first := newArcadeTestStore(t, arc, tenant)
	st := first.(*ArcadeStore)
	f, ad := newFakeAIM(t)
	snap, _ := first.Snapshot(ctx)
	desk := snap.Desks[0].ID
	st.SetAdapter(ad)
	p := VendorPolicy{Mode: VendorModeAntigravity, AgyModel: "gemini-3.8-flash-low"}
	if _, err := first.SaveVendorPolicy(ctx, desk, p); err != nil {
		t.Fatalf("save: %v", err)
	}
	run := runIDFor("t-1", 1)
	f.reply = chatReply(handoffBlock("t-1", run), "0.2", "")
	st.proj.mu.RLock()
	v, model := st.proj.vendorForLocked(desk, "backend")
	st.proj.mu.RUnlock()
	c, _, err := Screen(EgressPolicy{}, Target{Provider: "anthropic"}, Packet{TaskID: "t-1", RunID: run, DeskID: desk, Role: "backend",
		Vendor: v, Model: model, Parts: []PacketPart{{Path: ".gaos/packet/task.md", Kind: KindTask, Body: "# t-1"}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.proj.superviseRun(ctx, ad, desk, c, 1); err != nil {
		t.Fatalf("supervise: %v", err)
	}
	second, err := NewArcadeStoreFor(ctx, arc, 1, tenant)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	m, _ := second.Master(ctx, desk)
	if m.Vendors.ModeOrDefault() != VendorModeAntigravity || m.Vendors.AgyModel != p.AgyModel {
		t.Fatalf("the reopened product's vendor policy is %+v; want %+v", m.Vendors, p)
	}
	runs, _ := second.Runs(ctx, "t-1")
	if len(runs) != 1 || runs[0].Provider != VendorAntigravity {
		t.Fatalf("the reopened attempt is %+v; want provider antigravity, or its retry would resume a session on the wrong vendor", runs)
	}
}

func TestAPoolWhoseCopiesDisagreeIsRepairedByTheSync(t *testing.T) {
	s, f, id := publishedStore(t)
	ctx := context.Background()
	if _, err := s.SaveVendorPolicy(ctx, id, VendorPolicy{Mode: VendorModeAntigravity}); err != nil {
		t.Fatal(err)
	}
	f.agySplit = true
	if _, err := s.SyncBlueprint(ctx, id); err != nil {
		t.Fatalf("a sync over a pool whose members disagree failed: %v; want it provisioned and rewritten on every member", err)
	}
	if f.agySplit {
		t.Fatal("the sync never wrote the pool unconditionally, so its members still disagree")
	}
	want := []string{"antigravity GET", "antigravity POST", "antigravity PUT"}
	for i, w := range want {
		if i >= len(f.vendorCalls) || !strings.HasPrefix(f.vendorCalls[i], w) {
			t.Fatalf("the sync made the calls %v; want %v -- provisioning is idempotent and creates the missing copies before the write", f.vendorCalls, want)
		}
	}
	s.mu.RLock()
	_, drift := s.blueprintGate(id)
	s.mu.RUnlock()
	if drift != nil {
		t.Fatalf("after the repair the gate still says %v", drift)
	}
}

// vendorServer is the test server with the fake gateway attached to both the
// store and the server, the way main wires a configured adapter.
func vendorServer(t *testing.T) (*server, *MemStore) {
	t.Helper()
	s, st := newTestServer(t)
	_, ad := newFakeAIM(t)
	st.SetAdapter(ad)
	s.adapter = ad
	return s, st
}

func TestTheVendorsTabRendersThePolicyAndTheCatalog(t *testing.T) {
	s, _ := vendorServer(t)
	w := do(t, s, "GET", "/products/p-1?tab=vendors", nil, boCookies()...)
	body := w.Body.String()
	for _, want := range []string{
		"Which AI runs this product's tasks",
		`name="mode" value="claude" checked`,
		`name="role.backend" value="antigravity" checked`, // mixed mode's starting point
		"default (gemini-3.8-flash-high)",
		`<option value="gemini-3.8-flash-low"`,
		"Changes apply to each task's next new session",
	} {
		if w.Code != 200 || !strings.Contains(body, want) {
			t.Fatalf("GET the vendors tab = %d without %q; want the policy, the roles and the pool's models", w.Code, want)
		}
	}
	if !strings.Contains(body, `hx-put="products/p-1/vendors"`) {
		t.Fatal("the vendors tab has no form that saves to PUT /products/p-1/vendors")
	}
}

func TestPuttingTheVendorsFormSavesItAndRefusesABadOne(t *testing.T) {
	s, st := vendorServer(t)
	form := url.Values{"mode": {"mixed"}, "role.backend": {"antigravity"}, "role.architect": {"claude"}, "agyModel": {"gemini-3.8-flash-low"}}
	w := do(t, s, "PUT", "/products/p-1/vendors", form, boCookies()...)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "AI vendors saved: mixed by role, backend on Antigravity (gemini-3.8-flash-low).") {
		t.Fatalf("PUT the vendors form = %d; want 200 and the saved policy in the toast: %s", w.Code, w.Body)
	}
	m, _ := st.Master(context.Background(), "p-1")
	if m.Vendors.Mode != VendorModeMixed || m.Vendors.VendorFor("backend") != VendorAntigravity || m.Vendors.AgyModel != "gemini-3.8-flash-low" {
		t.Fatalf("the form saved %+v; want mixed, backend on Antigravity, gemini-3.8-flash-low", m.Vendors)
	}
	w = do(t, s, "PUT", "/products/p-1/vendors", url.Values{"mode": {"openai"}}, boCookies()...)
	if !strings.Contains(w.Body.String(), "The vendor mode must be claude, antigravity or mixed") {
		t.Fatalf("PUT a mode outside the three = %d without the refusal: %s", w.Code, w.Body)
	}
	if m, _ := st.Master(context.Background(), "p-1"); m.Vendors.Mode != VendorModeMixed {
		t.Fatalf("a refused form changed the policy to %+v", m.Vendors)
	}
}
