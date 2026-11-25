package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const testGroup = "120363000000000000@g.us"

func waTestNotifier(t *testing.T, h http.HandlerFunc) (*waNotifier, *httptest.Server) {
	t.Helper()
	srv := httptest.NewTLSServer(h)
	t.Cleanup(srv.Close)
	return &waNotifier{
		cfg:  waConfig{base: srv.URL, user: "u", pass: "p", group: testGroup},
		hc:   srv.Client(),
		logf: func(string, ...any) {},
		wait: func(time.Duration) {},
	}, srv
}

func TestAWA200ThatIsNotSUCCESSIsAFailure(t *testing.T) {
	w, _ := waTestNotifier(t, func(rw http.ResponseWriter, _ *http.Request) {
		_, _ = rw.Write([]byte(`{"code":"FAILED","message":"not logged in"}`))
	})
	if err := w.Send(context.Background(), "", "hi"); err == nil {
		t.Fatal("a 200 with code FAILED was treated as a success")
	}
	if st := w.Status(); st.Fail != 1 || st.Sent != 0 {
		t.Errorf("status %+v, want one failure", st)
	}
}

func TestAnAcceptedMessageIsSentToThePinnedGroupWithBasicAuth(t *testing.T) {
	var gotPhone, gotAuth string
	w, _ := waTestNotifier(t, func(rw http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		b := make([]byte, 512)
		n, _ := r.Body.Read(b)
		gotPhone = string(b[:n])
		_, _ = rw.Write([]byte(`{"code":"SUCCESS","results":{"message_id":"m1","status":"success"}}`))
	})
	if err := w.Send(context.Background(), "", "hello"); err != nil {
		t.Fatalf("send: %v", err)
	}
	if !strings.Contains(gotPhone, testGroup) || !strings.HasPrefix(gotAuth, "Basic ") {
		t.Errorf("phone %q auth %q", gotPhone, gotAuth)
	}
	st := w.Status()
	if st.Sent != 1 || strings.Contains(st.Last+st.Host+st.State, "120363") {
		t.Errorf("the status leaks or miscounts: %+v", st)
	}
}

func TestA429And400AreNeverRetriedAndATransportErrorIsRetriedOnce(t *testing.T) {
	for _, code := range []int{429, 400, 500} {
		var calls atomic.Int32
		w, _ := waTestNotifier(t, func(rw http.ResponseWriter, _ *http.Request) {
			calls.Add(1)
			rw.WriteHeader(code)
		})
		if err := w.Send(context.Background(), "", "x"); err == nil {
			t.Fatalf("%d was a success", code)
		}
		if calls.Load() != 1 {
			t.Errorf("a %d was tried %d times, want once", code, calls.Load())
		}
	}
	// A transport error: the server is gone. One retry, then the failure.
	w, srv := waTestNotifier(t, func(http.ResponseWriter, *http.Request) {})
	srv.Close()
	waits := 0
	w.wait = func(time.Duration) { waits++ }
	err := w.Send(context.Background(), "", "x")
	if err == nil || waits != 1 {
		t.Errorf("err %v after %d retry waits, want a failure after exactly one", err, waits)
	}
	if err != nil && strings.Contains(err.Error(), "127.0.0.1") {
		t.Errorf("the transport error leaks the host: %v", err)
	}
}

func TestTheWAConfigRefusesHTTPAndNeedsNoDefault(t *testing.T) {
	dir := t.TempDir()
	cred := filepath.Join(dir, "cred")
	group := filepath.Join(dir, "group")
	os.WriteFile(cred, []byte("user:pass\n"), 0o600)
	os.WriteFile(group, []byte(testGroup+"\n"), 0o600)
	t.Setenv("GAOS_WA_BASIC_AUTH_FILE", cred)
	t.Setenv("GAOS_WA_GROUP_FILE", group)

	t.Setenv("GAOS_WA_URL", "")
	if c := waConfigFromEnv(); c.on() || c.why != "" {
		t.Errorf("an unset URL must be off without an error: %+v", c)
	}
	t.Setenv("GAOS_WA_URL", "http://wa.example.org:7001")
	if c := waConfigFromEnv(); c.why == "" || !strings.Contains(c.why, "https") || newWANotifier(c) != nil {
		t.Errorf("an http URL must be refused: %+v", c)
	}
	t.Setenv("GAOS_WA_URL", "https://wa.example.org")
	if c := waConfigFromEnv(); c.why != "" || c.group != testGroup || newWANotifier(c) == nil {
		t.Errorf("a valid config was refused: %+v", c)
	}
	os.Chmod(group, 0o644)
	if c := waConfigFromEnv(); !strings.Contains(c.why, "0600") {
		t.Errorf("a readable group file was accepted: %q", c.why)
	}
	os.Chmod(group, 0o600)
	os.WriteFile(group, []byte("a group name"), 0o600)
	if c := waConfigFromEnv(); !strings.Contains(c.why, "group id") {
		t.Errorf("a name in place of a JID was accepted: %q", c.why)
	}
}

func TestAPinnedGroupThatIsGoneDisablesTheNotifier(t *testing.T) {
	w, _ := waTestNotifier(t, func(rw http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/user/my/groups" {
			_, _ = rw.Write([]byte(`{"code":"SUCCESS","results":{"data":[{"JID":"999@g.us","Name":"` + "JACK SPARROW" + `"}]}}`))
			return
		}
		_, _ = rw.Write([]byte(`{"code":"SUCCESS"}`))
	})
	w.VerifyGroup(context.Background())
	if err := w.Send(context.Background(), "", "x"); err == nil || !strings.Contains(w.Status().State, "disabled") {
		t.Fatalf("a group that is not in the list must disable sending (a same-named group must not rescue it): %v %q", err, w.Status().State)
	}
}

func TestAPinnedGroupThatExistsKeepsTheNotifierOn(t *testing.T) {
	w, _ := waTestNotifier(t, func(rw http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/user/my/groups" {
			_, _ = rw.Write([]byte(`{"code":"SUCCESS","results":{"data":[{"JID":"` + testGroup + `"}]}}`))
			return
		}
		_, _ = rw.Write([]byte(`{"code":"SUCCESS"}`))
	})
	w.VerifyGroup(context.Background())
	if err := w.Send(context.Background(), "", "x"); err != nil {
		t.Fatalf("send: %v", err)
	}
}
