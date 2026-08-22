package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
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

	timeout := a.Timeout
	if timeout == 0 {
		timeout = defaultDispatchTimeout
	}
	dc, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	args := append([]string(nil), a.Argv[1:]...)
	args = append(args, "-p", "--output-format", "json")
	if a.Model != "" {
		args = append(args, "--model", a.Model)
	}

	cmd := exec.CommandContext(dc, a.Argv[0], args...)
	cmd.Dir = ws
	cmd.Stdin = strings.NewReader(instructionOf(c.Packet()))
	cmd.Env = isolatedEnv(a.SessionEnv)

	// A process group of its own. Two reasons: D-2 wants work detached from the
	// orchestrator, and killing the group rather than the leader is the only
	// way a timeout reaches a tool that has spawned children of its own -- which
	// an agent that runs tests certainly has.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr

	runErr := cmd.Run()
	class, code := classifyExit(runErr)

	att := Attempt{
		Invocation: InvocationCLI,
		Model:      a.Model,
		Digest:     c.Digest(),
		ExitClass:  class,
		ExitCode:   code,
		CostBasis:  CostUnavailable,
		LogRef:     truncateLog(stdout.String(), stderr.String()),
	}

	// THE HANDOFF IS READ UNCONDITIONALLY, including after a crash.
	//
	// D-050: the document is the only authority on what happened. A run that
	// was killed may still have written `status: blocked` before it died, and
	// skipping the read because the exit looked bad would be reading meaning
	// into the exit code by omission -- the same mistake, in the one shape that
	// is easy to miss in review.
	h, err := ReadHandoff(ws)
	if err != nil {
		att.Failure = SigHandoffMissing
	} else {
		att.Handoff = &h
	}

	applyUsage(&att, stdout.Bytes(), h, err == nil)
	return att, nil
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
func instructionOf(p Packet) string {
	var b strings.Builder
	for _, kind := range []string{KindTask, KindRules, KindContext, KindRepo} {
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

// maxLogRef bounds what an attempt carries inline. BR-25 puts heavy payloads in
// object storage; there is no object storage yet, so the cap is what stands in
// for it and the comment is what stops it being mistaken for the design.
const maxLogRef = 8 << 10

func truncateLog(stdout, stderr string) string {
	s := stdout
	if stderr != "" {
		s += "\n--- stderr ---\n" + stderr
	}
	if len(s) > maxLogRef {
		return s[:maxLogRef] + fmt.Sprintf("\n... truncated, %d bytes total", len(s))
	}
	return s
}
