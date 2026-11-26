package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// Triage (D-091): the context builder, the reply parser and the re-check.

func noSleep(context.Context, time.Duration) {}

// fakeGitHub answers the three read calls the context needs.
type fakeGitHub struct {
	searchStatus int
	searchPaths  []string
	files        map[string]string // path -> text
	searches     atomic.Int32
}

func (f *fakeGitHub) handler(w http.ResponseWriter, r *http.Request) {
	p := r.URL.Path
	switch {
	case p == "/repos/acme-org/sample-service":
		_, _ = w.Write([]byte(`{"default_branch":"main"}`))
	case p == "/repos/acme-org/sample-service/git/ref/heads/main":
		_, _ = w.Write([]byte(`{"object":{"sha":"abc123head"}}`))
	case p == "/search/code":
		f.searches.Add(1)
		if f.searchStatus != 0 {
			w.WriteHeader(f.searchStatus)
			return
		}
		var items []map[string]string
		for _, sp := range f.searchPaths {
			items = append(items, map[string]string{"path": sp})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"items": items})
	case strings.HasPrefix(p, "/repos/acme-org/sample-service/contents/"):
		path := strings.TrimPrefix(p, "/repos/acme-org/sample-service/contents/")
		text, ok := f.files[path]
		if !ok || r.URL.Query().Get("ref") != "abc123head" {
			w.WriteHeader(404)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"type": "file", "encoding": "base64", "sha": "blob-" + path,
			"size": len(text), "content": base64.StdEncoding.EncodeToString([]byte(text))})
	default:
		w.WriteHeader(404)
	}
}

func newFakeCode(t *testing.T, f *fakeGitHub) *ghClient {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(f.handler))
	t.Cleanup(srv.Close)
	c := newGHClient(ghConfig{token: "ghp_" + strings.Repeat("a", 30), base: srv.URL})
	return c
}

func sampleDefect() Defect {
	return Defect{Key: "p-9/DEF-101", ProductID: "p-9", DefectID: "DEF-101", TargetRepos: []string{defRepo},
		Operations: []string{"editSampleRecord"}}
}

func TestDefectSearchErrorIsNotEmpty(t *testing.T) {
	// A 503 is an error, retried and then a block; it never reads as "nothing found".
	f := &fakeGitHub{searchStatus: 503}
	_, err := buildDefectContext(context.Background(), newFakeCode(t, f), sampleDefect(), "", noSleep)
	if !errors.Is(err, errSearchFailed) {
		t.Fatalf("a 503 search gave %v, want errSearchFailed", err)
	}
	if n := f.searches.Load(); n != defectSearchRetries {
		t.Errorf("%d search attempts, want %d (the retries)", n, defectSearchRetries)
	}
	// An empty answer is an empty context, not an error.
	empty := &fakeGitHub{}
	c, err := buildDefectContext(context.Background(), newFakeCode(t, empty), sampleDefect(), "", noSleep)
	if err != nil || !c.Built || len(c.Files) != 0 || c.HeadOf(defRepo) != "abc123head" {
		t.Errorf("an empty search gave %+v %v, want a built context with no files", c, err)
	}
	// And the store: a failed build blocks the card, an empty one stores a context.
	s, pid := defectFixture(t)
	report(t, s, pid, defItem(DefectKindXfail, "DEF-101", defCase1))
	key := pid + "/DEF-101"
	if err := buildAndStoreDefectContext(context.Background(), s, newFakeCode(t, f), pid, key, noSleep); err == nil {
		t.Error("a failed search must return its error")
	}
	card := cardByKey(s, defectCardKey(key, 1, "triage"))
	if card.Status != StatusBlocked || card.BlockReason != BlockSearchFailed {
		t.Errorf("the triage card is %s %q, want BLOCKED %s", card.Status, card.BlockReason, BlockSearchFailed)
	}
	s2, pid2 := defectFixture(t)
	report(t, s2, pid2, defItem(DefectKindXfail, "DEF-101", defCase1))
	if err := buildAndStoreDefectContext(context.Background(), s2, newFakeCode(t, empty), pid2, pid2+"/DEF-101", noSleep); err != nil {
		t.Fatal(err)
	}
	if d, _ := s2.DefectByKey(context.Background(), pid2, pid2+"/DEF-101"); !d.Context.Built {
		t.Error("an empty search stored no context")
	}
}

// One file with a secret-shaped string must not make the whole triage refuse: it is left out and
// the others stay (found live: a JWT in a source file blocked DEF-922's triage at the egress pass).
func TestDefectContextLeavesOutAFileTheEgressPassWouldRefuse(t *testing.T) {
	jwt := "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dBjftJeZ4CVPmB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	f := &fakeGitHub{
		searchPaths: []string{"pkg/records/fixture.go", "pkg/records/validate.go"},
		files: map[string]string{"pkg/records/fixture.go": "package records\nconst tok = \"" + jwt + "\"\n",
			"pkg/records/validate.go": "package records\n"},
	}
	c, err := buildDefectContext(context.Background(), newFakeCode(t, f), sampleDefect(), "", noSleep)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Files) != 1 || c.Files[0].Path != "pkg/records/validate.go" {
		t.Errorf("files = %+v; want only validate.go, the fixture holding a token left out", c.Files)
	}
	for _, file := range c.Files {
		if strings.Contains(file.Text, "eyJ") {
			t.Errorf("a token reached the slice of %s", file.Path)
		}
	}
}

// The files the first operation finds come before those of the later ones, whatever their names
// (found live: files were ordered by position then alphabetically, and the resolver of the defect's
// own operation lost its place to files found for the fourth).
func TestDefectContextOrdersFilesByTheOperationThatFoundThem(t *testing.T) {
	f := &opFakeGitHub{byOp: map[string][]string{
		"firstOp":  {"pkg/z_resolver.go", "pkg/y.go"},
		"secondOp": {"pkg/a_other.go"},
	}, files: map[string]string{"pkg/z_resolver.go": "package p\n", "pkg/y.go": "package p\n", "pkg/a_other.go": "package p\n"}}
	d := sampleDefect()
	d.Operations = []string{"firstOp", "secondOp"}
	c, err := buildDefectContext(context.Background(), newOpFakeCode(t, f), d, "", noSleep)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, file := range c.Files {
		got = append(got, file.Path)
	}
	if strings.Join(got, ",") != "pkg/z_resolver.go,pkg/y.go,pkg/a_other.go" {
		t.Errorf("files = %v; want the first operation's hits in search order, then the second's", got)
	}
}

type opFakeGitHub struct {
	byOp  map[string][]string
	files map[string]string
}

func (f *opFakeGitHub) handler(w http.ResponseWriter, r *http.Request) {
	p := r.URL.Path
	switch {
	case p == "/repos/acme-org/sample-service":
		_, _ = w.Write([]byte(`{"default_branch":"main"}`))
	case p == "/repos/acme-org/sample-service/git/ref/heads/main":
		_, _ = w.Write([]byte(`{"object":{"sha":"abc123head"}}`))
	case p == "/search/code":
		var items []map[string]string
		for op, paths := range f.byOp {
			if strings.Contains(r.URL.Query().Get("q"), op) {
				for _, sp := range paths {
					items = append(items, map[string]string{"path": sp})
				}
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"items": items})
	case strings.HasPrefix(p, "/repos/acme-org/sample-service/contents/"):
		path := strings.TrimPrefix(p, "/repos/acme-org/sample-service/contents/")
		text, ok := f.files[path]
		if !ok {
			w.WriteHeader(404)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"type": "file", "encoding": "base64", "sha": "blob-" + path,
			"size": len(text), "content": base64.StdEncoding.EncodeToString([]byte(text))})
	default:
		w.WriteHeader(404)
	}
}

func newOpFakeCode(t *testing.T, f *opFakeGitHub) *ghClient {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(f.handler))
	t.Cleanup(srv.Close)
	return newGHClient(ghConfig{token: "ghp_" + strings.Repeat("a", 30), base: srv.URL})
}

func TestDefectContextKeepsPathsAndLimits(t *testing.T) {
	big := strings.Repeat("x", 30<<10)
	f := &fakeGitHub{
		searchPaths: []string{"pkg/records/validate.go", "../escape.go", "db/migrations/001.go", "pkg/auth/token.go",
			"config/secrets.go", "pkg/records/a.go", "pkg/records/b.go", "pkg/records/c.go"},
		files: map[string]string{"pkg/records/validate.go": "package records\n", "pkg/records/a.go": big,
			"pkg/records/b.go": big, "pkg/records/c.go": big},
	}
	d := sampleDefect()
	d.Operations = []string{"a", "b", "c", "d", "e"}
	c, err := buildDefectContext(context.Background(), newFakeCode(t, f), d, "", noSleep)
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range c.Files {
		if strings.Contains(file.Path, "..") || strings.Contains(file.Path, "auth") ||
			strings.Contains(file.Path, "migrations") || strings.Contains(file.Path, "secret") {
			t.Errorf("a path the rules refuse was fetched: %s", file.Path)
		}
	}
	total := 0
	for _, file := range c.Files {
		total += len(file.Text)
	}
	if total > defectMaxFileBytes || len(c.Files) > defectMaxFiles {
		t.Errorf("%d files and %d bytes, past the limits", len(c.Files), total)
	}
	if len(c.Files) == 0 || c.Files[0].Path != "pkg/records/validate.go" || c.Files[0].SHA != "blob-pkg/records/validate.go" {
		t.Errorf("files = %+v, want the best-ranked file first with its blob sha", c.Files)
	}
	if c.Searches > defectMaxSearches {
		t.Errorf("%d searches, want at most %d", c.Searches, defectMaxSearches)
	}
}

// lockProbe is a code source that tries to take the store's write lock from
// inside every call: it can only succeed if nobody holds it.
type lockProbe struct {
	s      *MemStore
	calls  int
	locked int
}

func (p *lockProbe) probe() {
	p.calls++
	if !p.s.mu.TryLock() {
		p.locked++
		return
	}
	p.s.mu.Unlock()
}
func (p *lockProbe) defaultHead(context.Context, string, string, string) (string, error) {
	p.probe()
	return "h1", nil
}
func (p *lockProbe) searchCode(context.Context, string, string, string) ([]ghCodeHit, error) {
	p.probe()
	return []ghCodeHit{{Path: "pkg/a.go"}}, nil
}
func (p *lockProbe) fileAt(context.Context, string, string, string, string) (ghBlob, bool, error) {
	p.probe()
	return ghBlob{Path: "pkg/a.go", SHA: "b1", Text: "package pkg\n"}, true, nil
}

func TestDefectContextBuiltOutsideTheLock(t *testing.T) {
	s, pid := defectFixture(t)
	report(t, s, pid, defItem(DefectKindXfail, "DEF-101", defCase1))
	probe := &lockProbe{s: s}
	if err := buildAndStoreDefectContext(context.Background(), s, probe, pid, pid+"/DEF-101", noSleep); err != nil {
		t.Fatal(err)
	}
	if probe.calls < 3 {
		t.Fatalf("the source was called %d times, want the head, a search and a file", probe.calls)
	}
	if probe.locked != 0 {
		t.Errorf("the store lock was held during %d of %d GitHub calls", probe.locked, probe.calls)
	}
	d, _ := s.DefectByKey(context.Background(), pid, pid+"/DEF-101")
	if len(d.Context.Files) != 1 {
		t.Errorf("stored context: %+v", d.Context)
	}
}

func TestDefectTriageParseErrors(t *testing.T) {
	block := func(body string) string {
		return "text\n" + fence + TriageFence + "\n" + body + "\n" + fence + "\n"
	}
	good := "difficulty: easy\nclass: min-value\nrepo: " + defRepo + "\nfiles: a/b.go, c/d.go\nreason: a negative quantity"
	v, err := parseTriage(block(good))
	if err != nil || v.Difficulty != "easy" || len(v.Files) != 2 || v.Repo != defRepo {
		t.Fatalf("a good block gave %+v %v", v, err)
	}
	cases := map[string]string{
		"triage-missing: no block":      "no block at all",
		"triage-missing: not closed":    "x\n" + fence + TriageFence + "\ndifficulty: easy\n",
		"triage-missing: no difficulty": block("class: unknown\nreason: x"),
		"triage-bad-value: difficulty":  block("difficulty: trivial\nclass: unknown"),
		"triage-bad-value: class":       block("difficulty: medium\nclass: magic"),
		"triage-bad-value: reason":      block("difficulty: medium\nclass: unknown\nreason: " + strings.Repeat("r", 401)),
		"triage-bad-repo: shape":        block("difficulty: medium\nclass: unknown\nrepo: not a repo"),
		"triage-bad-repo: easy no repo": block("difficulty: easy\nclass: format\nfiles: a.go"),
	}
	for name, reply := range cases {
		_, err := parseTriage(reply)
		var te *triageError
		if !errors.As(err, &te) {
			t.Errorf("%s: got %v, want a triageError", name, err)
			continue
		}
		if want := strings.SplitN(name, ":", 2)[0]; te.Code != want {
			t.Errorf("%s: code %q, want %q", name, te.Code, want)
		}
	}
	// The last block wins: a stale block earlier in the reply is not read.
	two := block("difficulty: complex\nclass: auth") + block("difficulty: medium\nclass: unknown")
	if v, _ := parseTriage(two); v.Difficulty != "medium" {
		t.Errorf("two blocks gave %q, want the last (medium)", v.Difficulty)
	}
	// An unreadable reply fails the card with the class and the record stays open.
	s, pid := defectFixture(t)
	report(t, s, pid, defItem(DefectKindXfail, "DEF-101", defCase1))
	key := pid + "/DEF-101"
	card := cardByKey(s, defectCardKey(key, 1, "triage"))
	s.mu.Lock()
	s.taskByIDLocked(card.ID).Status = StatusReview
	s.mu.Unlock()
	card = cardByKey(s, defectCardKey(key, 1, "triage"))
	dt := &defectTriager{st: s, logf: func(string, ...any) {}}
	dt.afterRun(context.Background(), *card, "no verdict here")
	after := cardByKey(s, defectCardKey(key, 1, "triage"))
	if after.Status != StatusFailed || after.BlockReason != "triage-missing" {
		t.Errorf("the card is %s %q, want FAILED triage-missing", after.Status, after.BlockReason)
	}
	if d, _ := s.DefectByKey(context.Background(), pid, key); d.State != DefectStateOpen {
		t.Errorf("the record is %q, want open", d.State)
	}
}

func easyFixture(t *testing.T, globs []string, files map[string]string) (*MemStore, string, string, *TaskRecord) {
	t.Helper()
	s, pid := defectFixture(t)
	if _, err := s.SetAutoFix(context.Background(), pid, AutoFix{AllowedGlobs: globs}, "ops", "fine-grained"); err != nil {
		t.Fatal(err)
	}
	report(t, s, pid, defItem(DefectKindXfail, "DEF-101", defCase1))
	key := pid + "/DEF-101"
	c := DefectContext{Built: true, Repos: []DefectContextRepo{{Repo: defRepo, Head: "h1"}}}
	for p, txt := range files {
		c.Files = append(c.Files, DefectContextFile{Repo: defRepo, Path: p, SHA: "b-" + p, Text: txt})
	}
	if _, err := s.SetDefectContext(context.Background(), pid, key, c); err != nil {
		t.Fatal(err)
	}
	return s, pid, key, cardByKey(s, defectCardKey(key, 1, "triage"))
}

func TestDefectEasyIsRecheckedAndDowngraded(t *testing.T) {
	files := map[string]string{"pkg/records/validate.go": "package records\n", "docs/readme.md": "x"}
	easy := func(mod func(*TriageVerdict)) TriageVerdict {
		v := TriageVerdict{Difficulty: "easy", Class: "min-value", Repo: defRepo, Files: []string{"pkg/records/validate.go"}, Reason: "negative quantity"}
		if mod != nil {
			mod(&v)
		}
		return v
	}
	cases := []struct {
		name  string
		globs []string
		v     TriageVerdict
		files map[string]string
		want  string // "" = stays easy
	}{
		{"a good easy stands", []string{"pkg/**"}, easy(nil), files, ""},
		{"no allowed globs always downgrades", nil, easy(nil), files, "autofix-no-allowed-globs"},
		{"no files in the packet", []string{"pkg/**"}, easy(nil), nil, "autofix-no-files"},
		{"a class outside the allowlist", []string{"pkg/**"}, easy(func(v *TriageVerdict) { v.Class = "business-rule" }), files, "autofix-class"},
		{"a repo that is not a kept target", []string{"pkg/**"}, easy(func(v *TriageVerdict) { v.Repo = "acme-org/other" }), files, "autofix-repo"},
		{"a file the packet does not hold", []string{"pkg/**"}, easy(func(v *TriageVerdict) { v.Files = []string{"pkg/records/new.go"} }), files, "autofix-files-outside-packet"},
		{"a file outside the allowed globs", []string{"pkg/**"}, easy(func(v *TriageVerdict) { v.Files = []string{"docs/readme.md"} }), files, "autofix-files-outside-globs"},
		{"a built-in forbidden path", []string{"**"}, easy(func(v *TriageVerdict) { v.Files = []string{"pkg/auth/token.go"} }),
			map[string]string{"pkg/auth/token.go": "x"}, "autofix-files-outside-globs"},
		{"no files named", []string{"pkg/**"}, easy(func(v *TriageVerdict) { v.Files = nil }), files, "autofix-no-files"},
	}
	for _, c := range cases {
		s, pid, key, card := easyFixture(t, c.globs, c.files)
		if _, err := s.ResolveTriage(context.Background(), pid, card.ID, c.v); err != nil {
			t.Errorf("%s: %v", c.name, err)
			continue
		}
		d, _ := s.DefectByKey(context.Background(), pid, key)
		fix := cardByKey(s, defectCardKey(key, 1, "fix"))
		if fix == nil {
			t.Errorf("%s: no fix card was planned", c.name)
			continue
		}
		if c.want == "" {
			if d.Difficulty != "easy" || fix.Status != StatusPending || !hasLabel(fix.Labels, LabelDefectEasy) {
				t.Errorf("%s: difficulty %q, fix card %s %v; want easy, PENDING, defect:easy", c.name, d.Difficulty, fix.Status, fix.Labels)
			}
			continue
		}
		if d.Difficulty != "medium" || !strings.Contains(d.Reason, c.want) {
			t.Errorf("%s: difficulty %q reason %q; want medium naming %s", c.name, d.Difficulty, d.Reason, c.want)
		}
		if fix.Status != StatusBlocked || fix.BlockReason != BlockNotEasy || !hasLabel(fix.Labels, LabelDefectMedium) {
			t.Errorf("%s: fix card %s %q %v; want BLOCKED %s defect:medium", c.name, fix.Status, fix.BlockReason, fix.Labels, BlockNotEasy)
		}
		var said bool
		for _, e := range streamOf(s, DefectTriaged) {
			said = said || e.Payload.Rule == c.want
		}
		if !said {
			t.Errorf("%s: DefectTriaged does not carry the downgrade code %s", c.name, c.want)
		}
	}
	// The triage card ends DONE with the verdict rule, and the record is triaged.
	s, pid, key, card := easyFixture(t, []string{"pkg/**"}, files)
	if _, err := s.ResolveTriage(context.Background(), pid, card.ID, easy(nil)); err != nil {
		t.Fatal(err)
	}
	done := cardByKey(s, defectCardKey(key, 1, "triage"))
	if done.Status != StatusDone || !hasLabel(done.Labels, LabelDefectTriage) {
		t.Errorf("the triage card is %s %v, want DONE and still defect:triage", done.Status, done.Labels)
	}
	if d, _ := s.DefectByKey(context.Background(), pid, key); d.State != DefectStateTriaged {
		t.Errorf("state %q, want triaged", d.State)
	}
	var rule bool
	for _, e := range streamOf(s, TaskMoved) {
		rule = rule || (e.Payload.Rule == RuleDefectVerdict && e.Payload.To == StatusDone)
	}
	if !rule {
		t.Errorf("no TaskMoved to DONE named %s", RuleDefectVerdict)
	}
	// A verdict twice is refused: the record is no longer open.
	if _, err := s.ResolveTriage(context.Background(), pid, card.ID, easy(nil)); err == nil {
		t.Error("a second verdict was accepted")
	}
}

func TestDefectKindsOfFixCardsAreBornBlockedOrReady(t *testing.T) {
	// e2e is always BLOCKED autofix-not-easy, whatever the verdict says.
	s, pid := defectFixture(t)
	if _, err := s.SetAutoFix(context.Background(), pid, AutoFix{AllowedGlobs: []string{"pkg/**"}}, "ops", "fine-grained"); err != nil {
		t.Fatal(err)
	}
	it := defItem(DefectKindXfail, "DEF-101", defCase1)
	it.AutomationType, it.Backend, it.Level = "e2e", "", "WEB"
	report(t, s, pid, it)
	key := pid + "/DEF-101"
	if _, err := s.SetDefectContext(context.Background(), pid, key, DefectContext{Built: true,
		Repos: []DefectContextRepo{{Repo: defRepo, Head: "h"}},
		Files: []DefectContextFile{{Repo: defRepo, Path: "pkg/a.go", SHA: "b", Text: "x"}}}); err != nil {
		t.Fatal(err)
	}
	tri := cardByKey(s, defectCardKey(key, 1, "triage"))
	if _, err := s.ResolveTriage(context.Background(), pid, tri.ID, TriageVerdict{Difficulty: "easy", Class: "format", Repo: defRepo, Files: []string{"pkg/a.go"}}); err != nil {
		t.Fatal(err)
	}
	fix := cardByKey(s, defectCardKey(key, 1, "fix"))
	if fix.Status != StatusBlocked || fix.BlockReason != BlockNotEasy || !hasLabel(fix.Labels, LabelDefectEasy) {
		t.Errorf("an e2e fix card: %s %q %v", fix.Status, fix.BlockReason, fix.Labels)
	}
	// A report naming no repository the product owns blocks the fix card autofix-no-repo.
	s2, pid2 := defectFixture(t)
	it2 := defItem(DefectKindXfail, "DEF-102", defCase1)
	it2.TargetRepos = []string{"other-org/stranger"}
	report(t, s2, pid2, it2)
	k2 := pid2 + "/DEF-102"
	if d, _ := s2.DefectByKey(context.Background(), pid2, k2); len(d.TargetRepos) != 0 {
		t.Fatalf("a repository the product does not own was kept: %v", d.TargetRepos)
	}
	tri2 := cardByKey(s2, defectCardKey(k2, 1, "triage"))
	if _, err := s2.ResolveTriage(context.Background(), pid2, tri2.ID, TriageVerdict{Difficulty: "medium", Class: "unknown"}); err != nil {
		t.Fatal(err)
	}
	if fix := cardByKey(s2, defectCardKey(k2, 1, "fix")); fix.Status != StatusBlocked || fix.BlockReason != BlockNoRepo {
		t.Errorf("no-repo fix card: %+v", fix)
	}
	// parseDefectCardKey inverts defectCardKey.
	if k, g, j, ok := parseDefectCardKey(defectCardKey("p-1/DEF-101", 3, "fix")); !ok || k != "p-1/DEF-101" || g != 3 || j != "fix" {
		t.Errorf("parseDefectCardKey = %q %d %q %v", k, g, j, ok)
	}
}

func TestDefectTriagePacketIsDataAndComplexSendsANotice(t *testing.T) {
	s, pid := defectFixture(t)
	it := defItem(DefectKindXfail, "DEF-101", defCase1)
	it.Excerpt = defMarker
	report(t, s, pid, it)
	key := pid + "/DEF-101"
	c := DefectContext{Built: true, Repos: []DefectContextRepo{{Repo: defRepo, Head: "h1"}},
		Files: []DefectContextFile{{Repo: defRepo, Path: "a.go", SHA: "bb", Text: "x := ```tick```\n"}}}
	if _, err := s.SetDefectContext(context.Background(), pid, key, c); err != nil {
		t.Fatal(err)
	}
	card := cardByKey(s, defectCardKey(key, 1, "triage"))
	s.mu.RLock()
	parts := s.defectPartsLocked(*s.taskByIDLocked(card.ID))
	s.mu.RUnlock()
	if len(parts) != 1 || parts[0].Kind != KindContext {
		t.Fatalf("parts = %+v, want one context part", parts)
	}
	body := parts[0].Body
	if !strings.Contains(body, "untrusted data") || !strings.Contains(body, defMarker) || !strings.Contains(body, "````") {
		t.Errorf("the packet part is not fenced data: %q", body)
	}
	if got := contractsFor("", card.ID, "run-1", "triager"); !strings.Contains(got, TriageFence) {
		t.Error("the triager's contract does not ask for a gaos-triage block")
	}
	// A complex verdict sends one notice with no defect text.
	var kinds, texts []string
	s.mu.Lock()
	s.taskByIDLocked(card.ID).Status = StatusReview
	s.mu.Unlock()
	card = cardByKey(s, defectCardKey(key, 1, "triage"))
	dt := &defectTriager{st: s, logf: func(string, ...any) {},
		notify: func(_ context.Context, _ TaskRecord, kind, text string) {
			kinds, texts = append(kinds, kind), append(texts, text)
		}}
	dt.afterRun(context.Background(), *card, fence+TriageFence+"\ndifficulty: complex\nclass: auth\nreason: sign in\n"+fence+"\n")
	if len(kinds) != 1 || kinds[0] != "defect:complex" || strings.Contains(texts[0], defMarker) || !strings.HasSuffix(texts[0], "DEF-101 needs a person") {
		t.Errorf("notice kinds %v texts %v", kinds, texts)
	}
	fix := cardByKey(s, defectCardKey(key, 1, "fix"))
	if fix == nil || fix.Status != StatusBlocked || !hasLabel(fix.Labels, LabelDefectComplex) {
		t.Errorf("the complex fix card: %+v", fix)
	}
}

// Source a fix would edit comes before tests, and both before samples and documents (found live:
// a .http sample and a markdown page ranked ahead of the resolver and spent the six-file budget).
func TestContextClassPutsSourceBeforeSamplesAndDocuments(t *testing.T) {
	for p, want := range map[string]int{
		"pkg/graph/pharmacy/resolvers/pharmacy_mutation.resolvers.go":         0,
		"internal/storage/pharmacy/medication_dispense_status.go":             0,
		"internal/storage/pharmacy/medication_dispense_status_test.go":        1,
		"resources/_generated/pharmacy/editmedicationdispense.http":           2,
		"technical-docs/api-usage/graphql/pharmacy/editMedicationDispense.md": 2,
		"pkg/graph/generated/pharmacy_gen.go":                                 2,
		"resources/_oldresources/pharmacy/x.go":                               2,
	} {
		if got := contextClass(p); got != want {
			t.Errorf("contextClass(%q) = %d, want %d", p, got, want)
		}
	}
}

// Two files compete for the byte budget: a big interface file that only mentions the operation, found
// first, and the big resolver that implements it, found later. The implementation is kept (found
// live: the 44 KB resolver of editMedicationDispense lost its place to a 33 KB interface file).
func TestDefectContextPrefersTheFileThatImplementsTheOperation(t *testing.T) {
	iface := "package r\ntype MutationResolver interface {\n\tEditThing(ctx context.Context) error\n}\n" + strings.Repeat("// pad\n", 6000)
	impl := "package r\nfunc (r *mutationResolver) EditThing(ctx context.Context) error {\n\treturn nil\n}\n" + strings.Repeat("// pad\n", 6000)
	f := &opFakeGitHub{byOp: map[string][]string{"editThing": {"pkg/resolver.go", "pkg/mutation.resolvers.go"}},
		files: map[string]string{"pkg/resolver.go": iface, "pkg/mutation.resolvers.go": impl}}
	if len(iface)+len(impl) <= defectMaxFileBytes {
		t.Fatalf("the test files fit together (%d bytes); they must compete", len(iface)+len(impl))
	}
	d := sampleDefect()
	d.Operations = []string{"editThing"}
	c, err := buildDefectContext(context.Background(), newOpFakeCode(t, f), d, "", noSleep)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Files) != 1 || c.Files[0].Path != "pkg/mutation.resolvers.go" {
		var got []string
		for _, file := range c.Files {
			got = append(got, file.Path)
		}
		t.Errorf("files = %v; want the implementing resolver, which only fits instead of the interface", got)
	}
}
