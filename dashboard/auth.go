package main

// Back-office authentication (docs/designs/bo-login.md, D-079, BR-55).
//
// The shape is pustaka's (pustaka/main.go: signed token, credential
// fingerprint, throttle, same-origin check), copied because the two programs
// are separate Go modules, with the changes the design names: the cookie names
// the account, the credential is a per-account PBKDF2 hash, the throttle is
// per account and never evicts a locked account, and a deployment label is
// bound into the MAC.
//
// Nothing here reads a cookie's presence as identity. A request is signed in
// only when the MAC verifies, the expiry is in the future, the account still
// exists and the credential fingerprint still matches.

import (
	"bufio"
	"context"
	"crypto/hmac"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	pbkdf2Scheme   = "pbkdf2-sha256"
	pbkdf2Iter     = 600_000
	pbkdf2MinIter  = 100_000
	pbkdf2MaxIter  = 5_000_000
	pbkdf2SaltLen  = 16
	pbkdf2KeyLen   = 32
	authTTL        = 12 * time.Hour
	authTokenMax   = 512
	authMaxEmail   = 254
	authMinKey     = 32
	authMaxFails   = 5
	authWindow     = 15 * time.Minute
	authLockout    = 15 * time.Minute
	authUnknownMax = 4096
	authKnownSlots = 4
	authUnkSlots   = 2
	authSlotWait   = 2 * time.Second
)

// Texts the login form shows. They are constants so a test quotes the exact
// string and a reader sees them in one place.
const (
	msgNotConfigured = "Back-office sign-in is not configured on this server."
	msgNeedBoth      = "Enter the email and password you use for this server."
	msgNoMatch       = "That email and password do not match."
	msgBusy          = "The server is busy. Try again in a moment."
)

type account struct {
	email string
	iter  int
	salt  []byte
	hash  []byte
	fp    string // first 16 hex chars of SHA-256(stored hash line)
}

// attempts is one throttle entry.
type attempts struct {
	n     int
	first time.Time
	until time.Time
	seen  time.Time
}

type authority struct {
	accounts map[string]*account
	key      []byte
	label    string
	ttl      time.Duration
	now      func() time.Time
	dummy    *account // verified against when the email is unknown

	mu      sync.Mutex
	known   map[string]*attempts
	unknown map[string]*attempts

	knownSlots, unkSlots chan struct{}
	slotWait             time.Duration
}

// ---------- canonical email ----------

// canonEmail is applied once, to the submitted address and to every address in
// the users file, before the account lookup AND before the throttle key, so
// variants of one address share one counter.
func canonEmail(s string) (string, bool) {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" || len(s) > authMaxEmail || !strings.Contains(s, "@") || strings.Contains(s, ":") {
		return "", false
	}
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			return "", false
		}
	}
	return s, true
}

// ---------- password hashes ----------

func hashPassword(pw string) (string, error) {
	salt := make([]byte, pbkdf2SaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	return hashWith(pw, salt, pbkdf2Iter)
}

func hashWith(pw string, salt []byte, iter int) (string, error) {
	h, err := pbkdf2.Key(sha256.New, pw, salt, iter, pbkdf2KeyLen)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s$%d$%s$%s", pbkdf2Scheme, iter,
		base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(h)), nil
}

// parseHashLine reads "pbkdf2-sha256$<iter>$<salt b64>$<hash b64>".
func parseHashLine(line string) (*account, error) {
	f := strings.Split(line, "$")
	if len(f) != 4 || f[0] != pbkdf2Scheme {
		return nil, errors.New("not a " + pbkdf2Scheme + " hash")
	}
	iter, err := strconv.Atoi(f[1])
	if err != nil || iter < pbkdf2MinIter || iter > pbkdf2MaxIter {
		return nil, errors.New("iteration count out of range")
	}
	salt, err := base64.RawStdEncoding.DecodeString(f[2])
	if err != nil || len(salt) < 8 || len(salt) > 64 {
		return nil, errors.New("bad salt")
	}
	hash, err := base64.RawStdEncoding.DecodeString(f[3])
	if err != nil || len(hash) != pbkdf2KeyLen {
		return nil, errors.New("bad hash")
	}
	fp := sha256.Sum256([]byte(line))
	return &account{iter: iter, salt: salt, hash: hash, fp: hex.EncodeToString(fp[:])[:16]}, nil
}

func (a *account) verify(pw string) bool {
	got, err := pbkdf2.Key(sha256.New, pw, a.salt, a.iter, pbkdf2KeyLen)
	if err != nil {
		return false
	}
	return subtle.ConstantTimeCompare(got, a.hash) == 1
}

// parseUsers reads the users file. A bad line is logged by number, never by
// content, and skipped; a duplicate address keeps the first.
func parseUsers(r io.Reader, logf func(string, ...any)) map[string]*account {
	out := map[string]*account{}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 4096), 4096)
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		i := strings.Index(line, ":")
		if i < 0 {
			logf("auth: users file line %d: no ':' separator (skipped)", n)
			continue
		}
		email, ok := canonEmail(line[:i])
		if !ok {
			logf("auth: users file line %d: bad email (skipped)", n)
			continue
		}
		acc, err := parseHashLine(strings.TrimSpace(line[i+1:]))
		if err != nil {
			logf("auth: users file line %d: %v (skipped)", n, err)
			continue
		}
		if _, dup := out[email]; dup {
			logf("auth: users file line %d: duplicate account (skipped)", n)
			continue
		}
		acc.email = email
		out[email] = acc
	}
	return out
}

// ---------- construction ----------

func newAuthority(accts map[string]*account, key []byte, label string) *authority {
	a := &authority{
		accounts: accts, key: key, label: label, ttl: authTTL, now: time.Now,
		known: map[string]*attempts{}, unknown: map[string]*attempts{},
		knownSlots: make(chan struct{}, authKnownSlots), unkSlots: make(chan struct{}, authUnkSlots),
		slotWait: authSlotWait,
	}
	salt := make([]byte, pbkdf2SaltLen)
	_, _ = rand.Read(salt)
	if line, err := hashWith("not a password anybody has", salt, pbkdf2Iter); err == nil {
		a.dummy, _ = parseHashLine(line)
	}
	return a
}

// loadKeyFile reads the session key: raw bytes, whitespace-trimmed, no
// decoding, at least 32. Unset, unreadable or short logs and returns a random
// per-process key -- sessions then die at restart, which is safe, not weak.
func loadKeyFile(path string, logf func(string, ...any)) []byte {
	random := func() []byte {
		k := make([]byte, 32)
		if _, err := rand.Read(k); err != nil {
			log.Fatalf("auth: cannot generate a session key: %v", err)
		}
		return k
	}
	if path == "" {
		logf("auth: GAOS_BO_SESSION_KEY_FILE is unset; sessions end when the dashboard restarts")
		return random()
	}
	b, err := os.ReadFile(path)
	if err != nil {
		logf("auth: session key file unreadable (%v); using a per-process key", err)
		return random()
	}
	k := []byte(strings.TrimSpace(string(b)))
	if len(k) < authMinKey {
		logf("auth: session key file holds %d bytes, need %d; using a per-process key", len(k), authMinKey)
		return random()
	}
	return k
}

func warnIfOpen(path string, logf func(string, ...any)) {
	if st, err := os.Stat(path); err == nil && st.Mode().Perm()&0o077 != 0 {
		logf("auth: %s is readable by group or others; it should be mode 0600", path)
	}
}

// authFromEnv builds the authority. With no usable account, or (in prod) no
// label, the returned authority has no accounts and nobody can sign in.
func authFromEnv(prod bool) *authority {
	logf := log.Printf
	label := os.Getenv("GAOS_BO_LABEL")
	var accts map[string]*account
	if p := os.Getenv("GAOS_BO_USERS_FILE"); p != "" {
		warnIfOpen(p, logf)
		f, err := os.Open(p)
		if err != nil {
			logf("auth: users file unreadable: %v", err)
		} else {
			accts = parseUsers(f, logf)
			f.Close()
		}
	}
	if label == "" {
		if prod {
			logf("auth: GAOS_BO_LABEL is empty in prod; sign-in is disabled")
			accts = nil
		}
		label = "gaos"
	}
	if kp := os.Getenv("GAOS_BO_SESSION_KEY_FILE"); kp != "" {
		warnIfOpen(kp, logf)
	}
	if len(accts) == 0 {
		logf("auth: no back-office accounts; nobody can sign in")
	} else {
		logf("auth: %d back-office account(s)", len(accts))
	}
	return newAuthority(accts, loadKeyFile(os.Getenv("GAOS_BO_SESSION_KEY_FILE"), logf), label)
}

func (a *authority) configured() bool { return a != nil && len(a.accounts) > 0 }

// ---------- the session token ----------

// sign builds "v1.<b64url email>.<expiry>.<fp>.<b64url mac>". The payload has
// fixed shape and no dots inside a field, so splitting on the last dot is
// unambiguous.
func (a *authority) sign(email string, exp time.Time) string {
	acc := a.accounts[email]
	if acc == nil {
		return ""
	}
	payload := "v1." + base64.RawURLEncoding.EncodeToString([]byte(email)) + "." +
		strconv.FormatInt(exp.Unix(), 10) + "." + acc.fp
	return payload + "." + base64.RawURLEncoding.EncodeToString(a.mac(payload))
}

func (a *authority) mac(payload string) []byte {
	m := hmac.New(sha256.New, a.key)
	m.Write([]byte(a.label))
	m.Write([]byte{0})
	m.Write([]byte(payload))
	return m.Sum(nil)
}

// verify authenticates the MAC BEFORE parsing anything.
func (a *authority) verify(v string) (email string, exp time.Time, ok bool) {
	if a == nil || v == "" || len(v) > authTokenMax {
		return "", time.Time{}, false
	}
	i := strings.LastIndexByte(v, '.')
	if i < 0 {
		return "", time.Time{}, false
	}
	payload, raw := v[:i], v[i+1:]
	sig, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil || !hmac.Equal(a.mac(payload), sig) {
		return "", time.Time{}, false
	}
	f := strings.Split(payload, ".")
	if len(f) != 4 || f[0] != "v1" {
		return "", time.Time{}, false
	}
	eb, err := base64.RawURLEncoding.DecodeString(f[1])
	if err != nil {
		return "", time.Time{}, false
	}
	secs, err := strconv.ParseInt(f[2], 10, 64)
	if err != nil || a.now().Unix() >= secs {
		return "", time.Time{}, false
	}
	acc := a.accounts[string(eb)]
	if acc == nil || subtle.ConstantTimeCompare([]byte(f[3]), []byte(acc.fp)) != 1 {
		return "", time.Time{}, false
	}
	return acc.email, time.Unix(secs, 0), true
}

type sessionKey struct{}

type session struct {
	Email string
	Exp   time.Time
}

// sessionOf reads the verified session the gate stored on the request.
func sessionOf(r *http.Request) (session, bool) {
	s, ok := r.Context().Value(sessionKey{}).(session)
	return s, ok
}

// signedIn is the one reader of the cookie.
func (a *authority) signedIn(r *http.Request) bool {
	_, ok := a.sessionFrom(r)
	return ok
}

func (a *authority) sessionFrom(r *http.Request) (session, bool) {
	if a == nil {
		return session{}, false
	}
	if s, ok := sessionOf(r); ok {
		return s, true
	}
	c, err := r.Cookie(boCookie)
	if err != nil {
		return session{}, false
	}
	e, exp, ok := a.verify(c.Value)
	if !ok {
		return session{}, false
	}
	return session{Email: e, Exp: exp}, true
}

// ---------- login and the throttle ----------

type loginResult struct {
	Email      string // set on success
	Status     int
	Msg        string
	RetryAfter time.Duration
}

func ceilMinutes(d time.Duration) int { return int(math.Ceil(d.Minutes())) }

// login runs steps 2-6 of the design's order (the origin check is the gate's).
func (a *authority) login(rawEmail, password string) loginResult {
	if !a.configured() {
		return loginResult{Status: http.StatusUnprocessableEntity, Msg: msgNotConfigured}
	}
	email, ok := canonEmail(rawEmail)
	if !ok || password == "" {
		return loginResult{Status: http.StatusUnprocessableEntity, Msg: msgNeedBoth}
	}
	acc := a.accounts[email]
	if acc != nil {
		if left, locked := a.lockLeft(email); locked {
			return loginResult{Status: http.StatusUnprocessableEntity, RetryAfter: left,
				Msg: fmt.Sprintf("Too many attempts. Try again in %d minute(s).", ceilMinutes(left))}
		}
	}
	slots := a.unkSlots
	if acc != nil {
		slots = a.knownSlots
	}
	t := time.NewTimer(a.slotWait)
	defer t.Stop()
	select {
	case slots <- struct{}{}:
		defer func() { <-slots }()
	case <-t.C:
		return loginResult{Status: http.StatusUnprocessableEntity, Msg: msgBusy}
	}
	target := acc
	if target == nil {
		target = a.dummy
	}
	if target.verify(password) && acc != nil {
		a.mu.Lock()
		delete(a.known, email)
		a.mu.Unlock()
		return loginResult{Email: email, Status: http.StatusOK}
	}
	a.fail(email, acc != nil)
	return loginResult{Status: http.StatusUnprocessableEntity, Msg: msgNoMatch}
}

func (a *authority) lockLeft(email string) (time.Duration, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	e := a.known[email]
	if e == nil {
		return 0, false
	}
	now := a.now()
	e.seen = now
	if now.Before(e.until) {
		return e.until.Sub(now), true
	}
	return 0, false
}

// fail records one failed password check (step 6 is the only step that counts).
func (a *authority) fail(email string, known bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	now := a.now()
	tbl := a.unknown
	if known {
		tbl = a.known
	}
	e := tbl[email]
	if e == nil {
		if !known && len(tbl) >= authUnknownMax {
			var oldest string
			var ot time.Time
			for k, v := range tbl {
				if oldest == "" || v.seen.Before(ot) {
					oldest, ot = k, v.seen
				}
			}
			delete(tbl, oldest)
		}
		e = &attempts{first: now}
		tbl[email] = e
	}
	e.seen = now
	if now.Sub(e.first) > authWindow {
		e.n, e.first = 0, now
	}
	e.n++
	// Unknown emails only occupy memory; they never lock visibly.
	if known && e.n >= authMaxFails {
		e.n = 0
		e.until = now.Add(authLockout)
	}
}

// cookie builds the session cookie.
func (a *authority) cookie(email string) *http.Cookie {
	exp := a.now().Add(a.ttl)
	return &http.Cookie{
		Name: boCookie, Value: a.sign(email, exp), Path: "/", HttpOnly: true,
		SameSite: http.SameSiteLaxMode, Secure: secureCookies(), MaxAge: int(a.ttl.Seconds()),
	}
}

// ---------- the gate ----------

// publicPatterns is the fixed list of routes that answer a signed-out caller.
// Everything else that is not a bearer route needs a session, including any
// route added later.
var publicPatterns = map[string]bool{
	"GET /{$}":          true,
	"GET /healthz":      true,
	"GET /static/":      true,
	"POST /auth/login":  true,
	"POST /auth/logout": true,
	"GET /ui/needs":     true,
	"GET /ui/bo":        true,
}

type routeClass int

const (
	classBackOffice routeClass = iota
	classPublic
	classBearer
)

// gate wraps the mux. r.Pattern is empty outside the mux, so the matched
// pattern is looked up with mux.Handler. An empty one (404/405) goes to the
// mux, which answers as before: nothing is revealed and nothing is public.
// backOfficeCSP is the back office's Content-Security-Policy (D-082).
// base-uri is 'self' because page.html sets <base href>.
const backOfficeCSP = "frame-ancestors 'none'; object-src 'none'; base-uri 'self'; form-action 'self'"

func (s *server) gate(mux *http.ServeMux, bearer map[string]bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, pat := mux.Handler(r)
		if pat == "" {
			mux.ServeHTTP(w, r)
			return
		}
		class := classBackOffice
		switch {
		case publicPatterns[pat]:
			class = classPublic
		case bearer[pat]:
			class = classBearer
		}
		if class != classBearer {
			// D-082: the back office renders model-written text. The parts of a
			// policy that need no audit of the templates' inline handlers: no
			// framing, no plugins, no foreign <base>, no foreign form target.
			w.Header().Set("Content-Security-Policy", backOfficeCSP)
			// D-093: an address now names a product. It is not handed to
			// another origin in a Referer.
			w.Header().Set("Referrer-Policy", "same-origin")
		}
		if class != classBearer && !crossOriginOK(r) {
			http.Error(w, "cross-origin request refused", http.StatusForbidden)
			return
		}
		if class == classBackOffice {
			ss, ok := s.auth.sessionFrom(r)
			if !ok {
				if r.Header.Get("HX-Request") != "" {
					s.requireBO(w, r)
					return
				}
				// A page route is refused like any other (BR-55), but its
				// refusal is read by a person: the sign-in page, not JSON.
				// The handler is never called, so the body cannot say
				// whether the product in the URL exists (D-093).
				if pagePatterns[pat] {
					s.signedOutPage(w, r)
					return
				}
				jsonStatus(w, http.StatusUnauthorized, map[string]string{"error": "sign in to the back office"})
				return
			}
			r = r.WithContext(context.WithValue(r.Context(), sessionKey{}, ss))
		}
		mux.ServeHTTP(w, r)
	})
}

// crossOriginOK refuses a non-GET request that a browser marks cross-site, or
// whose Origin names another host. A request with neither header (curl, a
// test) passes: it still needs the session.
func crossOriginOK(r *http.Request) bool {
	switch r.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return true
	}
	switch r.Header.Get("Sec-Fetch-Site") {
	case "", "same-origin", "none":
	default:
		return false
	}
	if o := r.Header.Get("Origin"); o != "" {
		u, err := url.Parse(o)
		if err != nil || u.Host == "" || u.Host != r.Host {
			return false
		}
	}
	return true
}

// ---------- the hash-password subcommand ----------

// runHashPassword reads one line from in, writes one hash line to out.
func runHashPassword(in io.Reader, out io.Writer) error {
	line, err := bufio.NewReader(in).ReadString('\n')
	if err != nil && err != io.EOF {
		return err
	}
	pw := strings.TrimRight(line, "\r\n")
	if pw == "" {
		return errors.New("empty password")
	}
	h, err := hashPassword(pw)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(out, h)
	return err
}
