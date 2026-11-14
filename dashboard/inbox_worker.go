package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"time"
)

// The answer webhook (D-078): when the operator answers a QA tool question, or
// it expires, GAOS tells the QA tool.
//
//	POST {GAOS_QA_URL}/api/v1/gaos/inbox-answer
//	Authorization: Bearer <the GAOS_QA_TOKEN_FILE token>
//
// The destination and the credential are the ones the QA sync already uses
// (qaclient.go, validated by validQaURL); nothing in a request chooses them.
//
// Delivery is durable because its state is events. The answer is an
// ExternalQuestionAnswered; each attempt is an ExternalAnswerDelivery. A
// restart therefore finds exactly the work that is left: answered or expired
// items whose latest delivery event is neither "delivered" nor "gave-up". One
// worker, one item at a time, never on the answer's request path: the handler
// only nudges it. After inboxMaxAttempts failures it gives up and the item
// stays readable by GET, which is the QA tool's fallback.

const (
	inboxMaxAttempts = 5
	inboxTick        = 10 * time.Second
	inboxHookPath    = "/api/v1/gaos/inbox-answer"
)

// inboxBackoff is the wait after the Nth failed attempt (N = 1..4).
var inboxBackoff = []time.Duration{30 * time.Second, 2 * time.Minute, 10 * time.Minute, time.Hour}

// inboxAnswerHook is the webhook body.
type inboxAnswerHook struct {
	ExternalID  string `json:"externalId"`
	ProductID   string `json:"productId"`
	Status      string `json:"status"` // answered | expired
	Choice      int    `json:"choice"` // 1..3, 4 for the operator's own words, 0 when expired
	OptionLabel string `json:"optionLabel"`
	Text        string `json:"text"`
	AnsweredAt  int64  `json:"answeredAt"` // unix seconds; the deadline when expired
}

type inboxWorker struct {
	store   Store
	out     *qaOutbound
	backoff []time.Duration
	tick    time.Duration
	kick    chan struct{}
}

func newInboxWorker(st Store, out *qaOutbound) *inboxWorker {
	return &inboxWorker{store: st, out: out, backoff: inboxBackoff, tick: inboxTick, kick: make(chan struct{}, 1)}
}

// Kick asks for a pass soon. It never blocks and is safe on a nil worker.
func (w *inboxWorker) Kick() {
	if w == nil {
		return
	}
	select {
	case w.kick <- struct{}{}:
	default:
	}
}

// Run is the single worker loop.
func (w *inboxWorker) Run(ctx context.Context) {
	t := time.NewTicker(w.tick)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		case <-w.kick:
		}
		w.RunOnce(ctx, time.Now().Unix())
	}
}

// due says whether an item's next attempt is owed at now.
func (w *inboxWorker) due(x ExternalItem, now int64) bool {
	n := x.Delivery.Attempts
	if n == 0 {
		return true
	}
	i := n - 1
	if i >= len(w.backoff) {
		i = len(w.backoff) - 1
	}
	return now >= x.Delivery.LastAt+int64(w.backoff[i]/time.Second)
}

// RunOnce makes one pass over the pending items and reports how many it
// attempted. With the outbound unconfigured it does nothing and records
// nothing, so the work waits for the day it is configured.
func (w *inboxWorker) RunOnce(ctx context.Context, now int64) int {
	if w == nil || w.out == nil || w.out.why != "" {
		return 0
	}
	items, err := w.store.ExternalPending(ctx)
	if err != nil {
		log.Printf("inbox webhook: %v", err)
		return 0
	}
	tried := 0
	for _, x := range items {
		if ctx.Err() != nil {
			break
		}
		if !w.due(x, now) {
			continue
		}
		tried++
		attempt := x.Delivery.Attempts + 1
		outcome, why := DeliveryDelivered, ""
		if err := w.send(ctx, x); err != nil {
			why = err.Error()
			outcome = "retry"
			if attempt >= inboxMaxAttempts {
				outcome = DeliveryGaveUp
			}
		}
		if _, err := w.store.RecordExternalDelivery(ctx, x.ProductID, x.ExternalID, attempt, outcome, why, now); err != nil {
			log.Printf("inbox webhook: recording attempt %d for %s: %v", attempt, x.ExternalID, err)
		}
	}
	return tried
}

// send makes one POST. Any 2xx is delivery.
func (w *inboxWorker) send(ctx context.Context, x ExternalItem) error {
	body := inboxAnswerHook{ExternalID: x.ExternalID, ProductID: x.ProductID, Status: x.Status,
		Choice: x.Choice, OptionLabel: x.OptionLabel(), Text: x.Text, AnsweredAt: x.AnsweredAt}
	code, _, err := w.out.do(ctx, http.MethodPost, inboxHookPath, nil, body)
	if err != nil {
		return err
	}
	if code < 200 || code > 299 {
		return fmt.Errorf("the QA tools answered HTTP %d", code)
	}
	return nil
}
