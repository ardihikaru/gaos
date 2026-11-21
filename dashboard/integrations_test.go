package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
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
