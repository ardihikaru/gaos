package main

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

// MemStore's QA catalog (qacatalog.go, D-072). The domain rules live here once;
// ArcadeStore runs them on its projection and persists the Result
// (store_arcade_qa.go).
//
// Every write returns a Result with NO Floor. persist() rewrites every desk in
// Result.Floor, and while the floor is parked in replay s.floor() is the PAST:
// a result that carried one would save the past over the live desks, which is
// the failure readOnly exists to make impossible (BR-20). A QA write changes
// the master record's Qa document and QaCase vertices, and nothing else.

// qaNow is the clock, replaceable in tests.
var qaNow = func() int64 { return time.Now().Unix() }

var qaRejectedID = regexp.MustCompile(`^TC-[A-Z]+-[0-9]{3}$`)

func (s *MemStore) qaBook(productID string) map[string]QaCase {
	if s.qa == nil {
		s.qa = map[string]map[string]QaCase{}
	}
	b := s.qa[productID]
	if b == nil {
		b = map[string]QaCase{}
		s.qa[productID] = b
	}
	return b
}

// restoreQa loads cases read back from the graph. Boot only.
func (s *MemStore) restoreQa(cases []QaCase) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, c := range cases {
		s.qaBook(c.ProductID)[c.CaseID] = c.clone()
	}
}

// qaCasesOf is a product's cases in a stable order, as copies.
func (s *MemStore) qaCasesOf(productID string) []QaCase {
	b := s.qa[productID]
	out := make([]QaCase, 0, len(b))
	for _, c := range b {
		out = append(out, c.clone())
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CaseID < out[j].CaseID })
	return out
}

func (s *MemStore) QaCases(ctx context.Context, productID string, q QaQuery) (QaPage, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	m := s.masterPtr(productID)
	if m == nil {
		return QaPage{}, ErrNotFound
	}
	limit := q.Limit
	if limit <= 0 || limit > qaMaxItems {
		limit = qaMaxItems
	}
	var all []QaCase
	// A READ under RLock: qaBook creates the book on a miss, which is a map
	// write. Reading s.qa directly (a nil map reads as empty) is what keeps two
	// concurrent reads of a product with no cases from crashing the process.
	for _, c := range s.qa[productID] {
		if c.CatRev <= q.After || (q.Epic != "" && c.Epic != q.Epic) {
			continue
		}
		all = append(all, c.clone())
	}
	sort.Slice(all, func(i, j int) bool {
		if all[i].CatRev != all[j].CatRev {
			return all[i].CatRev < all[j].CatRev
		}
		return all[i].CaseID < all[j].CaseID
	})
	page := QaPage{Items: []QaCase{}, CatHead: m.Qa.CatHead}
	more := len(all) > limit
	if more {
		all = all[:limit]
	}
	for _, c := range all {
		page.Items = append(page.Items, c.out())
	}
	if more && len(all) > 0 {
		n := all[len(all)-1].CatRev
		page.Next = &n
	}
	return page, nil
}

func (s *MemStore) QaSummary(ctx context.Context, productID string) (QaSummary, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	m := s.masterPtr(productID)
	if m == nil {
		return QaSummary{}, ErrNotFound
	}
	sum := rollUp(*m, s.qaCasesOf(productID))
	sum.Name = s.productName(productID)
	return sum, nil
}

// qaResult builds a floor-less Result.
func (s *MemStore) qaResult(m *Master, toast string, cases []QaCase, ev ...Event) Result {
	c := m.Clone()
	return Result{Master: &c, NewEvents: ev, Toast: toast, QaCases: cases}
}

func (s *MemStore) QaImport(ctx context.Context, productID string, in QaImportIn) (QaImportOut, Result, error) {
	if len(in.Modules)+len(in.Epics)+len(in.Cases) > qaMaxItems {
		return QaImportOut{}, Result{}, reject(fmt.Sprintf("A batch holds at most %d items.", qaMaxItems))
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	m := s.masterPtr(productID)
	if m == nil {
		return QaImportOut{}, Result{}, ErrNotFound
	}
	out := QaImportOut{Rejected: []QaReject{}}
	book := s.qaBook(productID)
	var changed []QaCase
	epicsChanged := 0

	for _, mod := range in.Modules {
		if why := validateModule(mod); why != "" {
			out.Rejected = append(out.Rejected, QaReject{CaseID: mod.Key, Reason: why})
			continue
		}
		idx := -1
		for i := range m.Qa.Modules {
			if m.Qa.Modules[i].Key == mod.Key {
				idx = i
			}
		}
		switch {
		case idx < 0:
			m.Qa.Modules = append(m.Qa.Modules, mod)
			epicsChanged++
		default:
			if mod.Story == "" {
				mod.Story = m.Qa.Modules[idx].Story
			}
			if m.Qa.Modules[idx] != mod {
				m.Qa.Modules[idx] = mod
				epicsChanged++
			}
		}
	}

	for _, e := range in.Epics {
		if why := validateEpic(e); why != "" {
			out.Rejected = append(out.Rejected, QaReject{CaseID: e.Code, Reason: why})
			continue
		}
		known := false
		for i := range m.Qa.Epics {
			known = known || m.Qa.Epics[i].Code == e.Code
		}
		if !known && len(m.Qa.Epics) >= qaMaxEpics {
			out.Rejected = append(out.Rejected, QaReject{CaseID: e.Code, Reason: "epic-limit"})
			continue
		}
		// A story id is the seed's resolution of the module key; an import
		// that does not carry one keeps what is there.
		idx := -1
		for i := range m.Qa.Epics {
			if m.Qa.Epics[i].Code == e.Code {
				idx = i
			}
		}
		if idx < 0 {
			m.Qa.Epics = append(m.Qa.Epics, e)
			epicsChanged++
			continue
		}
		if e.Story == "" {
			e.Story = m.Qa.Epics[idx].Story
		}
		if e.Task == "" {
			e.Task = m.Qa.Epics[idx].Task
		}
		if m.Qa.Epics[idx] != e {
			m.Qa.Epics[idx] = e
			epicsChanged++
		}
	}

	for _, c := range in.Cases {
		c.ProductID = productID
		if why := validatePlanning(c); why != "" {
			out.Rejected = append(out.Rejected, QaReject{CaseID: c.CaseID, Reason: why})
			continue
		}
		have, exists := book[c.CaseID]
		if !exists {
			if len(book) >= qaMaxCases {
				out.Rejected = append(out.Rejected, QaReject{CaseID: c.CaseID, Reason: "case-limit"})
				continue
			}
			next := c.clone()
			next.Status, next.Result, next.RunAt, next.RunRef = "", "", 0, ""
			next.Implemented, next.Spec = false, ""
			if in.Seed {
				// The one exception (design §4.4): a case BORN from a seed
				// carries the script facts its source already knew.
				if why := validateSpec(c.Spec); why != "" {
					out.Rejected = append(out.Rejected, QaReject{CaseID: c.CaseID, Reason: why})
					continue
				}
				next.Implemented, next.Spec = c.Implemented, c.Spec
			}
			m.Qa.CatHead++
			next.CatRev = m.Qa.CatHead
			book[next.CaseID] = next
			changed = append(changed, next.clone())
			out.Created++
			continue
		}
		if samePlanning(have, c) {
			out.Unchanged++
			continue
		}
		have.Epic, have.Module, have.Features, have.UserStory = c.Epic, c.Module, append([]string(nil), c.Features...), c.UserStory
		have.Scenario, have.Polarity, have.Level, have.Pri = c.Scenario, c.Polarity, c.Level, c.Pri
		have.Decision, have.Reason = c.Decision, c.Reason
		m.Qa.CatHead++
		have.CatRev = m.Qa.CatHead
		book[have.CaseID] = have
		changed = append(changed, have.clone())
		out.Updated++
	}
	out.CatHead = m.Qa.CatHead

	if out.Created+out.Updated+epicsChanged == 0 {
		return out, Result{}, nil
	}
	e := s.emit(RecordUpdated, productID, qaEventText(s.productName(productID), "QA catalog imported",
		fmt.Sprintf("%d created", out.Created), fmt.Sprintf("%d updated", out.Updated),
		fmt.Sprintf("%d unchanged", out.Unchanged), fmt.Sprintf("%d rejected", len(out.Rejected))), KindInfo, EventPayload{})
	return out, s.qaResult(m, "QA catalog imported.", changed, e), nil
}

func (s *MemStore) QaApplyResults(ctx context.Context, productID string, in QaResultsIn) (QaResultsOut, Result, error) {
	if len(in.Items) > qaMaxItems {
		return QaResultsOut{}, Result{}, reject(fmt.Sprintf("A batch holds at most %d items.", qaMaxItems))
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	m := s.masterPtr(productID)
	if m == nil {
		return QaResultsOut{}, Result{}, ErrNotFound
	}
	out := QaResultsOut{Rejected: []QaReject{}}
	book := s.qaBook(productID)
	now := qaNow()
	var changed []QaCase

	for _, it := range in.Items {
		have, ok := book[it.CaseID]
		if !ok {
			out.Rejected = append(out.Rejected, QaReject{CaseID: it.CaseID, Reason: "unknown-case"})
			continue
		}
		why := ""
		switch {
		case it.Spec != nil && validateSpec(*it.Spec) != "":
			why = "bad-spec"
		case it.RunRef != nil && validateRunRef(*it.RunRef) != "":
			why = "bad-run-ref"
		case it.Result != nil && !qaResults[*it.Result]:
			why = "bad-result"
		case it.RunAt != nil && *it.RunAt < 0:
			why = "bad-run-at"
		// A run is one record: its result and its time travel together, so a
		// newer timestamp can never sit beside an older run's verdict.
		case (it.Result != nil) != (it.RunAt != nil) || (it.RunRef != nil && it.RunAt == nil):
			why = "run-incomplete"
		case it.RunAt != nil && *it.RunAt > now+qaFutureSlack:
			why = "future"
		case it.RunAt != nil && have.RunAt > *it.RunAt && !in.Force:
			why = "stale"
		}
		if why != "" {
			out.Rejected = append(out.Rejected, QaReject{CaseID: it.CaseID, Reason: why})
			continue
		}
		next := have.clone()
		if it.Implemented != nil {
			next.Implemented = *it.Implemented
		}
		if it.Spec != nil {
			next.Spec = *it.Spec
		}
		if it.RunAt != nil {
			next.RunAt, next.Result = *it.RunAt, *it.Result
			if it.RunRef != nil {
				next.RunRef = *it.RunRef
			} else if *it.RunAt != have.RunAt {
				next.RunRef = ""
			}
		}
		if next.Implemented == have.Implemented && next.Spec == have.Spec && next.Result == have.Result &&
			next.RunAt == have.RunAt && next.RunRef == have.RunRef {
			out.Unchanged++
			continue
		}
		book[next.CaseID] = next
		changed = append(changed, next.clone())
		out.Applied++
	}
	out.CatHead = m.Qa.CatHead
	if out.Applied == 0 {
		return out, Result{}, nil
	}
	e := s.emit(RecordUpdated, productID, qaEventText(s.productName(productID), "QA results applied",
		fmt.Sprintf("%d changed", out.Applied), fmt.Sprintf("%d unchanged", out.Unchanged),
		fmt.Sprintf("%d rejected", len(out.Rejected))), KindInfo, EventPayload{})
	res := s.qaResult(m, "QA results applied.", changed, e)
	// A case that became implemented runs after its spec merged (D-103 §4.2).
	for _, c := range changed {
		if c.Implemented {
			mr := s.planMergedRunsForCaseLocked(productID, c.CaseID, autoFixNow())
			res.NewEvents = append(res.NewEvents, mr.NewEvents...)
			res.PullRequests = append(res.PullRequests, mr.PullRequests...)
			if len(mr.Floor.Desks) > 0 {
				res.Floor = mr.Floor
			}
		}
	}
	return out, res, nil
}

func (s *MemStore) QaRecordSync(ctx context.Context, productID, dir string, side QaSyncSide) (Result, error) {
	if dir != "pull" && dir != "push" {
		return Result{}, reject(`A sync direction is "pull" or "push".`)
	}
	if !validCursor(side.Cursor) {
		return Result{}, reject("The sync cursor is short printable text.")
	}
	side.Note = qaTrim(side.Note, qaMaxNote)
	var ids []string
	for _, id := range side.Rejected {
		if qaRejectedID.MatchString(id) && len(ids) < qaMaxRejected {
			ids = append(ids, id)
		}
	}
	side.Rejected = ids
	s.mu.Lock()
	defer s.mu.Unlock()
	m := s.masterPtr(productID)
	if m == nil {
		return Result{}, ErrNotFound
	}
	if dir == "pull" {
		m.Qa.Pull = side
	} else {
		m.Qa.Push = side
	}
	verdict := "ok"
	if !side.OK {
		verdict = "failed"
	}
	e := s.emit(RecordUpdated, productID, qaEventText(s.productName(productID), "QA "+dir,
		verdict, fmt.Sprintf("%d items", side.Items)), KindInfo, EventPayload{})
	return s.qaResult(m, "QA "+dir+" "+verdict+".", nil, e), nil
}

// qaTrim keeps a one-line note.
func qaTrim(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > n {
		s = s[:n]
		// Never leave half a rune: back up to the last complete one.
		for len(s) > 0 && !utf8.ValidString(s) {
			s = s[:len(s)-1]
		}
	}
	return s
}

// decorateBoardQa puts an epic card's live case counts on it. It reads the
// catalog and writes nothing: the badge is a view of the catalog through
// QaEpic.Task, never state on the task (BR-54). Caller holds s.mu.
func (s *MemStore) decorateBoardQa(b *Board, m *Master) {
	if len(m.Qa.Epics) == 0 {
		return
	}
	sum := rollUp(*m, s.qaCasesOf(m.ID))
	byTask := map[string]QaEpicRoll{}
	for _, e := range sum.Epics {
		if e.Task != "" {
			byTask[e.Task] = e
		}
	}
	for i := range b.Cards {
		if e, ok := byTask[b.Cards[i].ID]; ok {
			b.Cards[i].Qa = &BoardQa{Epic: e.Code, Cases: e.Total, Automated: e.Automated, Fail: e.Fail, Pct: e.CoveragePct}
		}
	}
}
