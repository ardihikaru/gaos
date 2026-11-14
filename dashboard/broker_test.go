package main

import (
	"bufio"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// D-065: the live channel. Hints out of emit(), fanned out without ever
// blocking the store, and streamed as text/event-stream.

func TestTheBrokerFansOutAndDropsASlowSubscriber(t *testing.T) {
	b := NewBroker()
	fast, stopFast := b.Subscribe()
	defer stopFast()
	_, stopSlow := b.Subscribe() // never read
	defer stopSlow()

	done := make(chan struct{})
	go func() {
		for i := 1; i <= brokerBuffer+5; i++ {
			b.Publish(Event{Seq: i, Type: TaskMoved})
			<-fast
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Publish blocked on a subscriber that does not read; one stalled tab would hold the store's lock")
	}
	if got := b.Subscribers(); got != 1 {
		t.Fatalf("%d subscribers remain; want 1 -- the slow one is dropped, the reading one kept", got)
	}
	stopFast()
	stopFast() // ending twice is harmless
	if got := b.Subscribers(); got != 0 {
		t.Fatalf("%d subscribers remain after both ended; want 0", got)
	}
}

func TestOnlyTheLiveStorePublishes(t *testing.T) {
	s := NewMemStore(1)
	ch, stop := s.Live().Subscribe()
	defer stop()
	s.mu.Lock()
	s.emit(TaskMoved, s.desks[0].ID, "t: moved", KindInfo, EventPayload{Task: "t"})
	s.mu.Unlock()
	select {
	case e := <-ch:
		if e.Type != TaskMoved {
			t.Fatalf("the broker delivered %s; want the TaskMoved just emitted", e.Type)
		}
	case <-time.After(time.Second):
		t.Fatal("an emitted event never reached the live channel")
	}
	if newProjection().Live() != nil {
		t.Fatal("a bare projection has a broker; a replay or a fold would re-announce the past as live")
	}
}

func TestTheStreamSendsAHintForAQuestion(t *testing.T) {
	srv, s, desk := plannedProduct(t)
	f, ad := gatewayOn(t, s)
	ts := httptest.NewServer(srv.routes())
	defer ts.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL+"/events/stream", nil)
	if resp, err := http.DefaultClient.Do(req); err != nil || resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("the stream without the back-office session answered %v %v; want 401", resp, err)
	}
	req, _ = http.NewRequestWithContext(ctx, http.MethodGet, ts.URL+"/events/stream", nil)
	for _, c := range boCookies() {
		req.AddCookie(c)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" || resp.Header.Get("X-Accel-Buffering") != "no" {
		t.Fatalf("the stream answered Content-Type %q, X-Accel-Buffering %q; want text/event-stream and no",
			ct, resp.Header.Get("X-Accel-Buffering"))
	}
	for s.Live().Subscribers() == 0 {
		time.Sleep(time.Millisecond)
	}

	id := "wf-q-t-9"
	plantRunning(s, desk, id)
	askTurn(f, "q-"+id)
	go s.superviseRun(ctx, ad, desk, clearedFor(t, s, desk, id, 1), 1)

	sc := bufio.NewScanner(resp.Body)
	var event string
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "event: ") {
			event = strings.TrimPrefix(line, "event: ")
		}
		if event == "question" && strings.HasPrefix(line, "data: ") {
			if !strings.Contains(line, `"type":"QuestionAsked"`) || !strings.Contains(line, `"task":"`+id+`"`) ||
				strings.Contains(line, "PostgreSQL") {
				t.Fatalf("the question hint reads %s; want the type and the task, and not the question's content", line)
			}
			return
		}
	}
	t.Fatalf("the stream ended without a question hint: %v", sc.Err())
}

// A stream is one long request checked once at open. It must end with the
// session that opened it, or an expired cookie keeps reading (BR-55).
func TestTheEventStreamClosesAtSessionExpiry(t *testing.T) {
	srv, s, _ := plannedProduct(t)
	ts := httptest.NewServer(srv.routes())
	defer ts.Close()

	cookie := &http.Cookie{Name: boCookie, Value: newTestAuthority().sign(testEmail, time.Now().Add(2*time.Second))}
	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/events/stream", nil)
	req.AddCookie(cookie)
	resp, err := http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("the stream with a session two seconds from expiry answered %v %v; want 200", resp, err)
	}
	defer resp.Body.Close()
	_ = s
	done := make(chan struct{})
	go func() {
		bufio.NewReader(resp.Body).WriteTo(io.Discard)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(6 * time.Second):
		t.Fatal("the stream was still open four seconds after its session expired; want it closed at expiry")
	}
}
