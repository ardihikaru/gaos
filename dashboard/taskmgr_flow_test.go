package main

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

// D-104's uses end to end against a fake TaskMgr: the code check and its alert (TC-825), the
// login step of the notifier (TC-826), and the QA's notice and the write-back (TC-827).

const (
	tmCodeOK       = "DEMO-FEATURE-00001"
	tmCodeDeleted  = "DEMO-FEATURE-00002"
	tmCodeOther    = "OTHER-FEATURE-00003"
	tmCodeArchived = "DEMO-FEATURE-00004"
	tmCodeMissing  = "DEMO-FEATURE-09999"
	tmNumberDev    = "6281234567890"
	tmNumberQA     = "6289876543210"
)

func tmFlowFake() *fakeTM {
	return &fakeTM{
		tasks: map[string]tmTaskRow{
			tmCodeOK:       {ID: tmCodeOK, Status: "IN_PROGRESS", ProductCode: "DEMO", QAID: "u-qa"},
			tmCodeDeleted:  {ID: tmCodeDeleted, Status: "IN_PROGRESS", ProductCode: "DEMO", Deleted: true},
			tmCodeOther:    {ID: tmCodeOther, Status: "IN_PROGRESS", ProductCode: "OTHER"},
			tmCodeArchived: {ID: tmCodeArchived, Status: "ARCHIVED", ProductCode: "DEMO"},
		},
		users: []tmPerson{
			{ID: "u-dev", Username: "dev.one", Name: "Dev One", Role: "Developer", Active: true, WhatsApp: tmNumberDev, WAActive: true, GitHub: "dev", Checked: true},
			{ID: "u-qa", Username: "qa.one", Name: "QA One", Role: "QA", Active: true, WhatsApp: tmNumberQA, WAActive: true, GitHub: "qa-one", Checked: true},
		},
	}
}

// tmBind binds p-1 of st to DEMO and lists it for the PR template.
func tmBind(t *testing.T, st *MemStore) {
	t.Helper()
	t.Setenv("GAOS_PR_TEMPLATE_PRODUCTS", "p-1")
	if _, err := st.SetThirdParty(context.Background(), "p-1", ThirdPartyLink{Kind: ThirdPartyTaskMgr, Enabled: true, ProductCode: "DEMO"}, testEmail); err != nil {
		t.Fatal(err)
	}
}

// TC-825
func TestTaskCodeValidationAlert(t *testing.T) {
	ctx := context.Background()
	now := time.Unix(100_000, 0)
	n, _, phones := notifyFixture(t, 200)
	st := bundleStore(t, "loop/laundry-api#1", "loop/laundry-api#2", "loop/laundry-api#3", "loop/laundry-api#4",
		"loop/laundry-api#5", "loop/laundry-api#6", "loop/laundry-api#7")
	n.st = st
	tmBind(t, st)
	f := tmFlowFake()
	l := tmTestLink(t, st, f)
	var bodies []string
	run := func(l *taskmgrLink, ref, code string, opened time.Duration) {
		t.Helper()
		st.mu.Lock()
		r := st.pullRequests[keyOf(ref)]
		r.OpenedAt = now.Add(-opened).Unix()
		st.pullRequests[keyOf(ref)] = r
		st.mu.Unlock()
		row, _, err := st.ApplyBundleLinks(ctx, "p-1", keyOf(ref), prTemplate{Present: true, TaskCode: code}, now.Unix())
		if err != nil {
			t.Fatal(err)
		}
		p := &fixPRPoller{st: st, logf: func(string, ...any) {}, alert: prAlerter{post: recordPoster{&bodies}, nt: n}, tm: l}
		p.maybeAlertUnknown(ctx, row, now)
	}

	run(l, "loop/laundry-api#1", tmCodeMissing, 5*time.Minute)
	if len(bodies) != 0 || f.calls() != 0 {
		t.Errorf("a row inside its 10-minute grace was checked or alerted: %v, %d call(s)", bodies, f.calls())
	}
	run(l, "loop/laundry-api#1", tmCodeMissing, 11*time.Minute)
	run(l, "loop/laundry-api#1", tmCodeMissing, 12*time.Minute)
	if len(bodies) != 1 || !strings.Contains(bodies[0], tmCodeMissing+" is not an open task of this product in TaskMgr") || len(*phones) != 1 ||
		!strings.Contains((*phones)[0], "does not know") {
		t.Fatalf("an unknown code: %d comment(s) %v and %d line(s), want one of each, once", len(bodies), bodies, len(*phones))
	}
	if got := bundleRow(t, st, "loop/laundry-api#1").Alerted; got != alertUnknownTask {
		t.Errorf("the row must record the alert, got %q", got)
	}
	if bundleRow(t, st, "loop/laundry-api#1").BundleKey != "taskmgr:"+tmCodeMissing {
		t.Error("a row with an unknown code must keep its bundle")
	}
	for i, code := range []string{tmCodeDeleted, tmCodeOther, tmCodeArchived} {
		bodies = nil
		run(l, "loop/laundry-api#"+itoa(i+2), code, time.Hour)
		if len(bodies) != 1 {
			t.Errorf("%s gave %d comment(s), want one", code, len(bodies))
		}
		if len(bodies) == 1 && !strings.Contains(bodies[0], code+" is not an open task of this product in TaskMgr") {
			t.Errorf("every failing case must get the one text, which names no owner: %v", bodies)
		}
	}
	// A row alerted for a missing code is told once more when its code is wrong; the cache spares a call.
	calls := f.calls()
	if _, err := st.SetPRAlerted(ctx, "p-1", keyOf("loop/laundry-api#5"), alertNoTaskCode); err != nil {
		t.Fatal(err)
	}
	bodies = nil
	run(l, "loop/laundry-api#5", tmCodeMissing, time.Hour)
	if len(bodies) != 1 || f.calls() != calls {
		t.Errorf("a row alerted no-task-code with a wrong code: %d comment(s), %d new call(s); want one comment from the cache", len(bodies), f.calls()-calls)
	}
	bodies = nil
	run(l, "loop/laundry-api#6", tmCodeOK, time.Hour)
	if len(bodies) != 0 {
		t.Errorf("a valid code was alerted: %v", bodies)
	}

	// TaskMgr refusing the credential: no alert, and after it nothing is called.
	down := &fakeTM{force: "16"}
	l2 := tmTestLink(t, st, down)
	bodies = nil
	run(l2, "loop/laundry-api#7", "DEMO-FEATURE-08888", time.Hour)
	run(l2, "loop/laundry-api#7", "DEMO-FEATURE-08888", time.Hour)
	if len(bodies) != 0 || down.calls() != 1 || bundleRow(t, st, "loop/laundry-api#7").Alerted != "" {
		t.Errorf("with the breaker open: %d comment(s) and %d call(s); want none and one", len(bodies), down.calls())
	}
	// A product not in GAOS_PR_TEMPLATE_PRODUCTS is never checked.
	t.Setenv("GAOS_PR_TEMPLATE_PRODUCTS", "p-2")
	l3 := tmTestLink(t, st, tmFlowFake())
	run(l3, "loop/laundry-api#7", "DEMO-FEATURE-07777", time.Hour)
	if len(bodies) != 0 {
		t.Error("a product outside the template list was checked")
	}
}

// TC-826
func TestNotifyLoginThroughTaskMgr(t *testing.T) {
	ctx := context.Background()
	n, st, phones := notifyFixture(t, 200)
	tmBind(t, st)
	l := tmTestLink(t, st, tmFlowFake())
	if err := l.people.refresh(ctx); err != nil {
		t.Fatal(err)
	}
	n.tm = l
	task := TaskRecord{ID: "wf-pr-p-1-x-1-pr-reviewer", DeskID: "p-1", DedupKey: "loop/laundry-api#9:pr-reviewer"}

	n.Notify(ctx, task, "review:high", "review held")
	if len(*phones) != 1 || !strings.Contains((*phones)[0], tmNumberDev+"@s.whatsapp.net") {
		t.Fatalf("a login with no contact must reach its TaskMgr person: %v", *phones)
	}
	n.NotifyPR(ctx, "p-1", "loop", "laundry-api", 9, "pr:"+alertUnknownTask, "A pull request names a task-manager code TaskMgr does not know", "")
	if len(*phones) != 2 || !strings.Contains((*phones)[1], tmNumberDev+"@s.whatsapp.net") {
		t.Errorf("NotifyPR must take the same step: %v", *phones)
	}
	aliases := ""
	for _, e := range st.events {
		if e.Type == ContactNotified {
			aliases += fmt.Sprintf("%+v\n", e)
		}
	}
	if !strings.Contains(aliases, "tm:dev-one") {
		t.Errorf("the notice is not recorded under the tm: alias: %s", aliases)
	}
	if strings.Contains(fmt.Sprintf("%+v", st.events), tmNumberDev) {
		t.Error("a TaskMgr number reached the event stream")
	}

	*phones = nil
	n.Notify(ctx, TaskRecord{ID: "wf-e2e-1", DeskID: "p-1", DedupKey: "loop/laundry-api#9:e2e-auto"}, "e2e:approval", "approve the spec")
	if len(*phones) != 1 || !strings.Contains((*phones)[0], testGroup) {
		t.Errorf("an approval must stay with the group: %v", *phones)
	}
	*phones = nil
	n.NotifyPR(ctx, "p-1", "loop", "laundry-api", 9, "bundle:failure", "A test plan failed", "")
	if len(*phones) != 1 || !strings.Contains((*phones)[0], testGroup) {
		t.Errorf("a bundle kind must stay with the group: %v", *phones)
	}

	// A contact that filters the kind out keeps its opt-out: the group, not TaskMgr.
	if _, err := st.UpsertContact(ctx, Contact{GitHubLogin: "dev", DisplayName: "Dev", Destination: "555@s.whatsapp.net", NotifyOn: []string{"e2e:failure"}}, "o@x.y"); err != nil {
		t.Fatal(err)
	}
	*phones = nil
	n.Notify(ctx, task, "review:high", "review held")
	if len(*phones) != 1 || !strings.Contains((*phones)[0], testGroup) || strings.Contains((*phones)[0], tmNumberDev) {
		t.Errorf("a contact that filters the kind out must send it to the group: %v", *phones)
	}
	// A contact that wants it wins over TaskMgr.
	st.UpsertContact(ctx, Contact{GitHubLogin: "dev", DisplayName: "Dev", Destination: "555@s.whatsapp.net"}, "o@x.y")
	*phones = nil
	n.Notify(ctx, task, "review:high", "review held")
	if len(*phones) != 1 || !strings.Contains((*phones)[0], "555@s.whatsapp.net") {
		t.Errorf("a contact must win over TaskMgr: %v", *phones)
	}
	// Not ready (the binding disabled): the group.
	st.DeleteContact(ctx, "", "dev", "o@x.y")
	st.SetThirdParty(ctx, "p-1", ThirdPartyLink{Kind: ThirdPartyTaskMgr, ProductCode: "DEMO"}, testEmail)
	*phones = nil
	n.Notify(ctx, task, "review:high", "review held")
	if len(*phones) != 1 || !strings.Contains((*phones)[0], testGroup) {
		t.Errorf("with the binding disabled the line must go to the group: %v", *phones)
	}
}

// TC-827
func TestTestPlanReadyAndWriteBack(t *testing.T) {
	ctx := context.Background()
	st := bundleStore(t, "loop/laundry-api#1", "loop/laundry-api#2", "loop/laundry-api#3")
	tmBind(t, st)
	for _, ref := range []string{"loop/laundry-api#1", "loop/laundry-api#2"} {
		st.ApplyBundleLinks(ctx, "p-1", keyOf(ref), prTemplate{Present: true, TaskCode: tmCodeOK}, 1)
	}
	st.AdvancePullRequest(ctx, "p-1", keyOf("loop/laundry-api#1"), PRFields{State: PRStateMerged, CheckedAt: time.Now().Unix()})
	st.AdvancePullRequest(ctx, "p-1", keyOf("loop/laundry-api#2"), PRFields{State: PRStateClosed, CheckedAt: time.Now().Unix()})
	card, _, _, err := st.PlanBundleCard(ctx, "p-1", "taskmgr:"+tmCodeOK, 1)
	if err != nil {
		t.Fatal(err)
	}
	walkTo(t, st, card.ID, StatusReview)
	f := tmFlowFake()
	l := tmTestLink(t, st, f)
	if err := l.people.refresh(ctx); err != nil {
		t.Fatal(err)
	}
	var sent []caseProposalIn
	var direct []string
	tp := &testPlanner{st: qaCatalogStore{st}, qa: fakeProposer{sent: &sent, codes: map[string]bool{}}, logf: func(string, ...any) {}, tm: l,
		direct: func(_ context.Context, _ TaskRecord, kind, text, dest, alias string) {
			direct = append(direct, kind+"|"+dest+"|"+alias+"|"+text)
		}}
	reply := "```" + TestPlanFence + "\n" + `{"cases":[{"action":"create","epic":"OUTREG","title":"t","scenario":"s","polarity":"+","level":"E2E","priority":"P2"},{"action":"link","caseCode":"TC-OUTREG-004","note":"n"}]}` + "\n```"
	c, _ := st.APICardGet(ctx, "p-1", card.ID, "", "")
	tp.afterRun(ctx, c, reply)
	if done, _ := st.APICardGet(ctx, "p-1", card.ID, "", ""); done.Status != StatusDone {
		t.Fatalf("the planner card is %s, want DONE", done.Status)
	}
	if len(direct) != 1 || !strings.HasPrefix(direct[0], KindBundlePlanned+"|"+tmNumberQA+"@s.whatsapp.net|tm:qa-one|A test plan is ready: 1 draft case(s), 1 link(s)") {
		t.Errorf("the QA notice is %v; want one bundle:planned line to the task's QA", direct)
	}
	if len(f.comments) != 1 {
		t.Fatalf("the write-back wrote %d comment(s), want one", len(f.comments))
	}
	code, text, _ := strings.Cut(f.comments[0], "|")
	marker := "[gaos bundle taskmgr:" + tmCodeOK + " · card " + card.ID + "]"
	if code != tmCodeOK || !strings.Contains(text, "finished: 1 merged, 1 closed without merging") ||
		!strings.Contains(text, "https://github.com/loop/laundry-api/pull/1") || strings.Contains(text, "/pull/2") ||
		!strings.Contains(text, "TC-OUTREG-004") || !strings.HasSuffix(text, marker) || utf8.RuneCountInString(text) > tmCommentRunes {
		t.Errorf("the write-back on %s reads %q", code, text)
	}

	// Another product's task, or a pr: bundle: nothing is written and nobody is told.
	f.comments, direct = nil, nil
	tp.afterDone(ctx, c, "p-1", "taskmgr:"+tmCodeOther, nil, 1, 0, 0, nil)
	tp.afterDone(ctx, c, "p-1", "taskmgr:"+tmCodeMissing, nil, 1, 0, 0, nil)
	tp.afterDone(ctx, c, "p-1", "pr:loop/laundry-api#3", nil, 1, 0, 0, nil)
	if len(f.comments) != 0 || len(direct) != 0 {
		t.Errorf("another product's task, an unknown task or a pr: bundle got %v and %v", f.comments, direct)
	}
	// A plan with no cases writes back but tells nobody.
	tp.afterDone(ctx, c, "p-1", "taskmgr:"+tmCodeOK, nil, 0, 0, 2, nil)
	if len(f.comments) != 1 || len(direct) != 0 {
		t.Errorf("an empty plan: %d comment(s), %d line(s); want one and none", len(f.comments), len(direct))
	}

	// The text: case codes filtered, at most ten URLs, cut to 1500 runes with the marker kept.
	var rows []PullRequest
	for i := 0; i < 30; i++ {
		rows = append(rows, PullRequest{BundleKey: "taskmgr:X-1", Kind: PRKindReview, State: PRStateMerged,
			Repo: "loop/" + strings.Repeat("ŵ", 80), Number: i + 1})
	}
	long := taskMgrWriteBack("taskmgr:X-1", "card-1", rows, 1, 1, 0, []string{"TC-OK-001", "bad code<script>", "TC-OK-002"})
	if utf8.RuneCountInString(long) > tmCommentRunes || !strings.HasSuffix(long, "[gaos bundle taskmgr:X-1 · card card-1]") {
		t.Errorf("a long write-back is %d runes or lost its marker", utf8.RuneCountInString(long))
	}
	short := taskMgrWriteBack("taskmgr:X-1", "card-1", rows[:12], 1, 1, 0, []string{"TC-OK-001", "bad code<script>", "TC-OK-002"})
	if strings.Count(short, "https://github.com/") != 10 || !strings.Contains(short, "… and 2 more") ||
		!strings.Contains(short, "TC-OK-001, TC-OK-002") || strings.Contains(short, "<script>") {
		t.Errorf("the write-back must list ten URLs, say how many more, and keep only valid case codes: %s", short)
	}
}
