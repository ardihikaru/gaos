package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// The live channel (M15, D-065). emit() was named "the SSE seam" when the store
// was written; this is the broker that seam was waiting for.
//
// What travels is a HINT, never HTML and never a payload: the event's seq, its
// type, the product and the task. A browser that hears one re-fetches the
// fragment it affects through the ordinary routes, so the stream carries no
// second rendering path and no state a client could act on without asking.
//
// Delivery is best effort by design. A subscriber whose buffer is full is
// dropped rather than waited for -- one stalled browser tab must not hold the
// store's lock -- and reconnects with Last-Event-ID to catch up from the record.

const (
	brokerBuffer   = 64
	sseHeartbeat   = 25 * time.Second // under the edge's 300 s proxy_read_timeout
	sseCatchUpMax  = 200              // events replayed to a reconnecting client, at most
	sseRetryMillis = 3000
)

// Hint is one event as the stream sends it.
type Hint struct {
	Seq     int    `json:"seq"`
	Type    string `json:"type"`
	Kind    string `json:"kind"`
	Product string `json:"product"`
	Task    string `json:"task,omitempty"`
}

func hintOf(e Event) Hint {
	return Hint{Seq: e.Seq, Type: string(e.Type), Kind: e.Kind, Product: e.DeskID, Task: e.Payload.Task}
}

// sseName is the event name a browser listens for. Three, so a page can
// subscribe to what it renders: the inbox to questions, the board to tasks.
func sseName(t EventType) string {
	switch t {
	case QuestionAsked, QuestionAnswered, ExternalQuestionAsked, ExternalQuestionAnswered:
		return "question"
	case TaskMoved, TaskReordered, RunStarted, RunExited, TaskDispatched:
		return "task"
	}
	return "event"
}

// Broker fans events out to subscribers.
type Broker struct {
	mu   sync.Mutex
	subs map[chan Event]struct{}
}

func NewBroker() *Broker { return &Broker{subs: map[chan Event]struct{}{}} }

// Subscribe returns a channel of events and the function that ends the
// subscription. The channel is closed when the subscriber is dropped or ends.
func (b *Broker) Subscribe() (<-chan Event, func()) {
	ch := make(chan Event, brokerBuffer)
	b.mu.Lock()
	b.subs[ch] = struct{}{}
	b.mu.Unlock()
	var once sync.Once
	return ch, func() { once.Do(func() { b.drop(ch) }) }
}

func (b *Broker) drop(ch chan Event) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, ok := b.subs[ch]; ok {
		delete(b.subs, ch)
		close(ch)
	}
}

// Publish never blocks: a full subscriber is dropped and will catch up.
func (b *Broker) Publish(e Event) {
	if b == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	for ch := range b.subs {
		select {
		case ch <- e:
		default:
			delete(b.subs, ch)
			close(ch)
		}
	}
}

// Subscribers is how many are listening, for /healthz and the tests.
func (b *Broker) Subscribers() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.subs)
}

// liveStore is the optional half of the seam a store with a broker offers.
type liveStore interface {
	Live() *Broker
}

// handleStream is GET /events/stream: text/event-stream of hints.
func (s *server) handleStream(w http.ResponseWriter, r *http.Request) {
	if !s.requireBO(w, r) {
		return
	}
	ls, ok := s.store.(liveStore)
	fl, canFlush := w.(http.Flusher)
	if !ok || ls.Live() == nil || !canFlush {
		jsonStatus(w, http.StatusNotImplemented, map[string]string{"error": "this store has no live channel"})
		return
	}
	// Subscribe BEFORE reading the catch-up, so nothing written in between is
	// lost; a seq both paths deliver is skipped by the high-water mark below.
	ch, stop := ls.Live().Subscribe()
	defer stop()

	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("Connection", "keep-alive")
	h.Set("X-Accel-Buffering", "no") // the edge nginx must not hold the stream
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "retry: %d\n\n", sseRetryMillis)

	sent := 0
	if last, err := strconv.Atoi(r.Header.Get("Last-Event-ID")); err == nil {
		evs, _ := s.store.Events(r.Context())
		var miss []Event
		for _, e := range evs {
			if e.Seq > last {
				miss = append(miss, e)
			}
		}
		if len(miss) > sseCatchUpMax {
			miss = miss[len(miss)-sseCatchUpMax:]
		}
		for _, e := range miss {
			writeHint(w, e)
			sent = e.Seq
		}
	}
	fl.Flush()

	beat := time.NewTicker(sseHeartbeat)
	defer beat.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case e, open := <-ch:
			if !open {
				return // dropped as slow; the browser reconnects with Last-Event-ID
			}
			if e.Seq <= sent {
				continue
			}
			writeHint(w, e)
			sent = e.Seq
			fl.Flush()
		case <-beat.C:
			fmt.Fprint(w, ": beat\n\n")
			fl.Flush()
		}
	}
}

func writeHint(w http.ResponseWriter, e Event) {
	b, _ := json.Marshal(hintOf(e))
	fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", e.Seq, sseName(e.Type), b)
}
