package main

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

// The QA tool's questions in the inbox (D-078, docs/designs/qa-inbox-api.md).
//
// An item is a question that belongs to no task. The QA tool asks through the
// machine API, the operator answers in "Needs you" like any other question, and
// the decision goes back to the QA tool (inbox_worker.go). Like a task's
// question it has no table: the record is three events, and everything below is
// a projection of the stream, so it survives a restart exactly as the timeline
// does.
//
//	ExternalQuestionAsked     the question (Question.ExternalID names it)
//	ExternalQuestionAnswered  the operator's answer, or the expiry (Question.Status)
//	ExternalAnswerDelivery    one attempt to tell the QA tool (Rule: delivered | retry | gave-up)
//
// Identity is (product, externalId): asking again with the same id returns the
// item that is already there and writes nothing.

const (
	ExternalSource = "QA tool"

	externalDefaultHours = 7 * 24
	externalMaxHours     = 30 * 24
	externalMaxOpen      = 200 // open items per product; more is a runaway client
	externalTextMax      = 4000

	// Item states, as GET /v1/products/{id}/inbox/{externalId} reports them.
	ExtOpen     = "open"
	ExtAnswered = "answered"
	ExtExpired  = "expired"

	// Delivery states of an answered or expired item.
	DeliveryPending   = "pending"
	DeliveryDelivered = "delivered"
	DeliveryGaveUp    = "gave-up"
)

// ExternalAsk is one validated, sanitised question from the QA tool. The API
// handler builds it; the store trusts its shape and checks only what it owns.
type ExternalAsk struct {
	ExternalID     string
	Title          string
	Question       string
	Context        string
	Options        []QuestionOption // 2..3
	ExpiresInHours int
}

// ExternalDelivery is what the stream says about telling the QA tool.
type ExternalDelivery struct {
	State     string // pending | delivered | gave-up
	Attempts  int
	LastAt    int64
	LastError string
}

// ExternalItem is the projection of one item.
type ExternalItem struct {
	QuestionPayload
	ItemID    string // ei-<seq of the ask>: stable, because the seq is
	ProductID string
	Status    string // open | answered | expired
	Delivery  ExternalDelivery
}

// OptionLabel is the chosen option's label; "" for the operator's own words
// and for an item nobody answered.
func (x ExternalItem) OptionLabel() string {
	if x.Choice >= 1 && x.Choice < ChoiceOther && x.Choice <= len(x.Options) {
		return x.Options[x.Choice-1].Label
	}
	return ""
}

// defaultExternalOptions are what a question with no options of its own offers.
func defaultExternalOptions() []QuestionOption {
	return []QuestionOption{{Label: "Approve"}, {Label: "Reject"}, {Label: "Needs changes"}}
}

func externalKey(productID, externalID string) string { return productID + "\x00" + externalID }

// externalItemsLocked folds the stream into one item per (product, externalId),
// in the order they were asked. Caller holds s.mu (read or write).
func (s *MemStore) externalItemsLocked() []ExternalItem {
	idx := map[string]int{}
	var out []ExternalItem
	for _, e := range s.events {
		q := e.Payload.Question
		if q == nil {
			continue
		}
		key := externalKey(e.DeskID, q.ExternalID)
		switch e.Type {
		case ExternalQuestionAsked:
			if _, dup := idx[key]; dup {
				continue
			}
			idx[key] = len(out)
			x := ExternalItem{QuestionPayload: *q, ItemID: fmt.Sprintf("ei-%d", e.Seq), ProductID: e.DeskID,
				Status: ExtOpen, Delivery: ExternalDelivery{State: DeliveryPending}}
			x.Options = append([]QuestionOption(nil), q.Options...)
			out = append(out, x)
		case ExternalQuestionAnswered:
			if i, ok := idx[key]; ok && out[i].Status == ExtOpen {
				out[i].Choice, out[i].Text, out[i].AnsweredAt = q.Choice, q.Text, q.AnsweredAt
				out[i].Status = q.Status
			}
		case ExternalAnswerDelivery:
			if i, ok := idx[key]; ok {
				d := &out[i].Delivery
				d.Attempts, d.LastAt, d.LastError = q.Attempt, q.AttemptAt, e.Payload.Reason
				switch e.Payload.Rule {
				case DeliveryDelivered, DeliveryGaveUp:
					d.State = e.Payload.Rule
				default:
					d.State = DeliveryPending
				}
			}
		}
	}
	return out
}

// lapsed says an open item is past its deadline whether or not the sweep has
// written the expiry yet. Reads treat it as expired so the answer never races
// the sweep.
func (x ExternalItem) lapsed(now int64) bool {
	return x.Status == ExtOpen && x.Deadline > 0 && now >= x.Deadline
}

func (s *MemStore) externalLocked(productID, externalID string) (ExternalItem, bool) {
	for _, x := range s.externalItemsLocked() {
		if x.ProductID == productID && x.ExternalID == externalID {
			return x, true
		}
	}
	return ExternalItem{}, false
}

// openExternalLocked is the inbox rows for the QA tool's open items, as the
// Question rows the inbox, the chip and the JSON list already speak.
func (s *MemStore) openExternalLocked(productID string, now int64) []Question {
	var out []Question
	for _, x := range s.externalItemsLocked() {
		if x.Status != ExtOpen || x.lapsed(now) || (productID != "" && x.ProductID != productID) {
			continue
		}
		out = append(out, Question{QuestionPayload: x.QuestionPayload, ProductID: x.ProductID,
			TaskTitle: x.Title, External: true, ExpiresAt: x.Deadline})
	}
	return out
}

// AskExternal records a question from the QA tool. It reports whether this
// call created the item: the same (product, externalId) again returns the
// existing item, whatever state it has reached, and writes nothing.
func (s *MemStore) AskExternal(ctx context.Context, productID string, in ExternalAsk) (ExternalItem, bool, Result, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.masterPtr(productID) == nil {
		return ExternalItem{}, false, Result{}, ErrNotFound
	}
	if x, ok := s.externalLocked(productID, in.ExternalID); ok {
		return x, false, Result{}, nil
	}
	now := time.Now().Unix()
	open := 0
	for _, x := range s.externalItemsLocked() {
		if x.ProductID == productID && x.Status == ExtOpen && !x.lapsed(now) {
			open++
		}
	}
	if open >= externalMaxOpen {
		return ExternalItem{}, false, Result{}, reject(fmt.Sprintf("The inbox already holds %d open QA tool questions for this product; answer some first.", externalMaxOpen))
	}
	hours := in.ExpiresInHours
	if hours <= 0 {
		hours = externalDefaultHours
	}
	lead, _ := splitLead(in.Question)
	opts := append([]QuestionOption(nil), in.Options...)
	if len(opts) == 0 {
		opts = defaultExternalOptions()
	}
	q := &QuestionPayload{ID: in.ExternalID, ExternalID: in.ExternalID, Title: in.Title, Source: ExternalSource,
		Question: in.Question, Context: in.Context, Options: opts,
		AskedAt: now, Deadline: now + int64(hours)*3600}
	e := s.emit(ExternalQuestionAsked, productID, "QA tool asks: "+clipEllipsis(lead, 120), KindWarn, EventPayload{Question: q})
	x, _ := s.externalLocked(productID, in.ExternalID)
	return x, true, Result{NewEvents: []Event{e}, Toast: "The QA tool's question is in Needs you."}, nil
}

// ExternalGet is one item, with an overdue open one reported as expired.
func (s *MemStore) ExternalGet(ctx context.Context, productID, externalID string) (ExternalItem, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	x, ok := s.externalLocked(productID, externalID)
	if !ok {
		return ExternalItem{}, ErrNotFound
	}
	if x.lapsed(time.Now().Unix()) {
		x.Status, x.AnsweredAt = ExtExpired, x.Deadline
	}
	return x, nil
}

// AnswerExternal records the operator's answer. The same rule as a task's
// question (1..3 an offered option, 4 their own words), except that only the
// options the QA tool actually offered count.
func (s *MemStore) AnswerExternal(ctx context.Context, productID, externalID string, choice int, text string) (Result, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	x, ok := s.externalLocked(productID, externalID)
	if !ok {
		return Result{}, ErrNotFound
	}
	now := time.Now().Unix()
	switch {
	case x.Status == ExtAnswered:
		return Result{}, reject("That question was already answered.")
	case x.Status == ExtExpired || x.lapsed(now):
		return Result{}, reject("That question expired before it was answered.")
	}
	if err := validAnswer(choice, text); err != nil {
		return Result{}, err
	}
	if choice < ChoiceOther && choice > len(x.Options) {
		return Result{}, reject(fmt.Sprintf("Choice %d was not offered; pick 1 to %d, or 4 for your own words.", choice, len(x.Options)))
	}
	text = strings.TrimSpace(text)
	if utf8.RuneCountInString(text) > externalTextMax {
		return Result{}, reject(fmt.Sprintf("An answer is at most %d characters.", externalTextMax))
	}
	a := x.QuestionPayload
	a.Choice, a.Text, a.AnsweredAt, a.Status = choice, text, now, ExtAnswered
	e := s.emit(ExternalQuestionAnswered, productID, "QA tool question answered — "+answerSummary(a), KindInfo,
		EventPayload{Question: &a})
	return Result{NewEvents: []Event{e}, Toast: "Answer recorded — the QA tool is told."}, nil
}

// expireExternalLocked closes every open item past its deadline: one
// ExternalQuestionAnswered with Status expired, written once.
func (s *MemStore) expireExternalLocked(now int64) []Event {
	var ev []Event
	for _, x := range s.externalItemsLocked() {
		if x.Status != ExtOpen || x.Deadline == 0 || now < x.Deadline {
			continue
		}
		a := x.QuestionPayload
		a.Status, a.AnsweredAt = ExtExpired, x.Deadline
		ev = append(ev, s.emit(ExternalQuestionAnswered, x.ProductID, "QA tool question expired unanswered", KindInfo,
			EventPayload{Question: &a}))
	}
	return ev
}

// ExternalPending lists the answered or expired items whose decision the QA
// tool has not yet received and the worker has not given up on, oldest first.
// The worker decides which are due.
func (s *MemStore) ExternalPending(ctx context.Context) ([]ExternalItem, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []ExternalItem
	for _, x := range s.externalItemsLocked() {
		if (x.Status == ExtAnswered || x.Status == ExtExpired) && x.Delivery.State == DeliveryPending {
			out = append(out, x)
		}
	}
	return out, nil
}

// RecordExternalDelivery writes one delivery attempt as an event. outcome is
// delivered, retry or gave-up; reason is why an attempt failed.
func (s *MemStore) RecordExternalDelivery(ctx context.Context, productID, externalID string, attempt int, outcome, reason string, at int64) (Result, error) {
	switch outcome {
	case DeliveryDelivered, "retry", DeliveryGaveUp:
	default:
		return Result{}, reject(`A delivery outcome is "delivered", "retry" or "gave-up".`)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	x, ok := s.externalLocked(productID, externalID)
	if !ok {
		return Result{}, ErrNotFound
	}
	if x.Status == ExtOpen {
		return Result{}, reject("An open question has nothing to deliver.")
	}
	reason = qaTrim(reason, 200)
	kind := KindInfo
	if outcome == DeliveryGaveUp {
		kind = KindWarn
	}
	q := &QuestionPayload{ID: externalID, ExternalID: externalID, Attempt: attempt, AttemptAt: at}
	e := s.emit(ExternalAnswerDelivery, productID,
		fmt.Sprintf("QA tool answer: attempt %d %s", attempt, outcome), kind,
		EventPayload{Question: q, Rule: outcome, Reason: reason})
	return Result{NewEvents: []Event{e}}, nil
}
