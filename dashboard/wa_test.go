package main

import (
	"context"
	"net"
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
		cfg:  waConfig{base: srv.URL, user: "u", pass: "p", group: testGroup, products: map[string]bool{"p-1": true}},
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

func TestAnHTTPGatewayNeedsTheExplicitSwitchAndIsSaidToBeUnencrypted(t *testing.T) {
	dir := t.TempDir()
	cred, group := filepath.Join(dir, "cred"), filepath.Join(dir, "group")
	os.WriteFile(cred, []byte("user:pass\n"), 0o600)
	os.WriteFile(group, []byte(testGroup+"\n"), 0o600)
	t.Setenv("GAOS_WA_BASIC_AUTH_FILE", cred)
	t.Setenv("GAOS_WA_GROUP_FILE", group)
	t.Setenv("GAOS_WA_URL", "http://wa.example.org:7001")
	t.Setenv("GAOS_WA_ALLOW_HTTP", "")
	if c := waConfigFromEnv(); c.why == "" {
		t.Fatal("http was accepted without GAOS_WA_ALLOW_HTTP=1")
	}
	t.Setenv("GAOS_WA_ALLOW_HTTP", "1")
	t.Setenv("GAOS_WA_PRODUCTS", "p-6")
	c := waConfigFromEnv()
	if c.why != "" || !c.clearText || !c.products["p-6"] {
		t.Fatalf("http with the switch was refused or mis-read: %+v", c)
	}
	st := newWANotifier(c).Status()
	if !strings.Contains(st.State, "unencrypted") {
		t.Errorf("the status %q does not say the link is unencrypted", st.State)
	}
	// ftp and other schemes are refused whatever the switch says.
	t.Setenv("GAOS_WA_URL", "ftp://wa.example.org")
	if c := waConfigFromEnv(); c.why == "" {
		t.Error("an ftp URL was accepted")
	}
}

func TestTheGroupIsLinkedOnlyToTheProductsListed(t *testing.T) {
	w, _ := waTestNotifier(t, func(rw http.ResponseWriter, _ *http.Request) { _, _ = rw.Write([]byte(`{"code":"SUCCESS"}`)) })
	if !w.covers("p-1") || w.covers("p-2") {
		t.Errorf("covers p-1=%v p-2=%v, want true and false", w.covers("p-1"), w.covers("p-2"))
	}
	w.cfg.products = nil
	if w.covers("p-1") || !strings.Contains(w.Status().State, "no product") {
		t.Errorf("an empty list must link no product: %q", w.Status().State)
	}
}

func TestAnHTTPLinkIsOnlyToAPrivateAddressUnlessAnyIsSaid(t *testing.T) {
	for ip, want := range map[string]bool{"127.0.0.1": true, "10.10.0.9": true, "192.168.1.5": true, "172.16.0.1": true,
		"100.64.1.1": true, "100.127.255.1": true, "169.254.1.1": true, "::1": true,
		"93.184.216.34": false, "8.8.8.8": false, "100.128.0.1": false, "172.32.0.1": false} {
		if got := privateAddr(net.ParseIP(ip)); got != want {
			t.Errorf("privateAddr(%s) = %v, want %v", ip, got, want)
		}
	}
	// A public literal address is refused at connect time with the switch on 1...
	cfg := waConfig{base: "http://93.184.216.34:81", user: "u", pass: "p", group: testGroup, clearText: true,
		products: map[string]bool{"p-1": true}}
	w := newWANotifier(cfg)
	w.wait = func(time.Duration) {}
	err := w.Send(context.Background(), "", "x")
	if err == nil || !strings.Contains(err.Error(), "could not be reached") {
		t.Fatalf("a public http gateway was dialled with GAOS_WA_ALLOW_HTTP=1: %v", err)
	}
	// ...and a loopback one is reached.
	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, _ *http.Request) { _, _ = rw.Write([]byte(`{"code":"SUCCESS"}`)) }))
	defer srv.Close()
	cfg.base = srv.URL
	if err := newWANotifier(cfg).Send(context.Background(), "", "x"); err != nil {
		t.Errorf("a loopback http gateway was refused: %v", err)
	}
}

func TestTheStatusAlwaysSaysUnencryptedAndSaysNoProductAndSkipsTheBootSend(t *testing.T) {
	cfg := waConfig{base: "http://10.10.0.9:7001", user: "u", pass: "p", group: testGroup, clearText: true}
	w := newWANotifier(cfg)
	st := w.Status().State
	if !strings.Contains(st, "unencrypted") || !strings.Contains(st, "no product") {
		t.Errorf("status %q must say both facts", st)
	}
	w.disabled = "the pinned group is not in the device's group list"
	if st := w.Status().State; !strings.Contains(st, "unencrypted") || !strings.Contains(st, "disabled") {
		t.Errorf("a disabled http notifier hides the clear link: %q", st)
	}
	// With no product linked the credential is not sent at boot.
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hits++ }))
	defer srv.Close()
	cfg.base = srv.URL
	w2 := newWANotifier(cfg)
	w2.VerifyGroup(context.Background())
	if hits != 0 {
		t.Errorf("the group check sent the credential %d time(s) with no product linked", hits)
	}
}
