package main

import (
	"context"
	"errors"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Missing automation created on a staging deploy (D-103 §3,
// docs/designs/release-b-e2e-kinds-merge-unit-tests.md).
//
// On a product that turned StagingCreates on, a staging deploy of a repository
// with a kind plans, beside the run, one create card per uncovered case of that
// kind's create levels: product-wide (a QA case has no repository), capped, and
// never twice for a case. The QA tool writes the spec; a person approves it; it
// merges; mergedrun.go then runs it.

// stagingCreatePrefix starts a create card's key: staging-create/<productId>:<caseCode>#<n>.
const stagingCreatePrefix = "staging-create/"

// caseCodeRe is a case code that may go into a key: no key delimiter.
var caseCodeRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

// The blocking windows (§3.1 step 3), in seconds.
const (
	createOpenRowWindow   = 30 * 86400
	createMergedRowWindow = 14 * 86400
	createRetryWindow     = 7 * 86400
)

// stagingCreateKey is a create card's key.
func stagingCreateKey(productID, caseCode string, n int) string {
	return stagingCreatePrefix + productID + ":" + caseCode + "#" + strconv.Itoa(n)
}

// stagingCreateCase is the case of a staging-create/ key: after the first ':'
// following the prefix, before the last '#'.
func stagingCreateCase(key string) string {
	rest, ok := strings.CutPrefix(key, stagingCreatePrefix)
	if !ok {
		return ""
	}
	i := strings.Index(rest, ":")
	j := strings.LastIndex(rest, "#")
	if i < 0 || j < i {
		return ""
	}
	if c := rest[i+1 : j]; caseCodeOK(c) {
		return c
	}
	return ""
}

// envCapInt reads a positive integer setting, else def, at most max.
func envCapInt(name string, def, max int) int {
	n, err := strconv.Atoi(strings.TrimSpace(os.Getenv(name)))
	if err != nil || n <= 0 {
		return def
	}
	return min(n, max)
}

// createLevels are the case levels a create of a kind is for (§2.1, §3.1 step 2).
func createLevels(m Master, kind string) map[string]bool {
	switch kind {
	case RepoKindBE:
		return map[string]bool{"API": true}
	case RepoKindFullstack:
		lv := map[string]bool{"WEB": true, "E2E": true}
		hasBE := false
		for _, k := range m.RepoKinds {
			hasBE = hasBE || k == RepoKindBE
		}
		if !hasBE {
			lv["API"] = true
		}
		return lv
	}
	return nil
}

// kindOfLevel is the kind a create of a case level asks for (§3.1 step 4).
func kindOfLevel(level string) string {
	if level == "API" {
		return RepoKindBE
	}
	return RepoKindFullstack
}

// PlanStagingCards plans a staging deploy's run card and, when the product asks
// for it, the create cards (§3.1). A capped run is not an error: res.Capped is
// set and the creates still persist.
func (s *MemStore) PlanStagingCards(ctx context.Context, productID string, ev PREvent) (planned, existing []string, creates int, res Result, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	planned, existing, res, err = s.planPullRequestCardsLocked(productID, ev)
	if errors.Is(err, ErrPRSkipped) && strings.HasSuffix(err.Error(), ": cap") {
		res, err = Result{Capped: true}, nil
	}
	if err != nil {
		return planned, existing, 0, res, err
	}
	m := s.masterPtr(productID)
	newRun := len(planned) > 0 || (res.Capped && len(existing) == 0)
	kind := repoKindFor(*m, ev.Org, ev.Repo)
	if !m.StagingCreates || kind == "" || !newRun {
		return planned, existing, 0, res, nil
	}
	if specs, _ := gateUnassigned(*m, []prCardSpec{{Role: "qa"}}); len(specs) == 0 {
		return planned, existing, 0, res, nil
	}
	idx := s.deskIdxByID(productID)
	room := min(envCapInt("GAOS_E2E_CREATE_MAX", 2, 10), envCapInt("GAOS_E2E_CREATE_OPEN", 4, 50)-s.openCreatesLocked(productID))
	if room <= 0 {
		return planned, existing, 0, res, nil
	}
	levels := createLevels(*m, kind)
	var cases []QaCase
	for _, c := range s.qaBook(productID) {
		if c.Decision == "planned" && !c.Implemented && levels[c.Level] {
			if !caseCodeOK(c.CaseID) {
				continue // a case whose code could break a key is never planned (§3.1 step 2)
			}
			cases = append(cases, c)
		}
	}
	sort.Slice(cases, func(i, j int) bool {
		if cases[i].CatRev != cases[j].CatRev {
			return cases[i].CatRev < cases[j].CatRev
		}
		return cases[i].CaseID < cases[j].CaseID
	})
	now := autoFixNow()
	var items []cardItem
	for _, c := range cases {
		if len(items) >= room {
			break
		}
		if s.createBlockedLocked(productID, c, now) {
			continue
		}
		n := s.createAttemptsLocked(productID, c.CaseID) + 1
		k := kindOfLevel(c.Level)
		items = append(items, cardItem{
			Key:   stagingCreateKey(productID, c.CaseID, n),
			ID:    prWorkflowID(productID) + "-e2e-create-" + sanitizeIDPart(c.CaseID) + "-" + strconv.Itoa(n),
			Title: "E2E automation of " + c.CaseID + " (staging deploy of " + ev.Org + "/" + ev.Repo + ")",
			Spec:  prCardSpec{Job: prJobE2EBuild, Role: "qa", Labels: []string{LabelE2EAutomation, labelFamilyE2EKind + ":" + k}},
		})
	}
	if len(items) == 0 {
		return planned, existing, 0, res, nil
	}
	made, _, events := s.planCardsLocked(m, s.desks[idx], items,
		strconv.Itoa(len(items))+" automation(s) to create after the staging deploy of "+ev.Org+"/"+ev.Repo)
	res.NewEvents = append(res.NewEvents, events...)
	c := m.Clone()
	res.Master, res.Floor = &c, s.floor()
	return planned, existing, len(made), res, nil
}

// openCreatesLocked counts the product's open staging-create cards.
func (s *MemStore) openCreatesLocked(productID string) int {
	n := 0
	for i := range s.tasks {
		t := &s.tasks[i]
		if t.DeskID == productID && strings.HasPrefix(t.DedupKey, stagingCreatePrefix) && !taskEnded(t.Status) {
			n++
		}
	}
	return n
}

// createAttemptsLocked counts every staging-create card a case has had.
func (s *MemStore) createAttemptsLocked(productID, caseCode string) int {
	n := 0
	for i := range s.tasks {
		t := &s.tasks[i]
		if t.DeskID == productID && stagingCreateCase(t.DedupKey) == caseCode {
			n++
		}
	}
	return n
}

// createBlockedLocked is §3.1 step 3: an open card is creating the case; a spec
// pull request of it is open (30 d) or merged but not yet in the catalog (14 d);
// or it was closed, or a create failed, a week ago or less.
func (s *MemStore) createBlockedLocked(productID string, c QaCase, now int64) bool {
	var ended []*TaskRecord
	for i := range s.tasks {
		t := &s.tasks[i]
		if t.DeskID != productID {
			continue
		}
		cased := stagingCreateCase(t.DedupKey)
		if cased == "" && hasLabel(t.Labels, LabelE2EAutomation) {
			cased = caseFromReason(t.BlockReason)
		}
		if cased != c.CaseID {
			continue
		}
		if !taskEnded(t.Status) {
			return true
		}
		if strings.HasPrefix(t.DedupKey, stagingCreatePrefix) && (t.Status == StatusFailed || t.Status == StatusCanceled) {
			ended = append(ended, t)
		}
	}
	if len(ended) > 0 {
		for _, lc := range s.lastChangesLocked(ended) {
			if now-lc.At < createRetryWindow {
				return true
			}
		}
	}
	for _, r := range s.pullRequests {
		if r.ProductID != productID || r.Kind != PRKindQA || r.QACase != c.CaseID {
			continue
		}
		switch r.State {
		case PRStateOpen, PRStateDraft:
			if now-r.OpenedAt < createOpenRowWindow {
				return true
			}
		case PRStateMerged:
			if !c.Implemented && now-r.FinalAt < createMergedRowWindow {
				return true
			}
		case PRStateClosed:
			if now-r.FinalAt < createRetryWindow {
				return true
			}
		}
	}
	return false
}

// taskEnded reports whether a status is final.
func taskEnded(status string) bool {
	return status == StatusDone || status == StatusFailed || status == StatusCanceled
}
