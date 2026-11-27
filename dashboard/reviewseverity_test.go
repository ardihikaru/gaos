package main

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// The reviewer's severity on the card and the review event (D-102 §4, §5).

// fakeReader is a prReader whose answers a test sets.
type fakeReader struct {
	pr        ghPR
	prErr     error
	files     []ghFile
	filesErr  error
	def       string
	self      string
	selfErr   error
	protected map[string]bool
	reads     int
}

func (f *fakeReader) pullRequest(context.Context, string, string, int) (ghPR, error) {
	f.reads++
	return f.pr, f.prErr
}
func (f *fakeReader) pullFiles(context.Context, string, string, int) ([]ghFile, error) {
	f.reads++
	return f.files, f.filesErr
}
func (f *fakeReader) repoDefault(context.Context, string, string) (string, error) {
	f.reads++
	return f.def, nil
}
func (f *fakeReader) selfLogin(context.Context) (string, error) { return f.self, f.selfErr }
func (f *fakeReader) branchProtected(_ context.Context, _, _, b string) (bool, error) {
	f.reads++
	return f.protected[b], nil
}

func authoredBy(login string) ghPR {
	var pr ghPR
	pr.State, pr.User.Login = "open", login
	return pr
}

func configure(t *testing.T, st *MemStore, ids ...string) {
	t.Helper()
	if _, err := st.SetAssigned(context.Background(), "p-1", ids, true, testEmail); err != nil {
		t.Fatal(err)
	}
}

// TC-808
func TestReviewSetsTheSeverityLabelBeforePosting(t *testing.T) {
	for _, tc := range []struct {
		name, sev, body string
		postErr         error
	}{
		{"posted", "medium", "a.go:1 — x. Reason: y.", nil},
		{"held", "high", "found it: API_TOKEN=abcdef123456789", nil},
		{"post failed", "low", "x", &ghError{Status: 404}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			st, card := prReviewFixture(t)
			rv := &prReviewer{st: st, gh: &fakePoster{err: tc.postErr}, logf: func(string, ...any) {}}
			rv.afterRun(context.Background(), card, prReplyOf(tc.sev, tc.body))
			got := st.taskByIDLocked(card.ID)
			if severityOf(got.Labels) != tc.sev {
				t.Errorf("labels %v, want severity:%s", got.Labels, tc.sev)
			}
			if !hasLabel(got.Labels, LabelPRReviewAuto) && !hasLabel(got.Labels, LabelPRReviewNeedsManual) {
				t.Errorf("the pr-review label was lost: %v", got.Labels)
			}
		})
	}
	// One label per family: an operator's correction replaces it.
	st, card := prReviewFixture(t)
	(&prReviewer{st: st, gh: &fakePoster{}, logf: func(string, ...any) {}}).afterRun(context.Background(), card, prReplyOf("critical", "x. Reason: y."))
	if _, err := st.SetTaskLabel(context.Background(), "p-1", card.ID, LabelSeverityLow, testEmail); err != nil {
		t.Fatal(err)
	}
	if got := st.taskByIDLocked(card.ID); severityOf(got.Labels) != "low" || strings.Count(strings.Join(got.Labels, " "), "severity:") != 1 {
		t.Errorf("after a correction %v, want exactly severity:low", got.Labels)
	}
}

// TC-810 and the COMMENT half of TC-802
func TestReviewEventBySeverityAndAuthor(t *testing.T) {
	run := func(t *testing.T, sev string, configured bool, rd *fakeReader) (*fakePoster, TaskRecord) {
		t.Helper()
		st, card := prReviewFixture(t)
		if configured {
			configure(t, st, "a-10")
		}
		fp := &fakePoster{}
		rv := &prReviewer{st: st, gh: fp, logf: func(string, ...any) {}}
		if rd != nil {
			rv.rd = rd
		}
		rv.afterRun(context.Background(), card, prReplyOf(sev, "a.go:1 — x. Reason: y."))
		if len(fp.posted) != 1 {
			t.Fatalf("posted %d reviews", len(fp.posted))
		}
		st.mu.RLock()
		defer st.mu.RUnlock()
		return fp, st.taskByIDLocked(card.ID).Clone()
	}
	other := func() *fakeReader { return &fakeReader{pr: authoredBy("someone"), self: "gaos-bot"} }

	// An unconfigured product only ever comments (TC-802).
	for _, sev := range prSeverities {
		if fp, _ := run(t, sev, false, other()); fp.events[0] != ghEventComment {
			t.Errorf("unconfigured %s sent %s", sev, fp.events[0])
		}
	}
	// Configured: medium and worse request changes, with the closing line.
	for _, tc := range []struct {
		sev, event, status, reason string
	}{
		{"none", ghEventComment, StatusDone, "review posted"},
		{"medium", ghEventRequestChanges, StatusDone, "changes requested"},
		{"high", ghEventRequestChanges, StatusReview, ""},
		{"critical", ghEventRequestChanges, StatusReview, ""},
	} {
		fp, c := run(t, tc.sev, true, other())
		if fp.events[0] != tc.event || c.Status != tc.status || (tc.reason != "" && c.BlockReason != tc.reason) {
			t.Errorf("%s: event %s status %s reason %q; want %s %s %q", tc.sev, fp.events[0], c.Status, c.BlockReason, tc.event, tc.status, tc.reason)
		}
		if tc.event == ghEventRequestChanges && !strings.HasSuffix(fp.posted[0], "a maintainer dismisses this review.") {
			t.Errorf("%s: the body lacks the dismissal line", tc.sev)
		}
		if tc.event == ghEventComment && strings.Contains(fp.posted[0], "dismisses this review") {
			t.Errorf("%s: a comment carries the dismissal line", tc.sev)
		}
	}
	// Own pull request, unknown author, unknown own login, no reader: comment only.
	for name, rd := range map[string]*fakeReader{
		"own":            {pr: authoredBy("GAOS-Bot"), self: "gaos-bot"},
		"author unknown": {pr: authoredBy(""), self: "gaos-bot"},
		"read failed":    {prErr: errors.New("boom"), self: "gaos-bot"},
		"self unknown":   {pr: authoredBy("someone"), selfErr: &ghError{Status: 403}},
	} {
		fp, c := run(t, "medium", true, rd)
		if fp.events[0] != ghEventComment || !strings.Contains(c.BlockReason, "comment only") {
			t.Errorf("%s: event %s reason %q, want a comment only", name, fp.events[0], c.BlockReason)
		}
	}
	if fp, _ := run(t, "high", true, nil); fp.events[0] != ghEventComment {
		t.Errorf("with no reader the event is %s", fp.events[0])
	}
	// The client refuses any other event before a request.
	c := &ghClient{base: "http://127.0.0.1:1"}
	if err := c.postReview(context.Background(), "o", "r", 1, "x", "APPROVE"); err == nil || !strings.Contains(err.Error(), "refused") {
		t.Errorf("APPROVE answered %v", err)
	}
}

// TC-809
func TestBoardSeverityFilterSortAndMediumPlus(t *testing.T) {
	t.Setenv("GAOS_PR_MAX_OPEN", "50")
	ctx := context.Background()
	st := NewMemStore(1)
	var ids []string
	for i, sev := range []string{"low", "critical", "medium", "", "high"} {
		planned, _, _, err := st.PlanPullRequestCards(ctx, "p-1", prEvent(prTypeOpened, "laundry-api", 100+i))
		if err != nil {
			t.Fatal(err)
		}
		id := planned[0] // the reviewer card
		ids = append(ids, id)
		if sev != "" {
			if _, err := st.SetTaskLabel(ctx, "p-1", id, "severity:"+sev, "gaos"); err != nil {
				t.Fatal(err)
			}
		}
	}
	page := func(q BoardQuery) BoardPage {
		t.Helper()
		p, err := st.BoardPage(ctx, "p-1", q, BoardWindow{})
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	// The facet lists the levels present in ladder order; the Label facet never lists them.
	p := page(BoardQuery{View: "table"})
	if strings.Join(p.Facets.Severities, ",") != "low,medium,high,critical" {
		t.Errorf("severity facet %v", p.Facets.Severities)
	}
	for _, l := range p.Facets.Labels {
		if strings.HasPrefix(l, "severity:") {
			t.Errorf("the label facet lists %s", l)
		}
	}
	// medium+ is exactly medium, high and critical, in the table and on the board.
	mp := page(BoardQuery{View: "table", Severity: severityMediumPlus})
	got := map[string]bool{}
	for _, r := range mp.Rows {
		got[r.ID] = true
		if r.Severity == "" || prSeverityRank(r.Severity) < prSeverityRank("medium") {
			t.Errorf("medium+ listed %s (%s)", r.ID, r.Severity)
		}
		for _, c := range r.Chips {
			if strings.HasPrefix(c, "severity:") {
				t.Errorf("a chip repeats the severity: %v", r.Chips)
			}
		}
	}
	if len(got) != 3 || !got[ids[1]] || !got[ids[2]] || !got[ids[4]] {
		t.Errorf("medium+ rows %v", got)
	}
	bp := page(BoardQuery{Severity: "low"})
	n := 0
	for _, col := range bp.Columns {
		for _, c := range col.Cards {
			n++
			if c.Severity != "low" {
				t.Errorf("the board's low filter shows %s", c.Severity)
			}
		}
	}
	if n != 1 {
		t.Errorf("the board's low filter shows %d cards", n)
	}
	// An unknown value is dropped, and the address keeps a known one.
	if q := page(BoardQuery{Severity: "spicy"}).Query; q.Severity != "" {
		t.Errorf("an unknown severity stayed: %q", q.Severity)
	}
	if e := (BoardQuery{Label: "e2e:automation", Severity: severityMediumPlus}).Encode(); !strings.Contains(e, "severity=medium%2B") {
		t.Errorf("encoded %q", e)
	}
	// Severity sorts worst first by default; a card with none is last both ways.
	sev := func(rows []BoardCard) string {
		var out []string
		for _, r := range rows {
			if strings.HasPrefix(r.ID, "wf-pr-") && strings.HasSuffix(r.ID, "pr-reviewer") {
				out = append(out, def(r.Severity, "-"))
			}
		}
		return strings.Join(out, ",")
	}
	if s := sev(page(BoardQuery{View: "table", Sort: "severity"}).Rows); s != "critical,high,medium,low,-" {
		t.Errorf("severity desc: %s", s)
	}
	if s := sev(page(BoardQuery{View: "table", Sort: "severity", Dir: "asc"}).Rows); s != "low,medium,high,critical,-" {
		t.Errorf("severity asc: %s", s)
	}
}

// TC-809, rendered: the chip on the kanban, the two table columns, the select.
func TestBoardRendersTheSeverity(t *testing.T) {
	srv, st := newTestServer(t)
	ctx := context.Background()
	planned, _, _, err := st.PlanPullRequestCards(ctx, "p-1", prEvent(prTypeOpened, "laundry-api", 300))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.SetTaskLabel(ctx, "p-1", planned[0], LabelSeverityCritical, "gaos"); err != nil {
		t.Fatal(err)
	}
	kanban := boardHX(t, srv, "GET", "/products/p-1/board/live", nil).Body.String()
	if !strings.Contains(kanban, `class="card-label sev-critical"`) || strings.Contains(kanban, `>severity:critical<`) {
		t.Error("the kanban does not show the severity chip once")
	}
	full := boardHX(t, srv, "GET", "/products/p-1?tab=board", nil).Body.String()
	if !strings.Contains(full, `name="severity"`) || !strings.Contains(full, "Medium or worse") {
		t.Error("the Severity select is missing")
	}
	if !strings.Contains(full, `href="../docs/integrations/agents.html#severity"`) {
		t.Error("the Severity filter has no i link to its docs section")
	}
	table := boardHX(t, srv, "GET", "/products/p-1/board/live?view=table", nil).Body.String()
	for _, want := range []string{">Severity<", ">Labels<", `sev-critical" title="Review severity: critical">critical<`, ">pr-review:auto<"} {
		if !strings.Contains(table, want) {
			t.Errorf("the table lacks %q", want)
		}
	}
	// The JSON card keeps every label and adds the level.
	c, err := st.BoardCard(ctx, "p-1", planned[0])
	if err != nil || c.Severity != "critical" || !hasLabel(c.Labels, LabelSeverityCritical) {
		t.Errorf("card %+v err %v", c, err)
	}
}
