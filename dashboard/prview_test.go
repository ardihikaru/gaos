package main

import (
	"context"
	"fmt"
	"html"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

// The back office's Pull requests view (D-092, design §5). Names are invented (D-085); hostile strings
// are built from a random sentinel so no needle can occur by accident in markup.

func prSection(body string) string {
	i := strings.Index(body, `<section id="boPRs">`)
	if i < 0 {
		return ""
	}
	j := strings.Index(body[i:], `</section>`)
	if j < 0 {
		return body[i:]
	}
	return body[i : i+j]
}

func prGet(t *testing.T, f afFixture, path string) string {
	t.Helper()
	rec := do(t, f.srv, "GET", path, nil, boCookies()...)
	if rec.Code != 200 {
		t.Fatalf("GET %s answered %d: %s", path, rec.Code, rec.Body)
	}
	return rec.Body.String()
}

// prFixtureWithRows adds a review row and a QA row to the Auto-fix fixture's two fix pull requests.
func prFixtureWithRows(t *testing.T) afFixture {
	t.Helper()
	f := newAFFixture(t)
	ctx := context.Background()
	f.s.mu.Lock()
	m := f.s.masterPtr(f.pid)
	m.Repos = append(m.Repos, "loop/laundry-api")
	m.AutoFix.AutomationRepos = []string{"loop/laundry-auto"}
	f.s.mu.Unlock()
	now := time.Now().Unix()
	if _, err := f.s.UpsertPullRequest(ctx, PullRequest{ProductID: f.pid, Repo: "loop/laundry-api", Number: 31, Kind: PRKindReview,
		Title: "tidy the cart", HeadRef: "feature/cart", BaseRef: "main", TaskID: "T-31", OpenedAt: now - 7200}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.UpsertPullRequest(ctx, PullRequest{ProductID: f.pid, Repo: "loop/laundry-auto", Number: 8, Kind: PRKindQA,
		Title: "automate CART-12", HeadRef: "gaos/auto-cart-12", BaseRef: "main", QAJob: "job-7", QACase: "CART-12",
		QALink: "https://qa.example.test/jobs/job-7", OpenedAt: now - 3600}); err != nil {
		t.Fatal(err)
	}
	return f
}

// prBulk is enough pull requests to need many pages.
const prBulk = 200

func TestPRViewRowsAndFilters(t *testing.T) {
	f := prFixtureWithRows(t)
	rec := do(t, f.srv, "GET", "/ui/bo/prs", nil, boCookies()...)
	if rec.Code != 200 || rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("the view answered %d with Cache-Control %q", rec.Code, rec.Header().Get("Cache-Control"))
	}
	sec := prSection(rec.Body.String())
	if sec == "" {
		t.Fatal("the sheet carries no Pull requests section")
	}
	for _, want := range []string{
		`acme-org/sample-service#12`, `href="https://github.com/acme-org/sample-service/pull/12" target="_blank" rel="noopener noreferrer"`,
		`af-st af-st--draft">Draft`, `prs-kind--fix">Auto-fix`,
		`loop/laundry-api#31`, `tidy the cart`, `prs-kind--review">Review`, `feature/cart`,
		`loop/laundry-auto#8`, `prs-kind--qa">QA tool`, `CART-12`, `href="https://qa.example.test/jobs/job-7"`,
		`data-timing-open="ui/timing/` + f.pid + `/T-31"`, `hx-trigger="every 30s `, `ui/bo/prs/live`,
		`>Pull requests</a>`,
	} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Errorf("the view lacks %q", want)
		}
	}
	// The merged record is under the Merged tab, not the default Open one.
	if strings.Contains(sec, "acme-org/sample-service#9<") {
		t.Error("a merged pull request is listed under Open")
	}
	if body := prGet(t, f, "/ui/bo/prs?state=merged"); !strings.Contains(prSection(body), "acme-org/sample-service#9") || strings.Contains(prSection(body), "loop/laundry-api#31") {
		t.Error("the Merged tab does not show exactly the merged pull requests")
	}
	// The kind filter and the product filter.
	if body := prSection(prGet(t, f, "/ui/bo/prs?kind=qa")); !strings.Contains(body, "loop/laundry-auto#8") || strings.Contains(body, "loop/laundry-api#31") || strings.Contains(body, "sample-service#12") {
		t.Error("the QA filter does not narrow the list to QA pull requests")
	}
	if body := prSection(prGet(t, f, "/ui/bo/prs?product=zzz-unknown")); !strings.Contains(body, "loop/laundry-api#31") {
		t.Error("an unknown product must be ignored, not narrow the list to nothing")
	}
	// The poll answers the list as one out-of-band element and no drawer.
	live := prGet(t, f, "/ui/bo/prs/live?open=fix:"+f.pr1)
	if !strings.Contains(live, `id="prsLive"`) || !strings.Contains(live, `hx-swap-oob="true"`) || strings.Contains(live, `id="prsDrawer"`) {
		t.Error("the poll is not the list alone, out of band")
	}
	assertEmptyStatesAndTheCap(t)
	// The drawer: facts, related tasks, no GitHub call.
	dr := prSection(prGet(t, f, "/ui/bo/prs?open=fix:"+f.pr1))
	for _, want := range []string{`id="prsDrawer"`, `gaos/fix-def-101`, `Related tasks`, `data-timing-open="ui/timing/` + f.pid + `/`} {
		if !strings.Contains(dr, want) {
			t.Errorf("the drawer lacks %q", want)
		}
	}
}

func assertEmptyStatesAndTheCap(t *testing.T) {
	t.Helper()
	srv, _ := newTestServer(t)
	s := NewMemStore(1)
	srv.store = s
	f := afFixture{srv: srv, s: s, pid: "p-1"}
	body := prSection(prGet(t, f, "/ui/bo/prs"))
	for _, want := range []string{`No pull request is tied to a task yet`, `automationRepos`} {
		if !strings.Contains(body, want) {
			t.Errorf("an empty product lacks %q", want)
		}
	}
	s.mu.Lock()
	m := s.masterPtr("p-1")
	m.Repos = append(m.Repos, "loop/laundry-api")
	for n := 1; n <= prBulk+5; n++ {
		s.pullRequests[pullRequestKey("p-1", "loop/laundry-api", n)] = PullRequest{Key: pullRequestKey("p-1", "loop/laundry-api", n),
			ProductID: "p-1", Repo: "loop/laundry-api", Number: n, Kind: PRKindReview, State: PRStateOpen, OpenedAt: int64(1000 + n)}
	}
	// a row not read for over 30 days, and one of a repository the product stopped following
	s.pullRequests["old"] = PullRequest{Key: "old", ProductID: "p-1", Repo: "loop/laundry-api", Number: 999, Kind: PRKindReview, State: PRStateOpen, OpenedAt: 1, CheckedAt: 2}
	s.pullRequests["gone"] = PullRequest{Key: "gone", ProductID: "p-1", Repo: "acme-org/left", Number: 998, Kind: PRKindReview, State: PRStateOpen, OpenedAt: 3}
	s.mu.Unlock()
	body = prSection(prGet(t, f, "/ui/bo/prs"))
	if !strings.Contains(body, fmt.Sprintf("Showing 1–10 of %d", prBulk+7)) || strings.Count(body, `class="prs-r`)+strings.Count(body, `<tr class="af-r`) != tablePageSize {
		t.Errorf("the list is not one page of %d with its count announced: %q", tablePageSize, body[max(0, len(body)-400):])
	}
	// With the bulk gone, the two old rows are listed and say why they are not polled.
	s.mu.Lock()
	for n := 1; n <= prBulk+5; n++ {
		delete(s.pullRequests, pullRequestKey("p-1", "loop/laundry-api", n))
	}
	s.mu.Unlock()
	body = prSection(prGet(t, f, "/ui/bo/prs?state=all"))
	for _, want := range []string{"not followed since", "no longer followed"} {
		if !strings.Contains(body, want) {
			t.Errorf("a row that is no longer polled does not say %q", want)
		}
	}
}

func TestPRViewEscapesGitHubText(t *testing.T) {
	f := prFixtureWithRows(t)
	ctx := context.Background()
	sent := fmt.Sprintf("sent%d", time.Now().UnixNano())
	hostile := "<script>alert('" + sent + "')</script>‮evil​ <img src=x onerror=" + sent + ">"
	f.s.mu.Lock()
	k := pullRequestKey(f.pid, "loop/laundry-api", 31)
	r := f.s.pullRequests[k]
	r.Title, r.Author = hostile, hostile
	f.s.pullRequests[k] = r
	// A QA link that is not https, a branch name that is markup, a secret-shaped title.
	k2 := pullRequestKey(f.pid, "loop/laundry-auto", 8)
	r2 := f.s.pullRequests[k2]
	r2.QALink, r2.HeadRef = "javascript:alert('"+sent+"')", "<b>"+sent+"</b>"
	f.s.pullRequests[k2] = r2
	// The stored URL of a fix record is never used for its link.
	fx := f.s.fixPRs[f.pr1]
	fx.URL, fx.Branch = "https://evil.example.test/"+sent, "<i>"+sent+"</i>"
	f.s.fixPRs[f.pr1] = fx
	f.s.mu.Unlock()
	if _, err := f.s.UpsertPullRequest(ctx, PullRequest{ProductID: f.pid, Repo: "loop/laundry-api", Number: 33, Kind: PRKindReview,
		Title: "token AKIAIOSFODNN7EXAMPLE leaked", OpenedAt: time.Now().Unix()}); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/ui/bo/prs", "/ui/bo/prs?open=pr:" + k, "/ui/bo/prs?open=fix:" + f.pr1} {
		body := prGet(t, f, path)
		for _, bad := range []string{"<script>alert", "<img src=x", "javascript:", "evil.example.test", "<b>" + sent, "<i>" + sent, "‮", "​", "AKIAIOSFODNN7EXAMPLE"} {
			if strings.Contains(body, bad) {
				t.Errorf("%s: the page carries %q", path, bad)
			}
		}
		if !strings.Contains(body, "github.com/acme-org/sample-service/pull/12") {
			t.Errorf("%s: the fix pull request's link is not built from its owner, repository and number", path)
		}
	}
	if body := prGet(t, f, "/ui/bo/prs"); !strings.Contains(body, "&lt;script&gt;") || !strings.Contains(body, "withheld") {
		t.Error("the hostile title is neither escaped nor the secret withheld")
	}
}

// ---- the drawer's retarget block (design §6.1) ----

func TestTheDrawerExplainsEachDisabledRetarget(t *testing.T) {
	autoFixAdminsFile(t, 0o600, testEmail)
	open := func(f *rtFixture) string {
		return prGet(t, f.afFixture, "/ui/bo/prs?open=fix:"+f.pr1)
	}
	hint := func(body string) string {
		i := strings.Index(body, `id="prsRtHint"`)
		if i < 0 {
			return ""
		}
		j := strings.Index(body[i:], "</p>")
		return body[i : i+j]
	}
	for _, c := range []struct {
		name   string
		mod    func(*rtFixture)
		says   string
		off    bool
		nobase bool
	}{
		{"every condition holds", nil, "GAOS asks the AI to redo this fix on the new base", false, false},
		{"not an admin", func(f *rtFixture) { autoFixAdminsFile(t, 0o600, "someone-else@example.test") }, "Only accounts on the auto-fix admin list", true, false},
		{"a head GAOS did not create", func(f *rtFixture) { f.setFix(func(x *FixPR) { x.Branch = "feature/x" }) }, "only changes the base of pull requests it opened", true, false},
		{"closed", func(f *rtFixture) { f.setFix(func(x *FixPR) { x.State = FixPRClosed }) }, "This pull request is closed", true, false},
		{"base not known", func(f *rtFixture) { f.setFix(func(x *FixPR) { x.Base = "" }) }, "not known yet", true, false},
		{"already retargeted", func(f *rtFixture) { f.setFix(func(x *FixPR) { x.RetargetedTo = "fx-next" }) }, "already retargeted", true, false},
		{"a task is working", func(f *rtFixture) { f.setFixCard(StatusRunning) }, "A task is already working on this defect", true, false},
		{"three retargets", func(f *rtFixture) {
			f.s.mu.Lock()
			for i := 1; i <= retargetMaxChain; i++ {
				id := "fp-old-" + string(rune('0'+i))
				f.s.fixPRs[id] = FixPR{ID: id, ProductID: f.pid, DefectKey: f.pid + "/DEF-101", RetargetedFrom: "x", State: FixPRClosed}
			}
			f.s.mu.Unlock()
		}, "retargeted three times", true, false},
		{"no other allowed base", func(f *rtFixture) { f.setMaster(func(m *Master) { m.AutoFix.AllowedBases = []string{"develop"} }) }, "No other base branch is allowed", true, true},
	} {
		f := newRetargetFixture(t)
		if c.mod != nil {
			c.mod(f)
		}
		body := open(f)
		h := hint(body)
		if !strings.Contains(h, c.says) {
			t.Errorf("%s: the hint is %q; want it to say %q", c.name, h, c.says)
		}
		btn := body[strings.Index(body, `<button type="submit" class="btn btn-primary" aria-describedby="prsRtHint"`):]
		btn = btn[:strings.Index(btn, ">")]
		if got := strings.Contains(btn, " disabled"); got != c.off {
			t.Errorf("%s: the button disabled = %v, want %v", c.name, got, c.off)
		}
		if c.off && !strings.Contains(body, `aria-describedby="prsRtHint"`) {
			t.Errorf("%s: the disabled control is not tied to its hint", c.name)
		}
		if !strings.Contains(body, `hx-request='{"timeout":20000}'`) {
			t.Errorf("%s: the form has no time limit", c.name)
		}
		if !c.off && !strings.Contains(body, `<option value="staging">`) {
			t.Errorf("%s: the allowed base is not offered", c.name)
		}
		if c.name != "base not known" && strings.Contains(body, `<option value="develop">`) {
			t.Errorf("%s: the current base is offered", c.name)
		}
		if len(f.br.calls) != 0 {
			t.Errorf("%s: opening the drawer made %d GitHub call(s)", c.name, len(f.br.calls))
		}
	}
	// A review row has no retarget block at all.
	f := newRetargetFixture(t)
	f.s.mu.Lock()
	f.s.masterPtr(f.pid).Repos = append(f.s.masterPtr(f.pid).Repos, "loop/laundry-api")
	f.s.mu.Unlock()
	if _, err := f.s.UpsertPullRequest(context.Background(), PullRequest{ProductID: f.pid, Repo: "loop/laundry-api", Number: 31, Kind: PRKindReview, OpenedAt: time.Now().Unix()}); err != nil {
		t.Fatal(err)
	}
	if body := prGet(t, f.afFixture, "/ui/bo/prs?open=pr:"+pullRequestKey(f.pid, "loop/laundry-api", 31)); strings.Contains(body, "Change the base branch") {
		t.Error("a review pull request offers a retarget")
	}
}

// TC-758: Pull requests and Auto-fix are paged by ten (D-097). A row's link, the
// drawer's close and the poll keep the page; a state tab starts over; a page
// that is not there is clamped before anything writes it.
func TestThePRAndAutoFixTablesPageByTen(t *testing.T) {
	tabs := func(sec string) string {
		i := strings.Index(sec, `<nav class="af-seg"`)
		if i < 0 {
			return ""
		}
		return sec[i : i+strings.Index(sec[i:], "</nav>")]
	}
	rowHrefs := func(sec string) []string {
		var out []string
		for _, m := range regexp.MustCompile(`data-af-row="[^"]*" href="[^"]*" hx-get="([^"]+)"`).FindAllStringSubmatch(sec, -1) {
			out = append(out, html.UnescapeString(m[1]))
		}
		return out
	}
	check := func(name, base string, f afFixture, get func(*testing.T, afFixture, string) string, section func(string) string, _ func(string) string) {
		t.Helper()
		// The fixture holds a row or two of its own: the total is read, and must
		// make exactly two pages.
		p1 := section(get(t, f, base))
		m := regexp.MustCompile(`Showing 1–10 of (\d+)`).FindStringSubmatch(p1)
		total := 0
		if m != nil {
			total, _ = strconv.Atoi(m[1])
		}
		if n := len(rowHrefs(p1)); n != tablePageSize || total <= tablePageSize || total > 2*tablePageSize {
			t.Fatalf("%s page 1: %d row(s) of %d; want %d of 11–20, and \"Showing 1–10 of N\"", name, n, total, tablePageSize)
		}
		p2 := section(get(t, f, base+"?page=2"))
		hrefs := rowHrefs(p2)
		if len(hrefs) != total-tablePageSize {
			t.Fatalf("%s page 2: %d row(s), want %d", name, len(hrefs), total-tablePageSize)
		}
		for _, h := range hrefs {
			if !strings.Contains(h, "page=2") {
				t.Errorf("%s page 2: the row link %q does not keep page=2", name, h)
			}
		}
		if !strings.Contains(p2, `/live?page=2"`) {
			t.Errorf("%s page 2: the poll does not keep page=2", name)
		}
		if strings.Contains(tabs(p2), "page=") {
			t.Errorf("%s page 2: a state tab carries the page; a change of state starts on page 1", name)
		}
		open := section(get(t, f, "/"+strings.TrimPrefix(hrefs[0], "/")))
		if m := regexp.MustCompile(`data-af-close href="[^"]*" hx-get="([^"]+)"`).FindStringSubmatch(open); m == nil || !strings.Contains(m[1], "page=2") {
			t.Errorf("%s: the drawer opened from page 2 does not close back to page 2", name)
		}
		for _, bad := range []string{"-1", "abc"} {
			if n := len(rowHrefs(section(get(t, f, base+"?page="+bad)))); n != tablePageSize {
				t.Errorf("%s ?page=%s: %d row(s), want page 1", name, bad, n)
			}
		}
		far := section(get(t, f, base+"?page=99"))
		for _, h := range rowHrefs(far) {
			if !strings.Contains(h, "page=2") || strings.Contains(h, "page=99") {
				t.Errorf("%s ?page=99: the row link %q does not carry the clamped page=2", name, h)
			}
		}
	}

	// Pull requests: twelve review pull requests of one product.
	pf := newAFFixture(t)
	pf.s.mu.Lock()
	m := pf.s.masterPtr(pf.pid)
	m.Repos = append(m.Repos, "loop/laundry-api")
	for n := 1; n <= 12; n++ {
		k := pullRequestKey(pf.pid, "loop/laundry-api", n)
		pf.s.pullRequests[k] = PullRequest{Key: k, ProductID: pf.pid, Repo: "loop/laundry-api", Number: n, Kind: PRKindReview,
			State: PRStateOpen, OpenedAt: time.Now().Unix() - int64(n)}
	}
	pf.s.mu.Unlock()
	check("Pull requests", "/ui/bo/prs", pf, prGet, prSection, nil)

	// Auto-fix: twelve fix pull requests.
	af := newAFFixture(t)
	now := time.Now().Unix()
	af.s.mu.Lock()
	for i := 0; i < 12; i++ {
		key := fmt.Sprintf("%s/DEF-%d", af.pid, 2000+i)
		af.s.defects[key] = &Defect{Key: key, ProductID: af.pid, DefectID: fmt.Sprintf("DEF-%d", 2000+i), CaseID: defCase1,
			Title: fmt.Sprintf("defect %d", i), Difficulty: afDifficulties[0], State: DefectStateFixing, Gen: 1}
		af.s.fixPRs[fmt.Sprintf("pg-%02d", i)] = FixPR{ID: fmt.Sprintf("pg-%02d", i), ProductID: af.pid, DefectKey: key,
			Repo: "loop/laundry-api", Number: 3000 + i, State: FixPRDraft, CreatedAt: now - int64(i)*60, TaskID: fmt.Sprint("T-", i)}
	}
	af.s.mu.Unlock()
	check("Auto-fix", "/ui/bo/autofix", af, afGet, afSection, nil)
}
