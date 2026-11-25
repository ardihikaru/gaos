package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

// The WhatsApp notifier (D-088, docs/designs/gaos-pr-agent-flows.md §8.3).
//
// The gateway is a GoWA v8 instance the operator runs. One call sends one
// message:
//
//	POST {GAOS_WA_URL}/send/message
//	Authorization: Basic base64(user:pass)
//	{"phone": "<group jid>", "message": "<text>"}
//
// Four things the contract is easy to get wrong, and this file is careful
// about:
//
//   - The answer is {"code":"SUCCESS",...}. An HTTP 200 whose code is NOT
//     "SUCCESS" is a FAILURE. That is the silent-success guard.
//   - A 200 means WhatsApp ACCEPTED the message, not that it was delivered.
//     GAOS never says "delivered".
//   - The URL is https, and has no default: the credential and the message
//     must not cross a network in clear, and no instance's host lives in this
//     repository (D-085).
//   - The group is the PINNED JID from a file, never found by name: a group's
//     name is not unique and anyone can set one.
//
// Unset GAOS_WA_URL means the notifier is off; there is no separate switch.

const (
	waTimeout  = 10 * time.Second
	waMaxBody  = 64 << 10
	waMaxText  = 1000
	waRetryGap = 2 * time.Second
)

// waAnswer is GoWA's answer to a send.
type waAnswer struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Results struct {
		MessageID string `json:"message_id"`
		Status    string `json:"status"`
	} `json:"results"`
}

// waConfig is the notifier's configuration.
type waConfig struct {
	base  string
	user  string
	pass  string
	group string // the pinned group JID
	why   string // non-empty: GAOS_WA_URL was set and the notifier is unusable
}

// on reports whether the notifier was asked for.
func (c waConfig) on() bool { return c.base != "" }

// waConfigFromEnv reads the three settings.
func waConfigFromEnv() waConfig {
	var c waConfig
	raw := strings.TrimSpace(os.Getenv("GAOS_WA_URL"))
	if raw == "" {
		return c
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		c.why = "GAOS_WA_URL is not a URL"
		return c
	}
	if u.Scheme != "https" {
		c.why = "GAOS_WA_URL must be https: the credential and the message must not cross a network in clear"
		return c
	}
	c.base = strings.TrimRight(raw, "/")

	read := func(env, what string) (string, bool) {
		path := strings.TrimSpace(os.Getenv(env))
		if path == "" {
			c.why = env + " is not set"
			return "", false
		}
		st, err := os.Stat(path)
		if err != nil || !st.Mode().IsRegular() {
			c.why = "the WhatsApp " + what + " file cannot be read"
			return "", false
		}
		if st.Mode().Perm()&0o077 != 0 {
			c.why = "the WhatsApp " + what + " file is readable by group or others; it must be mode 0600"
			return "", false
		}
		b, err := os.ReadFile(path)
		if err != nil {
			c.why = "the WhatsApp " + what + " file cannot be read"
			return "", false
		}
		return strings.TrimSpace(string(b)), true
	}
	cred, ok := read("GAOS_WA_BASIC_AUTH_FILE", "credential")
	if !ok {
		return c
	}
	parts := strings.SplitN(cred, ":", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		c.why = "the WhatsApp credential file holds no user:password"
		return c
	}
	c.user, c.pass = parts[0], parts[1]
	group, ok := read("GAOS_WA_GROUP_FILE", "group")
	if !ok {
		return c
	}
	if !strings.HasSuffix(group, "@g.us") || len(group) < 8 {
		c.why = "the WhatsApp group file holds no group id (it must be a <digits>@g.us id)"
		return c
	}
	c.group = group
	return c
}

// waNotifier sends the messages.
type waNotifier struct {
	cfg  waConfig
	hc   *http.Client
	logf func(string, ...any)
	wait func(time.Duration) // the retry gap; replaced in tests

	mu       sync.Mutex
	sent     int
	fail     int
	last     string
	disabled string // non-empty: the pinned group could not be confirmed
}

// newWANotifier builds the notifier. A nil notifier means the setting is off.
func newWANotifier(cfg waConfig) *waNotifier {
	if !cfg.on() || cfg.why != "" {
		return nil
	}
	return &waNotifier{cfg: cfg, logf: log.Printf, wait: time.Sleep, hc: &http.Client{
		Timeout: waTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return errors.New("the WhatsApp gateway answered with a redirect; redirects are refused")
		},
	}}
}

// waStatus is the Integrations row. It never carries the group id, a number or
// a message.
type waStatus struct {
	State string
	Host  string
	Sent  int
	Fail  int
	Last  string
}

// Status is the notifier's row.
func (w *waNotifier) Status() waStatus {
	if w == nil {
		return waStatus{State: "disabled"}
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	state := "enabled"
	if w.disabled != "" {
		state = "disabled: " + w.disabled
	}
	return waStatus{State: state, Host: urlHost(w.cfg.base), Sent: w.sent, Fail: w.fail, Last: w.last}
}

// VerifyGroup checks, once at boot, that the pinned group still exists in the
// device's group list. A group that is not there disables the notifier (fail
// closed); a list that cannot be read leaves it on, because the pin is still
// the only thing that is ever sent to.
func (w *waNotifier) VerifyGroup(ctx context.Context) {
	if w == nil {
		return
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, w.cfg.base+"/user/my/groups", nil)
	if err != nil {
		return
	}
	req.Header.Set("Authorization", "Basic "+basicAuth(w.cfg.user, w.cfg.pass))
	resp, err := w.hc.Do(req)
	if err != nil {
		w.logf("wa: the group list could not be read; the pinned group is unverified")
		return
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, waMaxBody))
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		w.logf("wa: the group list answered %d; the pinned group is unverified", resp.StatusCode)
		return
	}
	if strings.Contains(string(raw), w.cfg.group) {
		return
	}
	w.mu.Lock()
	w.disabled = "the pinned group is not in the device's group list"
	w.mu.Unlock()
	w.logf("wa: notifications disabled: the pinned group is not in the device's group list")
}

// Group is the pinned JID, for the notifier's own use. It never reaches a log.
func (w *waNotifier) group() string { return w.cfg.group }

// Send sends one message to a destination (the pinned group when empty).
//
// It answers whether WhatsApp ACCEPTED it, and a failure is returned, never
// swallowed. One retry after two seconds, for a transport error only: a 400
// and a 429 (WhatsApp's anti-spam lockout) are never retried, and neither is
// any answer the gateway gave.
func (w *waNotifier) Send(ctx context.Context, destination, text string) error {
	if w == nil {
		return errNoWhatsApp
	}
	w.mu.Lock()
	off := w.disabled
	w.mu.Unlock()
	if off != "" {
		return errors.New("notifications are disabled: " + off)
	}
	if destination == "" {
		destination = w.cfg.group
	}
	text = trimField(text, waMaxText)
	if text == "" {
		return errors.New("the message is empty")
	}
	body, err := json.Marshal(map[string]string{"phone": destination, "message": text})
	if err != nil {
		return err
	}
	var terr error
	for attempt := 0; attempt < 2; attempt++ {
		var retry bool
		terr, retry = w.post(ctx, body)
		if terr == nil {
			w.record(true)
			return nil
		}
		if !retry {
			break
		}
		if attempt == 0 {
			w.wait(waRetryGap)
		}
	}
	w.record(false)
	return terr
}

// post is one attempt. The bool says whether the failure was a transport error
// and may be retried.
func (w *waNotifier) post(ctx context.Context, body []byte) (error, bool) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, w.cfg.base+"/send/message", bytes.NewReader(body))
	if err != nil {
		return err, false
	}
	req.Header.Set("Authorization", "Basic "+basicAuth(w.cfg.user, w.cfg.pass))
	req.Header.Set("Content-Type", "application/json")
	resp, err := w.hc.Do(req)
	if err != nil {
		// The error text of a transport failure can carry the URL; class only.
		return errors.New("the WhatsApp gateway could not be reached"), true
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, waMaxBody))
	var ans waAnswer
	_ = json.Unmarshal(raw, &ans)
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("the WhatsApp gateway answered %d", resp.StatusCode), false
	}
	// The silent-success guard: a 200 whose code is not SUCCESS is a failure.
	if ans.Code != "SUCCESS" {
		return fmt.Errorf("the WhatsApp gateway answered %q", ans.Code), false
	}
	return nil, false
}

// record counts a delivery and remembers the last outcome.
func (w *waNotifier) record(ok bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if ok {
		w.sent++
		w.last = "accepted · " + time.Now().UTC().Format(time.RFC3339)
		return
	}
	w.fail++
	w.last = "failed · " + time.Now().UTC().Format(time.RFC3339)
}

// basicAuth is the credential's encoding.
func basicAuth(user, pass string) string {
	return base64.StdEncoding.EncodeToString([]byte(user + ":" + pass))
}

var errNoWhatsApp = errors.New("no WhatsApp notifier is configured")
