package main

import (
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
