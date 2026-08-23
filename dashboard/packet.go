package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// The task packet on disk, and the contract the run must satisfy. M4.5:
// T-061 (requirement D-8), T-062 and T-063 (D-9, BR-30).
//
// Until this file existed, a real provider tool could be spawned and could not
// possibly succeed. It was handed a working directory with nothing in it and an
// instruction on stdin that never said what to produce, so it would do the work,
// exit 0, and fail the attempt with handoff-missing every single time. The two
// halves of that are the two halves of this file.
//
// D-8: "the frozen slice is materialised as a task packet of files in the leased
// workspace, and the packet -- not a prompt string -- is what the adapter hands
// over." The difference is a replay property: a prompt is something GAOS
// composed and would have to recompose identically, a packet is something it
// stored.
//
// D-9 and BR-30: "every invocation must produce a handoff document at a declared
// path in the declared shape." A contract nobody is told is not a contract, so
// the declaration travels IN the packet -- which also means it is inside the
// frozen slice and inside the digest, and a replay can prove what the run was
// asked for as well as what it was given.

// MaterialisePacket writes the cleared packet into the leased workspace and
// declares the handoff document empty.
//
// THE CLEARED PACKET, never the raw one, and that is the whole security
// property of this function. The workspace is the one directory the tool is
// pointed at and free to read; writing a part the egress pass redacted would put
// it on disk inside that directory, which is a longer way round to the leak
// BR-32 refuses at the front door.
//
// KindRepo parts are deliberately NOT written. They were read OFF this
// workspace by readWorkspace, so writing them back is at best a no-op and at
// worst the packet assembler overwriting the operator's own source with a copy
// that has been through a size limit. The files are already where the tool will
// look for them.
//
// Returns the paths it wrote, relative to the workspace, for the log line.
func MaterialisePacket(c Cleared) ([]string, error) {
	ws := c.Workspace()
	if ws == "" {
		return nil, fmt.Errorf("materialise: the cleared packet has no workspace")
	}
	if err := os.MkdirAll(ws, 0o755); err != nil {
		return nil, err
	}

	var written []string
	for _, part := range c.Packet().Parts {
		if part.Kind == KindRepo {
			continue
		}
		abs, err := insideWorkspace(ws, part.Path)
		if err != nil {
			return nil, err
		}
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			return nil, err
		}
		if err := os.WriteFile(abs, []byte(part.Body), 0o600); err != nil {
			return nil, err
		}
		written = append(written, part.Path)
	}

	// The handoff document, declared and empty. §7.6's packet layout has always
	// listed it and nothing created it; the adapter truncates it immediately
	// before the spawn, which is a different guarantee -- that covers a REUSED
	// workspace, this covers a fresh one, and a model told to write to a path
	// whose directory does not exist has been set up to fail.
	out, err := insideWorkspace(ws, HandoffPath)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		return nil, err
	}
	if err := os.WriteFile(out, nil, 0o600); err != nil {
		return nil, err
	}
	return append(written, HandoffPath), nil
}

// insideWorkspace resolves a packet path and refuses one that leaves.
//
// Screen already refuses an escaping part -- that is its step 1, and a refusal
// rather than a redaction because it means the assembler is wrong. This is the
// same check at the boundary that actually writes, and it is not redundant: the
// check that matters is the one next to the syscall, and a future caller that
// assembles a packet some other way meets this one whether or not it remembered
// the other.
func insideWorkspace(ws, rel string) (string, error) {
	clean := filepath.Clean(rel)
	if filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("materialise: packet part %q resolves outside the workspace", rel)
	}
	return filepath.Join(ws, clean), nil
}

// handoffContract is the text T-062 puts in the packet: what to produce, where,
// and in exactly what shape.
//
// Every value the parser treats as mandatory is filled in here rather than
// described. `task` and `run` are the ids GAOS will match the document against,
// and a model asked to "use the task id" will eventually use a different
// spelling of it; giving it the literal string removes the whole class.
//
// The example is deliberately a COMPLETE document rather than a schema. The
// contract is one closed shape with six keys (see handoff.go on why the parser
// is hand-written), and the cheapest way to communicate a closed shape is to
// show one filled in.
//
// It says the four ways to fail, because handoff.go collapses them into one
// signature and the reader of that signature cannot tell them apart afterwards.
func handoffContract(taskID, runID, role string) string {
	if role == "" {
		role = "backend"
	}
	return fmt.Sprintf(contractTemplate,
		HandoffPath, SigHandoffMissing,
		fence+"\n"+handoffExample(taskID, runID, role)+"\n"+fence,
		SigHandoffMissing)
}

// handoffExample is a COMPLETE document rather than a schema, and it is flush
// left rather than indented.
//
// Both of those are the parser's doing. The contract is one closed shape with
// six keys -- see handoff.go on why it is not YAML -- and the cheapest way to
// communicate a closed shape is to show one filled in. The indentation matters
// because handoff.go reads a leading space as "this key belongs to the open
// block": an example indented for looks, copied faithfully, parses to an empty
// document and fails validation. That would be a contract that punishes the
// models that follow it most carefully, so it goes in a fenced block instead.
//
// TestTheContractsOwnExampleParses is what keeps the two files honest about
// each other.
func handoffExample(taskID, runID, role string) string {
	return fmt.Sprintf(`---
task: %s
run: %s
role: %s
status: complete
criteria:
  - id: AC-1   met: true   evidence: "the file or test that proves it"
artifacts:
  - path: "src/thing.go"   kind: code
blockers:
  - "only when status is blocked: what needs a decision rather than a retry"
handoff:
  next_role: qa
  must_know: "one sentence the next agent would otherwise have to rediscover"
usage:
  tokens_in: 0
  tokens_out: 0
  basis: measured
---

Anything after the closing --- is free text and is ignored by the parser.`,
		taskID, runID, role)
}

const fence = "```"

// contractTemplate is the prose around the example. Held apart from
// handoffExample so the example can be handed to the parser on its own, which
// is exactly what the test does.
const contractTemplate = `# What this run must produce

Before you finish, write the handoff document to:

    %s

It is not optional and it is not prose. GAOS reads it as the ONLY authority on
what this run did -- the exit code is read for "crashed" versus "ran" and for
nothing else -- so an attempt that leaves no readable document at that path is
recorded as failed with the signature %q, whatever the work was.

## The shape, complete, with the values this run must use

Copy this, flush left, and change the values that describe what you actually
did. Do not indent the front matter: a leading space means "this key belongs to
the block above", and an indented document parses as an empty one.

%s

## The rules the parser enforces

- The document MUST start with --- and the front matter MUST close with ---.
- task, run, role, status and at least one criteria item are mandatory. The
  task and run values above are the ones GAOS will match against; use them
  exactly.
- status is exactly one of: complete | blocked | partial.
  - complete -- every acceptance criterion is met.
  - partial  -- real progress, something is left; say what in must_know.
  - blocked  -- it cannot be done as specified; put the reason in blockers.
    Blocked is a legitimate answer, and a better one than a plausible guess.
- Each criterion is one line: - id: <id>   met: true|false   evidence: "..."
- usage is optional; omit the whole block if you do not know the numbers. If you
  include basis, it is measured or estimated -- never a guess dressed up as a
  measurement.
- Extra keys are ignored, so add what you like. A MISSING mandatory key fails
  the whole attempt.

The four ways to fail are one failure to GAOS: no file, no front matter,
unparseable front matter, or a mandatory field absent. All four are recorded as
%s and nothing can tell them apart afterwards, so read the file back before you
stop.
`
