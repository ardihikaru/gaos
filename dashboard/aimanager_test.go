package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeAIM is an httptest ai-manager. It scripts the answer to each request id
// — how many 202s before the result, and the result's status and body — and
// counts the POSTs it receives per id, so a test can tell one turn from two.
// No binary is spawned and nothing spends money (M14.2).
type fakeAIM struct {
	mu       sync.Mutex
	posts    map[string]int
	bodies   []ChatRequest
	auth     []string
	pending  int    // 202s before the result
	status   int    // result status; 0 = 200
	reply    string // result body
	submit   int    // POST status; 0 = 202
	down     bool
	provider string

	// The provisioning half: files per workspace, as the worker would keep
	// them, and how many writes reached it (TC-293 asserts zero).
	spaces map[string][]WorkspaceFile
	writes int

	// The answer route (M15): every answer it received, keyed by question id,
	// and the status it answers with (0 = 202).
	answers      map[string][]fakeAnswer
	answerStatus int
}

type fakeAnswer struct {
	RequestID string
	AnswerRequest
}

func newFakeAIM(t *testing.T) (*fakeAIM, *AIMAdapter) {
	t.Helper()
	f := &fakeAIM{posts: map[string]int{}, provider: "claude", spaces: map[string][]WorkspaceFile{},
		answers: map[string][]fakeAnswer{}}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	ad := &AIMAdapter{
		Client: &AIMClient{Base: srv.URL, Creds: AIMCredentials{Login: "gaos", Secret: "chat-secret-MARKER", AdminKey: "admin-key-MARKER"}},
		Model:  "claude-sonnet-5", Provider: "anthropic", Tenant: "t1",
		PollEvery: time.Millisecond, Deadline: 5 * time.Second,
	}
	return f, ad
}

func (f *fakeAIM) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.auth = append(f.auth, r.Header.Get("Authorization"))
	if f.down {
		w.WriteHeader(http.StatusBadGateway)
		w.Write([]byte(`{"error":"claude worker unreachable"}`))
		return
	}
	switch {
	case r.Method == "POST" && r.URL.Path == "/v1/chat":
		id := r.Header.Get("X-Request-ID")
		var body ChatRequest
		b, _ := io.ReadAll(r.Body)
		json.Unmarshal(b, &body)
		f.bodies = append(f.bodies, body)
		f.posts[id]++
		if f.submit != 0 {
			w.WriteHeader(f.submit)
			w.Write([]byte(`{"error":"scripted refusal"}`))
			return
		}
		w.WriteHeader(http.StatusAccepted)
		fmt.Fprintf(w, `{"request_id":%q,"status":"pending"}`, id)
	case r.Method == "POST" && strings.HasPrefix(r.URL.Path, "/v1/questions/") && strings.HasSuffix(r.URL.Path, "/answer"):
		qid := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/v1/questions/"), "/answer")
		var a AnswerRequest
		b, _ := io.ReadAll(r.Body)
		json.Unmarshal(b, &a)
		f.answers[qid] = append(f.answers[qid], fakeAnswer{RequestID: r.Header.Get("X-Request-ID"), AnswerRequest: a})
		if f.answerStatus != 0 {
			w.WriteHeader(f.answerStatus)
			w.Write([]byte(`{"error":"scripted answer refusal"}`))
			return
		}
		w.WriteHeader(http.StatusAccepted)
		fmt.Fprintf(w, `{"request_id":%q,"question_id":%q,"status":"pending"}`, r.Header.Get("X-Request-ID"), qid)
	case strings.HasSuffix(r.URL.Path, "/result"):
		if f.pending > 0 {
			f.pending--
			w.WriteHeader(http.StatusAccepted)
			return
		}
		if f.status != 0 {
			w.WriteHeader(f.status)
		}
		w.Write([]byte(f.reply))
	case strings.HasPrefix(r.URL.Path, "/admin/"):
		f.serveAdmin(w, r)
	case r.URL.Path == "/v1/providers":
		fmt.Fprintf(w, `{"providers":[{"name":%q,"available":true},{"name":"minimax","available":true}]}`, f.provider)
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

// gatewayCleared is a screened packet for task T-1, run R-1.
func gatewayCleared(t *testing.T) Cleared {
	t.Helper()
	p := Packet{TaskID: "T-1", RunID: "R-1", Role: "backend", DeskID: "p-5",
		Parts: []PacketPart{{Path: ".gaos/packet/task.md", Kind: KindTask, Body: "# T-1\nadd the thing"},
			{Path: ".gaos/packet/handoff.md", Kind: KindContract, Body: gatewayContract("T-1", "R-1", "backend")}}}
	c, _, err := Screen(EgressPolicy{}, Target{Provider: "anthropic"}, p)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func handoffBlock(task, run string) string {
	return "```" + HandoffFence + "\n" + handoffExample(task, run, "backend") + "\n```"
}

func chatReply(text string, cost string, extra string) string {
	b, _ := json.Marshal(text)
	return fmt.Sprintf(`{"text":%s,"provider":"claude","model":"claude-sonnet-5","input_tokens":1200,"output_tokens":300,"cost_usd":%s,"session_id":"3f0c0000-0000-0000-0000-000000000001","workspace_id":"gaos-t1-p-5"%s}`, b, cost, extra)
}

func TestAGatewayTurnRecordsTheSessionTokensAndAMeasuredCost(t *testing.T) {
	f, ad := newFakeAIM(t)
	f.reply = chatReply("did it\n\n"+handoffBlock("T-1", "R-1"), "0.42", "")
	att, err := ad.Dispatch(context.Background(), gatewayCleared(t))
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if att.Outcome() != HandoffComplete || att.TokensIn != 1200 || att.TokensOut != 300 {
		t.Fatalf("attempt = %+v, want outcome complete with the reply's tokens", att)
	}
	if att.CostBasis != CostMeasured || att.CostUSD != 0.42 || att.Invocation != InvocationGateway {
		t.Fatalf("cost %v %q invocation %q, want 0.42 measured through the gateway (D-062)", att.CostUSD, att.CostBasis, att.Invocation)
	}
	if b := f.bodies[0]; !b.NewSession || b.WorkspaceID != "gaos-t1-p-5" || !strings.Contains(b.Prompt, "```"+HandoffFence) {
		t.Fatalf("the turn was sent as %+v; want a new session in the product's workspace with the reply contract", b)
	}
}

func TestA410OnResumeIsSessionLostAndOpensNoSessionInItsPlace(t *testing.T) {
	f, ad := newFakeAIM(t)
	f.submit = http.StatusGone
	att, err := ad.turn(context.Background(), gatewayCleared(t), "3f0c0000-0000-0000-0000-000000000001")
	if err != nil || att.Failure != SigSessionLost || att.Outcome() != "failed" {
		t.Fatalf("a 410 gave %+v %v, want a failed attempt with %s (BR-40)", att, err, SigSessionLost)
	}
	for _, b := range f.bodies {
		if b.NewSession {
			t.Fatalf("a lost session was silently replaced by a new one: %+v", b)
		}
	}
}

func TestASessionWarningFailsTheTurnEvenWithAValidHandoff(t *testing.T) {
	f, ad := newFakeAIM(t)
	f.reply = chatReply(handoffBlock("T-1", "R-1"), "0.1", `,"metadata":{"session_warning":"transcript not saved"}`)
	att, _ := ad.Dispatch(context.Background(), gatewayCleared(t))
	if att.Failure != SigTurnUnsaved || att.Outcome() != "failed" {
		t.Fatalf("a turn the gateway did not save gave %+v, want %s", att, SigTurnUnsaved)
	}
}

func TestA409IsBusyNotAFailedAttempt(t *testing.T) {
	f, ad := newFakeAIM(t)
	f.submit = http.StatusConflict
	att, err := ad.turn(context.Background(), gatewayCleared(t), "3f0c0000-0000-0000-0000-000000000001")
	if !errors.Is(err, ErrTurnRunning) || att.Failure != "" || att.ExitClass != "" {
		t.Fatalf("a 409 gave %+v %v, want ErrTurnRunning and no attempt at all", att, err)
	}
}

func TestAResubmittedRequestIDStartsNoSecondTurn(t *testing.T) {
	f, ad := newFakeAIM(t)
	f.pending = 3
	f.reply = chatReply(handoffBlock("T-1", "R-1"), "0.1", "")
	c := gatewayCleared(t)
	if _, err := ad.Dispatch(context.Background(), c); err != nil {
		t.Fatal(err)
	}
	if err := ad.Client.Submit(context.Background(), ad.RequestIDFor("R-1"), ChatRequest{Prompt: "again"}); err != nil {
		t.Fatal(err)
	}
	id := ad.RequestIDFor("R-1")
	if id != "gaos.t1.r-1" || f.posts[id] != 2 {
		t.Fatalf("request id %q posted %d times; the id must be deterministic so a resend is the same request", id, f.posts[id])
	}
	att, err := ad.Resume(context.Background(), c, RunRecord{Detail: map[string]string{"requestId": id}})
	if err != nil || att.Outcome() != HandoffComplete {
		t.Fatalf("polling the request id gave %+v %v, want the same completed attempt", att, err)
	}
}

func TestExtractHandoffTakesTheLastBlockAndKeepsTheProse(t *testing.T) {
	reply := "notes\n```go\nfmt.Println(1)\n```\n" + handoffBlock("T-1", "R-1")
	h, raw, err := ExtractHandoff(reply, "T-1", "R-1")
	if err != nil || h.Run != "R-1" || h.Status != HandoffComplete || !strings.Contains(raw, "task: T-1") {
		t.Fatalf("extract = %+v %q %v", h, raw, err)
	}
	bare := "```" + HandoffFence + "\ntask: T-1\nrun: R-1\nrole: qa\nstatus: blocked\ncriteria:\n  - id: AC-1   met: false   evidence: \"x\"\n```"
	if h, _, err := ExtractHandoff(bare, "T-1", "R-1"); err != nil || h.Status != HandoffBlocked {
		t.Fatalf("a block without --- lines: %+v %v", h, err)
	}
}

func TestARepliedHandoffForAnotherRunIsHandoffMissing(t *testing.T) {
	cases := map[string]string{
		"no block":         "I did the work but forgot the block.",
		"two runs":         handoffBlock("T-1", "R-0") + "\n" + handoffBlock("T-1", "R-1"),
		"the previous run": handoffBlock("T-1", "R-0"),
		"another task":     handoffBlock("T-9", "R-1"),
	}
	for name, reply := range cases {
		if _, _, err := ExtractHandoff(reply, "T-1", "R-1"); err == nil || !strings.HasPrefix(err.Error(), SigHandoffMissing) {
			t.Errorf("%s: err = %v, want %s", name, err, SigHandoffMissing)
		}
	}
}

func TestAGatewayCostThatIsMissingOrZeroIsUnavailable(t *testing.T) {
	zero, half := 0.0, 0.5
	cases := []struct {
		r    ChatReply
		want string
	}{
		{ChatReply{InputTokens: 10}, CostUnavailable},
		{ChatReply{CostUSD: &zero, OutputTokens: 900}, CostUnavailable},
		{ChatReply{CostUSD: &half, OutputTokens: 900}, CostMeasured},
	}
	for i, c := range cases {
		if usd, basis := gatewayCost(c.r); basis != c.want || (basis == CostUnavailable && usd != 0) {
			t.Errorf("case %d: %v %q, want %q and never a priced zero (BR-37)", i, usd, basis, c.want)
		}
	}
}

func TestTheGatewaySmokeRefusesAnUnreachableGatewayOrNoClaude(t *testing.T) {
	f, ad := newFakeAIM(t)
	if err := ad.Smoke(context.Background()); err != nil {
		t.Fatalf("smoke against a healthy gateway: %v", err)
	}
	f.provider = "minimax"
	if err := ad.Smoke(context.Background()); err == nil || !strings.Contains(err.Error(), "no available claude provider") {
		t.Fatalf("smoke with no claude provider: %v", err)
	}
	f.down = true
	if err := ad.Smoke(context.Background()); err == nil {
		t.Fatal("smoke passed against a gateway answering 502")
	}
}

func TestTheGatewayCredentialsNeverReachTheRecord(t *testing.T) {
	f, ad := newFakeAIM(t)
	f.reply = chatReply(handoffBlock("T-1", "R-1"), "0.1", "")
	att, _ := ad.Dispatch(context.Background(), gatewayCleared(t))
	f.down = true
	att2, err := ad.Dispatch(context.Background(), gatewayCleared(t))
	smoke := ad.Smoke(context.Background())
	evidence := fmt.Sprintf("%+v %+v %v %v", att, att2, err, smoke)
	for _, secret := range []string{"chat-secret-MARKER", "admin-key-MARKER"} {
		if strings.Contains(evidence, secret) {
			t.Fatalf("a credential reached the record: %s", evidence)
		}
	}
	for _, a := range f.auth {
		if strings.Contains(a, "admin-key-MARKER") {
			t.Fatal("the admin key was sent on a chat route; it is for provisioning only")
		}
	}
}

// serveAdmin answers the provisioning routes with the worker's manifest shape
// and its digest formula, so a sync against it proves the real arithmetic.
func (f *fakeAIM) serveAdmin(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID    string          `json:"id"`
		Files []WorkspaceFile `json:"files"`
	}
	json.NewDecoder(r.Body).Decode(&body)
	id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/admin/workspaces/"), "/files")
	switch {
	case r.Method == "POST" && r.URL.Path == "/admin/workspaces/provision":
		status := http.StatusOK
		if _, ok := f.spaces[body.ID]; !ok {
			f.spaces[body.ID], status = body.Files, http.StatusCreated
			f.writes++
		}
		w.WriteHeader(status)
		fmt.Fprintf(w, `{"workspace":%s,"chat_listed_now":true,"active":true}`, f.manifest(body.ID))
	case f.spaces[id] == nil:
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"error":"unknown workspace"}`))
	case r.Method == "PUT":
		f.spaces[id] = body.Files
		f.writes++
		w.Write([]byte(f.manifest(id)))
	default:
		w.Write([]byte(f.manifest(id)))
	}
}

func (f *fakeAIM) manifest(id string) string {
	files := make([]BlueprintFile, len(f.spaces[id]))
	for i, wf := range f.spaces[id] {
		sum := sha256.Sum256([]byte(wf.Content))
		files[i] = BlueprintFile{Path: wf.Path, SHA256: hex.EncodeToString(sum[:])}
	}
	return fmt.Sprintf(`{"id":%q,"manifest_digest":%q,"files":[]}`, id, BlueprintDigest(files))
}
