package main

import (
	"context"
	"fmt"
	"regexp"
	"strings"
)

// The QA tools' create-only door (design §4.12, D-072, BR-51, BR-53, BR-54).
//
// The QA tools may CREATE a case, never change one: GAOS owns the planning half
// of every case that exists. A case that names an epic GAOS has not seen makes
// the epic, and the epic makes ONE card -- born BLOCKED, so that nothing the QA
// side sends can ever become dispatchable agent work without an operator
// releasing it (BR-46). The card is added by a single-task routine that touches
// that task and no other.

const (
	// RuleQaEpicAdded names the actor of the one TaskMoved a new card gets.
	RuleQaEpicAdded = "qa-epic-added"
	// ReasonQaEpicAdded is the card's BlockReason: what holds it, in words an
	// operator reading the board can act on.
	ReasonQaEpicAdded = "added by the QA catalog; plan its design and review before unblocking"
)

var qaCaseInfix = regexp.MustCompile(`^TC-([A-Z]+)-[0-9]{3}$`)

// cleanQaText makes one QA-supplied string one plain line. The seed does the
// same (seed.go); these strings reach the board and, through a card title, an
// agent's task list.
func cleanQaText(s string) string { return cleanSeedText(s) }

func (s *MemStore) QaCreate(ctx context.Context, productID string, in QaCreateIn) (QaCreateOut, Result, error) {
	if len(in.Cases) > qaMaxItems {
		return QaCreateOut{}, Result{}, reject(fmt.Sprintf("A batch holds at most %d cases.", qaMaxItems))
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	m := s.masterPtr(productID)
	if m == nil {
		return QaCreateOut{}, Result{}, ErrNotFound
	}
	out := QaCreateOut{Rejected: []QaReject{}}
	book := s.qaBook(productID)
	var changed []QaCase

	// The epics array is read only for a code that does not exist yet.
	entry := map[string]QaEpic{}
	for _, e := range in.Epics {
		if _, dup := entry[e.Code]; !dup {
			entry[e.Code] = e
		}
	}
	known := map[string]bool{}
	for _, e := range m.Qa.Epics {
		known[e.Code] = true
	}
	var newEpics []QaEpic
	storyOfModule := func(key string) string {
		for _, md := range m.Qa.Modules {
			if md.Key == key {
				return md.Story
			}
		}
		return ""
	}

	for _, c := range in.Cases {
		c.ProductID = productID
		c.Scenario = cleanQaText(c.Scenario)
		reject := func(why string) { out.Rejected = append(out.Rejected, QaReject{CaseID: c.CaseID, Reason: why}) }
		switch {
		case c.Decision != DecisionPlanned || c.Reason != "":
			reject("decision-not-allowed")
			continue
		case c.Result != "" || c.RunAt != 0 || c.RunRef != "":
			reject("use-results")
			continue
		}
		if why := validatePlanning(c); why != "" {
			reject(why)
			continue
		}
		if mm := qaCaseInfix.FindStringSubmatch(c.CaseID); mm == nil || mm[1] != c.Epic {
			reject("bad-case-epic")
			continue
		}
		if c.Implemented || c.Spec != "" {
			if validateSpec(c.Spec) != "" || (c.Spec != "" && !c.Implemented) {
				reject("bad-spec")
				continue
			}
		}
		if have, exists := book[c.CaseID]; exists {
			if samePlanning(have, c) {
				out.Unchanged++
			} else {
				reject("exists-differs")
			}
			continue
		}
		if len(book) >= qaMaxCases {
			reject("case-limit")
			continue
		}
		if !known[c.Epic] {
			if len(newEpics) >= qaMaxNewEpicsPerRequest || len(m.Qa.Epics)+len(newEpics) >= qaMaxEpics {
				reject("epic-limit")
				continue
			}
			e, given := entry[c.Epic]
			if !given {
				e = QaEpic{Code: c.Epic, Title: c.Epic}
			}
			e.Code, e.Task, e.Story = c.Epic, "", ""
			e.Title, e.Service, e.Source = cleanQaText(e.Title), cleanQaText(e.Service), cleanQaText(e.Source)
			if why := validateEpic(e); why != "" {
				reject("bad-epic")
				continue
			}
			e.Story = storyOfModule(e.Module)
			newEpics = append(newEpics, e)
			known[c.Epic] = true
		}
		next := c.clone()
		next.Status, next.Result, next.RunAt, next.RunRef = "", "", 0, ""
		m.Qa.CatHead++
		next.CatRev = m.Qa.CatHead
		book[next.CaseID] = next
		changed = append(changed, next.clone())
		out.Created++
	}

	// One card per new epic: refused while the floor is parked (BR-20) and when
	// the product has no workflow with a review to hang it on. The epic is
	// recorded either way.
	var events []Event
	var liveDesks []Desk
	for i := range newEpics {
		e := &newEpics[i]
		why := ""
		switch {
		case s.replayAtSeq != liveMode:
			why = "replay"
		default:
			if id, evs, reason := s.qaAddCardLocked(m, *e); reason != "" {
				why = reason
			} else {
				e.Task = id
				events = append(events, evs...)
				out.CardsAdded++
			}
		}
		if why != "" && out.CardsDeferred == "" {
			out.CardsDeferred = why
		}
		m.Qa.Epics = append(m.Qa.Epics, *e)
		out.EpicsAdded++
	}
	if out.CardsAdded > 0 {
		// The LIVE desk, never s.floor(): while parked that is the folded past.
		if idx := s.deskIdxByID(productID); idx >= 0 {
			liveDesks = []Desk{s.desks[idx].Clone()}
		}
	}
	out.CatHead = m.Qa.CatHead

	if out.Created+out.EpicsAdded == 0 {
		return out, Result{}, nil
	}
	head := s.emit(RecordUpdated, productID, qaEventText(s.productName(productID), "QA cases created",
		fmt.Sprintf("%d created", out.Created), fmt.Sprintf("%d unchanged", out.Unchanged),
		fmt.Sprintf("%d epics", out.EpicsAdded), fmt.Sprintf("%d cards", out.CardsAdded),
		fmt.Sprintf("%d rejected", len(out.Rejected))), KindInfo, EventPayload{})
	// Ascending by seq, which persist's snapshot rung relies on: the card events
	// were emitted first, the summary last.
	res := s.qaResult(m, "QA cases created.", changed, append(events, head)...)
	res.Floor.Desks = liveDesks
	return out, res, nil
}

// qaAddCardLocked adds the card of one new epic, born BLOCKED. It answers the
// task id and the events, or a reason ("no-workflow") when there is nothing to
// hang the card on. It touches the one task it makes; nothing else (BR-54).
func (s *MemStore) qaAddCardLocked(m *Master, e QaEpic) (string, []Event, string) {
	idx := s.deskIdxByID(m.ID)
	if idx < 0 || s.desks[idx].WorkflowID == "" {
		return "", nil, "no-workflow"
	}
	wf := s.desks[idx].WorkflowID
	var review *TaskRecord
	ordinal := 0
	for i := range s.tasks {
		t := &s.tasks[i]
		if t.DeskID != m.ID || t.WorkflowID != wf {
			continue
		}
		ordinal++
		if taskKind(t.Kind) == TaskKindReview && (review == nil || t.Ordinal < review.Ordinal) {
			review = t
		}
	}
	if review == nil {
		return "", nil, "no-workflow"
	}
	id := wf + "-qa-" + strings.ToLower(e.Code)
	if s.taskByIDLocked(id) != nil {
		return id, nil, "" // an epic never gets a second card
	}
	task := TaskRecord{
		ID: id, WorkflowID: wf, DeskID: m.ID, Title: "Automate: " + e.Title, Role: "qa", Story: e.Story,
		Model: roles["qa"].Model, Kind: TaskKindImplement, Status: StatusPending,
		Deps: []string{review.ID}, Priority: review.Priority, Ordinal: ordinal,
	}
	s.tasks = append(s.tasks, task)
	planned := deskPayload(s.desks[idx])
	planned.WorkflowID = wf
	planned.Tasks = []PlannedTask{{ID: id, Title: task.Title, Role: task.Role, Deps: append([]string(nil), task.Deps...),
		Story: task.Story, Model: task.Model, Gate: false, Priority: task.Priority, Kind: TaskKindImplement}}
	evs := []Event{s.emit(TaskPlanned, m.ID, "1 task planned from the QA catalog", KindKnow, planned)}
	s.taskByIDLocked(id).BlockReason = ReasonQaEpicAdded
	if mv, ok := s.moveTaskLocked(id, StatusBlocked, RuleQaEpicAdded); ok {
		evs = append(evs, mv)
	}
	return id, evs, ""
}
