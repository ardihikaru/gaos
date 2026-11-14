package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// GAOS calling the QA tools (D-075, design §4.10, §4.11).
//
// The other half of the sync: the inbound API (api_v1.go) lets the QA tools
// call GAOS; this lets an operator's click make GAOS call the QA tools. Both
// end in the same two store methods, so there is one set of rules.
//
// The destination is operator configuration (GAOS_QA_URL), never request
// input, so there is no user-controlled URL to point at an internal service. It
// is also INERT until the QA tools implement their side (N1): with the URL or
// the token unset the buttons render disabled and the routes answer 409.
//
// What comes back is untrusted. A pull page is decoded strictly (a planning
// field in an item is a decode error), bounded in size and count, and applied
// through QaApplyResults like any inbound results post, so the QA tools can
// change automation facts and nothing else.

const (
	qaOutCall     = 15 * time.Second
	qaOutBudget   = 60 * time.Second
	qaOutMaxBody  = 2 << 20
	qaOutMaxPages = 20
	qaCooldown    = 30 * time.Second
)

// qaOutbound is the configured client. why is non-empty when it is not usable.
type qaOutbound struct {
	base  *url.URL
	token string
	why   string
	hc    *http.Client

	cooldown  time.Duration
	mu        sync.Mutex
	running   bool
	lastStart time.Time
}

// validQaURL accepts https, or http with an IP-literal host in loopback or the
// WireGuard subnet the QA tools sit on (10.10.0.0/24). It refuses userinfo, a
// query or fragment, and any host that GAOS's own gateway or database use: a
// bearer sent in cleartext to the wrong internal service is the failure this
// exists to prevent.
func validQaURL(raw string, denyHosts []string) (*url.URL, string) {
	u, err := url.Parse(strings.TrimSpace(raw))
	switch {
	case err != nil || u.Host == "":
		return nil, "GAOS_QA_URL is not a URL"
	case u.User != nil:
		return nil, "GAOS_QA_URL may not carry credentials"
	case u.RawQuery != "" || u.Fragment != "":
		return nil, "GAOS_QA_URL is a base address, with no query or fragment"
	}
	host := u.Hostname()
	for _, d := range denyHosts {
		if d != "" && strings.EqualFold(d, host) {
			return nil, "GAOS_QA_URL names a host GAOS already uses for its gateway or database"
		}
	}
	switch u.Scheme {
	case "https":
		return u, ""
	case "http":
		ip := net.ParseIP(host)
		_, wg, _ := net.ParseCIDR("10.10.0.0/24")
		if ip != nil && (ip.IsLoopback() || wg.Contains(ip)) {
			return u, ""
		}
		return nil, "plain http is allowed only to a loopback or 10.10.0.0/24 address; use https"
	}
	return nil, "GAOS_QA_URL is https (or http inside the tunnel)"
}

func newQaOutbound(rawURL, token string, denyHosts []string) *qaOutbound {
	o := &qaOutbound{cooldown: qaCooldown}
	o.hc = &http.Client{
		Timeout: qaOutCall,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return errors.New("the QA tools answered with a redirect; redirects are refused")
		},
	}
	switch {
	case strings.TrimSpace(rawURL) == "":
		o.why = "GAOS_QA_URL is not set"
	case token == "":
		o.why = "GAOS_QA_TOKEN_FILE holds no gaos_ token"
	default:
		if u, why := validQaURL(rawURL, denyHosts); why != "" {
			o.why = why
		} else {
			o.base, o.token = u, token
		}
	}
	return o
}

func qaOutboundFromEnv() *qaOutbound {
	hostOf := func(env string) string {
		if u, err := url.Parse(os.Getenv(env)); err == nil {
			return u.Hostname()
		}
		return ""
	}
	o := newQaOutbound(os.Getenv("GAOS_QA_URL"), readTokenFile("GAOS_QA_TOKEN_FILE"),
		[]string{hostOf("GAOS_ARCADE_URL"), hostOf("GAOS_AIM_URL")})
	if o.why != "" {
		log.Printf("qa sync: outbound is off (%s)", o.why)
	} else {
		log.Printf("qa sync: outbound to %s://%s", o.base.Scheme, o.base.Host)
	}
	return o
}

// begin takes the process-wide sync slot. One sync at a time, and one start per
// cooldown, whatever the product: iterating product ids does not get around it.
func (o *qaOutbound) begin() (release func(), code, reason string, wait time.Duration) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.running {
		return nil, "sync-running", "a sync is already running", 0
	}
	if since := time.Since(o.lastStart); !o.lastStart.IsZero() && since < o.cooldown {
		w := (o.cooldown - since).Round(time.Second)
		return nil, "cooldown", "the last sync started moments ago; wait " + w.String(), w
	}
	o.running, o.lastStart = true, time.Now()
	return func() { o.mu.Lock(); o.running = false; o.mu.Unlock() }, "", "", 0
}

// do sends one request and reads at most qaOutMaxBody. It returns the status
// and the body; a body over the cap is an error, not a truncation.
func (o *qaOutbound) do(ctx context.Context, method, path string, q url.Values, body any) (int, []byte, error) {
	u := *o.base
	u.Path = strings.TrimRight(u.Path, "/") + path
	u.RawQuery = q.Encode()
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return 0, nil, err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, u.String(), rd)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+o.token)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := o.hc.Do(req)
	if err != nil {
		// url.Error carries the URL, never the header; say only what failed.
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err
		}
		return 0, nil, fmt.Errorf("the QA tools could not be reached: %s", qaTrim(err.Error(), 100))
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, qaOutMaxBody+1))
	if err != nil {
		return resp.StatusCode, nil, err
	}
	if len(b) > qaOutMaxBody {
		return resp.StatusCode, nil, errors.New("the QA tools' answer is larger than 2 MiB")
	}
	return resp.StatusCode, b, nil
}

// ---------- the pull ----------

type qaPullPage struct {
	Product string         `json:"product"`
	Items   []QaResultItem `json:"items"`
	Next    string         `json:"next"`
	More    bool           `json:"more"`
}

// qaPull fetches pages of automation facts and applies each through st. It
// returns the side to record; it never returns an error for a failed sync,
// because a failed sync is a recorded outcome, not a fault.
func (o *qaOutbound) qaPull(ctx context.Context, st Store, productID string, from QaSyncSide) QaSyncSide {
	ctx, cancel := context.WithTimeout(ctx, qaOutBudget)
	defer cancel()
	side := QaSyncSide{At: qaNow(), Cursor: from.Cursor}
	var applied, unchanged, rejected int
	fail := func(why string) QaSyncSide {
		side.OK, side.Note = false, qaTrim(why, qaMaxNote)
		return side
	}
	for page := 0; page < qaOutMaxPages; page++ {
		q := url.Values{"product": {productID}, "limit": {strconv.Itoa(qaMaxItems)}}
		if side.Cursor != "" {
			q.Set("after", side.Cursor)
		}
		code, body, err := o.do(ctx, "GET", "/api/v1/gaos/results", q, nil)
		if err != nil {
			return fail(err.Error())
		}
		if code < 200 || code > 299 {
			return fail(fmt.Sprintf("the QA tools answered HTTP %d", code))
		}
		var pg qaPullPage
		if err := decodeStrict(bytes.NewReader(body), &pg); err != nil {
			return fail("the QA tools' page is not the agreed shape: " + qaTrim(err.Error(), 80))
		}
		switch {
		case pg.Product != productID:
			return fail("the QA tools' page is for another product; refused")
		case len(pg.Items) > qaMaxItems:
			return fail("the QA tools' page holds more than 200 items; refused")
		case !validCursor(pg.Next):
			return fail("the QA tools' cursor is not short printable text; refused")
		}
		if len(pg.Items) > 0 {
			out, _, err := st.QaApplyResults(ctx, productID, QaResultsIn{Source: "pull", Items: pg.Items})
			if err != nil {
				return fail("applying the page failed: " + qaTrim(err.Error(), 80))
			}
			applied += out.Applied
			unchanged += out.Unchanged
			rejected += len(out.Rejected)
		}
		side.Items += len(pg.Items)
		side.Cursor = pg.Next
		if !pg.More {
			side.OK = true
			side.Note = fmt.Sprintf("%d applied, %d unchanged, %d rejected", applied, unchanged, rejected)
			return side
		}
	}
	side.OK = true
	side.Note = fmt.Sprintf("%d applied, %d unchanged, %d rejected; more waits, press pull again", applied, unchanged, rejected)
	return side
}

// ---------- the push ----------

type qaPushBody struct {
	Product struct {
		ID   string `json:"id"`
		Name string `json:"name"`
		Repo string `json:"repo"`
	} `json:"product"`
	Cases   []QaCase `json:"cases"`
	CatHead int      `json:"catHead"`
}

type qaPushAnswer struct {
	Accepted int        `json:"accepted"`
	Rejected []QaReject `json:"rejected"`
}

// qaPush sends the cases whose planning half changed since the last push, in
// batches, advancing the cursor after each accepted batch and stopping at the
// first refusal.
func (o *qaOutbound) qaPush(ctx context.Context, st Store, p Product, from QaSyncSide) QaSyncSide {
	ctx, cancel := context.WithTimeout(ctx, qaOutBudget)
	defer cancel()
	side := QaSyncSide{At: qaNow(), Cursor: from.Cursor}
	var rejectedIDs []string
	fail := func(why string) QaSyncSide {
		side.OK, side.Note, side.Rejected = false, qaTrim(why, qaMaxNote), rejectedIDs
		return side
	}
	after, _ := strconv.Atoi(from.Cursor)
	for page := 0; page < qaOutMaxPages; page++ {
		pg, err := st.QaCases(ctx, p.ID, QaQuery{After: after, Limit: qaMaxItems})
		if err != nil {
			return fail("reading the catalog failed: " + qaTrim(err.Error(), 80))
		}
		if len(pg.Items) == 0 {
			side.OK = true
			side.Note = fmt.Sprintf("%d sent, %d rejected by the QA tools", side.Items, side.RejectedN)
			side.Rejected = rejectedIDs
			return side
		}
		var body qaPushBody
		body.Product.ID, body.Product.Name, body.Product.Repo = p.ID, p.Name, p.Repo
		body.Cases, body.CatHead = pg.Items, pg.CatHead
		code, raw, err := o.do(ctx, "POST", "/api/v1/gaos/catalog", nil, body)
		if err != nil {
			return fail(err.Error())
		}
		if code < 200 || code > 299 {
			return fail(fmt.Sprintf("the QA tools answered HTTP %d; stopped before cursor %d", code, after))
		}
		var ans qaPushAnswer
		if err := decodeStrict(bytes.NewReader(raw), &ans); err != nil {
			return fail("the QA tools' answer is not the agreed shape: " + qaTrim(err.Error(), 80))
		}
		for _, rj := range ans.Rejected {
			side.RejectedN++
			if qaRejectedID.MatchString(rj.CaseID) && len(rejectedIDs) < qaMaxRejected {
				rejectedIDs = append(rejectedIDs, rj.CaseID)
			}
		}
		side.Items += len(pg.Items)
		after = pg.Items[len(pg.Items)-1].CatRev
		side.Cursor = strconv.Itoa(after)
		if pg.Next == nil {
			side.OK = true
			side.Note = fmt.Sprintf("%d sent, %d rejected by the QA tools", side.Items, side.RejectedN)
			side.Rejected = rejectedIDs
			return side
		}
	}
	side.OK = true
	side.Note = fmt.Sprintf("%d sent, %d rejected; more waits, press push again", side.Items, side.RejectedN)
	side.Rejected = rejectedIDs
	return side
}
