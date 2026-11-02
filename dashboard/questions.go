package main

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
)

// M15, "Needs you" (D-064, BR-44).
//
// An agent that needs a decision only a human can make ends its turn with a
// question: three options, the first recommended, and the operator's own words
// as a fourth. The turn is over -- a gateway turn cannot pause (OQ-26) -- so
// the task waits in AWAITING_INPUT, out of the scheduler's reach, until the
// operator answers. The answer moves it to READY, and the next attempt carries
// the answer back on the same session.
//
// There is no question table. The record is the two events, QuestionAsked and
// QuestionAnswered, whose payloads carry the whole question; the list below is
// a projection of the event stream, so it survives a restart exactly as the
// timeline does and cannot disagree with it.

// QuestionPayload is the event's half of a question: what was asked, by which
// attempt, and -- on QuestionAnswered -- what the operator chose.
type QuestionPayload struct {
	ID       string           `json:"id"` // ai-manager's question id; the answer route is keyed by it
	RunID    string           `json:"runId"`
	Attempt  int              `json:"attempt"`
	Question string           `json:"question"`
	Context  string           `json:"context,omitempty"`
	Options  []QuestionOption `json:"options"`
	AskedAt  int64            `json:"askedAt"` // unix seconds; wall clock, for "waiting 12m"

	Choice     int    `json:"choice,omitempty"` // 1..3 an offered option, 4 the operator's own words
	Text       string `json:"text,omitempty"`   // the words; required for 4, a note otherwise
	AnsweredAt int64  `json:"answeredAt,omitempty"`
}

// Question is one row of the inbox.
type Question struct {
	QuestionPayload
	ProductID string `json:"productId"`
	TaskID    string `json:"taskId"`
	TaskTitle string `json:"taskTitle"`
	Role      string `json:"role"`
	Answered  bool   `json:"answered"`
}

// needsNotice is which events count toward the operator's notice count: what
// went wrong, and what is waiting on them.
func needsNotice(e Event) bool {
	return e.Kind == KindWarn || e.Kind == KindFail || e.Type == ApprovalRequested || e.Type == QuestionAsked
}

// questionsLocked folds the stream into one question per (task, id), newest
// state wins. Caller holds s.mu (read or write).
func (s *MemStore) questionsLocked() []Question {
	idx := map[string]int{}
	var out []Question
	for _, e := range s.events {
		q := e.Payload.Question
		if q == nil || (e.Type != QuestionAsked && e.Type != QuestionAnswered) {
			continue
		}
		key := e.Payload.Task + "\x00" + q.ID
		row := Question{QuestionPayload: *q, ProductID: e.DeskID, TaskID: e.Payload.Task,
			Answered: e.Type == QuestionAnswered}
		if i, ok := idx[key]; ok {
			out[i] = row
			continue
		}
		idx[key] = len(out)
		out = append(out, row)
	}
	return out
}

// openQuestionLocked is the question a task is waiting on: the newest
// unanswered one, and only while the task really is AWAITING_INPUT. A task
// canceled while it waited has no open question, whatever the stream says.
func (s *MemStore) openQuestionLocked(taskID string) (Question, bool) {
	t := s.taskPtr(taskID)
	if t == nil || t.Status != StatusAwaitingInput {
		return Question{}, false
	}
	qs := s.questionsLocked()
	for i := len(qs) - 1; i >= 0; i-- {
		if qs[i].TaskID == taskID {
			return qs[i], !qs[i].Answered
		}
	}
	return Question{}, false
}

// answerForLocked is the newest answered question of a task asked by an attempt
// before this one: what the next packet tells the agent, and -- when the asking
// attempt is the one just before -- the question the answer route resumes.
func (s *MemStore) answerForLocked(taskID string, attempt int) (Question, bool) {
	qs := s.questionsLocked()
	for i := len(qs) - 1; i >= 0; i-- {
		if qs[i].TaskID == taskID && qs[i].Attempt < attempt {
			return qs[i], qs[i].Answered
		}
	}
	return Question{}, false
}

func (s *MemStore) taskPtr(id string) *TaskRecord {
	for i := range s.tasks {
		if s.tasks[i].ID == id {
			return &s.tasks[i]
		}
	}
	return nil
}

// OpenQuestions lists every agent waiting on the operator, oldest first, for
// one product or ("") all of them.
func (s *MemStore) OpenQuestions(ctx context.Context, productID string) ([]Question, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := []Question{}
	for _, q := range s.questionsLocked() {
		if productID != "" && q.ProductID != productID {
			continue
		}
		open, ok := s.openQuestionLocked(q.TaskID)
		if !ok || open.ID != q.ID {
			continue
		}
		if t := s.taskPtr(q.TaskID); t != nil {
			q.TaskTitle, q.Role = t.Title, t.Role
		}
		out = append(out, q)
	}
	sort.SliceStable(out, func(a, b int) bool { return out[a].AskedAt < out[b].AskedAt })
	return out, nil
}

// AnswerQuestion records the operator's answer and puts the task back in the
// queue. The answer does not travel now: the next attempt carries it, through
// ai-manager's answer route, so the turn it starts is an ordinary TaskRun with
// a lease, a heartbeat and a record (BR-44).
func (s *MemStore) AnswerQuestion(ctx context.Context, productID, taskID string, choice int, text string) (Result, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t := s.taskPtr(taskID)
	if t == nil || t.DeskID != productID {
		return Result{}, ErrNotFound
	}
	q, ok := s.openQuestionLocked(taskID)
	if !ok {
		return Result{}, reject(fmt.Sprintf("%s is not waiting on an answer (it is %s).", taskID, t.Status))
	}
	if err := validAnswer(choice, text); err != nil {
		return Result{}, err
	}
	a := q.QuestionPayload
	a.Choice, a.Text, a.AnsweredAt = choice, strings.TrimSpace(text), time.Now().Unix()
	ans := s.emit(QuestionAnswered, productID, fmt.Sprintf("%s: answered — %s", taskID, answerSummary(a)),
		KindInfo, EventPayload{Task: taskID, Question: &a})
	ev := []Event{ans}
	if m, ok := s.moveTaskLocked(taskID, StatusReady, MoveAnswer); ok {
		ev = append(ev, m)
	}
	return s.result(fmt.Sprintf("Answer sent — %s resumes.", taskID), ev...), nil
}

// answerSummary is the answer in one line: the option's label, or the words.
func answerSummary(a QuestionPayload) string {
	if a.Choice >= 1 && a.Choice <= len(a.Options) && a.Choice < ChoiceOther {
		return fmt.Sprintf("option %d, %q", a.Choice, a.Options[a.Choice-1].Label)
	}
	return fmt.Sprintf("in their own words, %q", a.Text)
}

// questionBody renders a question, and its answer when there is one, as the
// markdown both the question artifact and the next packet's answer part carry.
func questionBody(q QuestionPayload) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# The question you asked the operator\n\n%s\n", q.Question)
	if q.Context != "" {
		fmt.Fprintf(&b, "\n%s\n", q.Context)
	}
	b.WriteString("\n## The options you offered\n\n")
	for i, o := range q.Options {
		rec := ""
		if i == 0 {
			rec = " (recommended)"
		}
		fmt.Fprintf(&b, "%d. %s%s — %s\n", i+1, o.Label, rec, o.Description)
	}
	b.WriteString("4. Other — the operator's own words\n")
	if q.Choice == 0 {
		return b.String()
	}
	b.WriteString("\n## The operator's answer\n\n")
	if q.Choice < ChoiceOther && q.Choice <= len(q.Options) {
		fmt.Fprintf(&b, "Option %d: %s\n", q.Choice, q.Options[q.Choice-1].Label)
		if q.Text != "" {
			fmt.Fprintf(&b, "\nTheir note: %s\n", q.Text)
		}
	} else {
		fmt.Fprintf(&b, "In their own words: %s\n", q.Text)
	}
	b.WriteString("\nAct on this answer; do not ask the same question again.\n")
	return b.String()
}

// questionFromReply converts the gateway's question into the event payload.
func questionFromReply(g *GatewayQuestion, runID string, attempt int) *QuestionPayload {
	if g == nil {
		return nil
	}
	return &QuestionPayload{ID: g.ID, RunID: runID, Attempt: attempt, Question: g.Question,
		Context: g.Context, Options: append([]QuestionOption(nil), g.Options...), AskedAt: time.Now().Unix()}
}
