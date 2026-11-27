package main

import (
	"strconv"
	"strings"
)

// A QA spec pull request that merged runs its case once (D-103 §4.2,
// docs/designs/release-b-e2e-kinds-merge-unit-tests.md). The QA tool merges an
// approved spec by GitHub auto-merge, pinned to the commit it pushed; GAOS never
// merges. GAOS plans the run only when the merged head is that commit, the
// product's qa role runs, and the catalog already shows the case implemented
// (the QA tool runs a case only from its synced spec_paths); otherwise the row's
// QANote says why, and a catalog sync may plan it later.

// qaMergedPrefix starts a merged spec's run key:
// qa-merged/<productId>:<owner>/<repo>#<n>:<caseCode>.
const qaMergedPrefix = "qa-merged/"

// The notes a merged QA row can carry.
const (
	QANoteUnapproved = "merged head is not the approved commit"
	QANoteUnassigned = "qa role unassigned"
	QANoteCatalog    = "waiting for the catalog"
	QANoteNoCase     = "no case recorded"
	QANotePlanned    = "run planned"
)

// qaMergedWindow is how long a merged row waits for the catalog (14 days).
const qaMergedWindow = 14 * 86400

// qaMergedKey is a merged spec's run key.
func qaMergedKey(productID, repo string, n int, caseCode string) string {
	return qaMergedPrefix + productID + ":" + strings.ToLower(repo) + "#" + strconv.Itoa(n) + ":" + caseCode
}

// qaMergedCase is the case of a qa-merged/ key: after its last ':'.
func qaMergedCase(key string) string {
	i := strings.LastIndex(key, ":")
	if !strings.HasPrefix(key, qaMergedPrefix) || i < 0 {
		return ""
	}
	if c := key[i+1:]; caseCodeOK(c) {
		return c
	}
	return ""
}

// planMergedRunLocked plans the run of a merged QA row's case, or records why
// not. headSHA is the merged head the poller read ("" when the caller is a
// catalog sync, which re-checks only the catalog). Caller holds s.mu.
func (s *MemStore) planMergedRunLocked(key, headSHA string) Result {
	row, ok := s.pullRequests[key]
	if !ok || row.Kind != PRKindQA || row.State != PRStateMerged {
		return Result{}
	}
	m := s.masterPtr(row.ProductID)
	idx := s.deskIdxByID(row.ProductID)
	if m == nil || idx < 0 {
		return Result{}
	}
	note := func(n string) Result {
		row.QANote = n
		s.pullRequests[key] = row
		return Result{PullRequests: []PullRequest{row}}
	}
	switch {
	case !caseCodeOK(row.QACase):
		return note(QANoteNoCase)
	case headSHA != "" && (row.QASha == "" || !strings.EqualFold(row.QASha, headSHA)):
		// The merged head is not what a person approved: never run it, and say so.
		res := note(QANoteUnapproved)
		res.Skipped = "unapproved-head"
		res.NewEvents = append(res.NewEvents, s.emit(RecordUpdated, row.ProductID,
			s.productName(row.ProductID)+": spec pull request "+row.Repo+"#"+strconv.Itoa(row.Number)+
				" merged at a head that is not the approved commit; its case was not run", KindInfo, EventPayload{}))
		return res
	}
	if specs, _ := gateUnassigned(*m, []prCardSpec{{Role: "qa"}}); len(specs) == 0 {
		return note(QANoteUnassigned)
	}
	if c, ok := s.qaBook(row.ProductID)[row.QACase]; !ok || !c.Implemented {
		return note(QANoteCatalog)
	}
	key2 := qaMergedKey(row.ProductID, row.Repo, row.Number, row.QACase)
	item := cardItem{Key: key2,
		ID:    prWorkflowID(row.ProductID) + "-e2e-merged-" + sanitizeIDPart(row.Repo+"-"+strconv.Itoa(row.Number)+"-"+row.QACase),
		Title: "E2E run of " + row.QACase + " after its spec merged",
		Spec:  prCardSpec{Job: prJobE2ERun, Role: "qa", Labels: []string{LabelE2ERun}}}
	_, _, events := s.planCardsLocked(m, s.desks[idx], []cardItem{item},
		"1 task(s) planned after the spec of "+row.QACase+" merged")
	res := note(QANotePlanned)
	res.NewEvents = append(res.NewEvents, events...)
	c := m.Clone()
	res.Master, res.Floor = &c, s.floor()
	return res
}

// planMergedRunsForCaseLocked is the catalog's side: a case that became
// implemented runs for every merged row of it that waited, within the window.
func (s *MemStore) planMergedRunsForCaseLocked(productID, caseCode string, now int64) Result {
	var out Result
	for k, r := range s.pullRequests {
		if r.ProductID != productID || r.Kind != PRKindQA || r.QACase != caseCode ||
			r.QANote != QANoteCatalog || now-r.FinalAt > qaMergedWindow {
			continue
		}
		res := s.planMergedRunLocked(k, "")
		out.NewEvents = append(out.NewEvents, res.NewEvents...)
		out.PullRequests = append(out.PullRequests, res.PullRequests...)
		if res.Master != nil {
			out.Master, out.Floor = res.Master, res.Floor
		}
	}
	return out
}

// caseCodeOK is the shape a case code must have to go into a key (D-103 §3.1):
// no key delimiter, not empty.
func caseCodeOK(c string) bool { return caseCodeRe.MatchString(c) }
