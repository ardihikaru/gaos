package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"
)

// The back office's Integrations view (D-085, docs/designs/open-source-docs.md
// §3.6). Every secret is a random sentinel made per run, so no needle can occur
// by accident in ordinary markup.

func sentinel(t *testing.T) string {
	t.Helper()
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(b)
}

// integrationsSection cuts the view out of the sheet, so a word elsewhere in
// the page cannot satisfy an assertion about the view.
func integrationsSection(body string) string {
	i := strings.Index(body, `<section id="boIntegrations">`)
	if i < 0 {
		return ""
	}
	j := strings.Index(body[i:], `</section>`)
	if j < 0 {
		return body[i:]
	}
	return body[i : i+j]
}

type integrationsFixture struct {
	srv                       *server
	secrets                   []string
	qaHost, qaPath, qaBodySen string
}

func newIntegrationsFixture(t *testing.T) integrationsFixture {
	t.Helper()
	srv, st, pids := apiFixture(t, func([]string) *apiConfig { return nil })
	f := integrationsFixture{srv: srv}

	tokA, tokQ, tokO, tokC := "gaos_"+sentinel(t), "gaos_"+sentinel(t), "gaos_"+sentinel(t), "gaos_"+sentinel(t)
	srv.api = newAPIConfig(tokA, tokQ, tokO, pids[0]+",p-2")
	srv.api.setQAOrigin("qa.example.org")
	srv.api.addClients([]*apiClient{{Name: "mobile-app", Kind: "mobile", Origin: "app.example.org",
		sum: sumOf(tokC), products: map[string]bool{"p-9": true}, scope: scopeClient}}, nil, func(string, ...any) {})
	if len(srv.api.clients) != 2 {
		t.Fatalf("fixture: %d clients admitted, want qa-tools and mobile-app", len(srv.api.clients))
	}

	f.qaBodySen, f.qaPath = sentinel(t), sentinel(t)
	qaSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(f.qaBodySen))
	}))
	t.Cleanup(qaSrv.Close)
	srv.qaOut = newQaOutbound(qaSrv.URL+"/"+f.qaPath, tokO, nil)
	if srv.qaOut.why != "" {
		t.Fatalf("fixture: the QA outbound is off: %s", srv.qaOut.why)
	}
	f.qaHost = qaSrv.URL // http://127.0.0.1:<port>

	login, secret, admin, aimPath, aimQuery := sentinel(t), sentinel(t), sentinel(t), sentinel(t), sentinel(t)
	ad := &AIMAdapter{Client: &AIMClient{Base: "https://aim.example.org:8443/" + aimPath + "?q=" + aimQuery,
		Creds: AIMCredentials{Login: login, Secret: secret, AdminKey: admin}}}
	ad.vendors.infos = []VendorInfo{{Name: "claude", Available: true}}
	ad.vendors.at = time.Now()
	srv.adapter = ad
	srv.bootChecked = time.Now().Add(-time.Hour)
	t.Setenv("GAOS_RUNNER", "manual")

	// One QA item asked through the route, answered in the back office, and
	// one delivery attempted against the failing QA server.
	if rec := call(t, srv, "POST", inboxPath(pids[0]), tokQ, askJSON("run-42")); rec.Code != 201 {
		t.Fatalf("fixture: the ask answered %d %s", rec.Code, rec.Body)
	}
	if _, err := st.AnswerExternal(context.Background(), pids[0], "run-42",
		ExternalAnswer{Choice: 1, By: AnswerSource{Kind: SourceKindGAOS, Client: ClientBackOffice, Actor: "operator"}}); err != nil {
		t.Fatalf("fixture: the answer: %v", err)
	}
	if n := newInboxWorker(st, srv.qaOut).RunOnce(context.Background(), time.Now().Unix()); n != 1 {
		t.Fatalf("fixture: the worker attempted %d deliveries, want 1", n)
	}

	for _, tok := range []string{tokA, tokQ, tokO, tokC} {
		sum := sha256.Sum256([]byte(tok))
		f.secrets = append(f.secrets, tok, hex.EncodeToString(sum[:]))
	}
	f.secrets = append(f.secrets, login, secret, admin, aimPath, aimQuery, f.qaPath, f.qaBodySen)
	return f
}

func TestTheIntegrationsViewShowsEachIntegrationAndNoSecret(t *testing.T) {
	f := newIntegrationsFixture(t)
	cs := boCookies()

	for _, path := range []string{"/ui/bo/integrations", "/ui/bo?view=integrations"} {
		rec := do(t, f.srv, "GET", path, nil, cs...)
		if rec.Code != 200 {
			t.Fatalf("the integrations view answered %d on %s", rec.Code, path)
		}
		if cc := rec.Header().Get("Cache-Control"); cc != "no-store" {
			t.Errorf("the %s view on %s is not no-store: %q", "integrations", path, cc)
		}
		body := rec.Body.String()
		if !strings.Contains(body, "Integrations</h2>") {
			t.Errorf("the integrations view does not show %q", "Integrations</h2>")
		}
		sec := integrationsSection(body)
		for _, want := range []string{"https://aim.example.org:8443", "manual", "claude", f.qaHost, "qa.example.org",
			"mobile-app", "mobile", "app.example.org", "p-9", "p-2", "retry", "the QA tools answered HTTP 500", "configured",
			"passed at boot"} {
			if !strings.Contains(sec, want) {
				t.Errorf("the integrations view does not show %q", want)
			}
		}
		for _, leak := range f.secrets {
			if strings.Contains(body, leak) {
				t.Errorf("the integrations view leaked %q", leak)
			}
		}
	}

	// The archive gains no-store on the sheet route too.
	for _, path := range []string{"/ui/bo/archive", "/ui/bo?view=archive"} {
		rec := do(t, f.srv, "GET", path, nil, cs...)
		if cc := rec.Header().Get("Cache-Control"); cc != "no-store" {
			t.Errorf("the %s view on %s is not no-store: %q", "archive", path, cc)
		}
	}

	// urlHost refuses what url.Parse could misread, and anything not http(s).
	for _, raw := range []string{"https://" + sentinel(t) + ":a/b@aim.example.org/", "ftp://aim.example.org/"} {
		if got := urlHost(raw); got != "" {
			t.Errorf("urlHost(%q) = %q, want \"\"", raw, got)
		}
	}
}

func TestTheIntegrationsViewNeedsASessionAndSaysWhatIsMissing(t *testing.T) {
	f := newIntegrationsFixture(t)
	for _, hx := range []bool{true, false} {
		req := httptest.NewRequest("GET", "/ui/bo/integrations", nil)
		if hx {
			req.Header.Set("HX-Request", "true")
		}
		rec := httptest.NewRecorder()
		f.srv.routes().ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("an unsigned request to %s answered %d", "/ui/bo/integrations", rec.Code)
		}
		if b := rec.Body.String(); strings.Contains(b, "Integrations</h2>") || strings.Contains(b, "mobile-app") ||
			strings.Contains(b, "app.example.org") || strings.Contains(b, f.qaHost) {
			t.Errorf("an unsigned request to %s rendered the view", "/ui/bo/integrations")
		}
	}
	rec := do(t, f.srv, "GET", "/ui/bo?view=integrations", nil)
	if b := rec.Body.String(); strings.Contains(b, "Integrations</h2>") || strings.Contains(b, "mobile-app") ||
		strings.Contains(b, "app.example.org") || strings.Contains(b, f.qaHost) {
		t.Errorf("an unsigned request to %s rendered the view", "/ui/bo?view=integrations")
	}

	// Nothing configured: nil api, qaOut and adapter.
	s, _ := newTestServer(t)
	t.Setenv("GAOS_RUNNER", "")
	rec = do(t, s, "GET", "/ui/bo/integrations", nil, boCookies()...)
	if rec.Code != 200 {
		t.Fatalf("an unconfigured integrations view answered %d", rec.Code)
	}
	sec := integrationsSection(rec.Body.String())
	for _, want := range []string{"unconfigured", "GAOS_QA_URL is not set", "disabled (no API token is configured)",
		"No machine client is registered.", "no delivery has been attempted", "no turn has run"} {
		if !strings.Contains(sec, want) {
			t.Errorf("the unconfigured integrations view does not say %q", want)
		}
	}
}

// ---- the status chips, the requirement chips and the info popovers (D-098,
// docs/designs/integrations-status-chips.md) ----

func TestIntegChipsFollowTheState(t *testing.T) {
	type want struct{ label, tone string }
	okPR := func(p *IntegrationsPage) {
		p.PR.GitHub = "on"
		p.BrainTownConsumer = BrainTownRow{State: "enabled"}
		p.PR.StagingState, p.PR.WA.State = "disabled", "disabled"
	}
	cases := []struct {
		name  string
		set   func(*IntegrationsPage)
		chip  func(*IntegrationsPage) StatusChip
		want  want
		never string // a tone the chip must not take ("" = no such check)
	}{
		{"ai-manager configured", func(p *IntegrationsPage) { p.AIM.Configured = true }, func(p *IntegrationsPage) StatusChip { return p.AIMChip }, want{"Connected", toneOK}, ""},
		{"ai-manager off", func(p *IntegrationsPage) {}, func(p *IntegrationsPage) StatusChip { return p.AIMChip }, want{"Off", toneBad}, ""},
		{"brain town enabled", func(p *IntegrationsPage) { p.BrainTown.State = "enabled" }, func(p *IntegrationsPage) StatusChip { return p.BrainTownChip }, want{"Enabled", toneOK}, ""},
		{"brain town enabled but failing", func(p *IntegrationsPage) { p.BrainTown = BrainTownRow{State: "enabled", Failing: true} }, func(p *IntegrationsPage) StatusChip { return p.BrainTownChip }, want{"Degraded", toneWarn}, toneOK},
		{"brain town unusable", func(p *IntegrationsPage) { p.BrainTown.State = "unusable: x" }, func(p *IntegrationsPage) StatusChip { return p.BrainTownChip }, want{"Unusable", toneBad}, toneOK},
		{"brain town unknown state", func(p *IntegrationsPage) { p.BrainTown.State = "something new" }, func(p *IntegrationsPage) StatusChip { return p.BrainTownChip }, want{"Disabled", toneBad}, toneOK},
		{"reader disabled", func(p *IntegrationsPage) { p.BrainTownConsumer.State = "disabled" }, func(p *IntegrationsPage) StatusChip { return p.BrainTownConsumerChip }, want{"Disabled", toneBad}, ""},
		{"qa outbound off", func(p *IntegrationsPage) {}, func(p *IntegrationsPage) StatusChip { return p.QAChip }, want{"Outbound off", toneBad}, ""},
		{"qa connected", func(p *IntegrationsPage) { p.QA.Outbound = true }, func(p *IntegrationsPage) StatusChip { return p.QAChip }, want{"Connected", toneOK}, ""},
		{"qa recent give-up", func(p *IntegrationsPage) { p.QA.Outbound, p.QA.GaveUp, p.QA.GaveUpRecent = true, 1, 1 }, func(p *IntegrationsPage) StatusChip { return p.QAChip }, want{"Degraded", toneWarn}, toneOK},
		{"qa old give-up clears", func(p *IntegrationsPage) { p.QA.Outbound, p.QA.GaveUp = true, 5 }, func(p *IntegrationsPage) StatusChip { return p.QAChip }, want{"Connected", toneOK}, ""},
		{"qa failing", func(p *IntegrationsPage) { p.QA.Outbound, p.QA.Failing = true, true }, func(p *IntegrationsPage) StatusChip { return p.QAChip }, want{"Degraded", toneWarn}, toneOK},
		{"api enabled", func(p *IntegrationsPage) {
			p.API = APIStatus{State: "enabled (admin=true)", QA: true, QAProducts: []string{"p-1"}}
		}, func(p *IntegrationsPage) StatusChip { return p.APIChip }, want{"Enabled", toneOK}, ""},
		{"api without the qa token", func(p *IntegrationsPage) {
			p.API = APIStatus{State: "enabled (admin=true)", QAProducts: []string{"p-1"}}
		}, func(p *IntegrationsPage) StatusChip { return p.APIChip }, want{"Degraded", toneWarn}, toneOK},
		{"api with no qa products", func(p *IntegrationsPage) { p.API = APIStatus{State: "enabled (admin=true)", QA: true} }, func(p *IntegrationsPage) StatusChip { return p.APIChip }, want{"Degraded", toneWarn}, toneOK},
		{"api disabled", func(p *IntegrationsPage) { p.API.State = "disabled (no API token is configured)" }, func(p *IntegrationsPage) StatusChip { return p.APIChip }, want{"Disabled", toneBad}, ""},
		{"github on", func(p *IntegrationsPage) { p.PR.GitHub = "on" }, func(p *IntegrationsPage) StatusChip { return p.PR.GitHubChip }, want{"On", toneOK}, ""},
		{"github off", func(p *IntegrationsPage) { p.PR.GitHub = "off (no token file)" }, func(p *IntegrationsPage) StatusChip { return p.PR.GitHubChip }, want{"Off", toneBad}, ""},
		{"staging unusable", func(p *IntegrationsPage) { p.PR.StagingState = "unusable: x" }, func(p *IntegrationsPage) StatusChip { return p.PR.StagingChip }, want{"Unusable", toneBad}, toneOK},
		{"whatsapp enabled", func(p *IntegrationsPage) { p.PR.WA.State = "enabled" }, func(p *IntegrationsPage) StatusChip { return p.PR.WAChip }, want{"Enabled", toneOK}, ""},
		{"whatsapp unencrypted", func(p *IntegrationsPage) {
			p.PR.WA.State = "enabled, over http to any address (unencrypted)"
		}, func(p *IntegrationsPage) StatusChip { return p.PR.WAChip }, want{"Degraded", toneWarn}, toneOK},
		{"whatsapp linked to no product", func(p *IntegrationsPage) {
			p.PR.WA.State = "enabled, linked to no product (GAOS_WA_PRODUCTS)"
		}, func(p *IntegrationsPage) StatusChip { return p.PR.WAChip }, want{"Degraded", toneWarn}, toneOK},
		{"pr flow connected with optional parts not configured", okPR, func(p *IntegrationsPage) StatusChip { return p.PRChip }, want{"Connected", toneOK}, ""},
		{"pr flow with github off and staging on", func(p *IntegrationsPage) {
			okPR(p)
			p.PR.GitHub, p.PR.StagingState = "off (no token file)", "enabled"
		}, func(p *IntegrationsPage) StatusChip { return p.PRChip }, want{"Off", toneBad}, ""},
		{"pr flow with the reader disabled", func(p *IntegrationsPage) {
			okPR(p)
			p.BrainTownConsumer.State = "disabled"
		}, func(p *IntegrationsPage) StatusChip { return p.PRChip }, want{"Partly connected", toneWarn}, toneOK},
		{"pr flow with staging unusable", func(p *IntegrationsPage) {
			okPR(p)
			p.PR.StagingState = "unusable: x"
		}, func(p *IntegrationsPage) StatusChip { return p.PRChip }, want{"Partly connected", toneWarn}, toneOK},
		{"pr flow with whatsapp unencrypted", func(p *IntegrationsPage) {
			okPR(p)
			p.PR.WA.State = "enabled, over http to any address (unencrypted)"
		}, func(p *IntegrationsPage) StatusChip { return p.PRChip }, want{"Partly connected", toneWarn}, toneOK},
	}
	for _, c := range cases {
		p := &IntegrationsPage{}
		c.set(p)
		integChips(p)
		got := c.chip(p)
		if got.Label != c.want.label || got.Tone != c.want.tone {
			t.Errorf("%s: the chip is %s · %s, want %s · %s", c.name, got.Label, got.Tone, c.want.label, c.want.tone)
		}
		if c.never != "" && got.Tone == c.never {
			t.Errorf("%s: the chip took the tone %q, which this state must never show", c.name, c.never)
		}
		if !chipLabels[got.Label] {
			t.Errorf("%s: the label %q is not in the closed set", c.name, got.Label)
		}
	}
}

// integBlocks cuts the section into its blocks, keyed by the heading text.
func integBlocks(sec string) map[string]string {
	out := map[string]string{}
	parts := strings.Split(sec, `<div class="block">`)
	for _, b := range parts[1:] {
		i := strings.Index(b, "<h4>")
		j := strings.Index(b, "</h4>")
		if i < 0 || j < i {
			continue
		}
		out[b[i+4:j]] = b
	}
	return out
}

var integNames = []string{"ai-manager", "Brain Town", "Brain Town reader", "Pull-request flow", "TaskMgr", "QA tools", "Machine API"}

func TestTheIntegrationsViewColoursEveryHeading(t *testing.T) {
	f := newIntegrationsFixture(t)
	f.srv.brainOff = BrainTownRow{State: "unusable: BRAIN_TOWN_URL is not set"}
	f.srv.btConsumerOff = BrainTownRow{State: "disabled"}
	sec := integrationsSection(do(t, f.srv, "GET", "/ui/bo/integrations", nil, boCookies()...).Body.String())
	blocks := integBlocks(sec)
	for _, name := range integNames {
		b, ok := blocks[name]
		if !ok {
			t.Errorf("the integrations view has no block headed %q", name)
			continue
		}
		head := b[:strings.Index(b, "</div>")]
		if n := strings.Count(head, `class="chip st-`); n != 1 {
			t.Errorf("the %s heading carries %d status chips, want 1", name, n)
		}
	}
	pr := blocks["Pull-request flow"]
	for _, row := range []string{"GitHub client", "Staging callback", "WhatsApp"} {
		i := strings.Index(pr, `<th scope="row">`+row+`</th>`)
		if i < 0 {
			t.Errorf("the pull-request block has no %s row", row)
			continue
		}
		cell := pr[i : i+strings.Index(pr[i:], "</tr>")]
		if n := strings.Count(cell, `class="chip st-`); n != 1 {
			t.Errorf("the pull-request %s row carries %d status chips, want 1", row, n)
		}
	}
	for _, want := range []string{`<td class="mono">unusable: BRAIN_TOWN_URL is not set</td>`, `<td class="mono">disabled</td>`,
		`<th scope="row">State</th><td class="mono">enabled (admin=`} {
		if !strings.Contains(sec, want) {
			t.Errorf("the integrations view does not show the state cell %q", want)
		}
	}
	if bt := blocks["Brain Town"]; !strings.Contains(bt, `<span class="chip st-bad">Unusable</span>`) {
		t.Errorf("the Brain Town heading does not show %q for an unusable feed", "Unusable")
	}
	for _, m := range regexpChip.FindAllStringSubmatch(sec, -1) {
		if !chipLabels[m[1]] {
			t.Errorf("a status chip reads %q, which is not in the closed set of labels", m[1])
		}
	}
	for _, h := range regexpH4.FindAllString(sec, -1) {
		if strings.Contains(h, "&middot;") {
			t.Errorf("a heading still carries its state after a middle dot: %s", h)
		}
	}
}

func TestTheIntegrationsViewMarksMandatoryIntegrations(t *testing.T) {
	f := newIntegrationsFixture(t)
	blocks := integBlocks(integrationsSection(do(t, f.srv, "GET", "/ui/bo/integrations", nil, boCookies()...).Body.String()))
	mandatory := map[string]bool{"ai-manager": true, "QA tools": true, "Machine API": true}
	for _, name := range integNames {
		b := blocks[name]
		head := b[:strings.Index(b, "</div>")]
		hasReq, hasOpt := strings.Contains(head, `class="chip need-req">Mandatory<`), strings.Contains(head, `class="chip need-opt">Optional<`)
		if mandatory[name] && (!hasReq || hasOpt) {
			t.Errorf("the %s heading is not marked Mandatory (and only Mandatory)", name)
		}
		if !mandatory[name] && (!hasOpt || hasReq) {
			t.Errorf("the %s heading is not marked Optional (and only Optional)", name)
		}
	}
	for _, name := range []string{"Contacts", "Registered clients"} {
		if b := blocks[name]; strings.Contains(b, `class="chip need-req"`) || strings.Contains(b, `class="chip need-opt"`) {
			t.Errorf("the %s list carries a requirement chip; only integrations do", name)
		}
	}
}

func TestEveryIntegrationInfoButtonOpensItsPopover(t *testing.T) {
	f := newIntegrationsFixture(t)
	sec := integrationsSection(do(t, f.srv, "GET", "/ui/bo/integrations", nil, boCookies()...).Body.String())
	targets := regexpPopTarget.FindAllStringSubmatch(sec, -1)
	if len(targets) != 7 {
		t.Fatalf("the integrations view has %d info buttons, want 7", len(targets))
	}
	seen := map[string]bool{}
	for _, m := range targets {
		id := m[1]
		if seen[id] {
			t.Errorf("the popover id %q is used by two buttons", id)
		}
		seen[id] = true
		open := `<div id="` + id + `" class="info-pop" popover>`
		if n := strings.Count(sec, open); n != 1 {
			t.Errorf("the button for %q points to %d popovers, want exactly 1", id, n)
			continue
		}
		pop := sec[strings.Index(sec, open):]
		pop = pop[:strings.Index(pop, "</div>")]
		a, b, c := strings.Index(pop, "<h5>What it does</h5>"), strings.Index(pop, "<h5>When connected</h5>"), strings.Index(pop, "<h5>When disconnected</h5>")
		if a < 0 || b < a || c < b {
			t.Errorf("the popover %q does not hold the three headings in order", id)
		}
		if id == "ip-api" && !strings.Contains(pop, "staging callback") {
			t.Errorf("the Machine API popover does not name the staging callback's own credential")
		}
	}
}

var (
	regexpChip      = regexp.MustCompile(`<span class="chip st-(?:ok|warn|bad)">([^<]*)</span>`)
	regexpH4        = regexp.MustCompile(`<h4>[^<]*</h4>`)
	regexpPopTarget = regexp.MustCompile(`popovertarget="([^"]+)"`)
)

// TC-848: the TaskMgr block (D-104 §7): its status in order of precedence, the counts for every
// signed-in account, and the people table and the binding form for an auto-fix admin only.
func TestIntegrationsShowTaskMgr(t *testing.T) {
	ctx := context.Background()
	f := newIntegrationsFixture(t)
	st := f.srv.store.(*MemStore)
	ps, _ := st.Products(ctx)
	pid := ps[0].ID
	t.Setenv("GAOS_AUTOFIX_ADMINS", "")
	block := func() string {
		t.Helper()
		b, ok := integBlocks(integrationsSection(do(t, f.srv, "GET", "/ui/bo/integrations", nil, boCookies()...).Body.String()))["TaskMgr"]
		if !ok {
			t.Fatal("the integrations view has no TaskMgr block")
		}
		return b
	}
	if b := block(); !strings.Contains(b, `<span class="chip st-bad">Off</span>`) || !strings.Contains(b, `class="chip third-party">Third-party<`) ||
		!strings.Contains(b, "GAOS_TASKMGR_GRPC_ADDR is not set") {
		t.Errorf("with no settings the block must read Off and carry the Third-party mark: %s", b)
	}

	f.srv.tm = tmTestLink(t, st, tmFlowFake())
	t.Setenv("GAOS_PR_TEMPLATE_PRODUCTS", pid)
	if _, err := st.SetThirdParty(ctx, pid, ThirdPartyLink{Kind: ThirdPartyTaskMgr, Enabled: true, ProductCode: "DEMO"}, testEmail); err != nil {
		t.Fatal(err)
	}
	if err := f.srv.tm.people.refresh(ctx); err != nil {
		t.Fatal(err)
	}
	b := block()
	if !strings.Contains(b, `<span class="chip st-ok">Connected</span>`) || !strings.Contains(b, pid+" &middot; DEMO") ||
		!strings.Contains(b, `<td class="mono">2 &middot; 2 &middot; 2 &middot; 0</td>`) {
		t.Errorf("a bound, refreshed product must read Connected with its code and the counts: %s", b)
	}
	for _, leak := range []string{"Dev One", "dev.one", maskDestination(tmNumberDev), tmNumberDev, tmNumberQA, "third-party\" hx-target", "/third-party"} {
		if strings.Contains(b, leak) {
			t.Errorf("an account that is not an admin sees %q", leak)
		}
	}
	autoFixAdminsFile(t, 0o600, testEmail)
	b = block()
	if !strings.Contains(b, "Dev One") || !strings.Contains(b, maskDestination(tmNumberDev)) || !strings.Contains(b, `hx-post="products/`+pid+`/third-party"`) {
		t.Errorf("an admin must see the people, masked, and the binding form: %s", b)
	}
	if strings.Contains(b, tmNumberDev) || strings.Contains(b, tmNumberQA) || strings.Contains(b, tmTestSecret) {
		t.Error("the block renders a whole number or the credential")
	}

	// The status, in order of precedence.
	for _, c := range []struct {
		name string
		row  TaskMgrRow
		want string
	}{
		{"unusable settings", TaskMgrRow{State: "unusable: x", Product: "p-1", Refreshed: "1m ago", InTemplates: true}, "Unusable"},
		{"auth latch", TaskMgrRow{State: "on", Breaker: "auth", Product: "p-1", Refreshed: "1m ago", InTemplates: true}, "Unusable"},
		{"unavailable", TaskMgrRow{State: "on", Breaker: "unavailable", Product: "p-1", Refreshed: "1m ago", InTemplates: true}, "Degraded"},
		{"stale", TaskMgrRow{State: "on", Stale: true, Product: "p-1", Refreshed: "1m ago", InTemplates: true}, "Degraded"},
		{"not in the template list", TaskMgrRow{State: "on", Product: "p-1", Refreshed: "1m ago"}, "Degraded"},
		{"connected", TaskMgrRow{State: "on", Product: "p-1", Refreshed: "1m ago", InTemplates: true}, "Connected"},
		{"never refreshed", TaskMgrRow{State: "on", Product: "p-1", InTemplates: true}, "Off"},
		{"no binding", TaskMgrRow{State: "on", Refreshed: "1m ago"}, "Off"},
	} {
		p := &IntegrationsPage{TM: c.row}
		integChips(p)
		if p.TMChip.Label != c.want || !chipLabels[p.TMChip.Label] {
			t.Errorf("%s reads %q, want %q", c.name, p.TMChip.Label, c.want)
		}
	}
}
