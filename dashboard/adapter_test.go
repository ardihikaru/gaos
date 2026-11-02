package main

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The CLI adapter. T-057, and the three decisions of 2026-08-21 that constrain
// it: D-049 (the pin and the smoke test), D-050 (the exit code may not decide
// an outcome) and D-051 (cost comes from the tool's own report).
//
// Nothing here runs the real provider tool, makes a network call or reads a
// credential. The fake CLI in testdata/fakecli reproduces the behaviours that
// were measured, including the one that failed the contract.

// fakeCLI builds an adapter pointed at the stand-in tool, and a workspace for
// it to run in.
//
// Argv is {"sh", <path>} rather than the script itself, so nothing depends on
// git having preserved an execute bit.
func fakeCLI(t *testing.T, mode string, exitCode string) (*CLIAdapter, string) {
	t.Helper()
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("no /bin/sh: the fake provider CLI is a shell script")
	}

	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(wd, "testdata", "fakecli", "claude")
	fixtures := filepath.Join(wd, "testdata", "handoff")

	t.Setenv("FAKE_CLI_MODE", mode)
	t.Setenv("FAKE_CLI_EXIT", exitCode)
	t.Setenv("FAKE_CLI_FIXTURES", fixtures)

	pin, err := ParsePin(">=2.0.0 <3.0.0")
	if err != nil {
		t.Fatal(err)
	}
	a := &CLIAdapter{
		Argv:     []string{"sh", script},
		Pin:      pin,
		Model:    "claude-sonnet-5",
		Provider: "anthropic",
		// The fake tool reads its mode from the environment, so those three
		// names are what a real deployment's session variables stand for.
		SessionEnv: []string{"FAKE_CLI_MODE", "FAKE_CLI_EXIT", "FAKE_CLI_FIXTURES"},
	}
	return a, t.TempDir()
}

// clearedFor screens a trivial packet so the test has a Cleared to dispatch.
// It goes through Screen rather than building one, because building one is what
// TestNothingOutsideTheEgressPassConstructsACleared forbids.
func clearedFor(t *testing.T, ws string) Cleared {
	t.Helper()
	p := Packet{
		TaskID: "t-1", DeskID: "d-1", Workspace: ws,
		Parts: []PacketPart{{
			Path: ".gaos/packet/task.md", Kind: KindTask, Body: "# t-1\nDo the thing.\n",
		}},
	}
	c, _, err := Screen(EgressPolicy{}, Target{Provider: "anthropic"}, p)
	if err != nil {
		t.Fatalf("the test packet did not clear the egress pass: %v", err)
	}
	return c
}

// TestTheAdapterNeverReadsTheOutcomeFromTheExitCode is D-050, and it is the
// single most important test in this file.
//
// The measurement behind D-049 found that Claude Code separates a crash from a
// completed run but NOT A REFUSAL FROM A SUCCESS: a task the model could not do
// exits 0 with terminal_reason: completed, byte-identical at the process level
// to one it did.
func TestTheAdapterNeverReadsTheOutcomeFromTheExitCode(t *testing.T) {
	a, ws := fakeCLI(t, "blocked", "0")

	att, err := a.Dispatch(context.Background(), clearedFor(t, ws))
	if err != nil {
		t.Fatalf("dispatch failed: %v", err)
	}

	if att.ExitClass != ExitRan || att.ExitCode != 0 {
		t.Fatalf("the fake tool exited %d as class %q, want a clean 0/ran; the rest of this test proves nothing otherwise", att.ExitCode, att.ExitClass)
	}
	if got := att.Outcome(); got != HandoffBlocked {
		t.Fatalf("the tool exited %d and wrote status %q, and the attempt reports outcome %q — D-050: the exit code may classify crash versus ran and nothing else, because Claude Code exits 0 with terminal_reason: completed for a task it could not do", att.ExitCode, HandoffBlocked, got)
	}
	if att.Handoff == nil || len(att.Handoff.Blockers) == 0 {
		t.Fatal("the blocked handoff carried no blocker; the reason a task could not be done is the whole value of the refusal the exit code hid")
	}
}

// TestAMissingHandoffIsAFailedAttemptWithItsSignature is BR-30's other half:
// exit 0 and write nothing at all.
func TestAMissingHandoffIsAFailedAttemptWithItsSignature(t *testing.T) {
	a, ws := fakeCLI(t, "nohandoff", "0")

	att, err := a.Dispatch(context.Background(), clearedFor(t, ws))
	if err != nil {
		t.Fatalf("dispatch failed: %v", err)
	}
	if att.Outcome() != "failed" || att.Failure != SigHandoffMissing {
		t.Fatalf("the tool exited 0 and wrote no handoff document, and the attempt came back as %q with signature %q; BR-30 says missing or unparseable is a failed attempt with the signature handoff-missing, never a silent pass", att.Outcome(), att.Failure)
	}

	// Prose where front matter belongs is the same failure with the same
	// signature -- they are one failure to the retry loop.
	a2, ws2 := fakeCLI(t, "garbage", "0")
	att2, err := a2.Dispatch(context.Background(), clearedFor(t, ws2))
	if err != nil {
		t.Fatalf("dispatch failed: %v", err)
	}
	if att2.Failure != SigHandoffMissing {
		t.Fatalf("an unparseable handoff came back with signature %q, want %q; splitting the class would give the guard two counters where BR-17 specifies one", att2.Failure, SigHandoffMissing)
	}
}

// TestACrashStillReadsTheHandoffItLeftBehind. A run that was killed may have
// written its document first, and skipping the read because the exit looked bad
// is reading meaning into the exit code by omission.
func TestACrashStillReadsTheHandoffItLeftBehind(t *testing.T) {
	a, ws := fakeCLI(t, "blocked", "1")

	att, err := a.Dispatch(context.Background(), clearedFor(t, ws))
	if err != nil {
		t.Fatalf("dispatch failed: %v", err)
	}
	if att.Handoff == nil {
		t.Fatal("a non-zero exit skipped the handoff read; the document is the authority whatever the process did, and a run killed after writing it still said what happened")
	}
	if att.Outcome() != HandoffBlocked {
		t.Fatalf("outcome is %q, want %q read from the document rather than from exit code %d", att.Outcome(), HandoffBlocked, att.ExitCode)
	}
}

// TestTheAdapterRunsTheToolInTheWorkspaceWithAnIsolatedEnvironment is D-051's
// closing measurement: an adapter that does not isolate context makes the
// frozen slice a lie about what the model received.
func TestTheAdapterRunsTheToolInTheWorkspaceWithAnIsolatedEnvironment(t *testing.T) {
	a, ws := fakeCLI(t, "complete", "0")

	// A variable the host has and the child must not inherit.
	t.Setenv("GAOS_SECRET_SHOULD_NOT_LEAK", "sk-ant-api03-AAAAAAAAAAAAAAAAAAAAAA")

	if _, err := a.Dispatch(context.Background(), clearedFor(t, ws)); err != nil {
		t.Fatalf("dispatch failed: %v", err)
	}

	// The tool wrote its document into cwd, so the document's presence in the
	// workspace is the proof cwd was the workspace.
	if _, err := os.Stat(filepath.Join(ws, HandoffPath)); err != nil {
		t.Fatalf("the child ran somewhere other than the workspace %q: %v", ws, err)
	}

	env := isolatedEnv(a.SessionEnv)
	for _, kv := range env {
		if strings.HasPrefix(kv, "GAOS_SECRET_SHOULD_NOT_LEAK=") {
			t.Fatal("the child inherited a host variable nobody named; D-051 measured 24k cache-read tokens arriving this way, which makes the frozen slice a lie about what the model received")
		}
	}
	var sawNamed bool
	for _, kv := range env {
		if strings.HasPrefix(kv, "FAKE_CLI_MODE=") {
			sawNamed = true
		}
	}
	if !sawNamed {
		t.Fatal("a NAMED session variable did not reach the child; the tool cannot find its own session, and BR-29's arrangement is that GAOS forwards names rather than values it read")
	}
}

// TestTheAdapterReadsCostAndTokensFromTheToolsOwnReport is D-051.
func TestTheAdapterReadsCostAndTokensFromTheToolsOwnReport(t *testing.T) {
	a, ws := fakeCLI(t, "complete", "0")

	att, err := a.Dispatch(context.Background(), clearedFor(t, ws))
	if err != nil {
		t.Fatalf("dispatch failed: %v", err)
	}
	if att.CostBasis != CostMeasured || att.CostUSD == 0 {
		t.Fatalf("the tool reported total_cost_usd and the attempt carries %v/%q; D-051 says the first adapter reports, so the basis is measured from the first run", att.CostUSD, att.CostBasis)
	}
	// 184320 input + 24576 cache-read + 0 cache-creation.
	if att.TokensIn != 208896 || att.TokensOut != 9104 {
		t.Fatalf("tokens parsed as in=%d out=%d, want in=208896 out=9104; the cache-read count is real spend the operator was billed for, and dropping it under-reports every run", att.TokensIn, att.TokensOut)
	}

	// A tool that reports nothing is UNAVAILABLE, never estimated and never
	// zero: BR-37 and D-044 forbid reporting an unpriceable model as zero,
	// because a zero is a figure the budget guard will let through.
	a2, ws2 := fakeCLI(t, "silent", "0")
	att2, err := a2.Dispatch(context.Background(), clearedFor(t, ws2))
	if err != nil {
		t.Fatalf("dispatch failed: %v", err)
	}
	if att2.CostBasis != CostUnavailable {
		t.Fatalf("a run whose tool reported nothing came back as %q, want %q; BR-37 and D-044 forbid an unpriceable figure being reported as zero or dressed up as an estimate", att2.CostBasis, CostUnavailable)
	}
}

// TestTheAdapterRefusesAPacketTheEgressPassNeverCleared is the belt behind the
// type discipline's braces.
func TestTheAdapterRefusesAPacketTheEgressPassNeverCleared(t *testing.T) {
	a, _ := fakeCLI(t, "complete", "0")

	// The zero value: what a caller who bypassed Screen would have.
	if _, err := a.Dispatch(context.Background(), Cleared{}); err == nil {
		t.Fatal("the adapter dispatched a zero-value Cleared; BR-32 says nothing reaches a model provider without passing the egress check first, and a type is only most of that guarantee")
	}
}

// TestTheSmokeTestRunsTheRealBinaryAndComparesThePin closes the exec half that
// smoke_test.go deliberately leaves pure.
func TestTheSmokeTestRunsTheRealBinaryAndComparesThePin(t *testing.T) {
	a, _ := fakeCLI(t, "complete", "0")
	if err := a.Smoke(context.Background()); err != nil {
		t.Fatalf("the smoke test refused the pinned fake tool: %v", err)
	}

	// A pin the tool cannot satisfy must refuse, and it is the same code path
	// a real drift takes.
	pin, err := ParsePin(">=9.0.0")
	if err != nil {
		t.Fatal(err)
	}
	a.Pin = pin
	if err := a.Smoke(context.Background()); err == nil {
		t.Fatal("a tool outside the pin passed the boot smoke test; the pin is inert without it, which is exactly what D-049 said the obligation was")
	}
}

// TestNothingAboveTheAdapterBranchesOnTheInvocationMode is BR-33 made
// checkable rather than trusted.
//
// "The execution mode may be recorded and never branched on. Every component
// above the adapter behaves identically whether a subprocess or an HTTP request
// produced the result." TaskRun.invocation exists so the prohibition can be
// checked, and this is the check.
func TestNothingAboveTheAdapterBranchesOnTheInvocationMode(t *testing.T) {
	fset := token.NewFileSet()
	sources, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}

	for _, name := range sources {
		switch name {
		case "adapter.go", "adapter_cli.go", "adapter_test.go", "modelconfig.go",
			"adapter_aimanager.go", "aimanager_test.go": // the gateway adapter is the seam too
			continue // the seam itself, and the one place that selects it
		}
		f, perr := parser.ParseFile(fset, name, nil, 0)
		if perr != nil {
			t.Fatalf("parsing %s: %v", name, perr)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			var x ast.Expr
			switch e := n.(type) {
			case *ast.BinaryExpr:
				if e.Op != token.EQL && e.Op != token.NEQ {
					return true
				}
				if isInvocationConst(e.X) || isInvocationConst(e.Y) {
					x = e
				}
			case *ast.SwitchStmt:
				if e.Tag != nil && isInvocationField(e.Tag) {
					x = e.Tag
				}
			}
			if x != nil {
				pos := fset.Position(x.Pos())
				t.Errorf("%s:%d branches on the invocation mode outside the adapter; BR-33 says the mode may be recorded and never branched on, and TaskRun.invocation exists so that prohibition can be checked rather than trusted", name, pos.Line)
			}
			return true
		})
	}
}

func isInvocationConst(e ast.Expr) bool {
	id, ok := e.(*ast.Ident)
	return ok && (id.Name == "InvocationCLI" || id.Name == "InvocationAPI" || id.Name == "InvocationGateway")
}

func isInvocationField(e ast.Expr) bool {
	sel, ok := e.(*ast.SelectorExpr)
	return ok && sel.Sel != nil && sel.Sel.Name == "Invocation"
}
