package main

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// The fix reply, its caps and the draft pull request (D-091, design §8). The
// repository, product and defect ids are invented, as D-085 requires.

const (
	fxFile     = "pkg/records/validate.go"
	fxTestPath = "pkg/records/validate_test.go"
	fxText     = "package records\n\nfunc check(qty int) bool {\n\tif qty == 0 {\n\t\treturn false\n\t}\n\treturn true\n}\n"
	fxTestBody = "package records\n\nimport \"testing\"\n\nfunc TestCheck(t *testing.T) {\n\tif check(-1) {\n\t\tt.Fatal(\"negative\")\n\t}\n}\n"
	fxBranch   = "gaos/fix-def-101"
)

func fixAF() AutoFix {
	return AutoFix{Enabled: true, AllowedGlobs: []string{"pkg/**"}, TestGlobs: []string{"**/*_test.go"}, CIConfirmed: true}
}

func goodFix() FixReply {
	return FixReply{Summary: "reject a non-positive quantity", Before: "quantity -1 is accepted", After: "quantity -1 is refused",
		Edits:    []fixEdit{{Path: fxFile, Find: "if qty == 0 {", Replace: "if qty <= 0 {"}},
		NewFiles: []fixNewFile{{Path: fxTestPath, Content: fxTestBody}}}
}

func fixReplyText(r FixReply) string {
	b, _ := json.Marshal(r)
	return "done\n```gaos-fix\n" + string(b) + "\n```\n"
}

func fxPacket() map[string]DefectContextFile {
	return map[string]DefectContextFile{
		fxFile:        {Repo: defRepo, Path: fxFile, SHA: "b-" + fxFile, Text: fxText},
		"cmd/main.go": {Repo: defRepo, Path: "cmd/main.go", SHA: "b-cmd/main.go", Text: "package main\n"},
	}
}

// fxLook is a tree lookup in which the packet's files exist.
func fxLook(mod func(string, *ghTreeEntry)) fixLookup {
	return func(p string) (ghTreeEntry, error) {
		e := ghTreeEntry{}
		if _, ok := fxPacket()[p]; ok {
			e = ghTreeEntry{Exists: true, Mode: "100644", Type: "blob", SHA: "b-" + p}
		}
		if mod != nil {
			mod(p, &e)
		}
		return e, nil
	}
}

func wantBlock(t *testing.T, name string, err error, reason string) {
	t.Helper()
	var fb *fixBlock
	if !errors.As(err, &fb) || fb.Reason != reason {
		t.Errorf("%s: got %v, want a block %s", name, err, reason)
	}
}

type fixEnv struct {
	s     *MemStore
	pid   string
	key   string
	card  TaskRecord
	fx    *defectFixer
	gh    *fakeGHW
	notes []string
}

func newFixEnv(t *testing.T) *fixEnv {
	t.Helper()
	return newFixEnvWith(t, TriageVerdict{Difficulty: "easy", Class: "min-value", Repo: defRepo,
		Files: []string{fxFile}, Reason: "negative quantity"})
}

func newFixEnvWith(t *testing.T, v TriageVerdict) *fixEnv {
	t.Helper()
	e := &fixEnv{gh: newFakeGHW()}
	s, pid, key, triage := easyFixture(t, []string{"pkg/**"}, map[string]string{fxFile: fxText})
	ctx := context.Background()
	if _, err := s.SetAutoFix(ctx, pid, fixAF(), "ops", tokenKindFine); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ResolveTriage(ctx, pid, triage.ID, v); err != nil {
		t.Fatal(err)
	}
	fix := cardByKey(s, defectCardKey(key, 1, "fix"))
	setStatus(s, fix.ID, StatusReview)
	e.s, e.pid, e.key = s, pid, key
	e.fx = &defectFixer{st: s, gh: newFakeWriter(t, e.gh), tokenKind: tokenKindFine, logf: func(string, ...any) {},
		notify: func(_ context.Context, _ TaskRecord, kind, text string) { e.notes = append(e.notes, kind+"|"+text) }}
	e.card = *cardByKey(s, defectCardKey(key, 1, "fix"))
	return e
}

func (e *fixEnv) now() TaskRecord { return *cardByKey(e.s, defectCardKey(e.key, 1, "fix")) }

func (e *fixEnv) run(reply string) { e.fx.afterRun(context.Background(), e.card, reply) }

func (e *fixEnv) defect() Defect {
	d, _ := e.s.DefectByKey(context.Background(), e.pid, e.key)
	return d
}

func TestFixPRPathRules(t *testing.T) {
	for _, p := range []string{"../x.go", "/etc/passwd", "pkg\\x.go", "pkg//x.go", "pkg/./x.go", "pkg/x\x00.go", ""} {
		r := goodFix()
		r.Edits[0].Path = p
		_, err := parseFix(fixReplyText(r))
		var fe *fixError
		if !errors.As(err, &fe) || fe.Code != "fix-bad-path" {
			t.Errorf("path %q: parse gave %v, want fix-bad-path", p, err)
		}
	}
	af := fixAF()
	af.AllowedGlobs = []string{"pkg/records/*.go"}
	r := goodFix()
	r.Edits = append(r.Edits, fixEdit{Path: "cmd/main.go", Find: "package main", Replace: "package main // x"})
	_, err := checkFix(r, fxPacket(), af, fxLook(nil))
	wantBlock(t, "an edit outside the allowed paths", err, BlockLimits)

	r = goodFix()
	r.NewFiles[0].Path = "pkg/records/helper.go"
	_, err = checkFix(r, fxPacket(), af, fxLook(nil))
	wantBlock(t, "a new file that is not a test", err, BlockLimits)

	r = goodFix()
	r.Edits[0].Path = "pkg/auth/login.go"
	_, err = checkFix(r, fxPacket(), fixAF(), fxLook(nil))
	wantBlock(t, "a built-in forbidden path", err, BlockLimits)

	r = goodFix()
	r.Edits[0].Path = "pkg/records/other.go"
	_, err = checkFix(r, fxPacket(), fixAF(), fxLook(nil))
	wantBlock(t, "a file the packet does not hold", err, BlockLimits)

	r = goodFix()
	r.NewFiles = append(r.NewFiles, fixNewFile{Path: "pkg/records/a_test.go", Content: "x\n"}, fixNewFile{Path: "pkg/records/b_test.go", Content: "x\n"})
	_, err = checkFix(r, fxPacket(), fixAF(), fxLook(nil))
	wantBlock(t, "four files", err, BlockLimits)

	r = goodFix()
	r.NewFiles = nil
	_, err = checkFix(r, fxPacket(), fixAF(), fxLook(nil))
	wantBlock(t, "no test file", err, BlockLimits)

	plan, err := checkFix(goodFix(), fxPacket(), fixAF(), fxLook(nil))
	if err != nil || len(plan.Files) != 2 {
		t.Fatalf("a good fix gave %+v %v", plan, err)
	}
}

func TestFixPREditFindMustOccurOnce(t *testing.T) {
	for name, find := range map[string]string{"zero": "if qty == 99 {", "twice": "\t"} {
		r := goodFix()
		r.Edits[0].Find = find
		_, err := checkFix(r, fxPacket(), fixAF(), fxLook(nil))
		wantBlock(t, name, err, BlockUnreadable)
	}
	// An edit that applies after another one is judged on the text it meets.
	r := goodFix()
	r.Edits = append(r.Edits, fixEdit{Path: fxFile, Find: "if qty <= 0 {", Replace: "if qty < 1 {"})
	if _, err := checkFix(r, fxPacket(), fixAF(), fxLook(nil)); err != nil {
		t.Errorf("two edits of one file: %v", err)
	}
}

func TestFixPRNewFileMustNotExist(t *testing.T) {
	look := fxLook(func(p string, e *ghTreeEntry) {
		if p == fxTestPath {
			*e = ghTreeEntry{Exists: true, Mode: "100644", Type: "blob", SHA: "x"}
		}
	})
	_, err := checkFix(goodFix(), fxPacket(), fixAF(), look)
	wantBlock(t, "a new file that exists", err, BlockLimits)

	e := newFixEnv(t)
	e.gh.files[fxTestPath] = "package records\n"
	e.run(fixReplyText(goodFix()))
	if c := e.now(); c.Status != StatusBlocked || c.BlockReason != BlockLimits {
		t.Errorf("the card is %s %q, want BLOCKED %s", c.Status, c.BlockReason, BlockLimits)
	}
	if n := e.gh.count("POST "); n != 0 {
		t.Errorf("%d GitHub write(s) were made for a refused fix", n)
	}
}

func TestFixPRLineCountIsMyers(t *testing.T) {
	cases := []struct {
		name string
		a, b string
		want int
	}{
		{"a line inserted at the top", "a\nb\nc\nd\n", "x\na\nb\nc\nd\n", 1},
		{"a line replaced", "a\nb\nc\n", "a\nB\nc\n", 2},
		{"a missing final newline", "a\nb\n", "a\nb", 0},
		{"crlf against lf", "a\r\nb\r\n", "a\nb\n", 0},
		{"a block moved down", "a\nb\nc\nd\n", "c\nd\na\nb\n", 4},
		{"empty to three lines", "", "a\nb\nc\n", 3},
		{"three lines to empty", "a\nb\nc\n", "", 3},
	}
	for _, c := range cases {
		if got := myersDistance(fixLines(c.a), fixLines(c.b)); got != c.want {
			t.Errorf("%s: %d changed lines, want %d", c.name, got, c.want)
		}
	}
}

func lines(n int, prefix string) string {
	var sb strings.Builder
	for i := 0; i < n; i++ {
		sb.WriteString(prefix + "\n")
	}
	return sb.String()
}

func TestFixPRNonTestAndTestCaps(t *testing.T) {
	run := func(nonTest, test int) error {
		r := goodFix()
		r.Edits[0] = fixEdit{Path: fxFile, Find: "if qty == 0 {", Replace: "if qty == 0 {\n" + strings.TrimSuffix(lines(nonTest, "\t_ = qty"), "\n")}
		r.NewFiles[0].Content = lines(test, "// t")
		_, err := checkFix(r, fxPacket(), fixAF(), fxLook(nil))
		return err
	}
	if err := run(40, 120); err != nil {
		t.Errorf("40 and 120 changed lines: %v", err)
	}
	wantBlock(t, "41 non-test lines", run(41, 10), BlockLimits)
	wantBlock(t, "121 test lines", run(5, 121), BlockLimits)
}

func TestFixPRBlobShaMovedBlocks(t *testing.T) {
	look := fxLook(func(p string, e *ghTreeEntry) {
		if p == fxFile {
			e.SHA = "moved"
		}
	})
	_, err := checkFix(goodFix(), fxPacket(), fixAF(), look)
	wantBlock(t, "a moved blob", err, BlockBaseMoved)

	e := newFixEnv(t)
	// The head now holds another version of the file than the packet pinned.
	e.gh.files[fxFile] = fxText
	e.gh.modes[fxFile] = "100644"
	d := e.defect()
	d.Context.Files[0].SHA = "stale"
	if _, err := e.s.SetDefectContext(context.Background(), e.pid, e.key, d.Context); err != nil {
		t.Fatal(err)
	}
	e.run(fixReplyText(goodFix()))
	if c := e.now(); c.Status != StatusBlocked || c.BlockReason != BlockBaseMoved {
		t.Errorf("the card is %s %q, want BLOCKED %s", c.Status, c.BlockReason, BlockBaseMoved)
	}
	if n := e.gh.count("POST "); n != 0 {
		t.Errorf("%d GitHub write(s) after the base moved", n)
	}
}

func TestFixPRModeIs100644(t *testing.T) {
	look := fxLook(func(p string, e *ghTreeEntry) {
		if p == fxFile {
			e.Mode = "120000"
		}
	})
	_, err := checkFix(goodFix(), fxPacket(), fixAF(), look)
	wantBlock(t, "a symlink", err, BlockLimits)
	look = fxLook(func(p string, e *ghTreeEntry) {
		if p == fxFile {
			e.Mode = "160000"
		}
	})
	_, err = checkFix(goodFix(), fxPacket(), fixAF(), look)
	wantBlock(t, "a submodule", err, BlockLimits)

	// An existing 100755 is kept, a new file is 100644, on the wire.
	e := newFixEnv(t)
	e.gh.modes[fxFile] = "100755"
	e.run(fixReplyText(goodFix()))
	var body struct {
		Tree []ghTreeItem `json:"tree"`
	}
	if err := json.Unmarshal(e.gh.body("POST /repos/acme-org/sample-service/git/trees"), &body); err != nil || len(body.Tree) != 2 {
		t.Fatalf("the tree request: %v %+v", err, body)
	}
	for _, it := range body.Tree {
		want := "100644"
		if it.Path == fxFile {
			want = "100755"
		}
		if it.Mode != want {
			t.Errorf("%s has mode %s, want %s", it.Path, it.Mode, want)
		}
	}
	// The client itself refuses any other mode.
	w := newFakeWriter(t, newFakeGHW())
	if _, err := w.createTree(context.Background(), "acme-org", "sample-service", "t", []ghTreeItem{{Path: "x", Mode: "120000", Type: "blob", SHA: "s"}}); err == nil {
		t.Error("createTree accepted a symlink entry")
	}
}

func TestFixPRBidiRefused(t *testing.T) {
	for name, content := range map[string]string{"right-to-left override": "// \u202e hidden\n", "zero width": "// a\u200bb\n", "NUL": "// a\x00b\n", "long line": "// " + strings.Repeat("x", 401) + "\n"} {
		r := goodFix()
		r.NewFiles[0].Content = content
		_, err := checkFix(r, fxPacket(), fixAF(), fxLook(nil))
		wantBlock(t, name, err, BlockLimits)
	}
	r := goodFix()
	r.Edits[0].Replace = "if qty <= 0 { // \u2066x"
	_, err := checkFix(r, fxPacket(), fixAF(), fxLook(nil))
	wantBlock(t, "an isolate in an edit", err, BlockLimits)
	r = goodFix()
	r.NewFiles[0].Content = "// token = AKIAIOSFODNN7EXAMPLE\n"
	_, err = checkFix(r, fxPacket(), fixAF(), fxLook(nil))
	wantBlock(t, "a secret in a new file", err, BlockEgress)

	// The pull-request text is stripped, never refused.
	in := prBodyIn{Defect: "DEF-101", Title: "t\u202e", Summary: "a\u202eb", Before: "x\u200by", After: "ok"}
	if body := renderPRBody("", in); hasBidi(body) {
		t.Error("the pull-request body holds a bidirectional or zero-width character")
	}
}

func TestFixPRCIWorkflowSecretsRefuse(t *testing.T) {
	cases := []struct {
		name, text string
		want       bool
	}{
		{"push, no filter, a secret", "name: ci\non: push\njobs:\n  a:\n    steps:\n      - run: echo ${{ secrets.DEPLOY_KEY }}\n", true},
		{"list form", "on: [push, pull_request]\njobs:\n  a:\n    env:\n      K: ${{ secrets.K }}\n", true},
		{"block form, filter matches gaos/**", "on:\n  push:\n    branches:\n      - 'gaos/**'\njobs:\n  a:\n    env:\n      K: ${{ secrets.K }}\n", true},
		{"block form, filter is main only", "on:\n  push:\n    branches: [main]\njobs:\n  a:\n    env:\n      K: ${{ secrets.K }}\n", false},
		{"a pull request into main", "on:\n  pull_request:\n    branches: [main]\njobs:\n  a:\n    env:\n      K: ${{ secrets.K }}\n", true},
		{"only the built-in token", "on: push\njobs:\n  a:\n    env:\n      K: ${{ secrets.GITHUB_TOKEN }}\n", false},
		{"a manual workflow", "on: workflow_dispatch\njobs:\n  a:\n    env:\n      K: ${{ secrets.K }}\n", false},
		{"inherit", "on: push\njobs:\n  a:\n    uses: ./.github/workflows/x.yml\n    secrets: inherit\n", true},
		{"ignored branches", "on:\n  push:\n    branches-ignore:\n      - 'gaos/**'\njobs:\n  a:\n    env:\n      K: ${{ secrets.K }}\n", false},
		{"a commented secret", "on: push\n# ${{ secrets.K }}\njobs: {}\n", false},
	}
	for _, c := range cases {
		_, got := ciSecretsRisk(map[string]string{".github/workflows/ci.yml": c.text}, fxBranch, "main", nil)
		if got != c.want {
			t.Errorf("%s: risk=%v, want %v", c.name, got, c.want)
		}
	}

	e := newFixEnv(t)
	e.gh.workflows[".github/workflows/ci.yml"] = cases[0].text
	e.run(fixReplyText(goodFix()))
	if c := e.now(); c.Status != StatusBlocked || c.BlockReason != BlockCISecrets {
		t.Errorf("the card is %s %q, want BLOCKED %s", c.Status, c.BlockReason, BlockCISecrets)
	}
	if n := e.gh.count("POST "); n != 0 {
		t.Errorf("%d GitHub write(s) behind a CI that runs secrets", n)
	}
	// A workflow that never sees the branch lets the fix through.
	e2 := newFixEnv(t)
	e2.gh.workflows[".github/workflows/ci.yml"] = cases[3].text
	e2.run(fixReplyText(goodFix()))
	if c := e2.now(); c.Status != StatusDone {
		t.Errorf("a harmless workflow left the card %s %q, want DONE", c.Status, c.BlockReason)
	}
}

func TestFixPRBranchExistsBlocks(t *testing.T) {
	e := newFixEnv(t)
	e.gh.refs[fxBranch] = "someone-elses"
	e.gh.commits["someone-elses"] = ghCommit{Message: "work in progress", Tree: "tree:x"}
	e.run(fixReplyText(goodFix()))
	if c := e.now(); c.Status != StatusBlocked || c.BlockReason != BlockBranchExists {
		t.Errorf("the card is %s %q, want BLOCKED %s", c.Status, c.BlockReason, BlockBranchExists)
	}
	if e.gh.count("POST /repos/acme-org/sample-service/pulls") != 0 || e.gh.count("DELETE") != 0 {
		t.Error("a branch that is not ours was used or deleted")
	}
	if e.gh.refs[fxBranch] != "someone-elses" {
		t.Error("the existing branch was moved")
	}
	// Ours by trailer but another tree is not adopted either.
	e2 := newFixEnv(t)
	e2.gh.refs[fxBranch] = "ours-old"
	e2.gh.commits["ours-old"] = ghCommit{Message: "fix\n\n" + fixTrailer(e2.key) + "\n", Tree: "tree:other"}
	e2.run(fixReplyText(goodFix()))
	if c := e2.now(); c.BlockReason != BlockBranchExists {
		t.Errorf("a stale branch of ours was adopted (%s %q)", c.Status, c.BlockReason)
	}
}

func TestFixPRCrashAdoptsByTrailer(t *testing.T) {
	e := newFixEnv(t)
	e.gh.prFail = 500
	e.run(fixReplyText(goodFix()))
	if c := e.now(); c.Status != StatusFailed {
		t.Fatalf("the first run left the card %s, want FAILED", c.Status)
	}
	// The crash left the commit and the branch behind: put them back.
	e.gh.refs[fxBranch] = e.gh.lastCommit
	e.gh.prFail = 0
	setStatus(e.s, e.card.ID, StatusReview)
	refsBefore := e.gh.count("POST /repos/acme-org/sample-service/git/refs")
	e.run(fixReplyText(goodFix()))
	if c := e.now(); c.Status != StatusDone {
		t.Fatalf("the resumed run left the card %s %q, want DONE", c.Status, c.BlockReason)
	}
	if n := e.gh.count("POST /repos/acme-org/sample-service/git/refs") - refsBefore; n != 0 {
		t.Errorf("the resumed run created %d ref(s); it must adopt the one that is there", n)
	}
	if len(e.gh.prs) != 1 {
		t.Errorf("%d pull request(s) exist, want 1", len(e.gh.prs))
	}
	if e.gh.count("DELETE") != 1 {
		t.Errorf("%d delete(s); only the first run's failed branch is deleted", e.gh.count("DELETE"))
	}
	d := e.defect()
	if d.State != DefectStateFixing || d.FixPR == "" {
		t.Errorf("the defect is %s with pull request %q, want fixing", d.State, d.FixPR)
	}
}

func TestFixPRDeleteRefOnlyOurs(t *testing.T) {
	// A pull request that cannot be created deletes the ref this run made.
	e := newFixEnv(t)
	e.gh.prFail = 500
	e.run(fixReplyText(goodFix()))
	if got := e.gh.count("DELETE /repos/acme-org/sample-service/git/refs/heads/" + fxBranch); got != 1 {
		t.Errorf("%d delete(s) of the branch this run created, want 1", got)
	}
	if _, ok := e.gh.refs[fxBranch]; ok {
		t.Error("the branch is still there")
	}
	// A ref that was adopted is not ours to delete.
	e2 := newFixEnv(t)
	e2.gh.prFail = 500
	e2.run(fixReplyText(goodFix()))
	e2.gh.refs[fxBranch] = e2.gh.lastCommit
	before := e2.gh.count("DELETE")
	setStatus(e2.s, e2.card.ID, StatusReview)
	e2.run(fixReplyText(goodFix()))
	if e2.gh.count("DELETE") != before {
		t.Error("an adopted branch was deleted when the pull request failed")
	}
	// And the client deletes nothing outside gaos/fix-.
	f := newFakeGHW()
	w := newFakeWriter(t, f)
	for _, b := range []string{"main", "feature/x", "gaos/other", "gaos/fix-../main"} {
		if err := w.deleteRef(context.Background(), "acme-org", "sample-service", b); err == nil {
			t.Errorf("deleteRef(%q) was allowed", b)
		}
	}
	if f.count("DELETE") != 0 {
		t.Error("a delete reached GitHub for a branch outside gaos/fix-")
	}
}

func TestFixPRNeverReadyOrMerge(t *testing.T) {
	e := newFixEnv(t)
	e.run(fixReplyText(goodFix()))
	if c := e.now(); c.Status != StatusDone {
		t.Fatalf("the card is %s %q, want DONE", c.Status, c.BlockReason)
	}
	var pr struct {
		Draft bool   `json:"draft"`
		Head  string `json:"head"`
		Base  string `json:"base"`
	}
	if err := json.Unmarshal(e.gh.body("POST /repos/acme-org/sample-service/pulls"), &pr); err != nil || !pr.Draft || pr.Head != fxBranch || pr.Base != "main" {
		t.Errorf("the pull request was %+v (%v), want a draft from %s into main", pr, err, fxBranch)
	}
	e.gh.mu.Lock()
	for _, c := range e.gh.calls {
		if strings.HasPrefix(c, "PUT ") || strings.HasPrefix(c, "PATCH ") || strings.HasPrefix(c, "DELETE ") ||
			strings.Contains(c, "/merge") || strings.Contains(c, "ready_for_review") {
			t.Errorf("GitHub saw %q; nothing may merge, update or ready a pull request", c)
		}
	}
	e.gh.mu.Unlock()
	if e.gh.count("POST /repos/acme-org/sample-service/issues/7/comments") != 1 || e.gh.count("POST /repos/acme-org/sample-service/issues/7/labels") != 1 {
		t.Error("the pull request did not get exactly one comment and one label call")
	}
	for _, l := range fixPRLabels {
		if !e.gh.labels[l] {
			t.Errorf("label %s was not created", l)
		}
	}
	if len(e.gh.refs) != 2 || e.gh.refs["main"] != "head1" {
		t.Errorf("refs are %v; the default branch must not move", e.gh.refs)
	}
	d := e.defect()
	if d.State != DefectStateFixing || len(streamOf(e.s, FixPROpened)) != 1 {
		t.Errorf("defect %s, %d FixPROpened event(s); want fixing and one", d.State, len(streamOf(e.s, FixPROpened)))
	}

	// A plan without drafts: blocked, no non-draft fallback, the ref removed.
	e2 := newFixEnv(t)
	e2.gh.prFail, e2.gh.prDraftMsg = 422, true
	e2.run(fixReplyText(goodFix()))
	if c := e2.now(); c.Status != StatusBlocked || c.BlockReason != BlockDraftUnsupported {
		t.Errorf("the card is %s %q, want BLOCKED %s", c.Status, c.BlockReason, BlockDraftUnsupported)
	}
	if n := e2.gh.count("POST /repos/acme-org/sample-service/pulls"); n != 1 {
		t.Errorf("%d pull request attempt(s), want 1 (no fallback)", n)
	}
	if _, ok := e2.gh.refs[fxBranch]; ok {
		t.Error("the branch of a refused draft was left behind")
	}
}

func TestFixPRBodySlotsAndFence(t *testing.T) {
	in := prBodyIn{Defect: "DEF-101", Title: "negative `quantity`", Summary: "s", Before: "x ```evil``` {{after}}\nsecond", After: "ok @team",
		Files: []string{fxFile}, Limits: "2 file(s)"}
	tpl := "T {{defect}} / {{title}} / {{summary}}\n{{before}}\nA:\n{{after}}\nF:\n{{files}}\nL: {{limits}}"
	body := renderPRBody(tpl, in)
	for _, want := range []string{"T DEF-101", "negative quantity", "- `" + fxFile + "`", "L: 2 file(s)", "did not run it", "ok (at)team"} {
		if !strings.Contains(body, want) {
			t.Errorf("the body lacks %q", want)
		}
	}
	if strings.Contains(body, "evil```") {
		t.Errorf("the body holds an unstripped fence: %q", body)
	}
	if strings.Count(body, "{{after}}") != 1 {
		t.Error("text that looks like a slot was expanded (it must stay as the agent wrote it)")
	}
	for _, line := range strings.Split(body, "\n") {
		if strings.Contains(line, "`") && !strings.HasPrefix(line, "```") && !strings.HasPrefix(line, "- `") {
			t.Errorf("a backtick reached the body outside a fence: %q", line)
		}
	}
	if f := fenceFor("a ````` b"); len(f) != 6 {
		t.Errorf("the fence for a run of five backticks is %q, want six", f)
	}
	def := renderPRBody("", in)
	if !strings.Contains(def, "## Draft fix for DEF-101") || !strings.Contains(def, "not run by GAOS") {
		t.Error("the default template did not render")
	}
	if !templateHasSlots(defaultPRTemplate) || templateHasSlots("{{before}}") {
		t.Error("templateHasSlots disagrees with the slots")
	}
}

func TestFixPRSummaryOneLine(t *testing.T) {
	got := prSummaryLine("fix\nthe  bug\r\n@alice `x` <b>\u202e" + strings.Repeat("y", 100))
	if strings.ContainsAny(got, "\n\r`<>@") || hasBidi(got) || len([]rune(got)) > prSummaryMax {
		t.Errorf("the summary %q is not one safe line of at most 72 characters", got)
	}
	if !strings.Contains(prSummaryLine("ping @alice"), "(at)alice") {
		t.Error("@ was not rendered as (at)")
	}
	if prSummaryLine(" \n\t ") != prDefaultFixed {
		t.Error("an empty summary has no default")
	}
	plan := fixPlan{Files: []fixFile{{Path: fxFile}}}
	if got := fixCommitSubject(plan, "reject a negative quantity", "DEF-101"); got != "fix(pkg): reject a negative quantity (DEF-101)" {
		t.Errorf("the commit subject is %q", got)
	}
	if got := fixBranch(Defect{DefectID: "DEF-101", Gen: 1}); got != "gaos/fix-def-101" {
		t.Errorf("the branch is %q", got)
	}
	if got := fixBranch(Defect{DefectID: "DEF-101", Gen: 3}); got != "gaos/fix-def-101-3" {
		t.Errorf("the branch of generation 3 is %q", got)
	}
	if got := fixBranch(Defect{CaseID: "TC-" + strings.Repeat("A", 80), Gen: 1}); len(got) > len(fixRefPrefix)+60 {
		t.Errorf("a long slug was not cut: %q", got)
	}
}

func TestFixPRWhatsAppText(t *testing.T) {
	e := newFixEnv(t)
	e.run(fixReplyText(goodFix()))
	want := "fix:pr|p-1 · auto-fix draft PR DEF-101: https://github.example/acme-org/sample-service/pull/7"
	if len(e.notes) != 1 || e.notes[0] != want {
		t.Fatalf("notifications %q, want exactly %q", e.notes, want)
	}
	if strings.Contains(e.notes[0], "quantity") || strings.Contains(e.notes[0], "validate") {
		t.Error("the line carries a code or defect text")
	}
	// A complex verdict at triage time sends its own line.
	s, pid, key, triage := easyFixture(t, []string{"pkg/**"}, map[string]string{fxFile: fxText})
	var notes []string
	dt := &defectTriager{st: s, logf: func(string, ...any) {},
		notify: func(_ context.Context, _ TaskRecord, kind, text string) { notes = append(notes, kind+"|"+text) }}
	setStatus(s, triage.ID, StatusReview)
	card := *cardByKey(s, defectCardKey(key, 1, "triage"))
	dt.afterRun(context.Background(), card, "```gaos-triage\ndifficulty: complex\nclass: billing\nreason: prices\n```\n")
	if len(notes) != 1 || notes[0] != "defect:complex|"+pid+" · DEF-101 needs a person" {
		t.Errorf("complex notifications %q", notes)
	}
}

// A workflow with a paths: filter runs for a pull request only when a changed file matches (found
// live: his-backend's one secret-using workflow runs for one service's directory only, and the fix
// never touches it, yet the whole fix was blocked). Unknown files and negations stay conservative.
func TestFixPRCIPathFilters(t *testing.T) {
	secret := "      K: ${{ secrets.K }}\n"
	wf := func(on string) string { return "on:\n  pull_request:\n" + on + "jobs:\n  a:\n    env:\n" + secret }
	for _, c := range []struct {
		name, text string
		changed    []string
		want       bool
	}{
		{"paths, a changed file outside them", wf("    paths:\n      - 'internal/service/chart/**'\n"), []string{"pkg/graph/x.go"}, false},
		{"paths, a changed file inside them", wf("    paths:\n      - 'internal/service/chart/**'\n"), []string{"internal/service/chart/a.go"}, true},
		{"paths, one of two changed files inside", wf("    paths:\n      - 'internal/service/chart/**'\n"), []string{"pkg/x.go", "internal/service/chart/a.go"}, true},
		{"paths unknown (nil changed)", wf("    paths:\n      - 'internal/service/chart/**'\n"), nil, true},
		{"a negated path is not reasoned about", wf("    paths:\n      - '**'\n      - '!docs/**'\n"), []string{"docs/a.md"}, true},
		{"paths-ignore covering every changed file", wf("    paths-ignore:\n      - 'docs/**'\n"), []string{"docs/a.md"}, false},
		{"paths-ignore, a file it does not cover", wf("    paths-ignore:\n      - 'docs/**'\n"), []string{"docs/a.md", "pkg/x.go"}, true},
		{"no paths filter", wf("    branches: [main]\n"), []string{"pkg/x.go"}, true},
	} {
		_, got := ciSecretsRisk(map[string]string{".github/workflows/ci.yml": c.text}, fxBranch, "main", c.changed)
		if got != c.want {
			t.Errorf("%s: risk=%v, want %v", c.name, got, c.want)
		}
	}
}
