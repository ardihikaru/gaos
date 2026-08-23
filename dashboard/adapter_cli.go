package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// The CLI adapter. T-057, requirement D-7, and the first supported tool is
// Claude Code pinned at @anthropic-ai/claude-code@2.1.238 (D-049).
//
// THIS FILE IS THE ONLY PLACE ALLOWED TO KNOW A TOOL'S FLAGS. §7.6.3: "the
// adapter is the only place allowed to know a tool's flags -- OQ-23. Protects
// the model gateway's prohibition on leaking provider shapes upward." A second
// file that knew `-p` meant non-interactive would be the first of the small,
// gradual changes that turn the second adapter from an addition into a rewrite.
//
// What D-049 measured the tool to do, and what each buys here:
//
//	runs non-interactively from a working directory   -> -p, cmd.Dir
//	takes its instruction from a file on stdin        -> cmd.Stdin
//	leaves structured output on disk                  -> --output-format json
//	pins to a version with a machine-readable --version -> Smoke
//
// The fifth point it FAILED -- an exit code that separates a refusal from a
// success -- is why Dispatch below reads the handoff document unconditionally
// and never reads meaning into cmd.Wait's error. See D-050 and adapter.go.

// defaultDispatchTimeout bounds a single invocation. Generous, because an agent
// editing a repository is not a request; finite, because a hung tool with no
// heartbeat behind it (M4.2 does not exist yet) would otherwise wedge the desk
// until the dashboard restarts.
const defaultDispatchTimeout = 20 * time.Minute

// smokeTimeout bounds the boot check. Small on purpose: this runs before the
// listener is up, and a hung provider tool must not hold the boot open.
const smokeTimeout = 10 * time.Second

type CLIAdapter struct {
	// Argv is the command PREFIX, not a command name. Production is
	// []string{"claude"}. The tests are []string{"sh", ".../testdata/fakecli/claude"},
	// which is how the fake tool runs without depending on git having preserved
	// an execute bit -- the single most annoying "works on my checkout" failure
	// this design can avoid for free.
	Argv []string

	Pin      VersionPin
	Model    string
	Provider string
	Local    bool
	Timeout  time.Duration

	// PermissionMode is how the tool is told it may act without being asked.
	//
	// A tool run non-interactively cannot be asked anything, so its default --
	// prompt the human -- resolves to "refuse", and an agent that may not write
	// a file cannot write the handoff document either. The whole invocation then
	// fails as handoff-missing for a reason that has nothing to do with the work.
	//
	// The value is deliberately NOT a blanket bypass. What makes accepting edits
	// reasonable here is D-3: the tool runs in a workspace this orchestrator holds
	// an exclusive lease on, materialised from a packet the egress pass cleared.
	// The bound is the lease, not the prompt.
	//
	// Empty means the flag is not passed at all, which is what a tool with no
	// such concept needs -- and what keeps this the only file that knows one
	// tool has the flag.
	PermissionMode string

	// ContractInSystemPrompt repeats the handoff contract as a standing
	// instruction rather than leaving it only in the packet.
	//
	// MEASURED, on the first real call this repository ever made: the agent read
	// the packet, did the work well, and never wrote the handoff document. The
	// contract was the last section of a long stdin, behind the objective, the
	// rules, the record and a source file -- and by the time the work was done
	// it read as background rather than as an instruction. The run was recorded
	// as handoff-missing, which is correct and useless: the work existed and
	// GAOS could not tell.
	//
	// A system prompt is the channel for "this holds for the whole session",
	// which is exactly what the contract is. The packet keeps its copy -- that
	// is the RECORD of what was asked, and it is what the frozen slice contains
	// -- so this is deliberate duplication rather than a move.
	ContractInSystemPrompt bool

	// SessionEnv names the environment variables the tool needs in order to
	// find its own session. NAMED, never inherited -- see isolatedEnv.
	SessionEnv []string
}

func (a *CLIAdapter) Name() string {
	if len(a.Argv) == 0 {
		return "cli"
	}
	return a.Argv[len(a.Argv)-1]
}

func (a *CLIAdapter) Mode() string { return InvocationCLI }

func (a *CLIAdapter) Target() Target {
	return Target{Provider: a.Provider, Local: a.Local}
}

// Smoke runs the tool's --version and compares it to the pin.
func (a *CLIAdapter) Smoke(ctx context.Context) error {
	if len(a.Argv) == 0 {
		return fmt.Errorf("cli adapter has no command configured")
	}
	sc, cancel := context.WithTimeout(ctx, smokeTimeout)
	defer cancel()

	cmd := exec.CommandContext(sc, a.Argv[0], append(append([]string(nil), a.Argv[1:]...), "--version")...)
	cmd.Env = isolatedEnv(a.SessionEnv)
	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("%s --version: %w", a.Name(), err)
	}
	_, err = checkPin(a.Name(), a.Pin, string(out))
	return err
}

// Dispatch runs one invocation against a cleared packet.
func (a *CLIAdapter) Dispatch(ctx context.Context, c Cleared) (Attempt, error) {
	// Unreachable if the type discipline held: the only way to obtain a Cleared
	// is Screen, and Screen never returns a zero value with a nil error. It is
	// here because a zero value is always constructible in Go, and BR-32 is not
	// a rule this file is willing to leave to a reader's care.
	if c.empty() {
		return Attempt{}, reject("refusing to dispatch a packet the egress pass never cleared")
	}
	if len(a.Argv) == 0 {
		return Attempt{}, fmt.Errorf("cli adapter has no command configured")
	}

	ws := c.Workspace()
	if ws == "" {
		return Attempt{}, fmt.Errorf("cleared packet has no workspace")
	}

	if err := refuseIfUnderTest(a.Argv); err != nil {
		return Attempt{}, err
	}

	timeout := a.Timeout
	if timeout == 0 {
		timeout = defaultDispatchTimeout
	}

	// context.WithoutCancel, and this is the line that makes D-2 true.
	//
	// Before it, dc descended from the caller's ctx, which serve() builds from
	// signal.NotifyContext(SIGINT, SIGTERM). So a graceful shutdown cancelled
	// the context, CommandContext killed the child, and "work runs in detached
	// processes that survive the orchestrator" was false on precisely the path
	// an operator uses. (SIGKILL was already fine, by accident: the Go runtime
	// never runs, nothing kills the child, and Setpgid below means no terminal
	// signal reaches it either. The ungraceful path satisfied D-2 and the
	// graceful one violated it, which is the wrong way round.)
	//
	// The only cancellation left is the timeout, which is a property of the RUN
	// rather than of the orchestrator supervising it.
	dc, cancel := context.WithTimeout(context.WithoutCancel(ctx), timeout)
	defer cancel()

	args := append([]string(nil), a.Argv[1:]...)
	args = append(args, "-p", "--output-format", "json")
	if a.Model != "" {
		args = append(args, "--model", a.Model)
	}
	if a.PermissionMode != "" {
		args = append(args, "--permission-mode", a.PermissionMode)
	}
	if a.ContractInSystemPrompt {
		if c := partOfKind(c.Packet(), KindContract); c != "" {
			args = append(args, "--append-system-prompt", c)
		}
	}

	// The handoff document is emptied before the invocation, which handoff.go
	// has claimed since it was written -- "it is declared and empty before the
	// invocation starts, per the §7.6 packet layout" -- and which nothing
	// actually did. Without it a retry in a reused workspace reads the previous
	// attempt's document and scores the wrong run.
	if err := truncateHandoff(ws); err != nil {
		return Attempt{}, err
	}

	logPath, stdout, stderr, err := openRunLogs(ws, c.Digest())
	if err != nil {
		return Attempt{}, err
	}
	defer stdout.Close()
	defer stderr.Close()

	cmd := exec.CommandContext(dc, a.Argv[0], args...)
	cmd.Dir = ws
	cmd.Stdin = strings.NewReader(instructionOf(c.Packet()))
	cmd.Env = isolatedEnv(a.SessionEnv)

	// A process group of its own. Three reasons now: D-2 wants work detached
	// from the orchestrator; killing the group rather than the leader is the
	// only way a timeout reaches a tool that has spawned children of its own,
	// which an agent that runs tests certainly has; and the reconciler's
	// kill-then-requeue branch needs a group to signal, because by then the
	// leader may already be gone and its children may not be.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	// Files rather than bytes.Buffer, and this is what makes adoption COMPLETE
	// rather than partial.
	//
	// The tool's JSON envelope carries total_cost_usd, and D-051 requires
	// costBasis: measured from the first run. In a buffer that envelope lives in
	// the orchestrator's address space -- so an orchestrator that died mid-run
	// takes the cost with it, and the process that adopts the run can recover
	// the outcome from the handoff but never what it cost. On disk, collect()
	// runs identically on the spawn path and the adoption path.
	cmd.Stdout, cmd.Stderr = stdout, stderr

	// Start, not Run: the pid does not exist until Start returns, and the
	// RunRecord has to be on disk before anything can be adopted.
	if err := cmd.Start(); err != nil {
		return Attempt{}, err
	}
	// Stamp the process detail into the RunRecord the runner already wrote. A
	// read-modify-write rather than a callback, because the alternative is a
	// mutable field on an adapter two runs share, and the seam's signature is
	// BR-32 and does not get widened for this.
	//
	// No RunRecord means nobody is supervising -- the operator-triggered
	// dispatch route -- and then there is nothing to adopt and nothing to
	// stamp.
	if err := stampRunDetail(ws, runDetail(cmd)); err != nil {
		return Attempt{}, err
	}

	runErr := cmd.Wait()
	class, code := classifyExit(runErr)

	att := collect(ws, logPath, c.Digest(), a.Model)
	att.ExitClass, att.ExitCode = class, code
	return att, nil
}

// Resume rebuilds an Attempt for a run this process did not start.
//
// It waits for the process to go away and then reads the same evidence
// Dispatch reads. The pid probe is corroboration, not authority: whether the
// run should have been adopted at all was decided by the reconciler from the
// lease and the beat, per D-4, before this was ever called.
func (a *CLIAdapter) Resume(ctx context.Context, c Cleared, r RunRecord) (Attempt, error) {
	if r.RunID == "" || r.Workspace == "" {
		return Attempt{}, fmt.Errorf("resume: the run record names no run or no workspace")
	}
	timeout := a.Timeout
	if timeout == 0 {
		timeout = defaultDispatchTimeout
	}
	dc, cancel := context.WithTimeout(context.WithoutCancel(ctx), timeout)
	defer cancel()

	for alive(r.Detail) {
		select {
		case <-dc.Done():
			// The adopted run outlived the timeout. It is still not this
			// function's business to kill it -- that is the reconciler's, which
			// owns the decision and the lease.
			att := collect(r.Workspace, r.LogRef, r.Digest, r.Model)
			att.ExitClass, att.ExitCode = ExitCrashed, -1
			return att, nil
		case <-time.After(resumePollInterval):
		}
	}

	att := collect(r.Workspace, r.LogRef, r.Digest, r.Model)
	// An adopted run has no exit status to read: nobody was waiting on it. That
	// is honest rather than lossy, because D-050 already forbids reading meaning
	// into the exit code -- ExitRan says the process ran, and the handoff says
	// what it did.
	att.ExitClass, att.ExitCode = ExitRan, 0
	return att, nil
}

// resumePollInterval is how often adoption checks whether the child is gone.
// Coarse on purpose: this is a liveness poll on a run that may take twenty
// minutes, not a latency-sensitive path.
const resumePollInterval = 2 * time.Second

// collect is the tail both Dispatch and Resume run, and it is ONE function
// rather than two on purpose.
//
// BR-33 asks that nothing above the adapter be able to tell the modes apart.
// The same argument applies within a mode: an adopted attempt assembled by
// different code from a supervised one would drift, and the drift would show up
// as a scoring difference nobody could explain.
func collect(ws, logPath, digest, model string) Attempt {
	att := Attempt{
		Invocation: InvocationCLI,
		Model:      model,
		Digest:     digest,
		CostBasis:  CostUnavailable,
		LogRef:     logPath,
	}

	// THE HANDOFF IS READ UNCONDITIONALLY, including after a crash.
	//
	// D-050: the document is the only authority on what happened. A run that
	// was killed may still have written `status: blocked` before it died, and
	// skipping the read because the exit looked bad would be reading meaning
	// into the exit code by omission -- the same mistake, in the one shape that
	// is easy to miss in review.
	//
	// The bytes are kept as well as the parse. T-064 stores them as an Artifact,
	// and a document that failed to parse is still the best evidence there is
	// about an attempt that failed to produce one -- so the raw text is carried
	// on both branches and the failure signature is set on one.
	raw, rerr := os.ReadFile(filepath.Join(ws, HandoffPath))
	if rerr == nil {
		att.HandoffRaw = string(raw)
	}
	h, err := ParseHandoff(raw)
	if rerr != nil {
		err = fmt.Errorf("%s: %w", SigHandoffMissing, rerr)
	}
	if err != nil {
		att.Failure = SigHandoffMissing
	} else {
		att.Handoff = &h
	}

	out, _ := os.ReadFile(logPath)
	applyUsage(&att, out, h, err == nil)
	return att
}

// cliResult is the shape D-051 measured: "its JSON result carries
// total_cost_usd and a modelUsage breakdown per model with input, output and
// both cache-token counts."
type cliResult struct {
	TotalCostUSD float64 `json:"total_cost_usd"`
	ModelUsage   map[string]struct {
		InputTokens              int64 `json:"inputTokens"`
		OutputTokens             int64 `json:"outputTokens"`
		CacheReadInputTokens     int64 `json:"cacheReadInputTokens"`
		CacheCreationInputTokens int64 `json:"cacheCreationInputTokens"`
	} `json:"modelUsage"`
}

// applyUsage fills in the cost fields, per D-051 and BR-37.
//
// The tool's own figure wins where it makes one -- D-051 reversed §7.6.2's
// assumption that CLI mode is usually estimated. Where it reports nothing the
// basis is UNAVAILABLE and never estimated: BR-37 and D-044 forbid reporting an
// unpriceable model as zero, and dressing an absent number up as an "estimate"
// is the same lie with better manners. A zero is a figure the budget guard will
// let through.
func applyUsage(att *Attempt, stdout []byte, h Handoff, parsed bool) {
	if r, ok := parseCLIResult(stdout); ok {
		att.CostUSD = r.TotalCostUSD
		for _, u := range r.ModelUsage {
			att.TokensIn += u.InputTokens + u.CacheReadInputTokens + u.CacheCreationInputTokens
			att.TokensOut += u.OutputTokens
		}
		att.CostBasis = CostMeasured
		return
	}
	// The tool said nothing. The document may still carry a usage block, which
	// §7.6 permits it to omit entirely.
	if parsed && h.Usage != nil {
		att.TokensIn, att.TokensOut = h.Usage.TokensIn, h.Usage.TokensOut
		if h.Usage.Basis != "" {
			att.CostBasis = h.Usage.Basis
		}
	}
}

// parseCLIResult finds the tool's JSON envelope in its stdout.
//
// Scans lines from the end because --output-format json prints the result last
// and an agent's own chatter precedes it. Tolerant of not finding one: an
// absent envelope is CostUnavailable, not an error, because the run itself may
// have been fine and the cost is evidence rather than the outcome.
func parseCLIResult(stdout []byte) (cliResult, bool) {
	lines := bytes.Split(stdout, []byte("\n"))
	for i := len(lines) - 1; i >= 0; i-- {
		line := bytes.TrimSpace(lines[i])
		if len(line) == 0 || line[0] != '{' {
			continue
		}
		var r cliResult
		if err := json.Unmarshal(line, &r); err == nil && (r.TotalCostUSD != 0 || len(r.ModelUsage) > 0) {
			return r, true
		}
	}
	return cliResult{}, false
}

// instructionOf is what goes to the tool on stdin: the objective, the rules and
// the context, in that order.
//
// The packet, not a prompt string. D-8, and BRD §7.6.1's "the packet -- not a
// prompt string -- is what the adapter hands over". The difference matters at
// replay time: a prompt is a thing GAOS composed and would have to recompose
// identically, and a packet is a thing it stored.
// The contract goes LAST, after the repository dump, and the ordering is not
// cosmetic. It is the one part of the packet whose instructions must be
// followed rather than merely read, and the end of a long input is where a
// model attends to an instruction best. It is also on disk, at
// .gaos/packet/handoff.md, because a tool that summarises its input rather than
// reading it whole still has somewhere to look.
// partOfKind returns the body of the first part of a kind, or empty.
func partOfKind(p Packet, kind string) string {
	for _, part := range p.Parts {
		if part.Kind == kind {
			return part.Body
		}
	}
	return ""
}

func instructionOf(p Packet) string {
	var b strings.Builder
	for _, kind := range []string{KindTask, KindRules, KindContext, KindRepo, KindContract} {
		for _, part := range p.Parts {
			if part.Kind != kind {
				continue
			}
			fmt.Fprintf(&b, "===== %s =====\n%s\n\n", part.Path, part.Body)
		}
	}
	return b.String()
}

// isolatedEnv builds the child's environment EXPLICITLY rather than inheriting
// os.Environ().
//
// D-051's closing measurement is the reason: "a default invocation inherited
// the host's project configuration and roughly 24k cache-read tokens, so a
// trivial prompt cost $0.13." The money is the small half. The large half is
// the sentence after it: "An adapter that does not isolate context is not
// merely wasteful -- it makes the frozen slice of T-055 a lie, because what the
// model received is not what GAOS recorded."
//
// So the child gets what it needs to run and to find its own session, and
// nothing else. The session variables are NAMED by configuration, which also
// keeps BR-29 checkable: GAOS never holds somebody else's provider credential,
// and here it can be seen not to -- it forwards names, never values it read.
func isolatedEnv(session []string) []string {
	env := []string{
		"PATH=" + envOr("PATH", "/usr/local/bin:/usr/bin:/bin"),
		"HOME=" + envOr("HOME", "/tmp"),
		"TMPDIR=" + envOr("TMPDIR", "/tmp"),
		"LANG=" + envOr("LANG", "C.UTF-8"),
	}
	for _, k := range session {
		if v, ok := os.LookupEnv(k); ok {
			env = append(env, k+"="+v)
		}
	}
	return env
}

// ---------------------------------------------------------------- the run's
// files, its process, and the two guards in front of both.
//
// UNIX ONLY, and stated rather than discovered. Setpgid, killing a process
// group and reading /proc are all Linux-shaped, and this package has no build
// tags. The deploy target is a distroless Linux container (deploy/), so the
// honest choice is to say so here rather than to add _unix.go/_other.go pairs
// that nobody would maintain and no CI would exercise.

// openRunLogs creates the files a run writes to, and returns the path the
// Attempt will carry as its LogRef.
//
// LogRef used to be up to 8 KiB of inline text with a comment saying the cap
// stood in for object storage "because there is no object storage yet". A file
// in the workspace is not object storage either, but it IS a reference, which
// is what BR-25 asked for -- "heavy payloads stored EXTERNAL or by reference,
// never inline on a vertex the scheduler traverses" -- and unlike the inline
// text it survives the process that produced it.
func openRunLogs(ws, digest string) (string, *os.File, *os.File, error) {
	dir := filepath.Join(ws, ".gaos", "run", "log")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", nil, nil, err
	}
	stem := digest
	if len(stem) > 16 {
		stem = stem[:16]
	}
	if stem == "" {
		stem = "run"
	}
	outPath := filepath.Join(dir, stem+".log")
	out, err := os.OpenFile(outPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return "", nil, nil, err
	}
	errPath := filepath.Join(dir, stem+".err")
	errf, err := os.OpenFile(errPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		out.Close()
		return "", nil, nil, err
	}
	return outPath, out, errf, nil
}

// truncateHandoff declares the document and empties it, which handoff.go has
// always said happens before an invocation starts and which nothing did. A
// retry in a reused workspace would otherwise read the previous attempt's
// document, and the adopting orchestrator's `h.Run == record.RunID` check would
// be comparing against a run that had already finished.
func truncateHandoff(ws string) error {
	path := filepath.Join(ws, HandoffPath)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, nil, 0o600)
}

// runDetail is what the CLI adapter puts in RunRecord.Detail, and the only code
// in the package that may read it back is alive() below.
//
// procStart is field 22 of /proc/<pid>/stat, the process's start time in clock
// ticks since boot. It is here because PID reuse is real: a bare kill(pid, 0)
// after a reboot, or after enough process churn, finds a stranger and reports a
// long-dead run as alive. Where it cannot be read the field is empty and
// adoption falls back to the lease and the beat, which D-4 already treats as
// the two independent signals.
func runDetail(cmd *exec.Cmd) map[string]string {
	pid := cmd.Process.Pid
	d := map[string]string{
		"pid":  strconv.Itoa(pid),
		"pgid": strconv.Itoa(pid), // Setpgid makes the child its own group leader
	}
	if s := procStart(pid); s != "" {
		d["procStart"] = s
	}
	return d
}

func procStart(pid int) string {
	body, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return ""
	}
	// The comm field is parenthesised and may contain spaces, so fields are
	// counted from after the closing paren rather than from the start.
	i := bytes.LastIndexByte(body, ')')
	if i < 0 {
		return ""
	}
	fields := strings.Fields(string(body[i+1:]))
	// After the paren, field 1 is state; starttime is field 22 overall, which
	// is index 19 here.
	if len(fields) < 20 {
		return ""
	}
	return fields[19]
}

// Stop makes a CLI run dead: SIGTERM to the process GROUP, a grace period, then
// SIGKILL.
//
// The group and not the leader. The leader may already be gone -- that is a
// large part of why the caller could not tell whether the run was alive -- while
// the test runner or compiler it spawned is very much still there, holding the
// workspace open. Setpgid in Dispatch is what makes the group addressable, and
// this is the second reason it is set.
//
// Negative pid is the group. Idempotent by construction: signalling a group
// that is gone returns ESRCH, which is the success case here.
func (a *CLIAdapter) Stop(r RunRecord) error {
	pgid, err := strconv.Atoi(r.Detail["pgid"])
	if err != nil || pgid <= 0 {
		// Nothing was ever recorded, so nothing was ever spawned by this
		// adapter. There is nothing to kill and saying so is not a failure.
		return nil
	}
	_ = syscall.Kill(-pgid, syscall.SIGTERM)
	deadline := time.Now().Add(stopGracePeriod)
	for time.Now().Before(deadline) {
		if !alive(r.Detail) {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	_ = syscall.Kill(-pgid, syscall.SIGKILL)
	return nil
}

// stopGracePeriod is how long a run gets to exit on its own after SIGTERM.
// Short: by the time this is called the run is already unsupervised and its
// workspace is about to be handed to somebody else.
const stopGracePeriod = 3 * time.Second

// alive reports whether the recorded process is still there AND is still the
// same process. Signal 0 checks for existence without delivering anything.
func alive(detail map[string]string) bool {
	pid, err := strconv.Atoi(detail["pid"])
	if err != nil || pid <= 0 {
		return false
	}
	if err := syscall.Kill(pid, 0); err != nil {
		return false
	}
	if want := detail["procStart"]; want != "" {
		if got := procStart(pid); got != "" && got != want {
			// The pid exists and is somebody else's. Reporting this as alive
			// would keep a finished run open forever.
			return false
		}
	}
	return true
}

// stampRunDetail records the process detail on the RunRecord the runner wrote
// before the spawn. Absent record, nothing to stamp.
func stampRunDetail(ws string, detail map[string]string) error {
	r, ok, err := ReadRunRecord(ws)
	if err != nil || !ok {
		return err
	}
	r.Detail = detail
	return WriteRunRecord(ws, r)
}

// WriteRunRecord persists the record, atomically for the same reason the lease
// is: a reader must see the whole of one version or the whole of the other.
func WriteRunRecord(ws string, r RunRecord) error {
	path := filepath.Join(ws, RunRecordPath)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	body, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, body, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

// ReadRunRecord reads it back, for an orchestrator that was not running when it
// was written.
func ReadRunRecord(ws string) (RunRecord, bool, error) {
	if ws == "" {
		return RunRecord{}, false, nil
	}
	body, err := os.ReadFile(filepath.Join(ws, RunRecordPath))
	if os.IsNotExist(err) {
		return RunRecord{}, false, nil
	}
	if err != nil {
		return RunRecord{}, false, err
	}
	var r RunRecord
	if err := json.Unmarshal(body, &r); err != nil {
		return RunRecord{}, false, fmt.Errorf("run record at %s is unreadable: %w", ws, err)
	}
	return r, true, nil
}
