package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"
)

// D-108 (docs/designs/autofix-dedup-and-tabs.md): a retarget closes the pull request it
// replaced, the Auto-fix page lists duplicates and an admin closes one, each row says its
// branch and its trigger, Cards waiting pages by five, and the page has three sections.

func recByID(t *testing.T, s *MemStore, pid, id string) FixPR {
	t.Helper()
	recs, _ := s.FixPRs(context.Background(), pid)
	for _, r := range recs {
		if r.ID == id {
			return r
		}
	}
	t.Fatalf("no record %s", id)
	return FixPR{}
}

func lastEventOf(s *MemStore, typ EventType) Event {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for i := len(s.events) - 1; i >= 0; i-- {
		if s.events[i].Type == typ {
			return s.events[i]
		}
	}
	return Event{}
}

// TC-906
func TestARetargetClosesThePullRequestItReplaced(t *testing.T) {
	e, old, card := retargetedEnv(t, "staging")
	ctx := context.Background()
	e.fx.afterRun(ctx, card, fixReplyText(goodFix()))
	if n := e.gh.count("PATCH /repos/acme-org/sample-service/pulls/" + strconv.Itoa(old.Number)); n != 1 {
		t.Fatalf("the replaced pull request was closed %d times, want once", n)
	}
	if b := string(e.gh.body("PATCH /repos/acme-org/sample-service/pulls/" + strconv.Itoa(old.Number))); !strings.Contains(b, `"state":"closed"`) {
		t.Errorf("the close sent %s; want state closed", b)
	}
	if r := recByID(t, e.s, e.pid, old.ID); r.State != FixPRClosed {
		t.Errorf("the replaced record is %s; want closed", r.State)
	}
	if ev := lastEventOf(e.s, FixPRStateChanged); ev.Payload.Rule != RuleRetarget || ev.Payload.To != FixPRClosed {
		t.Errorf("the close event is %+v; want rule %s to closed", ev.Payload, RuleRetarget)
	}
	d := e.defect()
	if d.State != DefectStateFixing || d.FixPR == old.ID || d.Gen != 2 {
		t.Errorf("the defect is %s on %q gen %d; want fixing on the new record, generation 2", d.State, d.FixPR, d.Gen)
	}
	if c := cardByKey(e.s, defectCardKey(e.key, 3, "fix")); c != nil {
		t.Errorf("the close planned a card %s; want none", c.ID)
	}
	if c := string(e.gh.body("POST /repos/acme-org/sample-service/issues/" + strconv.Itoa(old.Number) + "/comments")); !strings.Contains(c, "GAOS closed this pull request; its branch is kept.") {
		t.Errorf("the replaced pull request's comment is %q; want the closed sentence", c)
	}
}

// TC-907
func TestARetargetCloseFailureIsFlaggedAndSwept(t *testing.T) {
	e, old, card := retargetedEnv(t, "staging")
	ctx := context.Background()
	e.gh.closeFail = 500
	e.fx.afterRun(ctx, card, fixReplyText(goodFix()))
	if r := recByID(t, e.s, e.pid, old.ID); r.State != FixPRDraft {
		t.Fatalf("a failed close left the record %s; want draft", r.State)
	}
	if c := string(e.gh.body("POST /repos/acme-org/sample-service/issues/" + strconv.Itoa(old.Number) + "/comments")); !strings.Contains(c, "left open for a person to close") {
		t.Errorf("after a failed close the comment is %q; want the left-open sentence", c)
	}
	recs, _ := e.s.FixPRs(ctx, e.pid)
	dups := afDuplicates(recs, map[string]Defect{e.key: e.defect()})
	if len(dups) != 1 || dups[0].Rule != dupSuperseded || dups[0].Close != old.ID {
		t.Fatalf("the duplicate check gave %+v; want one superseded finding for %s", dups, old.ID)
	}
	e.gh.closeFail = 0
	e.fx.resumeFixPRs(ctx)
	if r := recByID(t, e.s, e.pid, old.ID); r.State != FixPRClosed {
		t.Errorf("the boot sweep left the record %s; want closed", r.State)
	}
}

// TC-908
func TestAfDuplicatesRules(t *testing.T) {
	rec := func(id, key, state string, created int64, n int) FixPR {
		return FixPR{ID: id, ProductID: "p-1", DefectKey: key, Repo: defRepo, Number: n, State: state, CreatedAt: created, Base: "main"}
	}
	// S: open replaced by open, and by merged; none when the successor was closed unmerged.
	a, b := rec("a", "k1", FixPRDraft, 1, 1), rec("b", "k1", FixPRDraft, 2, 2)
	a.RetargetedTo, b.RetargetedFrom = "b", "a"
	got := afDuplicates([]FixPR{a, b}, map[string]Defect{"k1": {Key: "k1", FixPR: "b"}})
	if len(got) != 1 || got[0].Rule != dupSuperseded || got[0].Close != "a" || got[0].Keep != "b" {
		t.Errorf("an open replaced record gave %+v; want one superseded finding closing a", got)
	}
	b.State = FixPRMerged
	if got := afDuplicates([]FixPR{a, b}, nil); len(got) != 1 || got[0].Close != "a" {
		t.Errorf("a record replaced by a merged one gave %+v; want it superseded", got)
	}
	b.State = FixPRClosed
	if got := afDuplicates([]FixPR{a, b}, nil); len(got) != 0 {
		t.Errorf("a record whose successor closed unmerged gave %+v; want none", got)
	}
	// The chain A -> B -> C: two superseded findings, and the same-defect rule adds nothing.
	a, b, c := rec("a", "k1", FixPRDraft, 1, 1), rec("b", "k1", FixPRDraft, 2, 2), rec("c", "k1", FixPRReady, 3, 3)
	a.RetargetedTo, b.RetargetedTo = "b", "c"
	got = afDuplicates([]FixPR{c, b, a}, map[string]Defect{"k1": {Key: "k1", FixPR: "c"}})
	if len(got) != 2 || got[0].Close != "a" || got[1].Close != "b" || got[0].Rule != dupSuperseded || got[1].Rule != dupSuperseded {
		t.Errorf("the chain gave %+v; want a then b superseded", got)
	}
	// D: the defect's own record is kept even when it is older; else the newest.
	x, y := rec("x", "k2", FixPRDraft, 5, 5), rec("y", "k2", FixPRDraft, 9, 6)
	got = afDuplicates([]FixPR{x, y}, map[string]Defect{"k2": {Key: "k2", FixPR: "x"}})
	if len(got) != 1 || got[0].Rule != dupSameDefect || got[0].Close != "y" || got[0].Keep != "x" {
		t.Errorf("two open records of one defect gave %+v; want y closed, the defect's x kept", got)
	}
	got = afDuplicates([]FixPR{x, y}, map[string]Defect{"k2": {Key: "k2"}})
	if len(got) != 1 || got[0].Close != "x" || got[0].Keep != "y" {
		t.Errorf("with no live record the check gave %+v; want the newest kept", got)
	}
	// Final records are never closed; one open record of a defect is not a duplicate.
	z := rec("z", "k3", FixPRClosed, 1, 7)
	w := rec("w", "k3", FixPRDraft, 2, 8)
	if got := afDuplicates([]FixPR{z, w}, nil); len(got) != 0 {
		t.Errorf("a closed and an open record gave %+v; want none", got)
	}
	// The order: superseded before same defect.
	got = afDuplicates([]FixPR{x, y, rec("a2", "k1", FixPRDraft, 100, 1), func() FixPR { r := rec("a1", "k1", FixPRDraft, 0, 1); r.RetargetedTo = "a2"; return r }()}, nil)
	if len(got) != 2 || got[0].Rule != dupSuperseded || got[1].Rule != dupSameDefect {
		t.Errorf("the order is %+v; want superseded first", got)
	}
}

// dedupFixture is the retarget fixture with fp-1 replaced by an open fp-3, a write client
// and an admin account.
func dedupFixture(t *testing.T) (*rtFixture, *fakeGHW) {
	t.Helper()
	f := newRetargetFixture(t)
	gh := newFakeGHW()
	f.srv.fixWriter = newFakeWriter(t, gh)
	ctx := context.Background()
	now := time.Now().Unix()
	if _, err := f.s.UpsertFixPR(ctx, FixPR{ID: "fp-3", ProductID: f.pid, DefectKey: f.pid + "/DEF-101", Repo: defRepo, Number: 14,
		State: FixPRDraft, CreatedAt: now, Branch: "gaos/fix-def-101-2", Base: "staging", RetargetedFrom: f.pr1}); err != nil {
		t.Fatal(err)
	}
	f.s.mu.Lock()
	r := f.s.fixPRs[f.pr1]
	r.RetargetedTo = "fp-3"
	f.s.fixPRs[f.pr1] = r
	f.s.defects[f.pid+"/DEF-101"].FixPR = "fp-3"
	f.s.mu.Unlock()
	return f, gh
}

func (f *rtFixture) closePost(t *testing.T, id string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	return do(t, f.srv, "POST", "/products/"+f.pid+"/prs/"+id+"/close", url.Values{"product": {f.pid}}, cookies...)
}

// TC-909
func TestTheCloseRoute(t *testing.T) {
	f, gh := dedupFixture(t)
	if rec := f.closePost(t, f.pr1); rec.Code != http.StatusUnauthorized {
		t.Errorf("no session answered %d; want 401", rec.Code)
	}
	autoFixAdminsFile(t, 0o600, "someone@example.test")
	if rec := f.closePost(t, f.pr1, boCookies()...); rec.Code != http.StatusForbidden {
		t.Errorf("a non-admin answered %d; want 403", rec.Code)
	}
	autoFixAdminsFile(t, 0o600, testEmail)
	if rec := f.closePost(t, "fp-404", boCookies()...); rec.Code != http.StatusNotFound {
		t.Errorf("an unknown record answered %d; want 404", rec.Code)
	}
	if rec := f.closePost(t, "fp-3", boCookies()...); rec.Code != http.StatusConflict {
		t.Errorf("a record that is no duplicate answered %d; want 409", rec.Code)
	}
	gh.closeFail = 500
	if rec := f.closePost(t, f.pr1, boCookies()...); rec.Code != http.StatusBadGateway {
		t.Errorf("a GitHub failure answered %d; want 502", rec.Code)
	}
	if r := recByID(t, f.s, f.pid, f.pr1); r.State != FixPRDraft {
		t.Errorf("a failed close left the record %s; want draft", r.State)
	}
	gh.closeFail = 0
	f.s.mu.Lock()
	r := f.s.fixPRs[f.pr1]
	r.Branch = "feature/mine"
	f.s.fixPRs[f.pr1] = r
	f.s.mu.Unlock()
	if rec := f.closePost(t, f.pr1, boCookies()...); rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("a branch that is not gaos/fix- answered %d; want 422", rec.Code)
	}
	f.s.mu.Lock()
	r.Branch = "gaos/fix-def-101"
	f.s.fixPRs[f.pr1] = r
	f.s.mu.Unlock()
	rec := f.closePost(t, f.pr1, boCookies()...)
	if rec.Code != http.StatusOK {
		t.Fatalf("the close answered %d: %s", rec.Code, rec.Body)
	}
	if gh.prStates[12] != "closed" {
		t.Errorf("GitHub was not asked to close #12")
	}
	if r := recByID(t, f.s, f.pid, f.pr1); r.State != FixPRClosed {
		t.Errorf("the record is %s after the close; want closed", r.State)
	}
	if ev := lastEventOf(f.s, FixPRStateChanged); ev.Payload.Rule != RuleDedup {
		t.Errorf("the close event's rule is %q; want %s", ev.Payload.Rule, RuleDedup)
	}
	if d := f.defect(); d.State != DefectStateFixing || d.FixPR != "fp-3" {
		t.Errorf("the close moved the defect to %s on %q; want it untouched", d.State, d.FixPR)
	}
	if body := rec.Body.String(); !strings.Contains(body, `<section id="boAutoFix">`) || !strings.Contains(body, "Closed acme-org/sample-service#12.") {
		t.Errorf("the answer is not the Auto-fix view with the toast")
	}

	// Already merged on GitHub: no PATCH, the record takes GitHub's state.
	f2, gh2 := dedupFixture(t)
	gh2.prStates = map[int]string{12: "merged"}
	if rec := f2.closePost(t, f2.pr1, boCookies()...); rec.Code != http.StatusOK {
		t.Fatalf("an already merged pull request answered %d", rec.Code)
	}
	if gh2.count("PATCH ") != 0 {
		t.Errorf("an already merged pull request was closed again")
	}
	if r := recByID(t, f2.s, f2.pid, f2.pr1); r.State != FixPRMerged {
		t.Errorf("the already merged record is %s; want merged", r.State)
	}

	// The replay view writes nothing and calls nobody.
	f3, gh3 := dedupFixture(t)
	if _, err := f3.s.Replay(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	if rec := f3.closePost(t, f3.pr1, boCookies()...); rec.Code != http.StatusConflict {
		t.Errorf("a replay answered %d; want 409", rec.Code)
	}
	if len(gh3.calls) != 0 {
		t.Errorf("a replay called GitHub: %v", gh3.calls)
	}
}

// TC-910
func TestAClosedNonLiveRecordLeavesTheDefect(t *testing.T) {
	f, _ := dedupFixture(t)
	ctx := context.Background()
	// fp-1 is not the defect's record (fp-3 is): its close changes only itself.
	f.s.mu.Lock()
	r := f.s.fixPRs[f.pr1]
	r.RetargetedTo = "" // not replaced, so only the live-record rule protects the defect
	f.s.fixPRs[f.pr1] = r
	f.s.mu.Unlock()
	if _, err := f.s.AdvanceFixPR(ctx, f.pid, FixPR{ID: f.pr1, State: FixPRClosed, UpdatedAt: time.Now().Unix()}); err != nil {
		t.Fatal(err)
	}
	if d := f.defect(); d.State != DefectStateFixing || d.Gen != 1 {
		t.Errorf("closing a record the defect does not wait on moved it to %s generation %d", d.State, d.Gen)
	}
	// Closing the defect's own record still plans the person-only second attempt.
	if _, err := f.s.AdvanceFixPR(ctx, f.pid, FixPR{ID: "fp-3", State: FixPRClosed, UpdatedAt: time.Now().Unix()}); err != nil {
		t.Fatal(err)
	}
	if d := f.defect(); d.State != DefectStateTriaged || d.Gen != 2 {
		t.Errorf("closing the defect's own record left it %s generation %d; want triaged, 2", d.State, d.Gen)
	}
}

// TC-911
func TestTheAutoFixRowShowsBranchAndTrigger(t *testing.T) {
	old := FixPR{ID: "o", DefectKey: "k", Repo: defRepo, Number: 4, Base: "develop", State: FixPRClosed, CreatedAt: 1}
	byID := map[string]FixPR{"o": old}
	cases := []struct {
		fx     FixPR
		d      Defect
		text   string
		openID string
	}{
		{FixPR{ID: "n", DefectKey: "k", RetargetedFrom: "o", CreatedAt: 2}, Defect{Key: "k", Gen: 2}, "Retarget of acme-org/sample-service#4 (base develop)", "o"},
		{FixPR{ID: "n", DefectKey: "k"}, Defect{}, "The defect record is gone", ""},
		{FixPR{ID: "n", DefectKey: "k"}, Defect{Key: "k", Kind: DefectKindReview, Repo: defRepo, CaseID: "review:" + defRepo + "#88", ReviewBranch: "feat/x"}, "Review finding on acme-org/sample-service#88 (feat/x)", ""},
		{FixPR{ID: "n", DefectKey: "k"}, Defect{Key: "k", Kind: DefectKindUnit, UnitBase: "develop", UnitSHA: "0123456789abc"}, "Unit tests failed on develop after merge 0123456", ""},
		{FixPR{ID: "n", DefectKey: "k", CreatedAt: 5}, Defect{Key: "k", Kind: DefectKindFail, Gen: 2, CaseID: "C-1"}, "Second attempt after acme-org/sample-service#4 was closed", "o"},
		{FixPR{ID: "n", DefectKey: "k"}, Defect{Key: "k", Kind: DefectKindXfail, CaseID: "C-1‮"}, "QA report C-1 (xfail)", ""},
	}
	for _, c := range cases {
		text, openID := afTrigger(c.fx, c.d, byID)
		if text != c.text || openID != c.openID {
			t.Errorf("the trigger of %+v is %q (%q); want %q (%q)", c.d, text, openID, c.text, c.openID)
		}
	}
	f := newAFFixture(t)
	f.s.mu.Lock()
	r := f.s.fixPRs[f.pr1]
	r.Base = "develop"
	f.s.fixPRs[f.pr1] = r
	f.s.mu.Unlock()
	sec := afSection(afGet(t, f, "/ui/bo/autofix"))
	for _, want := range []string{`<th scope="col" class="af-md">Branch</th>`, `<code>gaos/fix-def-101</code> &rarr; <code>develop</code>`,
		`Triggered by QA report`} {
		if !strings.Contains(sec, want) {
			t.Errorf("the Fix pull requests table does not show %q", want)
		}
	}
	if !strings.Contains(sec, `&rarr; &mdash;`) {
		t.Errorf("a record with no base does not show the dash")
	}
}

// TC-912
func TestCardsWaitingPagesByFive(t *testing.T) {
	f := newAFFixture(t)
	f.s.mu.Lock()
	var tmpl TaskRecord
	for _, t := range f.s.tasks {
		if t.Status == StatusReady && strings.Contains(strings.ToLower(t.DedupKey), "def-102") {
			tmpl = t
		}
	}
	for i := 0; i < 11; i++ {
		c := tmpl
		c.ID = "T-wait-" + strconv.Itoa(i)
		c.DedupKey = defectCardKey(f.pid+"/DEF-9"+strconv.Itoa(10+i), 1, "triage")
		f.s.tasks = append(f.s.tasks, c)
	}
	f.s.mu.Unlock()
	p := f.srv.autoFixPage(context.Background(), url.Values{"section": {"waiting"}, "product": {f.pid}}, time.Now(), false)
	total := p.WaitPager.Total
	if total < 6 || len(p.Waiting) != 5 || p.WaitPager.Pages != (total+4)/5 {
		t.Fatalf("Cards waiting shows %d of %d over %d pages; want 5 a page", len(p.Waiting), total, p.WaitPager.Pages)
	}
	last := f.srv.autoFixPage(context.Background(), url.Values{"section": {"waiting"}, "wpage": {"99"}}, time.Now(), false)
	if last.WaitPager.Page != last.WaitPager.Pages || len(last.Waiting) != total-(last.WaitPager.Pages-1)*5 {
		t.Errorf("wpage=99 shows page %d with %d cards; want the last page", last.WaitPager.Page, len(last.Waiting))
	}
	if p.WaitPager.Next == nil || !strings.Contains(p.WaitPager.Next.Get, "wpage=2") || !strings.Contains(p.WaitPager.Next.Get, "product="+f.pid) ||
		!strings.Contains(p.WaitPager.Next.Get, "section=waiting") || strings.Contains(p.WaitPager.Next.Get, "open=") {
		t.Errorf("the next-page link is %+v; want wpage=2 with the product and the section, and no open", p.WaitPager.Next)
	}
}

// TC-913
func TestTheAutoFixSectionsCarryTheQuery(t *testing.T) {
	f := newAFFixture(t)
	def := afSection(afGet(t, f, "/ui/bo/autofix"))
	if !strings.Contains(def, "af-prs") || strings.Contains(def, `class="af-prods"`) || strings.Contains(def, `<h4>Cards waiting`) {
		t.Errorf("the default section is not Fix pull requests alone")
	}
	if !strings.Contains(def, `aria-current="true">Fix pull requests`) {
		t.Errorf("the active section is not marked aria-current=true")
	}
	w := afSection(afGet(t, f, "/ui/bo/autofix?section=waiting&product="+f.pid))
	if !strings.Contains(w, `<h4>Cards waiting`) || strings.Contains(w, "af-prs") || !strings.Contains(w, `name="section" value="waiting"`) {
		t.Errorf("section=waiting does not render only Cards waiting with the section kept in the form")
	}
	if !strings.Contains(w, "ui/bo/autofix/live?product="+f.pid+"&amp;section=waiting") {
		t.Errorf("the poll does not carry the section")
	}
	if !strings.Contains(w, `<select name="product">`) {
		t.Errorf("the filter form is missing from the Cards waiting section")
	}
	pr := afSection(afGet(t, f, "/ui/bo/autofix?section=products"))
	if !strings.Contains(pr, `class="af-prods"`) || strings.Contains(pr, "af-prs") {
		t.Errorf("section=products does not render only Auto-fix by product")
	}
	if o := afSection(afGet(t, f, "/ui/bo/autofix?section=waiting&open="+f.pr1)); !strings.Contains(o, "af-prs") || !strings.Contains(o, `id="afDrawer"`) {
		t.Errorf("an open drawer does not force Fix pull requests")
	}
	if bad := afSection(afGet(t, f, "/ui/bo/autofix?section=%22%3E")); !strings.Contains(bad, "af-prs") || strings.Contains(bad, `section=%22`) {
		t.Errorf("an unknown section is not ignored")
	}
	p := f.srv.autoFixPage(context.Background(), url.Values{"section": {"waiting"}}, time.Now(), false)
	for _, tab := range p.StateTabs {
		if !strings.Contains(tab.Href, "section=waiting") {
			t.Errorf("a state link %q drops the section", tab.Href)
		}
	}
}
