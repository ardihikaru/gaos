package main

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

// The board. M14.6, D-061, I-9, I-10, BR-43.
//
// A product's kanban is a PROJECTION of its task graph, never a second source
// of truth (D-001): columns are groups of task statuses, lanes are stories,
// and a move is a REQUEST the task state machine may refuse. A refused move
// writes nothing and answers 409, so a client board rolls back. Some columns
// are owned by something else and are never drop targets: the scheduler owns
// PENDING to READY (dependencies), the budget guard owns DEFERRED.
//
// The JSON shape is the sisflow REST provider's -- GET …/board answers
// {columns, rows, cards}, PUT …/cards/{id}/move takes {column, row, index} --
// so the server-rendered board ships first and a richer client can replace
// its front end without touching this file (OQ-25).

// BoardColumn is one column. Statuses are the task statuses it shows.
type BoardColumn struct {
	ID       string   `json:"id"`
	Label    string   `json:"label"`
	Statuses []string `json:"-"`
	AddCard  bool     `json:"addCard"` // always false: cards are planned, not created here
}

// BoardRow is one swimlane: a story, or "plan" for the tasks around the stories.
type BoardRow struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

// BoardCard is one task.
type BoardCard struct {
	ID     string `json:"id"`
	Label  string `json:"label"`
	Column string `json:"column"`
	Row    string `json:"row"`
	Status string `json:"status"` // the exact status; a column groups several
	Role   string `json:"role"`
	Rank   int    `json:"rank"`
	// Moves are the columns this card may be moved to now, for a client
	// that renders only legal actions. The server checks again either way.
	Moves []string `json:"moves"`
}

// Board is a product's board.
type Board struct {
	ProductID string        `json:"productId"`
	Columns   []BoardColumn `json:"columns"`
	Rows      []BoardRow    `json:"rows"`
	Cards     []BoardCard   `json:"cards"`
}

// CardMove is the body of PUT …/cards/{id}/move.
type CardMove struct {
	Column string `json:"column"`
	Row    string `json:"row"`
	Index  int    `json:"index"`
}

// Column ids.
const (
	ColBacklog  = "backlog"
	ColReady    = "ready"
	ColWorking  = "working"
	ColNeedsYou = "needs-you"
	ColReview   = "review"
	ColBlocked  = "blocked"
	ColFailed   = "failed"
	ColDone     = "done"
	ColCanceled = "canceled"

	PlanLane = "plan"
)

var boardColumns = []BoardColumn{
	{ID: ColBacklog, Label: "Backlog", Statuses: []string{StatusPending}},
	{ID: ColReady, Label: "Ready", Statuses: []string{StatusReady}},
	{ID: ColWorking, Label: "Working", Statuses: []string{StatusDispatched, StatusRunning}},
	// M15: an agent that asked the operator a question waits here (BR-44).
	{ID: ColNeedsYou, Label: "Needs you", Statuses: []string{StatusAwaitingInput}},
	{ID: ColReview, Label: "Review", Statuses: []string{StatusReview}},
	{ID: ColBlocked, Label: "Blocked", Statuses: []string{StatusBlocked, StatusDeferred}},
	{ID: ColFailed, Label: "Failed", Statuses: []string{StatusFailed}},
	{ID: ColDone, Label: "Done", Statuses: []string{StatusDone}},
	{ID: ColCanceled, Label: "Canceled", Statuses: []string{StatusCanceled}},
}

func columnOf(status string) string {
	for _, c := range boardColumns {
		for _, st := range c.Statuses {
			if st == status {
				return c.ID
			}
		}
	}
	return ColBacklog
}

func laneOf(t TaskRecord) string {
	if t.Story == "" {
		return PlanLane
	}
	return t.Story
}

// Board actions, the Rule of TaskMoved.
const (
	MoveDispatch = "dispatch"
	MoveApprove  = "approve"
	MoveRedo     = "redo"
	MoveRetry    = "retry"
	MoveUnblock  = "unblock"
	MoveCancel   = "cancel"
	// MoveAnswer is the only way out of AWAITING_INPUT but cancel (M15).
	MoveAnswer = "answer"
)

// The other actors that move a task, named in TaskMoved's Rule (T-164). The
// board's actions above are the operator's; these are the machine's. Recovery
// reuses RuleAdopted and RuleRequeued from turns.go.
const (
	MoveSchedule     = "schedule"      // the scheduler: PENDING -> READY -> DISPATCHED
	MoveStart        = "start"         // the runner: DISPATCHED -> RUNNING
	MoveExit         = "exit"          // the runner: RUNNING -> REVIEW | BLOCKED | FAILED | AWAITING_INPUT
	MoveRefused      = "refused"       // a dispatch the egress pass or the drift gate refused
	MoveNotStarted   = "not-started"   // a dispatched turn that never reached the gateway
	MoveDriftRelease = "drift-release" // a sync released what Blueprint drift had blocked
)

// legalMove is the table (D-061). It answers the action a move means, or the
// reason it is refused. The state machine is still asked afterwards: this
// table says what an operator may REQUEST, taskTransitions what may happen.
func legalMove(t TaskRecord, to string) (string, string) {
	from := columnOf(t.Status)
	switch {
	case t.Status == StatusDeferred || (to == ColBlocked && from != ColBlocked):
		return "", "the budget guard and the runner own blocked and deferred work; a card cannot be dropped there"
	case to == ColNeedsYou:
		return "", "only an agent puts a card here, by asking a question; it cannot be dropped there"
	case from == ColNeedsYou && to != ColCanceled:
		return "", "the agent is waiting on your answer; answer its question to move the card on"
	case from == ColDone || from == ColCanceled:
		return "", "a " + strings.ToLower(t.Status) + " task is finished; plan a new task instead"
	case to == ColBacklog || (from == ColBacklog && to == ColReady):
		return "", "the scheduler moves work between backlog and ready, by its dependencies"
	case from == ColReady && to == ColWorking:
		return MoveDispatch, ""
	case from == ColReview && to == ColDone:
		return MoveApprove, ""
	case from == ColReview && to == ColReady:
		return MoveRedo, ""
	case from == ColFailed && to == ColReady:
		return MoveRetry, ""
	case from == ColBlocked && to == ColReady:
		return MoveUnblock, ""
	case to == ColCanceled && from == ColWorking:
		return "", "a live turn cannot be stopped through the gateway (OQ-26); cancel it when it ends"
	case to == ColCanceled:
		return MoveCancel, ""
	}
	return "", fmt.Sprintf("a card cannot move from %s to %s", from, to)
}

func legalMoves(t TaskRecord) []string {
	var out []string
	for _, c := range boardColumns {
		if c.ID == columnOf(t.Status) {
			continue
		}
		if a, _ := legalMove(t, c.ID); a != "" {
			out = append(out, c.ID)
		}
	}
	return out
}

// Board projects a product's tasks. Read-only.
func (s *MemStore) Board(ctx context.Context, productID string) (Board, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	m := s.masterPtr(productID)
	if m == nil {
		return Board{}, ErrNotFound
	}
	return boardOf(productID, m, s.tasks), nil
}

// boardOf is the projection itself, pure: the master's stories are the lanes
// and the product's tasks are the cards. The live board passes the store's
// tasks and the task fold's test passes the folded ones (TC-308), so the two
// are compared through one function rather than two that could drift apart.
func boardOf(productID string, m *Master, tasks []TaskRecord) Board {
	// Cards is never nil: a product with no tasks answers "cards": [], not
	// null, which a client board iterating it would fall over on.
	b := Board{ProductID: productID, Columns: boardColumns, Cards: []BoardCard{}}
	b.Rows = append(b.Rows, BoardRow{ID: PlanLane, Label: "Plan"})
	for _, st := range m.Stories {
		b.Rows = append(b.Rows, BoardRow{ID: st.ID, Label: st.ID + " · " + st.Want})
	}
	for _, t := range tasks {
		if t.DeskID != productID {
			continue
		}
		b.Cards = append(b.Cards, BoardCard{ID: t.ID, Label: t.Title, Column: columnOf(t.Status), Row: laneOf(t),
			Status: t.Status, Role: t.Role, Rank: t.Rank, Moves: legalMoves(t)})
	}
	order := map[string]int{}
	for i, c := range boardColumns {
		order[c.ID] = i
	}
	sort.SliceStable(b.Cards, func(i, j int) bool {
		a, c := b.Cards[i], b.Cards[j]
		if a.Column != c.Column {
			return order[a.Column] < order[c.Column]
		}
		return a.Rank < c.Rank
	})
	return b
}

// MoveCard is a move on the board: a request (BR-43). A refusal writes no
// event and returns ErrRejected with the reason, which the route answers 409.
func (s *MemStore) MoveCard(ctx context.Context, productID, taskID string, mv CardMove) (Result, error) {
	s.mu.Lock()
	i := -1
	for k := range s.tasks {
		if s.tasks[k].ID == taskID && s.tasks[k].DeskID == productID {
			i = k
		}
	}
	if i < 0 {
		s.mu.Unlock()
		return Result{}, ErrNotFound
	}
	t := s.tasks[i]
	if mv.Row != "" && mv.Row != laneOf(t) {
		s.mu.Unlock()
		return Result{}, reject("a card's lane is its story, which is a fact of the plan; it cannot be moved to another lane")
	}
	if mv.Column == "" || mv.Column == columnOf(t.Status) {
		defer s.mu.Unlock()
		return s.reorderLocked(productID, t, mv.Index), nil
	}
	action, why := legalMove(t, mv.Column)
	if action == "" {
		s.mu.Unlock()
		return Result{}, reject(why)
	}
	if action == MoveDispatch {
		s.mu.Unlock()
		return s.dispatchNow(ctx, productID, taskID)
	}
	to := map[string]string{MoveApprove: StatusDone, MoveRedo: StatusReady, MoveRetry: StatusReady,
		MoveUnblock: StatusReady, MoveCancel: StatusCanceled}[action]
	if err := canMoveTask(t, to); err != nil {
		s.mu.Unlock()
		return Result{}, err
	}
	e, _ := s.moveTaskLocked(taskID, to, action)
	r := s.result(fmt.Sprintf("%s: %s.", taskID, action), e)
	s.mu.Unlock()
	return r, nil
}

// reorderLocked places a card at index inside its column and renumbers the
// column. Caller holds s.mu.
func (s *MemStore) reorderLocked(productID string, t TaskRecord, index int) Result {
	col := columnOf(t.Status)
	index = reorderColumn(s.tasks, productID, t.ID, index)
	e := s.emit(TaskReordered, productID, fmt.Sprintf("%s moved to position %d in %s", t.ID, index+1, col), KindInfo,
		EventPayload{Task: t.ID, Version: index, Rule: col})
	return s.result("Card moved.", e)
}

// reorderColumn places the task at index inside its column and renumbers the
// column's ranks from zero. It answers the index it used, clamped. Pure over
// the slice it is given: the live board calls it on the store's tasks and the
// task fold replays TaskReordered through it, so a replayed column is ranked
// by the same rule as the live one (T-164).
func reorderColumn(tasks []TaskRecord, productID, taskID string, index int) int {
	self := -1
	for k := range tasks {
		if tasks[k].ID == taskID && tasks[k].DeskID == productID {
			self = k
		}
	}
	if self < 0 {
		return index
	}
	col := columnOf(tasks[self].Status)
	var idx []int
	for k := range tasks {
		if tasks[k].DeskID == productID && columnOf(tasks[k].Status) == col && k != self {
			idx = append(idx, k)
		}
	}
	sort.SliceStable(idx, func(a, b int) bool { return tasks[idx[a]].Rank < tasks[idx[b]].Rank })
	if index < 0 || index > len(idx) {
		index = len(idx)
	}
	order := append(append(append([]int(nil), idx[:index]...), self), idx[index:]...)
	for rank, k := range order {
		tasks[k].Rank = rank
	}
	return index
}

// dispatchNow is the board's "ready → working": the scheduler's own route for
// one task, with every gate it applies -- the graph lease (BR-38), the drift
// gate (BR-39), the egress pass (BR-32) -- and the runner switch, which is the
// permission to spend.
func (s *MemStore) dispatchNow(ctx context.Context, productID, taskID string) (Result, error) {
	s.mu.Lock()
	ad := s.adapter
	if ad == nil || !runnerEnabled() {
		s.mu.Unlock()
		return Result{}, reject("the runner is off (GAOS_RUNNER); dispatching spends money and needs it on")
	}
	for _, o := range s.tasks {
		if o.DeskID == productID && o.ID != taskID && (o.Status == StatusDispatched || o.Status == StatusRunning) {
			s.mu.Unlock()
			return Result{}, reject(fmt.Sprintf("%s already has a live turn (%s); one at a time per product (BR-38)", productID, o.ID))
		}
	}
	if _, drift := s.blueprintGate(productID); drift != nil {
		s.mu.Unlock()
		return Result{}, reject(drift.Reason)
	}
	attempt := s.nextAttemptLocked(taskID)
	s.mu.Unlock()

	d := Dispatch{TaskID: taskID, DeskID: productID}
	c, v, err := s.clearFor(d, attempt)
	if err != nil {
		s.refuseDispatch(d, v)
		return Result{}, reject(v.Reason)
	}
	s.mu.Lock()
	e, _ := s.moveTaskLocked(taskID, StatusDispatched, MoveDispatch)
	r := s.result(taskID+": dispatched.", e)
	s.mu.Unlock()
	go func() {
		if _, err := s.superviseRun(context.WithoutCancel(ctx), ad, productID, c, attempt); err != nil {
			s.moveTaskByID(taskID, StatusReady, MoveNotStarted)
		}
	}()
	return r, nil
}
