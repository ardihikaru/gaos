package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// The unit-results route (D-106, docs/designs/release-b4-unit-tests.md §5.4).
//
// With UnitChecks.Source "jenkins", the organisation's Jenkins runs the unit
// tests of each merge build on its own agents and posts the result:
//
//	POST /api/v1/products/{id}/unit-results
//	Authorization: Basic base64(user:pass)   -- the product's unit-results credential
//	{"repo":"owner/repo","commit":"<40 hex>","branch":"develop","result":"failed",
//	 "total":2,"failing":[{"test":"TestX","file":"pkg/x_test.go"}],"summary":"","runUrl":"https://…"}
//
// The credential is a line of GAOS_UNIT_RESULTS_BASIC_AUTH_FILE, checked by the
// staging callback's gate (jenkinsGate) but never shared with it. The result
// finishes the pending watch of that merge commit through the poller's own
// store call, so the outcomes, the held defect, the pass-close and the notices
// are D-105's. GAOS still executes nothing.

// The body's limits (§5.4).
const (
	unitResultsMaxBody    = 64 << 10
	unitResultsMaxFailing = 50
	unitResultsMaxEntry   = 512 // bytes of a test name or a file; longer is dropped as withheld
	unitResultsMaxURL     = 512
	unitResultsLateFor    = 24 * time.Hour // a result over a no-ci watch is still accepted this long
	unitResultsRetryAfter = "60"
)

// The notices the route sends itself (§5.3); every other unit notice comes from
// the poller's re-send scan.
const (
	unitWithheldNotice = "unit-test annotations on a merge were all withheld; nothing filed"
	unitConflictNotice = "conflicting unit result on a merge; the first result stands, see the log"
)

type unitResultBody struct {
	Repo    string              `json:"repo"`    // owner/repo
	Commit  string              `json:"commit"`  // the merge commit, 40 hex (any case; lower-cased)
	Branch  string              `json:"branch"`  // the branch Jenkins built
	Result  string              `json:"result"`  // passed | failed | build-failed
	Total   int                 `json:"total"`   // failing leaf tests in the run; 0..100000
	Failing []unitResultFailure `json:"failing"` // at most 50
	Summary string              `json:"summary"` // build-failed only; capped at 2 KiB
	RunURL  string              `json:"runUrl"`  // optional; https, else stored empty
}

type unitResultFailure struct {
	Test string `json:"test"`
	File string `json:"file"` // may be ""
}

// unitResultCheck is §5.4's validation, in order: "" accepts, else the reason
// of the 422. Only the fields that carry the result are refused.
func unitResultCheck(b *unitResultBody) string {
	b.Commit = strings.ToLower(strings.TrimSpace(b.Commit))
	b.Repo = strings.TrimSpace(b.Repo)
	switch {
	case !ghOwnerRepoOK(b.Repo):
		return "repo must be owner/repo"
	case !unitSHARe.MatchString(b.Commit):
		return "commit must be the 40-hex merge commit"
	case !baseBranchRe.MatchString(b.Branch) || strings.Contains(b.Branch, ".."):
		return "branch must be a branch name"
	case b.Result != UnitPassed && b.Result != UnitFailed && b.Result != unitResultBuildFailed:
		return "result must be passed, failed or build-failed"
	case b.Total < 0 || b.Total > unitTotalMax:
		return "total must be 0 to 100000"
	case len(b.Failing) > unitResultsMaxFailing:
		return "failing names at most 50 tests"
	case len(b.Failing) > 0 && b.Result != UnitFailed:
		return "failing is empty unless the result is failed"
	case len(b.Failing) == 0 && b.Result == UnitFailed:
		return "a failed result names at least one test; use build-failed"
	case b.Summary != "" && b.Result != unitResultBuildFailed:
		return "summary is empty unless the result is build-failed"
	}
	return ""
}

func ghOwnerRepoOK(s string) bool {
	_, _, ok := ghOwnerRepo(s)
	return ok
}

// unitResultIn turns a valid body into the poller's input, through the
// annotations' screening (§5.2): each failing entry is kept only when its name
// is a Go test, its file is empty or a safe path, both are one printable line,
// unchanged by stripBR53 and not hit by screenBody; a dropped entry is counted
// as withheld. The first 10 kept become Failing; Total is clamped to
// [kept, 100000], kept counted before that cut. pass reports a passed result;
// withheld a failed result whose every entry was dropped. runURL is the build
// link, or "" when it is not one GAOS may show.
func unitResultIn(b unitResultBody) (in UnitDefectIn, pass bool, withheld bool, runURL string) {
	runURL = unitRunURL(b.RunURL)
	switch b.Result {
	case UnitPassed:
		return in, true, false, runURL
	case unitResultBuildFailed:
		in.Build = true
		text := strings.Join(strings.Fields(strings.ReplaceAll(b.Summary, "\n", " ")), " ")
		text = stripBR53(text)
		switch {
		case screenBody(b.Summary) || screenBody(text):
			text = "(withheld: the summary matched a secret detector)"
		case text == "":
			text = "(Jenkins sent no summary)"
		}
		in.Summary = capBytes(text, unitSummaryMax)
		return in, false, false, runURL
	}
	var kept []unitFailure
	for _, f := range b.Failing {
		test, file := f.Test, f.File
		ok := len(test) <= unitResultsMaxEntry && len(file) <= unitResultsMaxEntry &&
			unitTestRe.MatchString(test) && (file == "" || contextPathOK(file)) &&
			unitPrintable(test) && unitPrintable(file) && stripBR53(test) == test && stripBR53(file) == file &&
			!screenBody(test) && !screenBody(file)
		if !ok {
			continue
		}
		k := unitFailure{Test: test, File: file}
		dup := false
		for _, o := range kept {
			dup = dup || o == k
		}
		if !dup {
			kept = append(kept, k)
		}
	}
	if len(kept) == 0 {
		return in, false, true, runURL
	}
	in.Failing = kept
	if len(in.Failing) > unitMaxFailing {
		in.Failing = append([]unitFailure(nil), kept[:unitMaxFailing]...)
	}
	in.Total = min(max(b.Total, len(kept)), unitTotalMax)
	return in, false, false, runURL
}

// unitRunURL keeps a build link only when it is one: at most 512 bytes, https,
// a host, no userinfo, unchanged by stripBR53 and not hit by screenBody. It is
// display only, so anything else is stored empty, never refused (§5.4).
func unitRunURL(s string) string {
	if s == "" || len(s) > unitResultsMaxURL || stripBR53(s) != s || screenBody(s) {
		return ""
	}
	u, err := url.Parse(s)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil {
		return ""
	}
	return s
}

// unitPostedClass is the class a stored outcome compares in (§5.4 state 6b):
// "passed", "failed" (failed, updated, dedup, refused:*) or "" for anything else.
func unitPostedClass(o string) string {
	switch {
	case o == UnitPassed:
		return UnitPassed
	case o == UnitFailed || o == UnitUpdated || o == UnitDedup || strings.HasPrefix(o, "refused:"):
		return UnitFailed
	}
	return ""
}

// unitSameResult reports whether a posted result repeats the stored one.
func unitSameResult(w UnitWatch, result string) bool {
	if w.PostedResult != "" {
		return w.PostedResult == result
	}
	want := result
	if want == unitResultBuildFailed {
		want = UnitFailed
	}
	return unitPostedClass(w.Outcome) == want
}

// apiUnitResults is POST /v1/products/{id}/unit-results (D-106 §5.4).
func (s *server) apiUnitResults(w http.ResponseWriter, r *http.Request) {
	id, ok := s.jenkinsGate(w, r, s.unitResults, "jenkins-off")
	if !ok {
		return
	}
	src := jenkinsSource(r)
	ref := "-"
	say := func(status int, what string) {
		if s.unitResults.logf != nil {
			s.unitResults.logf("unit-results: %s %s from %s %d %s", id, ref, src, status, what)
		}
	}
	reject := func(reason string) {
		say(http.StatusUnprocessableEntity, "rejected")
		apiErr(w, http.StatusUnprocessableEntity, "rejected", reason)
	}

	var b unitResultBody
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, unitResultsMaxBody))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&b); err != nil {
		reject("the body must be {repo, commit, branch, result, total, failing, summary, runUrl}")
		return
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		reject("the body must be {repo, commit, branch, result, total, failing, summary, runUrl}")
		return
	}
	if why := unitResultCheck(&b); why != "" {
		reject(why)
		return
	}
	repo := strings.ToLower(b.Repo)
	ref = repo + "@" + idSHA(b.Commit)
	in, pass, withheld, runURL := unitResultIn(b)

	ctx := r.Context()
	m, err := s.store.Master(ctx, id)
	if err != nil {
		say(http.StatusNotFound, "not-found")
		apiErr(w, http.StatusNotFound, "not-found", "no such product")
		return
	}
	switch {
	case !m.UnitChecks.Enabled:
		say(http.StatusConflict, "unit-checks-off")
		apiErr(w, http.StatusConflict, "unit-checks-off", "unit checks are off for this product")
		return
	case m.UnitChecks.SourceName() != unitSourceJenkins:
		say(http.StatusConflict, "not-jenkins")
		apiErr(w, http.StatusConflict, "not-jenkins", "this product's unit results come from GitHub Actions")
		return
	}
	key := unitWatchKey(id, repo, b.Commit)
	posted := UnitPosted{RunURL: runURL, From: src, Result: b.Result}
	for attempt := 0; attempt < 2; attempt++ {
		wt, found, err := s.store.UnitWatch(ctx, key)
		if err != nil {
			apiStoreErr(w, err)
			return
		}
		if !found {
			if !unitFixTarget(m, repo) {
				say(http.StatusUnprocessableEntity, "not-target")
				apiErr(w, http.StatusUnprocessableEntity, "not-target", "the repository is not a fix target of this product")
				return
			}
			say(http.StatusConflict, "no-watch")
			w.Header().Set("Retry-After", unitResultsRetryAfter)
			apiErr(w, http.StatusConflict, "no-watch", "GAOS has seen no such merge yet")
			return
		}
		posted.Late = false
		if wt.State == UnitWatchDone {
			late := wt.Outcome == UnitNoCI && time.Now().Unix()-wt.DoneAt <= int64(unitResultsLateFor/time.Second)
			if !late {
				s.unitResultDone(ctx, w, wt, b.Result, say)
				return
			}
			posted.Late = true
		}
		if b.Branch != wt.Base {
			say(http.StatusConflict, "branch-mismatch")
			apiErr(w, http.StatusConflict, "branch-mismatch", "the watched merge is on another branch")
			return
		}
		var res Result
		switch {
		case !unitFixTarget(m, wt.Repo):
			res, err = s.store.FinishUnitWatchFrom(ctx, key, UnitInconclusive, nil, nil, posted)
		case pass:
			res, err = s.store.FinishUnitWatchFrom(ctx, key, UnitPassed, nil, &UnitPassIn{RunID: 0}, posted)
		case withheld:
			res, err = s.store.FinishUnitWatchFrom(ctx, key, "refused:withheld", nil, nil, posted)
		default:
			res, err = s.store.FinishUnitWatchFrom(ctx, key, UnitFailed, &in, nil, posted)
		}
		if err != nil {
			say(http.StatusUnprocessableEntity, "store")
			apiStoreErr(w, err)
			return
		}
		if len(res.UnitWatches) == 0 {
			// The poller ended the watch between the read and the finish (§5.5):
			// read it again and answer for the stored state.
			continue
		}
		out := res.UnitWatches[0].Outcome
		if withheld && out == "refused:withheld" {
			s.unitSay(ctx, id, wt.PR, unitWithheldNotice)
		}
		say(http.StatusAccepted, out)
		apiJSON(w, http.StatusAccepted, map[string]any{"accepted": true, "outcome": out})
		return
	}
	say(http.StatusConflict, "busy")
	apiErr(w, http.StatusConflict, "already-finished", "the watch changed while the result was posted; post it again")
}

// unitResultDone answers a post for a watch that already ended (§5.4 state 6b,
// 6c): the same result is 200 duplicate; a different one is 409, logged, and
// the group is told once per watch.
func (s *server) unitResultDone(ctx context.Context, w http.ResponseWriter, wt UnitWatch, result string, say func(int, string)) {
	if unitSameResult(wt, result) {
		say(http.StatusOK, "duplicate "+wt.Outcome)
		apiJSON(w, http.StatusOK, map[string]any{"accepted": true, "duplicate": true, "outcome": wt.Outcome})
		return
	}
	say(http.StatusConflict, fmt.Sprintf("already-finished conflict stored=%s posted=%s", wt.Outcome, result))
	if res, err := s.store.MarkUnitWatchConflict(ctx, wt.Key); err != nil {
		log.Printf("unit-results: the conflict could not be recorded: %v", err)
	} else if len(res.UnitWatches) > 0 {
		s.unitSay(ctx, wt.ProductID, wt.PR, unitConflictNotice)
	}
	apiJSON(w, http.StatusConflict, map[string]any{"error": "already-finished", "outcome": wt.Outcome})
}

// unitSay sends one group notice of kind unit:failure through the poller's
// notifier; with none it is only logged.
func (s *server) unitSay(ctx context.Context, productID string, pr int, text string) {
	if s.unitNotify == nil {
		log.Printf("unit-results: notice not sent (no notifier): %s", text)
		return
	}
	s.unitNotify(ctx, productID, pr, text, "")
}

// parseJenkinsOrigin reads GAOS_JENKINS_ORIGIN: an https origin with a host, an
// optional port, and no path, query, fragment or userinfo. It answers the
// origin as scheme://host, lower-cased.
func parseJenkinsOrigin(raw string) (string, bool) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.Opaque != "" ||
		(u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" || strings.Contains(raw, "?") || strings.Contains(raw, "#") {
		return "", false
	}
	return strings.ToLower(u.Scheme + "://" + u.Host), true
}
