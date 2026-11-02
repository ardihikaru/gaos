package main

import "context"

// The model gateway seam. M4.4, requirement D-7, rule BR-33.
//
// BRD §7.6: "Two interchangeable adapters, a provider's command-line tool
// spawned as a detached subprocess and a direct HTTP call, selected by one
// deployment-wide setting." What makes them interchangeable is one rule:
//
//	"the contract is the mode-independent part. Both modes freeze a task packet,
//	 both must produce a handoff document in the same shape at the same path,
//	 and both record the same evidence. Nothing above the adapter -- not the
//	 scheduler, not the reviewer, not the fold -- may behave differently
//	 depending on which one ran (BR-33)."
//
// §7.6.3's warning is what this file is shaped by: "Two modes are only
// interchangeable if nobody above the adapter can tell them apart. The way that
// gets lost is small and gradual: a handoff document shaped around one tool's
// output format, a reviewer that special-cases a CLI exit code, a scheduler
// that knows a subprocess is slower."
//
// So: Attempt is the mode-independent shape, Attempt.Outcome() is the only
// thing anything above here should ask, and Invocation is recorded and never
// branched on.

// Recorded on TaskRun.invocation. 04-execution.sql constrains it to 'cli|api';
// 11-blueprint.sql widens that to 'cli|api|gateway' (D-062).
const (
	InvocationCLI     = "cli"
	InvocationAPI     = "api"
	InvocationGateway = "gateway" // a turn through ai-manager, D-055
)

// Recorded on TaskRun.costBasis. 04-execution.sql constrains it to
// 'measured|estimated|unavailable'.
const (
	CostMeasured    = "measured"
	CostEstimated   = "estimated"
	CostUnavailable = "unavailable"
)

// What the exit code is allowed to say, and the whole of it. See D-050.
const (
	ExitRan     = "ran"     // the process produced an exit status, whatever it was
	ExitCrashed = "crashed" // it could not be started, or a signal killed it
)

// SigHandoffMissing is BR-30's failure signature, one constant for all four
// ways the document can fail: absent, no front matter, unparseable, or missing
// a mandatory field.
//
// One name because they are one failure to the retry loop. A second signature
// would split a class the guard treats as a single thing, and the guard is what
// makes the loop terminate.
const SigHandoffMissing = "handoff-missing"

// Attempt is what an invocation produced, in the mode-independent shape.
// Every field here is recordable on a TaskRun (04-execution.sql).
type Attempt struct {
	// BR-33: RECORDED, never branched on above this seam. TaskRun.invocation
	// exists so the prohibition can be checked rather than trusted, and
	// TestNothingAboveTheAdapterBranchesOnTheInvocationMode is that check.
	Invocation string
	Model      string
	Digest     string // the Cleared packet's digest, so the attempt names its input

	// ExitClass is the ONLY thing the exit code is permitted to say.
	//
	// D-050, measured: Claude Code separates a crash from a completed run but
	// NOT A REFUSAL FROM A SUCCESS. An unknown flag, an unknown model, a failed
	// authentication and an exhausted budget all exit 1 with a distinguishing
	// terminal_reason, while a task the model could not do exits 0 with
	// terminal_reason: completed -- byte-identical, at the process level, to
	// one it did.
	ExitClass string
	ExitCode  int // kept as evidence; interpreted by nobody

	// Handoff is nil when the document was missing or unparseable.
	Handoff *Handoff

	// HandoffRaw is the document as the run wrote it, kept because T-064 stores
	// it as an Artifact for the next agent and what that agent should read is
	// what the last one wrote. Re-rendering our parse of it would silently drop
	// everything the parser ignores -- including the free text after the front
	// matter, which is where an agent explains itself.
	//
	// Present even when Handoff is nil: a document that failed to parse is still
	// the best evidence there is about an attempt that failed to produce one.
	HandoffRaw string

	Failure string // SigHandoffMissing, or empty

	CostUSD   float64
	TokensIn  int64
	TokensOut int64
	CostBasis string

	LogRef string // where the raw tool output was stored

	// Through the gateway only; empty for the CLI. Recorded, never branched on.
	RequestID string
	SessionID string
	// Prompt and Reply are the turn's transcript, kept because the session
	// that carried them expires (BR-41). Empty for the CLI, whose evidence is
	// its log on disk.
	Prompt string
	Reply  string
	// Observed is the context the worker reports it offered the model
	// (metadata.context.workspace), compared with the pinned Blueprint after
	// the turn (T-156). Nil when the reply carried none.
	Observed *ObservedContext
}

// Outcome is what happened to the WORK.
//
// Read the body: it does not mention ExitCode, and that omission is D-050 in
// one function. "The exit code may only classify crash versus ran; everything
// about the work itself is read from the mandatory handoff document." A
// reviewer that special-cased a CLI exit code is the exact failure §7.6.3 names
// as fatal to two-mode interchangeability.
func (a Attempt) Outcome() string {
	if a.Handoff == nil {
		return "failed" // and a.Failure is SigHandoffMissing
	}
	return a.Handoff.Status
}

// Adapter is the seam. Two implementations are planned: CLIAdapter here, and an
// HTTP one behind the identical interface (T-058).
type Adapter interface {
	// Name identifies the tool for a log line and /healthz. Not a routing key.
	Name() string

	// Mode is InvocationGateway (or, once T-058 lands, InvocationAPI), for the record.
	Mode() string

	// Target is what the egress pass needs to answer L-6's no-egress question.
	Target() Target

	// Smoke is the boot check. §7.6.3: supported CLIs are "pinned per release
	// with a smoke test per tool", because the interface is not versioned for
	// programmatic use and moves without notice.
	Smoke(ctx context.Context) error

	// Dispatch takes a Cleared and nothing else. THAT SIGNATURE IS BR-32:
	// a Cleared can only come from Screen, so there is no route from a Packet
	// to a provider that does not pass the egress check first.
	Dispatch(ctx context.Context, c Cleared) (Attempt, error)

	// Resume produces an Attempt from a run THIS PROCESS DID NOT START.
	//
	// D-2 detaches the work so it outlives the orchestrator, which means an
	// orchestrator that restarts finds a live child it cannot cmd.Wait() on --
	// you can only wait on a process you forked. So adoption cannot be a
	// variation on Dispatch; it needs its own entry point.
	//
	// It reads the same evidence Dispatch reads: the handoff document BR-30
	// requires of every mode, and the log the run recorded on disk. That is what
	// makes an adopted run and a supervised one indistinguishable above the seam
	// (BR-33), and TestAnAdoptedRunAndASupervisedRunProduceTheSameAttempt is the
	// check rather than the promise.
	//
	// Resume does NOT decide whether the run should be adopted. That decision is
	// the reconciler's, from the lease and the beat alone, per D-4's pseudocode.
	// Anything this learns about the process is corroboration inside the
	// adapter, never a branch above it.
	Resume(ctx context.Context, c Cleared, r RunRecord) (Attempt, error)

	// Stop makes a run dead, and it is on the seam rather than in the reconciler
	// because BR-33 would not survive it being anywhere else.
	//
	// Boot reconciliation has to be able to say "make this dead" -- D-054, the
	// kill half of kill-then-requeue -- and what that MEANS is entirely
	// mode-specific: for a CLI run it is a signal to a process group, for the
	// API adapter (T-058) it will be cancelling a request. A reconciler that
	// knew about pids would be a reconciler that only worked for one mode, and
	// BR-33 says nothing above this line may branch on the mode at all.
	//
	// Idempotent: stopping a run that is already gone is not an error, because
	// the caller reached here precisely because it could not tell.
	Stop(r RunRecord) error
}

// RunRecord is what recovery hands an adapter for a turn this process did not
// start. Since M14.7 it is built from the TaskRun in the graph -- there is no
// file on this host -- and the gateway adapter reads the request id from it.
//
// Detail is deliberately opaque above the adapter. The gateway puts its
// request id in it; the HTTP adapter (T-058) will put something else -- a poll
// URL. NOTHING ABOVE THE ADAPTER READS IT, which is what keeps BR-33 true when a second mode
// arrives: a reconciler that understood pids would be a reconciler that only
// worked for one mode.
type RunRecord struct {
	RunID     string            `json:"runId"`
	TaskID    string            `json:"taskId"`
	Attempt   int               `json:"attempt"`
	Workspace string            `json:"workspace"`
	Digest    string            `json:"digest"`
	Model     string            `json:"model"`
	StartedAt int64             `json:"startedAt"`
	LogRef    string            `json:"logRef"`
	Detail    map[string]string `json:"detail"`
}
