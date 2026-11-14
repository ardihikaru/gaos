package main

import (
	"crypto/pbkdf2"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

// Back-office authentication (docs/designs/bo-login.md, D-079, BR-55).

func login(t *testing.T, s *server, email, password string) *httptest.ResponseRecorder {
	t.Helper()
	return do(t, s, "POST", "/auth/login", url.Values{"email": {email}, "password": {password}, "tenant": {"loop-eng"}})
}

func sessionCookie(w *httptest.ResponseRecorder) *http.Cookie {
	for _, c := range w.Result().Cookies() {
		if c.Name == boCookie {
			return c
		}
	}
	return nil
}

func TestAForgedBackOfficeCookieIsSignedOut(t *testing.T) {
	s, _ := newTestServer(t)
	forged := &http.Cookie{Name: boCookie, Value: "1"}
	for _, path := range []string{"/products", "/ui/bo/agents", "/events"} {
		w := do(t, s, "GET", path, nil, forged)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("GET %s with the forged cookie gaos_bo=1 answered %d, want 401", path, w.Code)
		}
	}
	w := do(t, s, "GET", "/ui/bo", nil, forged)
	if !strings.Contains(w.Body.String(), "Sign in to the back office") {
		t.Fatal("the back-office sheet opened for the forged cookie gaos_bo=1; want the sign-in sheet")
	}
}

func TestASignedInCookieOpensTheBackOffice(t *testing.T) {
	s, _ := newTestServer(t)
	w := login(t, s, "  ARDI@Example.Test ", testPassword)
	if w.Code != http.StatusOK {
		t.Fatalf("a correct login answered %d, want 200", w.Code)
	}
	c := sessionCookie(w)
	if c == nil || !c.HttpOnly || c.MaxAge <= 0 {
		t.Fatalf("the session cookie is %+v, want one that is HttpOnly with a Max-Age", c)
	}
	if got := do(t, s, "GET", "/products", nil, c); got.Code != http.StatusOK {
		t.Fatalf("GET /products with the issued cookie answered %d, want 200", got.Code)
	}
	wrong := login(t, s, testEmail, "not the password")
	unknown := login(t, s, "nobody@example.test", "not the password")
	if wrong.Code != 422 || unknown.Code != 422 || sessionCookie(wrong) != nil || sessionCookie(unknown) != nil {
		t.Fatalf("wrong password %d, unknown email %d, want 422 and no cookie for both", wrong.Code, unknown.Code)
	}
	if !strings.Contains(wrong.Body.String(), msgNoMatch) || !strings.Contains(unknown.Body.String(), msgNoMatch) {
		t.Fatal("a wrong password and an unknown email must both answer: " + msgNoMatch)
	}
}

func TestACookieThatWasTamperedWithIsRefused(t *testing.T) {
	a := newTestAuthority()
	exp := time.Now().Add(time.Hour)
	good := a.sign(testEmail, exp)
	if _, _, ok := a.verify(good); !ok {
		t.Fatal("a freshly signed cookie did not verify")
	}
	parts := strings.Split(good, ".")
	flip := func(s string) string {
		b := []byte(s)
		if b[0] == 'A' {
			b[0] = 'B'
		} else {
			b[0] = 'A'
		}
		return string(b)
	}
	other := newTestAuthority()
	other.label = "another-deployment"
	late := newTestAuthority()
	late.now = func() time.Time { return exp.Add(time.Second) }
	cases := map[string]struct {
		a *authority
		v string
	}{
		"edited email":     {a, strings.Join([]string{parts[0], flip(parts[1]), parts[2], parts[3], parts[4]}, ".")},
		"edited expiry":    {a, strings.Join([]string{parts[0], parts[1], "99999999999", parts[3], parts[4]}, ".")},
		"edited fp":        {a, strings.Join([]string{parts[0], parts[1], parts[2], flip(parts[3]), parts[4]}, ".")},
		"truncated MAC":    {a, good[:len(good)-6]},
		"no MAC":           {a, strings.Join(parts[:4], ".")},
		"oversized":        {a, good + strings.Repeat("A", 600)},
		"empty":            {a, ""},
		"wrong deployment": {other, good},
		"expired":          {late, good},
	}
	names := make([]string, 0, len(cases))
	for n := range cases {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		if _, _, ok := cases[n].a.verify(cases[n].v); ok {
			t.Fatalf("a cookie with %s verified; want it refused", n)
		}
	}
}

func TestAPasswordChangeEndsOldSessions(t *testing.T) {
	old := newTestAuthority()
	cookie := old.sign(testEmail, time.Now().Add(time.Hour))
	line, err := hashWith("a different password", []byte("another-salt-123"), pbkdf2MinIter)
	if err != nil {
		t.Fatal(err)
	}
	edited := newAuthority(parseUsers(strings.NewReader(testEmail+":"+line+"\n"), func(string, ...any) {}), testKey, testLabel)
	if _, _, ok := edited.verify(cookie); ok {
		t.Fatal("a session survived a password change in the users file; want it refused")
	}
	removed := newAuthority(map[string]*account{}, testKey, testLabel)
	if _, _, ok := removed.verify(cookie); ok {
		t.Fatal("a session survived the removal of its account; want it refused")
	}
}

func TestWithNoAccountsNobodySignsIn(t *testing.T) {
	s, _ := newTestServer(t)
	s.auth = newAuthority(nil, testKey, testLabel)
	w := login(t, s, testEmail, testPassword)
	if w.Code != 422 || !strings.Contains(w.Body.String(), msgNotConfigured) || sessionCookie(w) != nil {
		t.Fatalf("login with no accounts answered %d, want 422 with %q and no cookie", w.Code, msgNotConfigured)
	}
	if got := do(t, s, "GET", "/products", nil, &http.Cookie{Name: boCookie, Value: "1"}); got.Code != 401 {
		t.Fatalf("gaos_bo=1 with no accounts answered %d, want 401", got.Code)
	}
	if got := do(t, s, "GET", "/products", nil, boCookies()...); got.Code != 401 {
		t.Fatalf("a once-valid cookie with no accounts answered %d, want 401", got.Code)
	}
}

func TestFiveFailuresLockTheAccount(t *testing.T) {
	two := testEmail + ":" + testHashLine() + "\nother@example.test:" + testHashLine() + "\n"
	s, _ := newTestServer(t)
	s.auth = newAuthority(parseUsers(strings.NewReader(two), func(string, ...any) {}), testKey, testLabel)

	variants := []string{testEmail, strings.ToUpper(testEmail), "  " + testEmail + " ", testEmail, testEmail}
	for i, v := range variants {
		if w := login(t, s, v, "wrong"); w.Code != 422 || !strings.Contains(w.Body.String(), msgNoMatch) {
			t.Fatalf("failure %d answered %d, want 422 with %q", i+1, w.Code, msgNoMatch)
		}
	}
	w := login(t, s, testEmail, testPassword)
	if w.Code != 422 || !strings.Contains(w.Body.String(), "Too many attempts") || sessionCookie(w) != nil {
		t.Fatalf("the right password after five failures answered %d, want 422 \"Too many attempts\" and no cookie", w.Code)
	}
	if ra := w.Header().Get("Retry-After"); ra == "" {
		t.Fatal("a locked login sent no Retry-After")
	}
	if got := login(t, s, "other@example.test", testPassword); got.Code != 200 {
		t.Fatalf("another account answered %d while this one was locked, want 200", got.Code)
	}

	// Unknown emails never lock visibly and never evict a locked account.
	for i := 0; i < authUnknownMax+50; i++ {
		s.auth.fail("junk"+strings.Repeat("x", i%7)+hex.EncodeToString([]byte{byte(i), byte(i >> 8)})+"@x.test", false)
	}
	if n := len(s.auth.unknown); n > authUnknownMax {
		t.Fatalf("the unknown-email table holds %d keys, want at most %d", n, authUnknownMax)
	}
	if _, locked := s.auth.lockLeft(testEmail); !locked {
		t.Fatal("a flood of unknown emails ended the lock on a real account")
	}
	// The lock ends by itself.
	s.auth.now = func() time.Time { return time.Now().Add(authLockout + time.Minute) }
	if got := login(t, s, testEmail, testPassword); got.Code != 200 {
		t.Fatalf("after the lockout the right password answered %d, want 200", got.Code)
	}
}

func TestTheUsersFileParser(t *testing.T) {
	good := testHashLine()
	other, _ := hashWith("x", []byte("another-salt-123"), pbkdf2MinIter)
	tooFew := strings.Replace(good, "$100000$", "$99999$", 1)
	tooMany := strings.Replace(good, "$100000$", "$5000001$", 1)
	file := strings.Join([]string{
		"# a comment",
		"",
		"  First@Example.Test  :" + good,
		"first@example.test:" + other, // duplicate, keeps the first
		"no-separator-line",
		"nobody:" + good,           // no @
		"a:b@example.test:" + good, // email containing ':' splits at the first one, so the rest is not a hash
		"ctl\x01@example.test:" + good,
		"low@example.test:" + tooFew,
		"high@example.test:" + tooMany,
		"garbage@example.test:pbkdf2-sha256$100000$!!$!!",
		"second@example.test:" + other,
	}, "\n")
	var logs []string
	got := parseUsers(strings.NewReader(file), func(f string, _ ...any) { logs = append(logs, f) })
	var emails []string
	for e := range got {
		emails = append(emails, e)
	}
	sort.Strings(emails)
	if strings.Join(emails, ",") != "first@example.test,second@example.test" {
		t.Fatalf("accepted accounts = %v, want [first@example.test second@example.test]", emails)
	}
	if got["first@example.test"].fp == got["second@example.test"].fp {
		t.Fatal("two different hashes share one fingerprint")
	}
	if len(logs) != 8 {
		t.Fatalf("%d lines were reported as skipped, want 8: %v", len(logs), logs)
	}
	for _, l := range logs {
		if strings.Contains(l, "pbkdf2") || strings.Contains(l, "example.test") {
			t.Fatalf("a skipped-line report quotes line content: %q", l)
		}
	}
}

func TestHashPasswordRoundTrip(t *testing.T) {
	// RFC 7914 section 11: PBKDF2-HMAC-SHA256("passwd", "salt", 1, 64); first 32 bytes.
	k, err := pbkdf2.Key(sha256.New, "passwd", []byte("salt"), 1, 32)
	if err != nil || hex.EncodeToString(k) != "55ac046e56e3089fec1691c22544b605f94185216dde0465e68b9d57c20dacbc" {
		t.Fatalf("PBKDF2 vector = %x, %v; want 55ac046e…20dacbc", k, err)
	}
	var out strings.Builder
	if err := runHashPassword(strings.NewReader("s3cret pass\n"), &out); err != nil {
		t.Fatal(err)
	}
	line := strings.TrimSpace(out.String())
	acc, err := parseHashLine(line)
	if err != nil || !acc.verify("s3cret pass") || acc.verify("s3cret pas") {
		t.Fatalf("hash-password output %q did not round-trip (%v)", line, err)
	}
	if !strings.HasPrefix(line, "pbkdf2-sha256$600000$") {
		t.Fatalf("a new hash is %q, want the 600000-iteration form", line)
	}
	if err := runHashPassword(strings.NewReader("\n"), &out); err == nil {
		t.Fatal("hash-password accepted an empty password")
	}
}

// The read-only GET patterns. A GET that is not on this list fails the test:
// a state change must be a POST/PUT/DELETE (the origin check covers those), and
// the reason is written next to each entry.
var readOnlyGET = map[string]string{
	"GET /events":                          "reads the event log",
	"GET /events/stream":                   "a hint stream",
	"GET /tasks/{id}/context":              "reads one packet",
	"GET /ui/bo/{view}":                    "renders a back-office pane",
	"GET /products":                        "lists products",
	"GET /products/{id}":                   "renders one product",
	"GET /products/{id}/board":             "renders the board",
	"GET /products/{id}/readiness":         "reads readiness",
	"GET /questions":                       "lists open questions",
	"GET /products/{id}/questions":         "lists one product's questions",
	"GET /ui/answer/{id}/{taskId}":         "renders the answer sheet",
	"GET /ui/answer-ext/{id}/{externalId}": "renders the external answer sheet",
	"GET /ui/cw":                           "opens the wizard",
}

func samplePath(pattern string) (method, path string) {
	method, path, _ = strings.Cut(pattern, " ")
	path = strings.ReplaceAll(path, "{$}", "")
	for strings.Contains(path, "{") {
		i, j := strings.Index(path, "{"), strings.Index(path, "}")
		path = path[:i] + "x" + path[j+1:]
	}
	return method, path
}

func TestEveryRouteIsClassified(t *testing.T) {
	s, _ := newTestServer(t)
	s.routes()
	if len(s.patterns) < 30 || len(s.bearer) < 5 {
		t.Fatalf("routes() recorded %d patterns and %d bearer patterns; the table is not being filled", len(s.patterns), len(s.bearer))
	}
	registered := map[string]bool{}
	for _, p := range s.patterns {
		registered[p] = true
	}
	for p := range publicPatterns {
		if !registered[p] {
			t.Errorf("the public list names %q, which is not a registered pattern", p)
		}
	}
	for p := range s.bearer {
		if registered[p] || publicPatterns[p] {
			t.Errorf("%q is both bearer and back office/public; the classes must not overlap", p)
		}
	}
	forged := &http.Cookie{Name: boCookie, Value: "1"}
	for _, p := range s.patterns {
		if publicPatterns[p] {
			continue
		}
		method, path := samplePath(p)
		if method == "GET" {
			if _, ok := readOnlyGET[p]; !ok {
				t.Errorf("GET pattern %q is not on the read-only list; a GET must not change state", p)
			}
		}
		for _, cs := range [][]*http.Cookie{nil, {forged}} {
			w := do(t, s, method, path, url.Values{}, cs...)
			if w.Code != http.StatusUnauthorized {
				t.Errorf("%s %s signed out answered %d, want 401", method, path, w.Code)
			}
			r := httptest.NewRequest(method, path, nil)
			for _, c := range cs {
				r.AddCookie(c)
			}
			jw := httptest.NewRecorder()
			s.routes().ServeHTTP(jw, r)
			if jw.Code != http.StatusUnauthorized || !strings.Contains(jw.Header().Get("Content-Type"), "json") {
				t.Errorf("%s %s signed out (not htmx) answered %d %q, want 401 JSON", method, path, jw.Code, jw.Header().Get("Content-Type"))
			}
		}
	}
	for p := range readOnlyGET {
		if !registered[p] {
			t.Errorf("the read-only list names %q, which is not registered", p)
		}
	}
	// An unmatched path is not public: the mux answers 404, never 200.
	if w := do(t, s, "GET", "/no/such/route", nil); w.Code != http.StatusNotFound {
		t.Errorf("an unknown path answered %d, want 404", w.Code)
	}
	// A path the mux would redirect (trailing slash) is not a way around the gate.
	if w := do(t, s, "GET", "/static", nil); w.Code == http.StatusOK {
		t.Errorf("GET /static answered 200 signed out, want a redirect or 401")
	}
	// Replay changed global state as a GET; it is a POST now.
	if w := do(t, s, "GET", "/replay/floor?t=1", nil, boCookies()...); w.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET /replay answered %d, want 405", w.Code)
	}
}

func TestACrossOriginPostIsRefused(t *testing.T) {
	s, _ := newTestServer(t)
	send := func(method, path string, hdr map[string]string, cs ...*http.Cookie) int {
		r := httptest.NewRequest(method, path, strings.NewReader(url.Values{"email": {testEmail}, "password": {testPassword}}.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r.Header.Set("HX-Request", "true")
		for k, v := range hdr {
			r.Header.Set(k, v)
		}
		for _, c := range cs {
			r.AddCookie(c)
		}
		w := httptest.NewRecorder()
		s.routes().ServeHTTP(w, r)
		return w.Code
	}
	evil := map[string]string{"Origin": "https://evil.example"}
	site := map[string]string{"Sec-Fetch-Site": "cross-site"}
	for name, hdr := range map[string]map[string]string{"foreign Origin": evil, "cross-site fetch": site, "null Origin": {"Origin": "null"}} {
		for _, target := range [][2]string{{"POST", "/auth/login"}, {"POST", "/auth/logout"}, {"PUT", "/products/p-1/brd"}} {
			if got := send(target[0], target[1], hdr, boCookies()...); got != http.StatusForbidden {
				t.Errorf("%s %s with a %s answered %d, want 403", target[0], target[1], name, got)
			}
		}
	}
	for name, hdr := range map[string]map[string]string{
		"same Origin":      {"Origin": "http://example.com"},
		"same-origin":      {"Sec-Fetch-Site": "same-origin"},
		"no browser hints": nil,
	} {
		if got := send("POST", "/auth/login", hdr); got != http.StatusOK {
			t.Errorf("a login with %s answered %d, want 200", name, got)
		}
	}
	// The bearer API is exempt: a machine sends no browser headers and the token is the check.
	if got := send("POST", "/v1/seed/product", site); got == http.StatusForbidden {
		t.Error("a /v1 route was refused by the origin check; bearer routes are exempt")
	}
}

func TestTheSessionKeyFile(t *testing.T) {
	dir := t.TempDir()
	write := func(name, content string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	var logs []string
	logf := func(f string, _ ...any) { logs = append(logs, f) }
	key := strings.Repeat("k", authMinKey)

	if got := loadKeyFile(write("ok", "  "+key+"\n"), logf); string(got) != key {
		t.Fatalf("a 32-byte key file with whitespace around it loaded as %q, want the trimmed key", got)
	}
	if len(logs) != 0 {
		t.Fatalf("a good key file logged %v", logs)
	}
	short := loadKeyFile(write("short", key[:authMinKey-1]), logf)
	if len(short) != 32 || string(short) == key[:authMinKey-1] || len(logs) != 1 {
		t.Fatal("a 31-byte key file was accepted; want a random per-process key and a log line")
	}
	a, b := loadKeyFile("", logf), loadKeyFile(filepath.Join(dir, "missing"), logf)
	if len(a) != 32 || len(b) != 32 || string(a) == string(b) {
		t.Fatal("an unset or unreadable key file did not fall back to two different random keys")
	}
	if len(logs) != 3 {
		t.Fatalf("fallbacks logged %d lines, want 3", len(logs))
	}
}
