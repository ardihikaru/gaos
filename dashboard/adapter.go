package main

import (
	"context"
	"errors"
	"os/exec"
)

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

// Recorded on TaskRun.invocation. 04-execution.sql constrains it to 'cli|api'.
const (
	InvocationCLI = "cli"
	InvocationAPI = "api"
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
	Failure string // SigHandoffMissing, or empty

	CostUSD   float64
	TokensIn  int64
	TokensOut int64
	CostBasis string

	LogRef string // where the raw tool output was stored
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

	// Mode is InvocationCLI or InvocationAPI, for the record.
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
}

// classifyExit is the whole of the exit code's authority.
//
// Deliberately tiny, deliberately in its own function, and deliberately the
// only place in the package that reads an *exec.ExitError. If a second reader
// of exit status ever appears, it will be visible in a grep for this name.
func classifyExit(err error) (string, int) {
	if err == nil {
		return ExitRan, 0
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		if ee.ExitCode() < 0 {
			return ExitCrashed, -1 // signalled: killed, or the timeout fired
		}
		return ExitRan, ee.ExitCode()
	}
	// Could not start: binary missing, workspace gone, context deadline before
	// the process existed.
	return ExitCrashed, -1
}
