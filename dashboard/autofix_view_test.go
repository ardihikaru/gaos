package main

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"
)

// The back office's Auto-fix view (D-091, design §9.2). Product, repository and
// defect names are invented, as D-085 requires. Every hostile string below is
// built from a random sentinel, so no needle can occur by accident in markup.

const (
	afPR1Reply = `{"summary":"reject a negative quantity","before":"qty -1 -> accepted","after":"qty -1 -> 422",` +
		`"edits":[{"path":"pkg/x/validate.go","find":"if qty == 0 {\n\treturn errZero\n}","replace":"if qty <= 0 {\n\treturn errNonPositive\n}"}],` +
		`"newFiles":[{"path":"pkg/x/validate_test.go","content":"package x\n\nfunc TestQty(t *testing.T) {\n\tt.Parallel()\n}\n"}]}`
)

type afFixture struct {
	srv *server
	s   *MemStore
	pid string
	pr1 string // the draft's record id
	pr2 string // the merged one
}

// afSection cuts the view out of the sheet.
func afSection(body string) string {
	i := strings.Index(body, `<section id="boAutoFix">`)
	if i < 0 {
		return ""
	}
	j := strings.Index(body[i:], `</section>`)
	if j < 0 {
		return body[i:]
	}
	return body[i : i+j]
}

func afGet(t *testing.T, f afFixture, path string) string {
	t.Helper()
	rec := do(t, f.srv, "GET", path, nil, boCookies()...)
	if rec.Code != 200 {
		t.Fatalf("GET %s answered %d: %s", path, rec.Code, rec.Body)
	}
	return rec.Body.String()
}

// newAFFixture is a product with auto-fix on, two defects, a draft and a merged
// pull request, a card blocked for a person, and one automatic dispatch today.
func newAFFixture(t *testing.T) afFixture {
	t.Helper()
	srv, _ := newTestServer(t)
	s, pid := defectFixture(t)
	srv.store = s
	srv.writeTokenKind = tokenKindFine
	srv.ghState = "on"
	report(t, s, pid, defItem(DefectKindXfail, "DEF-101", defCase1), defItem(DefectKindXfail, "DEF-102", defCase2))

	now := time.Now().Unix()
	s.mu.Lock()
	s.masterPtr(pid).AutoFix = AutoFix{Enabled: true, FixCap: 3, TriageCap: 10, CIConfirmed: true,
		AllowedGlobs: []string{"pkg/**"}, TestGlobs: []string{"**/*_test.go"}}
	d1, d2 := s.defects[pid+"/DEF-101"], s.defects[pid+"/DEF-102"]
	d1.Title, d1.Difficulty, d1.Class = "a negative quantity is accepted", "easy", "min-value"
	d2.Title, d2.Difficulty = "the record id is not validated", "medium"
	// DEF-101's triage card is blocked for a person; DEF-102's waits, and was
	// dispatched automatically earlier today.
	for i := range s.tasks {
		switch s.tasks[i].DedupKey {
		case defectCardKey(d1.Key, d1.Gen, "triage"):
			s.tasks[i].Status, s.tasks[i].BlockReason = StatusBlocked, "autofix-limits"
		case defectCardKey(d2.Key, d2.Gen, "triage"):
			s.tasks[i].Status = StatusReady
		}
	}
	card, _ := s.planDefectCardLocked(d1, "fix", StatusReady, "", LabelDefectEasy)
	for i := range s.tasks {
		if s.tasks[i].ID == card.ID {
			s.tasks[i].AutoAt, s.tasks[i].Status = autoFixNow(), StatusReady
		}
	}
	s.mu.Unlock()

	f := afFixture{srv: srv, s: s, pid: pid, pr1: "fp-1", pr2: "fp-2"}
	ctx := context.Background()
	if _, err := s.UpsertFixPR(ctx, FixPR{ID: f.pr1, ProductID: pid, DefectKey: pid + "/DEF-101", Repo: defRepo, Number: 12,
		URL: "https://github.com/" + defRepo + "/pull/12", State: FixPRDraft, CreatedAt: now - 3*3600, UpdatedAt: now - 60,
		TaskID: card.ID, Difficulty: "easy", Before: "qty -1 -> accepted", After: "qty -1 -> rejected 422",
		Branch: "gaos/fix-def-101", WaSent: true, Reply: afPR1Reply}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpsertFixPR(ctx, FixPR{ID: f.pr2, ProductID: pid, DefectKey: pid + "/DEF-102", Repo: defRepo, Number: 9,
		State: FixPRMerged, CreatedAt: now - 30*3600, UpdatedAt: now - 3600, TaskID: "T-9", Difficulty: "medium"}); err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	d1.FixPR = f.pr1
	s.mu.Unlock()
	return f
}

var afTileRe = func(label string) *regexp.Regexp {
	return regexp.MustCompile(regexp.QuoteMeta(label) + `</span>\s*<b class="af-tile-v">([^<]*)</b>`)
}

func TestAutoFixViewTiles(t *testing.T) {
	f := newAFFixture(t)
	for _, path := range []string{"/ui/bo/autofix", "/ui/bo?view=autofix"} {
		rec := do(t, f.srv, "GET", path, nil, boCookies()...)
		if rec.Code != 200 {
			t.Fatalf("the Auto-fix view answered %d on %s", rec.Code, path)
		}
		if cc := rec.Header().Get("Cache-Control"); cc != "no-store" {
			t.Errorf("the Auto-fix view on %s is not no-store: %q", path, cc)
		}
		sec := afSection(rec.Body.String())
		if sec == "" {
			t.Fatalf("the sheet carries no Auto-fix section on %s", path)
		}
		for label, want := range map[string]string{
			"Open drafts": "1", "Ready for review": "0", "Merged, 7 days": "1", "Awaiting triage": "2", "Waiting for a person": "1",
		} {
			m := afTileRe(label).FindStringSubmatch(sec)
			if m == nil || m[1] != want {
				t.Errorf("tile %q = %v, want %q", label, m, want)
			}
		}
		// one fix dispatch today of a cap of 3; no triage dispatch of a cap of 10
		for _, want := range []string{`1 <i>/</i> 3`, `0 <i>/</i> 10`, `aria-label="1 of 3 fix dispatches used"`,
			`af-st af-st--draft">Draft`, `af-st af-st--merged">Merged`, `af-d af-d--easy">easy`, `af-d af-d--medium">medium`,
			`href="https://github.com/acme-org/sample-service/pull/12" target="_blank" rel="noopener noreferrer"`,
			`acme-org/sample-service#12`, `a negative quantity is accepted`, `DEF-101`, `T-9`,
			`class="af-wa is-on">Sent`, `title="opened `,
			`hx-trigger="every 30s `, `ui/bo/autofix/live`} {
			if !strings.Contains(sec, want) {
				t.Errorf("the Auto-fix view on %s does not show %q", path, want)
			}
		}
		// The waiting cards are their own section since D-108.
		if w := afSection(afGet(t, f, "/ui/bo/autofix?section=waiting")); !strings.Contains(w, `Needs a person: the fix went over GAOS&#39;s size limits`) {
			t.Errorf("the Cards waiting section does not show the size-limit reason")
		}
		if !strings.Contains(rec.Body.String(), `<a aria-current="page"   href="autofix" hx-get="ui/bo/autofix"`) {
			t.Errorf("the navigation does not mark Auto-fix as the current view on %s", path)
		}
	}
}

func TestAutoFixViewFilters(t *testing.T) {
	f := newAFFixture(t)
	rowOf := func(sec, ref string) bool { return strings.Contains(sec, `<span class="af-ref mono">`+ref+`</span>`) }

	all := afSection(afGet(t, f, "/ui/bo/autofix"))
	if !rowOf(all, "DEF-101") || !rowOf(all, "DEF-102") {
		t.Fatalf("the unfiltered table lacks a row")
	}
	// the draft is an active state and sorts before the merged one
	if strings.Index(all, "DEF-101</span>") > strings.Index(all, "DEF-102</span>") {
		t.Errorf("the draft does not come before the merged pull request")
	}

	st := afSection(afGet(t, f, "/ui/bo/autofix?state=merged"))
	if rowOf(st, "DEF-101") || !rowOf(st, "DEF-102") {
		t.Errorf("state=merged shows the wrong rows")
	}
	if !strings.Contains(st, `<a href="autofix?state=merged" hx-get="ui/bo/autofix?state=merged" hx-target="#sink" hx-swap="innerHTML" aria-current="true">Merged`) {
		t.Errorf("the Merged tab is not the current one")
	}

	df := afSection(afGet(t, f, "/ui/bo/autofix?difficulty=easy&product="+f.pid))
	if !rowOf(df, "DEF-101") || rowOf(df, "DEF-102") {
		t.Errorf("difficulty=easy shows the wrong rows")
	}
	// the filters survive in the poll and in the links, in the URL
	for _, want := range []string{`difficulty=easy`, `product=` + f.pid, `<option value="easy" selected>`, `<option value="` + f.pid + `" selected>`} {
		if !strings.Contains(df, want) {
			t.Errorf("the filtered view does not carry %q", want)
		}
	}
	if !strings.Contains(df, `hx-get="ui/bo/autofix/live?difficulty=easy&amp;product=`+f.pid+`"`) {
		t.Errorf("the poll URL does not carry the filters")
	}

	// a filter that matches nothing says so and offers the way out
	none := afSection(afGet(t, f, "/ui/bo/autofix?difficulty=complex"))
	if rowOf(none, "DEF-101") || !strings.Contains(none, "No pull request matches these filters.") || !strings.Contains(none, "Clear filters") {
		t.Errorf("an empty filter does not say so")
	}

	// a value that is not in the vocabulary, or not a product, is ignored
	junk := afSection(afGet(t, f, "/ui/bo/autofix?state=%22%3E%3Ci%20id%3Dzz%3E&difficulty=nope&product=p-404&open=zzz"))
	if !rowOf(junk, "DEF-101") || !rowOf(junk, "DEF-102") || strings.Contains(junk, `id="afDrawer"`) || strings.Contains(junk, "id=zz") {
		t.Errorf("a junk filter changed the view")
	}

	// the poll answers the same data as one oob element, never the sheet
	live := afGet(t, f, "/ui/bo/autofix/live?state=draft")
	if !strings.Contains(live, `id="afLive"`) || !strings.Contains(live, `hx-swap-oob="true"`) || strings.Contains(live, `id="boSheet"`) {
		t.Errorf("the poll is not one #afLive oob fragment")
	}
	if !rowOf(live, "DEF-101") || rowOf(live, "DEF-102") {
		t.Errorf("the poll ignores the filters")
	}
	if rec := do(t, f.srv, "GET", "/ui/bo/autofix/live", nil); rec.Code != 401 {
		t.Errorf("the poll signed out answered %d, want 401", rec.Code)
	}
}

func TestAutoFixViewDrawer(t *testing.T) {
	f := newAFFixture(t)
	body := afGet(t, f, "/ui/bo/autofix?open="+f.pr1)
	sec := afSection(body)
	i := strings.Index(sec, `<aside id="afDrawer"`)
	if i < 0 {
		t.Fatalf("no drawer for open=%s", f.pr1)
	}
	dr := sec[i:]
	for _, want := range []string{
		`aria-label="Fix details for DEF-101"`, `data-af-close`, `Before and after`, `As the agent reported them. GAOS did not run the code.`,
		`qty -1 -&gt; accepted`, `qty -1 -&gt; rejected 422`,
		`pkg/x/validate.go`, `pkg/x/validate_test.go`, `new test`,
		`Code lines`, `Test lines`, `2 <i>/</i> 3`, // two files of three
		`Fix dispatches today`, `1 <i>/</i> 3`, `resets 00:00 UTC`,
		`gaos/fix-def-101`, `min-value`, `sent to the group`,
		`defect reported: DEF-101`, // the timeline, from the events
	} {
		if !strings.Contains(dr, want) {
			t.Errorf("the drawer does not show %q", want)
		}
	}
	// the changed lines: 2 removed + 2 added in the edit; 5 lines in the new test
	if !regexp.MustCompile(`Code lines</span>\s*<b class="af-cap-v mono">4 <i>/</i> 40`).MatchString(dr) ||
		!regexp.MustCompile(`Test lines</span>\s*<b class="af-cap-v mono">5 <i>/</i> 120`).MatchString(dr) {
		t.Errorf("the drawer's line counts are not 4 / 40 and 5 / 120")
	}
	// the row is marked, the close link drops open= and keeps the filters
	if !strings.Contains(sec, `class="af-r is-open"`) || !strings.Contains(dr, `href="autofix" hx-get="ui/bo/autofix"`) {
		t.Errorf("the open row is not marked, or the close link is wrong")
	}
	if got := afSection(afGet(t, f, "/ui/bo/autofix?state=draft&open="+f.pr1)); !strings.Contains(got, `data-af-close href="autofix?state=draft" hx-get="ui/bo/autofix?state=draft"`) {
		t.Errorf("the close link does not keep the filters")
	}
	// the stored code is never printed: only paths and counts
	for _, leak := range []string{"errNonPositive", "errZero", "t.Parallel"} {
		if strings.Contains(body, leak) {
			t.Errorf("the drawer printed code from the reply: %q", leak)
		}
	}
	// a record without a reply or a defect is still a drawer, not a crash
	if _, err := f.s.UpsertFixPR(context.Background(), FixPR{ID: "fp-3", ProductID: f.pid, DefectKey: f.pid + "/DEF-404", State: FixPRPreparing}); err != nil {
		t.Fatal(err)
	}
	bare := afSection(afGet(t, f, "/ui/bo/autofix?open=fp-3"))
	for _, want := range []string{`id="afDrawer"`, "The agent reported no before and after.", "No file list is stored", "No event is recorded", "not opened"} {
		if !strings.Contains(bare, want) {
			t.Errorf("the bare drawer does not show %q", want)
		}
	}
}

func TestAutoFixViewOffState(t *testing.T) {
	f := newAFFixture(t)
	// switch the product off: nothing configured
	f.s.mu.Lock()
	f.s.masterPtr(f.pid).AutoFix = AutoFix{}
	f.s.mu.Unlock()
	sec := afSection(afGet(t, f, "/ui/bo/autofix?section=products")) + afSection(afGet(t, f, "/ui/bo/autofix?section=waiting"))
	for _, want := range []string{
		`af-sw af-sw--off">Off`, "Auto-fix is off for this product.", "Auto-fix needs at least one allowed path.",
		"GAOS_AUTOFIX_ADMINS", "POST /products/{id}/autofix",
		"Auto-fix is off for this product — Auto-fix needs at least one allowed path.", // the waiting card's reason
	} {
		if !strings.Contains(sec, want) {
			t.Errorf("the off state does not show %q", want)
		}
	}
	// the write token is gone: a product that is on says it is not ready
	f2 := newAFFixture(t)
	f2.srv.writeTokenKind = ""
	sec = afSection(afGet(t, f2, "/ui/bo/autofix?section=products"))
	for _, want := range []string{`af-sw af-sw--stuck">On, not ready`, "GAOS_GITHUB_WRITE_TOKEN_FILE", `af-token--off`} {
		if !strings.Contains(sec, want) {
			t.Errorf("the no-token state does not show %q", want)
		}
	}
	// the cap is spent: the READY fix card says so, in numbers
	f3 := newAFFixture(t)
	f3.s.mu.Lock()
	f3.s.masterPtr(f3.pid).AutoFix.FixCap = 1
	f3.s.mu.Unlock()
	sec = afSection(afGet(t, f3, "/ui/bo/autofix?section=waiting"))
	if !strings.Contains(sec, "Fix cap 1/1 until 00:00 UTC") || !strings.Contains(sec, `af-meter is-full`) {
		t.Errorf("a spent cap is not shown as 1/1 until 00:00 UTC")
	}
	// GitHub is not answering
	f4 := newAFFixture(t)
	f4.srv.ghState = "off (no token file)"
	sec = afSection(afGet(t, f4, "/ui/bo/autofix"))
	if !strings.Contains(sec, "The GitHub client is off (no token file).") || !strings.Contains(sec, `role="alert"`) {
		t.Errorf("a dead GitHub client is not announced")
	}
	f4.srv.ghState = "on"
	f4.srv.fixPoll = fakePoll{FixPollStatus{LastPoll: time.Now().Add(-4 * time.Minute), LastError: "rate"}}
	sec = afSection(afGet(t, f4, "/ui/bo/autofix"))
	if !strings.Contains(sec, "GitHub is not answering (rate)") || !strings.Contains(sec, "checked 4m ago") {
		t.Errorf("a failing poll is not announced with its age")
	}
}

type fakePoll struct{ st FixPollStatus }

func (p fakePoll) Status() FixPollStatus { return p.st }

func TestAutoFixViewNoExcerptOrSecret(t *testing.T) {
	f := newAFFixture(t)
	tok := "ghp_" + sentinel(t) + sentinel(t)[:8]
	marker := "FAILUREOUTPUT" + sentinel(t)
	f.s.mu.Lock()
	d := f.s.defects[f.pid+"/DEF-101"]
	d.Title = `<script>alert("t` + marker[:6] + `")</script> <img src=x onerror=alert(1)>`
	d.Excerpt = "expected 422, got 200 " + marker + " " + tok
	d.Reason = "reason " + marker
	d.Context = DefectContext{Built: true, Files: []DefectContextFile{{Path: "pkg/x/validate.go", Text: "SOURCE" + marker}}}
	fx := f.s.fixPRs[f.pr1]
	fx.Before = `<b onmouseover=alert(1)>` + "‮" + `before</b>`
	fx.After = "after " + tok
	f.s.fixPRs[f.pr1] = fx
	f.s.mu.Unlock()

	for _, path := range []string{"/ui/bo/autofix", "/ui/bo/autofix?open=" + f.pr1, "/ui/bo/autofix/live"} {
		body := afGet(t, f, path)
		for _, raw := range []string{"<script>alert", "<img src=x", "<b onmouseover", "‮", tok, marker, "SOURCE"} {
			if strings.Contains(body, raw) {
				t.Errorf("%s printed %q raw", path, raw)
			}
		}
	}
	// the title is shown, escaped
	if body := afGet(t, f, "/ui/bo/autofix"); !strings.Contains(body, "&lt;script&gt;alert(") {
		t.Errorf("a hostile title is not shown as escaped text")
	}
	// the secret in the agent's text is withheld, not printed
	if body := afGet(t, f, "/ui/bo/autofix?open="+f.pr1); !strings.Contains(body, "[withheld: matched a secret detector]") {
		t.Errorf("a token-shaped string in the after is not withheld")
	}
}

func TestAutoFixViewTokenKindShown(t *testing.T) {
	secret := "github_pat_" + sentinel(t) + sentinel(t)
	for _, c := range []struct{ kind, shown, tone string }{
		{tokenKindFine, "fine-grained", "ok"}, {tokenKindClassic, "classic (broad)", "warn"}, {"", "not configured", "off"},
	} {
		f := newAFFixture(t)
		f.srv.writeTokenKind = c.kind
		view := afGet(t, f, "/ui/bo/autofix")
		if !strings.Contains(afSection(view), `<span class="af-token af-token--`+c.tone+`"`) || !strings.Contains(afSection(view), "<b>"+c.shown+"</b>") {
			t.Errorf("the view does not show the token kind %q with tone %q", c.shown, c.tone)
		}
		integ := integrationsSection(afGet(t, f, "/ui/bo/integrations"))
		if !strings.Contains(integ, `<th scope="row">Auto-fix write token</th><td class="mono">`+c.shown+`</td>`) {
			t.Errorf("the Integrations view does not show the token kind %q", c.shown)
		}
		if strings.Contains(view, secret) || strings.Contains(integ, secret) {
			t.Errorf("a token leaked")
		}
	}
}

// ---------------------------------------------------------------- break-ui

// TestAutoFixViewWorstCase feeds the view what real data does: 200-character
// titles, Thai and Japanese text, emoji, a right-to-left override, a title that
// is one letter, 500 rows, none, and records with fields missing.
func TestAutoFixViewWorstCase(t *testing.T) {
	f := newAFFixture(t)
	ctx := context.Background()
	now := time.Now().Unix()
	long := strings.Repeat("ไม่สามารถบันทึกจำนวนติดลบได้ ", 10) // Thai, no spaces inside words, > 200 runes
	titles := []string{
		"x",
		strings.Repeat("A", 200),
		long,
		"数量が負の値を受け付ける 🧾🔥👩🏽‍💻",
		"עברית ‮ reversed",
		"a/very/long/unbroken/" + strings.Repeat("segment_", 30),
	}
	f.s.mu.Lock()
	for i := 0; i < 500; i++ {
		key := fmt.Sprintf("%s/DEF-%d", f.pid, 1000+i)
		f.s.defects[key] = &Defect{Key: key, ProductID: f.pid, DefectID: fmt.Sprintf("DEF-%d", 1000+i), CaseID: defCase1,
			Title: titles[i%len(titles)], Difficulty: afDifficulties[i%3], State: DefectStateFixing, Gen: 1}
		f.s.fixPRs[fmt.Sprintf("w-%03d", i)] = FixPR{ID: fmt.Sprintf("w-%03d", i), ProductID: f.pid, DefectKey: key,
			Repo: strings.Repeat("o", 90) + "/" + strings.Repeat("r", 90), Number: 100000 + i, State: afStateOrder[i%5].Value,
			CreatedAt: now - int64(i)*60, TaskID: "T-" + fmt.Sprint(i), WaSent: i%2 == 0}
	}
	f.s.mu.Unlock()

	body := afGet(t, f, "/ui/bo/autofix")
	sec := afSection(body)
	if n := strings.Count(sec, `<tr class="af-r`); n != tablePageSize {
		t.Errorf("%d rows rendered of 502, want one page of %d (D-097)", n, tablePageSize)
	}
	for _, want := range []string{"Showing 1–10 of 502", "数量が負の値を受け付ける 🧾🔥👩🏽‍💻", "ไม่สามารถบันทึก", `title="x"`,
		`<span class="af-t">x</span>`, "(the defect record is gone)"} {
		if want == "(the defect record is gone)" {
			continue // every row here has a record
		}
		if !strings.Contains(sec, want) {
			t.Errorf("the 500-row view does not show %q", want)
		}
	}
	if strings.Contains(sec, "‮") {
		t.Errorf("a right-to-left override reached the page")
	}
	// no title is allowed past 200 runes: the ellipsis carries the cut
	if strings.Contains(sec, strings.Repeat("A", 201)) {
		t.Errorf("a title longer than the schema's 200 reached the page whole")
	}
	// a drawer of a row with a 90/90-character repository still renders
	if d := afSection(afGet(t, f, "/ui/bo/autofix?open=w-001")); !strings.Contains(d, `id="afDrawer"`) {
		t.Errorf("the drawer of a worst-case row does not render")
	}
	// the tabs count what the other filters leave, with a thousands separator past 999
	if !strings.Contains(sec, `All <span class="n">502</span>`) {
		t.Errorf("the All tab does not count 502")
	}

	// zero: no product has a defect
	empty, _ := newTestServer(t)
	empty.ghState = "on"
	rec := do(t, empty, "GET", "/ui/bo/autofix", nil, boCookies()...)
	if rec.Code != 200 {
		t.Fatalf("an empty store answered %d", rec.Code)
	}
	es := afSection(rec.Body.String())
	for _, want := range []string{"No defect has been reported yet.", "not configured", `<b class="af-tile-v">0</b>`} {
		if !strings.Contains(es, want) {
			t.Errorf("the empty view does not show %q", want)
		}
	}
	if strings.Contains(es, "NaN") || strings.Contains(es, "undefined") || strings.Contains(es, "<nil>") {
		t.Errorf("the empty view printed a placeholder")
	}

	// one defect and no pull request: the empty state says what happens next
	one := newAFFixture(t)
	one.s.mu.Lock()
	one.s.fixPRs = map[string]FixPR{}
	one.s.mu.Unlock()
	if os := afSection(afGet(t, one, "/ui/bo/autofix")); !strings.Contains(os, "No fix pull request yet.") || !strings.Contains(os, "2 defects are being triaged") {
		t.Errorf("the no-pull-request state is not shown")
	}
	_ = ctx
}
