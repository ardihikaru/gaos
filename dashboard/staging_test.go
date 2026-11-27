package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const (
	stagingPass1 = "correct-horse-battery"
	stagingPass2 = "another-long-password"
)

func stagingServer(t *testing.T) (*server, *MemStore) {
	t.Helper()
	srv, st := newTestServer(t)
	path := filepath.Join(t.TempDir(), "staging")
	body := "# one line per product\np-1:jenkins:" + stagingPass1 + "\np-2:jenkins2:" + stagingPass2 + "\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GAOS_STAGING_BASIC_AUTH_FILE", path)
	srv.staging = stagingAuthFromEnv()
	if !srv.staging.enabled() {
		t.Fatalf("the credentials did not load: %s", srv.staging.why)
	}
	return srv, st
}

func stagingCall(srv *server, product, user, pass, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/v1/products/"+product+"/staging", strings.NewReader(body))
	if user != "" {
		req.SetBasicAuth(user, pass)
	}
	req.RemoteAddr = "192.0.2.7:4444"
	rec := httptest.NewRecorder()
	srv.routes().ServeHTTP(rec, req)
	return rec
}

const stagingOK = `{"repo":"loop/laundry-api","branch":"staging","commit":"0123456789abcdef0123456789abcdef01234567","environment":"staging"}`

func TestTheStagingCallbackPlansACardAndAReplayPlansNone(t *testing.T) {
	srv, st := stagingServer(t)
	rec := stagingCall(srv, "p-1", "jenkins", stagingPass1, stagingOK)
	if rec.Code != http.StatusAccepted || !strings.Contains(rec.Body.String(), `"taskId"`) {
		t.Fatalf("answered %d %s, want 202 with a task", rec.Code, rec.Body.String())
	}
	cards := prCards(t, st, "p-1")
	if len(cards) != 1 {
		t.Fatalf("%d cards, want 1", len(cards))
	}
	for _, c := range cards {
		if c.Labels[0] != LabelE2ERun {
			t.Errorf("the card is labelled %v, want e2e:run", c.Labels)
		}
	}
	// The same commit again: no second card.
	rec = stagingCall(srv, "p-1", "jenkins", stagingPass1, stagingOK)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "duplicate") {
		t.Errorf("a replay answered %d %s, want 200 duplicate", rec.Code, rec.Body.String())
	}
	if n := len(prCards(t, st, "p-1")); n != 1 {
		t.Errorf("a replay made %d cards", n)
	}
	// A new commit is a new card.
	rec = stagingCall(srv, "p-1", "jenkins", stagingPass1, strings.Replace(stagingOK, "0123456789abcdef", "fedcba9876543210", 1))
	if rec.Code != http.StatusAccepted {
		t.Errorf("a new commit answered %d", rec.Code)
	}
	if _, ok := srv.staging.LastCallback("p-1"); !ok {
		t.Error("the last callback was not recorded")
	}
}

func TestTheStagingCallbackRefusesWithoutTheCredential(t *testing.T) {
	srv, st := stagingServer(t)
	for name, c := range map[string][2]string{"none": {"", ""}, "wrong password": {"jenkins", "wrong-password-xx"}, "wrong user": {"nobody", stagingPass1}} {
		rec := stagingCall(srv, "p-1", c[0], c[1], stagingOK)
		if rec.Code != http.StatusUnauthorized || !strings.Contains(rec.Header().Get("WWW-Authenticate"), `Basic realm="gaos"`) {
			t.Errorf("%s answered %d %q, want 401 with the Basic challenge", name, rec.Code, rec.Header().Get("WWW-Authenticate"))
		}
		if strings.Contains(rec.Body.String(), stagingPass1) {
			t.Errorf("%s: the answer carries the credential", name)
		}
	}
	if n := len(prCards(t, st, "p-1")); n != 0 {
		t.Errorf("a refused call planned %d cards", n)
	}
}

func TestAnotherProductsCredentialIsNotFound(t *testing.T) {
	srv, _ := stagingServer(t)
	rec := stagingCall(srv, "p-1", "jenkins2", stagingPass2, stagingOK)
	if rec.Code != http.StatusNotFound {
		t.Errorf("product 2's credential on product 1 answered %d, want 404", rec.Code)
	}
}

func TestTheStagingCallbackRefusesARepositoryTheProductDoesNotOwn(t *testing.T) {
	srv, _ := stagingServer(t)
	for name, body := range map[string]string{
		"foreign repo": strings.Replace(stagingOK, "loop/laundry-api", "loop/other", 1),
		"bad sha":      strings.Replace(stagingOK, "0123456789abcdef", "not-a-sha", 1),
		"short sha":    strings.Replace(stagingOK, "0123456789abcdef0123456789abcdef01234567", "0123456789ab", 1),
		"bad env":      strings.Replace(stagingOK, `"environment":"staging"`, `"environment":"prod"`, 1),
		"unknown key":  strings.Replace(stagingOK, `"branch"`, `"extra":1,"branch"`, 1),
	} {
		if rec := stagingCall(srv, "p-1", "jenkins", stagingPass1, body); rec.Code != http.StatusUnprocessableEntity {
			t.Errorf("%s answered %d, want 422", name, rec.Code)
		}
	}
}

func TestRepeatedFailuresAreThrottledWithRetryAfter(t *testing.T) {
	srv, _ := stagingServer(t)
	now := time.Now()
	srv.staging.now = func() time.Time { return now }
	for i := 0; i < stagingMaxFails; i++ {
		stagingCall(srv, "p-1", "jenkins", "wrong-password-xx", stagingOK)
	}
	rec := stagingCall(srv, "p-1", "jenkins", stagingPass1, stagingOK)
	if rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") == "" {
		t.Fatalf("answered %d, want 429 with Retry-After even for the right credential", rec.Code)
	}
	// After the lock, the right credential works again.
	now = now.Add(stagingLock + time.Second)
	if rec := stagingCall(srv, "p-1", "jenkins", stagingPass1, stagingOK); rec.Code != http.StatusAccepted {
		t.Errorf("after the lock answered %d, want 202", rec.Code)
	}
}

func TestTheStagingCredentialFileIsRefusedWhenReadableOrMalformed(t *testing.T) {
	dir := t.TempDir()
	write := func(mode os.FileMode, body string) *stagingAuth {
		p := filepath.Join(dir, "f")
		os.Remove(p)
		if err := os.WriteFile(p, []byte(body), mode); err != nil {
			t.Fatal(err)
		}
		t.Setenv("GAOS_STAGING_BASIC_AUTH_FILE", p)
		return stagingAuthFromEnv()
	}
	if a := write(0o644, "p-1:u:"+stagingPass1); a.enabled() || !strings.Contains(a.why, "0600") {
		t.Errorf("a readable file was accepted: %+v", a.why)
	}
	if a := write(0o600, "p-1:u:short"); a.enabled() {
		t.Error("a short password was accepted")
	}
	if a := write(0o600, "justonefield"); a.enabled() {
		t.Error("a malformed line was accepted")
	}
	t.Setenv("GAOS_STAGING_BASIC_AUTH_FILE", "")
	if a := stagingAuthFromEnv(); a.enabled() || a.why != "" {
		t.Error("an unset file must be off, not an error")
	}
	srv, _ := newTestServer(t)
	srv.staging = stagingAuthFromEnv()
	if rec := stagingCall(srv, "p-1", "u", "p", stagingOK); rec.Code != http.StatusServiceUnavailable {
		t.Errorf("a callback with the feature off answered %d, want 503", rec.Code)
	}
}

// TC-877
func TestTheJenkinsLockIgnoresTheCallersForwardedFor(t *testing.T) {
	// The source: X-Real-IP, then the rightmost public X-Forwarded-For hop, then the peer.
	src := func(real, xff, peer string) string {
		r := httptest.NewRequest(http.MethodPost, "/", nil)
		r.RemoteAddr = peer
		if real != "" {
			r.Header.Set("X-Real-IP", real)
		}
		if xff != "" {
			r.Header.Set("X-Forwarded-For", xff)
		}
		return jenkinsSource(r)
	}
	for _, c := range []struct{ real, xff, peer, want string }{
		{"198.51.100.9", "203.0.113.1, 198.51.100.8", "10.10.0.1:5000", "198.51.100.9"},
		{"", "203.0.113.1, 198.51.100.8, 10.0.0.3", "10.10.0.1:5000", "198.51.100.8"},
		{"not-an-ip", "203.0.113.1, 127.0.0.1", "10.10.0.1:5000", "203.0.113.1"},
		{"", "192.168.1.4, fe80::1", "192.0.2.7:4444", "192.0.2.7"},
		{"2001:db8:aa:bb:1::7", "", "192.0.2.7:4444", "2001:db8:aa:bb::/64"},
		{"", "", "[2001:db8:aa:bb::9]:4444", "2001:db8:aa:bb::/64"},
		{"::ffff:198.51.100.9", "", "192.0.2.7:4444", "198.51.100.9"},
	} {
		if got := src(c.real, c.xff, c.peer); got != c.want {
			t.Errorf("X-Real-IP %q, X-Forwarded-For %q, peer %q keyed as %q; want %q", c.real, c.xff, c.peer, got, c.want)
		}
	}

	srv, st, pid, _ := unitResultsServer(t)
	sp := filepath.Join(t.TempDir(), "staging")
	if err := os.WriteFile(sp, []byte("p-1:jenkins:"+stagingPass1+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GAOS_STAGING_BASIC_AUTH_FILE", sp)
	srv.staging = stagingAuthFromEnv()
	srv.staging.logf = func(string, ...any) {}
	h := srv.routes()
	post := func(route, product, user, pass string, hdr ...string) int {
		body := urBody(1, "passed")
		if route == routeStaging {
			body = stagingOK
		}
		req := httptest.NewRequest(http.MethodPost, "/v1/products/"+product+"/"+route, strings.NewReader(body))
		if user != "" {
			req.SetBasicAuth(user, pass)
		}
		req.RemoteAddr = "10.10.0.1:5000" // the edge
		for i := 0; i+1 < len(hdr); i += 2 {
			req.Header.Set(hdr[i], hdr[i+1])
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Code
	}
	reset := func() {
		for _, a := range []*stagingAuth{srv.staging, srv.unitResults} {
			a.mu.Lock()
			a.fails = map[string]stagingFails{}
			a.mu.Unlock()
		}
	}
	wrong := "wrong-password-0123456789-0123456789"
	for _, route := range []string{routeStaging, routeUnitResults} {
		user, pass := "jenkins", stagingPass1
		if route == routeUnitResults {
			user, pass = "jenkins-p1", urPass1
		}
		// A fresh caller-written first hop per attempt does not escape the lock.
		reset()
		for i := 0; i < stagingMaxFails; i++ {
			post(route, "p-1", user, wrong, "X-Real-IP", "198.51.100.66", "X-Forwarded-For", fmt.Sprintf("203.0.113.%d, 198.51.100.66", i))
		}
		if got := post(route, "p-1", user, pass, "X-Real-IP", "198.51.100.66", "X-Forwarded-For", "203.0.113.99, 198.51.100.66"); got != http.StatusTooManyRequests {
			t.Errorf("%s: a fresh first hop escaped the lock (%d)", route, got)
		}
		// Without X-Real-IP the rightmost public hop is the key, so the first hop picks nothing.
		reset()
		for i := 0; i < stagingMaxFails; i++ {
			post(route, "p-1", user, wrong, "X-Forwarded-For", fmt.Sprintf("203.0.113.%d, 198.51.100.66", i))
		}
		if got := post(route, "p-1", user, pass, "X-Forwarded-For", "203.0.113.98, 198.51.100.66"); got != http.StatusTooManyRequests {
			t.Errorf("%s: without X-Real-IP a fresh first hop escaped the lock (%d)", route, got)
		}
		// A first hop equal to Jenkins' address does not lock Jenkins out.
		reset()
		for i := 0; i < stagingMaxFails+2; i++ {
			post(route, "p-1", user, wrong, "X-Real-IP", "198.51.100.66", "X-Forwarded-For", "198.51.100.9, 198.51.100.66")
		}
		if got := post(route, "p-1", user, pass, "X-Real-IP", "198.51.100.9"); got == http.StatusTooManyRequests || got == http.StatusUnauthorized {
			t.Errorf("%s: a caller naming Jenkins in its first hop locked Jenkins out (%d)", route, got)
		}
		// Rotating the path id on every attempt does not escape the lock.
		reset()
		for i := 0; i < stagingMaxFails; i++ {
			post(route, fmt.Sprintf("p-%d", 100+i), user, wrong, "X-Real-IP", "198.51.100.67")
		}
		if got := post(route, "p-999", user, wrong, "X-Real-IP", "198.51.100.67"); got != http.StatusTooManyRequests {
			t.Errorf("%s: the sixth bad attempt with a rotated id answered %d; want 429", route, got)
		}
		// Interleaved success: a success clears only src|route|p, never src|route.
		reset()
		for i := 0; i < 4; i++ {
			post(route, "p-1", "nobody", wrong, "X-Real-IP", "198.51.100.68")
		}
		if got := post(route, "p-1", user, pass, "X-Real-IP", "198.51.100.68"); got == http.StatusTooManyRequests || got == http.StatusUnauthorized {
			t.Fatalf("%s: the valid post between the guesses answered %d", route, got)
		}
		for i := 0; i < 4; i++ {
			post(route, "p-1", "nobody", wrong, "X-Real-IP", "198.51.100.68")
		}
		if got := post(route, "p-1", "nobody", wrong, "X-Real-IP", "198.51.100.68"); got != http.StatusTooManyRequests {
			t.Errorf("%s: 4 guesses, a valid post and 4 more guesses, then the next answered %d; want 429", route, got)
		}
		// Two IPv6 addresses in one /64 share a bucket; another /64 does not.
		reset()
		for i := 0; i < stagingMaxFails; i++ {
			post(route, "p-1", user, wrong, "X-Real-IP", fmt.Sprintf("2001:db8:1:2::%x", i+1))
		}
		if got := post(route, "p-1", user, pass, "X-Real-IP", "2001:db8:1:2:ffff::1"); got != http.StatusTooManyRequests {
			t.Errorf("%s: another address in the /64 escaped the lock (%d)", route, got)
		}
		if got := post(route, "p-1", user, pass, "X-Real-IP", "2001:db8:1:3::1"); got == http.StatusTooManyRequests {
			t.Errorf("%s: another /64 was locked", route)
		}
	}
	// Unit results: a wrong-product credential counts against its own product,
	// whatever path id it rotates through; product B's lock does not block product A.
	reset()
	for i := 0; i < stagingMaxFails; i++ {
		post(routeUnitResults, fmt.Sprintf("p-%d", 100+i), "jenkins-p2", urPass2, "X-Real-IP", "198.51.100.70")
	}
	if got := post(routeUnitResults, "p-1", "jenkins-p2", urPass2, "X-Real-IP", "198.51.100.70"); got != http.StatusTooManyRequests {
		t.Errorf("a wrong-product credential rotating the path id answered %d; want 429", got)
	}
	watchOn(t, st, pid, "develop", 1, 1000)
	if got := post(routeUnitResults, "p-1", "jenkins-p1", urPass1, "X-Real-IP", "198.51.100.70"); got != http.StatusAccepted {
		t.Errorf("product 2's lock blocked product 1 from the same source (%d)", got)
	}
	// A staging lock does not block unit results.
	reset()
	for i := 0; i < stagingMaxFails; i++ {
		post(routeStaging, "p-1", "jenkins", wrong, "X-Real-IP", "198.51.100.71")
	}
	if got := post(routeStaging, "p-1", "jenkins", stagingPass1, "X-Real-IP", "198.51.100.71"); got != http.StatusTooManyRequests {
		t.Errorf("the staging lock did not hold (%d)", got)
	}
	if got := post(routeUnitResults, "p-1", "jenkins-p1", urPass1, "X-Real-IP", "198.51.100.71"); got == http.StatusTooManyRequests {
		t.Error("a staging lock blocked unit results")
	}
	// 10,000 distinct sources never grow the map past 4096; a new key past the cap is 429.
	reset()
	for i := 0; i < 10000; i++ {
		code := post(routeUnitResults, "p-1", "nobody", wrong, "X-Real-IP", fmt.Sprintf("198.18.%d.%d", i/256, i%256))
		if i >= stagingMaxKeys && code != http.StatusTooManyRequests {
			t.Fatalf("source %d past the cap answered %d; want 429", i, code)
		}
	}
	if n := srv.unitResults.failCount(); n > stagingMaxKeys {
		t.Errorf("the failure map holds %d keys; want at most %d", n, stagingMaxKeys)
	}
	// The unit-results file refuses a 31-character password at load.
	short := filepath.Join(t.TempDir(), "short")
	if err := os.WriteFile(short, []byte("p-1:u:"+strings.Repeat("y", 31)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GAOS_UNIT_RESULTS_BASIC_AUTH_FILE", short)
	if a := unitResultsAuthFromEnv(); a.enabled() {
		t.Error("the unit-results file accepted a 31-character password")
	}
}
