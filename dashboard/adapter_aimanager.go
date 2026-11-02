package main

import (
	"context"
	"errors"
	"fmt"
	"os"
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

	// Tenant names the workspace: gaos-<tenant>-<product>. Workspace, when
	// set, overrides the derived id for every product (a shared workspace,
	// until M14.4 provisions one per product).
	Tenant    string
	Workspace string

	PollEvery     time.Duration // between result polls; default 2s
	Deadline      time.Duration // whole-turn ceiling; default 660s (> ai-manager's 600s)
	MaxSessionAge time.Duration // a task's session older than this is rotated; default 25 days
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
func (a *AIMAdapter) WorkspaceFor(productID string) string {
	if a.Workspace != "" {
		return a.Workspace
	}
	return "gaos-" + safeID(a.Tenant) + "-" + safeID(productID)
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
	return instructionOf(kept) + "\n===== the handoff =====\n" + gatewayContract(p.TaskID, p.RunID, p.Role)
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
	if ws := c.Workspace(); ws != "" {
		if err := stampRunDetail(ws, map[string]string{"requestId": reqID, "gateway": a.Client.Base}); err != nil {
			return Attempt{}, err
		}
	}
	prompt := RenderPrompt(c)
	req := ChatRequest{Prompt: prompt, Model: a.Model}
	if sessionID != "" {
		req.SessionID = sessionID
	} else {
		req.NewSession, req.WorkspaceID = true, a.WorkspaceFor(p.DeskID)
	}
	if err := a.Client.Submit(ctx, reqID, req); err != nil {
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
		TokensIn: r.InputTokens, TokensOut: r.OutputTokens, LogRef: a.writeReply(c, r.Text), SessionID: r.SessionID,
		Reply: r.Text}
	att.CostUSD, att.CostBasis = gatewayCost(r)
	h, raw, err := ExtractHandoff(r.Text, p.TaskID, p.RunID)
	att.HandoffRaw = raw
	switch {
	case r.SessionWarning() != "":
		att.Failure = SigTurnUnsaved // BR-40: the session no longer matches the record
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

// writeReply keeps the full reply beside the run's other evidence when there
// is a local workspace, and returns where. The session is transport (BR-41).
func (a *AIMAdapter) writeReply(c Cleared, text string) string {
	ws := c.Workspace()
	if ws == "" {
		return ""
	}
	path, out, errf, err := openRunLogs(ws, c.Digest())
	if err != nil {
		return ""
	}
	defer out.Close()
	defer errf.Close()
	if _, err := out.WriteString(text); err != nil {
		os.Remove(path)
		return ""
	}
	return path
}
