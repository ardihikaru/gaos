package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"
)

// AIMAdapter is the gateway adapter: one turn through ai-manager per attempt
// (D-055). It satisfies the same seam as CLIAdapter, so nothing above it can
// tell which ran (BR-33); M14.5 reshapes the seam to Submit/Poll once the
// runner stops supervising a process.
//
// What it records differently, and only in the evidence: invocation is
// "gateway" (D-062), the cost basis is measured from ai-manager's cost_usd,
// and the run record's opaque Detail carries the request id rather than a pid
// — which is what lets Resume poll a turn this process did not start, and
// what makes the reconciler degrade a requeue to an orphan (there is no
// process to make dead).
type AIMAdapter struct {
	Client   *AIMClient
	Model    string
	Provider string
	Local    bool

	// Tenant names the workspace: gaos-<tenant>-<product>. There is no shared
	// override any more (M17.5, D-071): every product has its own root.
	Tenant string

	PollEvery     time.Duration // between result polls; default 2s
	Deadline      time.Duration // whole-turn ceiling; default 660s (> ai-manager's 600s)
	MaxSessionAge time.Duration // a task's session older than this is rotated; default 25 days

	vendors vendorCache // the gateway's provider list, for the vendors pane (M16)
}

const (
	defaultAIMPoll     = 2 * time.Second
	defaultAIMDeadline = 660 * time.Second
)

// ErrCannotStop: ai-manager exposes no cancel (OQ-26). A turn ends on its own.
var ErrCannotStop = errors.New("ai-manager cannot cancel a turn in flight (OQ-26)")

func (a *AIMAdapter) Name() string   { return "ai-manager" }
func (a *AIMAdapter) Mode() string   { return InvocationGateway }
func (a *AIMAdapter) Target() Target { return Target{Provider: a.Provider, Local: a.Local} }

// Smoke is the boot check: the gateway answers and has a claude provider.
// There is no tool version to pin any more — that is ai-claude-cli's to hold.
func (a *AIMAdapter) Smoke(ctx context.Context) error {
	sc, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	names, err := a.Client.Providers(sc)
	if err != nil {
		return fmt.Errorf("ai-manager smoke: %w", err)
	}
	if !slices.Contains(names, "claude") {
		return fmt.Errorf("ai-manager smoke: no available claude provider (have %v); sessions and workspaces need it", names)
	}
	return nil
}

var unsafeID = regexp.MustCompile(`[^a-z0-9._-]+`)

func safeID(s string) string {
	return strings.Trim(unsafeID.ReplaceAllString(strings.ToLower(s), "-"), "-.")
}

// WorkspaceFor derives a product's workspace id, gaos-<tenant>-<product>. It
// is derived once and never renamed: ai-claude-cli keys transcripts by it.
//
// There is no shared-workspace override (M17.5, D-071): one folder for every
// product would give them one root and one initial context, and the rule the
// operator gave is that each product has its own.
func (a *AIMAdapter) WorkspaceFor(productID string) string {
	return workspaceID(a.Tenant, productID, "")
}

// workspaceID derives a workspace id for a tenant and a product, with an
// optional suffix. One derivation for every id, so they cannot drift apart in
// their format or their length cap.
func workspaceID(tenant, productID, suffix string) string {
	id := "gaos-" + safeID(tenant) + "-" + safeID(productID) + suffix
	if len(id) <= maxWorkspaceIDLen {
		return id
	}
	sum := sha256.Sum256([]byte(id))
	return id[:maxWorkspaceIDLen-7] + "-" + hex.EncodeToString(sum[:])[:6]
}

// RequestIDFor is deterministic, so a lost 202 can be resent without a second
// turn, and a restarted orchestrator can poll it (D-060).
func (a *AIMAdapter) RequestIDFor(runID string) string {
	id := "gaos." + safeID(a.Tenant) + "." + safeID(runID)
	if len(id) > 128 {
		id = id[:128]
	}
	return id
}

// RenderPrompt is what the turn sends: the cleared packet's parts in their
// usual order, with the file contract replaced by the reply contract. The
// packet's own contract part stays in the frozen slice as evidence of what
// was asked; the gateway asks for the same document in a different place.
func RenderPrompt(c Cleared) string {
	p := c.Packet()
	kept := p
	kept.Parts = nil
	for _, part := range p.Parts {
		if part.Kind != KindContract && part.Kind != KindHandoff {
			kept.Parts = append(kept.Parts, part)
		}
	}
	return instructionOf(kept) + "\n" + contractsFor(p.Kind, p.TaskID, p.RunID, p.Role)
}

// contractsFor is what the model is told to produce.
//
// An ordinary task owes a handoff. A design task owes a design block AND a
// handoff; a review task owes a review block AND a handoff. The handoff is
// always last, because the handoff contract says "Copy it as the last thing
// in your reply" (gatewayContractTemplate) and the handoff parser takes the
// last gaos-handoff block.
//
// The kind reaches here on the packet (Packet.Kind), because the adapter has
// no other way to know: it sees the packet and nothing else.
func contractsFor(kind, taskID, runID, role string) string {
	handoff := gatewayContract(taskID, runID, role)
	// The PR reviewer owes its own block, then the handoff (D-088). It is an
	// implement task, so the role and not the kind selects the contract.
	if role == "pr-reviewer" && taskKind(kind) == TaskKindImplement {
		return prReviewContract(taskID, runID, role)
	}
	// The defect triager owes a gaos-triage block, then the handoff (D-091).
	if role == "triager" && taskKind(kind) == TaskKindImplement {
		return triageContract(taskID, runID, role)
	}
	// The defect fixer owes a gaos-fix block, then the handoff (D-091).
	if role == "fixer" && taskKind(kind) == TaskKindImplement {
		return fixContract(taskID, runID, role)
	}
	switch taskKind(kind) {
	case TaskKindDesign:
		return designContract(taskID, runID, role) + "\n===== the handoff =====\n" + handoff
	case TaskKindReview:
		return reviewContract(taskID, runID, role) + "\n===== the handoff =====\n" + handoff
	default:
		return "\n===== the handoff =====\n" + handoff
	}
}

// Dispatch runs one turn: persist the request id, submit, poll to a result.
func (a *AIMAdapter) Dispatch(ctx context.Context, c Cleared) (Attempt, error) {
	return a.turn(ctx, c, c.Packet().SessionID)
}

// turn is Dispatch with an optional session to resume (D-056): the runner
// puts the task's resumable session on the packet, or "" for a new one.
func (a *AIMAdapter) turn(ctx context.Context, c Cleared, sessionID string) (Attempt, error) {
	if c.empty() {
		return Attempt{}, reject("refusing to dispatch a packet the egress pass never cleared")
	}
	p := c.Packet()
	reqID := a.RequestIDFor(p.RunID)
	prompt := RenderPrompt(c)
	req := ChatRequest{Prompt: prompt, Model: a.Model, AskUser: true, Caller: "gaos"}
	if p.Vendor != "" && p.Vendor != VendorClaude {
		// A session lives with the provider that opened it, so a resumed turn
		// names the vendor too; and GAOS_MODEL is a Claude model, never sent
		// to another vendor -- the product's own choice is, or none (M16).
		req.Provider, req.Model = p.Vendor, p.Model
	} else if p.Model != "" {
		req.Model = p.Model
	}
	if sessionID != "" {
		req.SessionID = sessionID
	} else {
		req.NewSession = true
		// The packet names the workspace the turn runs in (M17.5): a review
		// runs in the product's own reviewer workspace, never in the author's.
		req.WorkspaceID = p.WorkspaceID
		if req.WorkspaceID == "" {
			req.WorkspaceID = a.WorkspaceFor(p.DeskID)
		}
	}
	if err := a.submit(ctx, reqID, req, p.Answer); err != nil {
		if att, ok := a.answered(c, err); ok {
			att.Prompt, att.RequestID = prompt, reqID
			return att, nil
		}
		return Attempt{}, err
	}
	att, err := a.poll(ctx, c, reqID)
	att.Prompt = prompt
	return att, err
}

// submit starts the turn. A turn that carries the operator's answer goes
// through ai-manager's answer route, which marks the question answered and
// resumes the session that asked it (M15). When that route refuses for any
// reason -- already answered by another request (409), expired (410), unknown
// (404), or the question store switched off or down (503) -- the turn is sent
// as an ordinary one: the answer is in the packet either way, so the agent
// still reads it, and only the bucket's record of who answered is left as it
// was. Falling back is safe even if the answer route did start the turn and
// only its reply was lost: the request id is the same, and ai-manager treats a
// repeated id as the request it already holds.
func (a *AIMAdapter) submit(ctx context.Context, reqID string, req ChatRequest, ans *PacketAnswer) error {
	if ans != nil && ans.QuestionID != "" && req.SessionID != "" {
		err := a.Client.SubmitAnswer(ctx, ans.QuestionID, reqID,
			AnswerRequest{Choice: ans.Choice, Text: ans.Text, Prompt: req.Prompt})
		if err == nil || ctx.Err() != nil {
			return err
		}
	}
	return a.Client.Submit(ctx, reqID, req)
}

// Resume polls a turn this process did not start, by the request id the run
// record carries. Same evidence, same Attempt, as a supervised run (BR-33).
func (a *AIMAdapter) Resume(ctx context.Context, c Cleared, r RunRecord) (Attempt, error) {
	reqID := r.Detail["requestId"]
	if reqID == "" {
		return Attempt{}, fmt.Errorf("resume: the run record names no gateway request")
	}
	return a.poll(ctx, c, reqID)
}

// Stop cannot make a gateway turn dead. The reconciler already degrades a
// requeue to an orphan when no process was recorded, so this is never the
// only thing standing between a workspace and a second writer.
func (a *AIMAdapter) Stop(RunRecord) error { return ErrCannotStop }

func (a *AIMAdapter) poll(ctx context.Context, c Cleared, reqID string) (Attempt, error) {
	every, deadline := a.PollEvery, a.Deadline
	if every <= 0 {
		every = defaultAIMPoll
	}
	if deadline <= 0 {
		deadline = defaultAIMDeadline
	}
	dc, cancel := context.WithTimeout(context.WithoutCancel(ctx), deadline)
	defer cancel()
	for {
		reply, done, err := a.Client.Result(dc, reqID)
		if err != nil {
			if att, ok := a.answered(c, err); ok {
				return att, nil
			}
			return Attempt{}, err
		}
		if done {
			att := a.attemptFrom(c, reply)
			att.RequestID = reqID
			return att, nil
		}
		select {
		case <-dc.Done():
			return a.failed(c, ExitCrashed, SigGatewayFailed, "the turn passed its deadline"), nil
		case <-time.After(every):
		}
	}
}

// answered turns a gateway answer that IS an outcome into an Attempt. A 409 is
// not one: it is busy, returned as ErrTurnRunning for the caller to retry.
func (a *AIMAdapter) answered(c Cleared, err error) (Attempt, bool) {
	switch {
	case errors.Is(err, ErrSessionGone):
		return a.failed(c, ExitRan, SigSessionLost, err.Error()), true
	case errors.Is(err, ErrRequestUnknown):
		// Not a crash of the work: the gateway lost track of it, and the turn
		// may still be running. Recovery holds the product (D-060).
		return a.failed(c, ExitCrashed, SigRequestLost, err.Error()), true
	case errors.Is(err, ErrGatewayTransient):
		return a.failed(c, ExitCrashed, SigGatewayFailed, err.Error()), true
	}
	return Attempt{}, false
}

func (a *AIMAdapter) failed(c Cleared, class, sig, why string) Attempt {
	return Attempt{Invocation: InvocationGateway, Model: a.Model, Digest: c.Digest(), ExitClass: class,
		ExitCode: -1, Failure: sig, HandoffRaw: why, CostBasis: CostUnavailable}
}

// attemptFrom reads a completed reply: the handoff block, the cost, and the
// session warning that fails a turn even when its handoff is valid.
func (a *AIMAdapter) attemptFrom(c Cleared, r ChatReply) Attempt {
	p := c.Packet()
	model := r.Model
	if model == "" {
		model = a.Model
	}
	att := Attempt{Invocation: InvocationGateway, Model: model, Digest: c.Digest(), ExitClass: ExitRan,
		TokensIn: r.InputTokens, TokensOut: r.OutputTokens, SessionID: r.SessionID,
		Reply: r.Text, Observed: r.ObservedContext()}
	att.CostUSD, att.CostBasis = gatewayCost(r)
	h, raw, err := ExtractHandoff(r.Text, p.TaskID, p.RunID)
	att.HandoffRaw = raw
	switch {
	case r.SessionWarning() != "":
		att.Failure = SigTurnUnsaved // BR-40: the session no longer matches the record
	case r.Question != nil && len(r.Question.Options) == 3:
		// M15: the agent stopped to ask. No handoff is owed, and one that came
		// anyway is kept as evidence but does not decide where the task goes.
		q := *r.Question
		att.Question = &q
		if err == nil {
			att.Handoff = &h
		}
	case err != nil:
		att.Failure = SigHandoffMissing
	default:
		att.Handoff = &h
	}
	return att
}

// gatewayCost is D-062: cost_usd is Claude Code's own total_cost_usd, passed
// through, so it is measured. Missing, or zero while tokens were spent, is
// unavailable and never zero (BR-37).
func gatewayCost(r ChatReply) (float64, string) {
	if r.CostUSD == nil || (*r.CostUSD == 0 && r.InputTokens+r.OutputTokens > 0) {
		return 0, CostUnavailable
	}
	return *r.CostUSD, CostMeasured
}

// instructionOf renders a packet's parts as one prompt, in a fixed order:
// the task, the DESIGN, the rules, the context, the repository, the contract.
// Each part is headed by its path, so the model can say which part it is
// answering.
//
// The design comes before the rules because that is the order a reviewer needs
// them in: the thing under review, and then the rubric it is judged against.
//
// KindDesign is in this list, and it has to be: a part that is in the packet
// but not in this order is a part the model never reads, and the packet would
// be a frozen slice that lies about what was sent.
func instructionOf(p Packet) string {
	var b strings.Builder
	for _, kind := range []string{KindTask, KindDesign, KindRules, KindContext, KindRepo, KindContract} {
		for _, part := range p.Parts {
			if part.Kind != kind {
				continue
			}
			fmt.Fprintf(&b, "===== %s =====\n%s\n\n", part.Path, part.Body)
		}
	}
	return b.String()
}
