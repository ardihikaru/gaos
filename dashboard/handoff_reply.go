package main

import (
	"fmt"
	"strings"
)

// The handoff travels in the reply. D-057, amending D-050 and BR-30.
//
// Through the gateway the model writes into a folder on another host, so GAOS
// cannot read a file it left. The contract therefore moves from "a declared
// path" to "a declared block": the reply must end with exactly one fenced
// block tagged gaos-handoff, holding the same front matter the file did.
// ExtractHandoff takes the LAST such block and runs it through the unchanged
// ParseHandoff, so the two contracts can never drift apart in what they
// accept.
//
// Two checks are new, and both exist because of D-056 (one session per task):
// in a resumed session the model can see, and repeat, the block it wrote for
// the previous attempt. So the block must echo THIS run's id, and two blocks
// that disagree about the run are a failure, not a choice.

// HandoffFence is the info string that marks the block.
const HandoffFence = "gaos-handoff"

// Failure signatures the gateway adds beside SigHandoffMissing. Each is one
// row of the error mapping in architecture/gateway.html.
const (
	// SigSessionLost: the gateway answered 410. BR-40: never a silent fresh
	// start; the next attempt opens a new session and says so.
	SigSessionLost = "session-lost"
	// SigTurnUnsaved: metadata.session_warning — the reply is valid but the
	// turn was not saved, so the session no longer matches the record.
	SigTurnUnsaved = "turn-unsaved"
	// SigGatewayFailed: a 5xx, a transport error, or a request the gateway
	// no longer knows. The exit class is crashed.
	SigGatewayFailed = "gateway-failed"
)

// handoffBlocks returns the body of every gaos-handoff fenced block in reply,
// in order. A block opens on a line that is exactly ```gaos-handoff and
// closes on the next line that is exactly ```; an unclosed block is ignored,
// because a truncated reply did not finish its handoff.
func handoffBlocks(reply string) []string {
	var out []string
	var cur []string
	in := false
	for _, raw := range strings.Split(reply, "\n") {
		line := strings.TrimRight(raw, " \t\r")
		switch {
		case !in && line == fence+HandoffFence:
			in, cur = true, nil
		case in && line == fence:
			out = append(out, strings.Join(cur, "\n"))
			in = false
		case in:
			cur = append(cur, raw)
		}
	}
	return out
}

// ExtractHandoff reads the handoff from a reply. It returns the parsed
// document and the raw block (stored as the handoff artifact, T-064), or an
// error carrying SigHandoffMissing.
func ExtractHandoff(reply, taskID, runID string) (Handoff, string, error) {
	blocks := handoffBlocks(reply)
	if len(blocks) == 0 {
		return Handoff{}, "", fmt.Errorf("%s: the reply has no %s block", SigHandoffMissing, fence+HandoffFence)
	}
	parsed := make([]Handoff, len(blocks))
	for i, b := range blocks {
		h, err := ParseHandoff([]byte(asFrontMatter(b)))
		if err != nil && i == len(blocks)-1 {
			return Handoff{}, b, err
		}
		parsed[i] = h
	}
	last := parsed[len(parsed)-1]
	for _, h := range parsed[:len(parsed)-1] {
		if h.Run != "" && h.Run != last.Run {
			return Handoff{}, blocks[len(blocks)-1], fmt.Errorf(
				"%s: the reply has %d %s blocks for different runs (%s and %s)",
				SigHandoffMissing, len(blocks), HandoffFence, h.Run, last.Run)
		}
	}
	if runID != "" && last.Run != runID {
		return Handoff{}, blocks[len(blocks)-1], fmt.Errorf(
			"%s: the handoff names run %q, and this run is %q", SigHandoffMissing, last.Run, runID)
	}
	if taskID != "" && last.Task != taskID {
		return Handoff{}, blocks[len(blocks)-1], fmt.Errorf(
			"%s: the handoff names task %q, and the brief named %q", SigHandoffMissing, last.Task, taskID)
	}
	return last, blocks[len(blocks)-1], nil
}

// asFrontMatter accepts the block with or without its --- lines, so a model
// that drops them because the fence already delimits the block is not failed
// for a formatting choice the contract does not need.
func asFrontMatter(b string) string {
	t := strings.TrimSpace(b)
	if strings.HasPrefix(t, "---") {
		return t + "\n"
	}
	return "---\n" + t + "\n---\n"
}

// gatewayContract is the reply contract, the gateway's counterpart of
// handoffContract. Same shape, same example, same four ways to fail — only
// the destination changes from a file to the end of the reply.
func gatewayContract(taskID, runID, role string) string {
	if role == "" {
		role = "backend"
	}
	return fmt.Sprintf(gatewayContractTemplate,
		HandoffFence, SigHandoffMissing,
		fence+HandoffFence+"\n"+handoffExample(taskID, runID, role)+"\n"+fence,
		runID, HandoffFence)
}

const gatewayContractTemplate = `# What this run must produce

End your reply with the handoff, as ONE fenced block tagged %s. There is no
file to write: GAOS reads the handoff from your reply and from nowhere else,
and it is the ONLY authority on what this run did. A reply without a readable
block is recorded as failed with the signature %q, whatever the work was.

## The block, complete, with the values this run must use

Copy it as the last thing in your reply, flush left, and change the values that
describe what you actually did:

%s

## The rules the parser enforces

- task, run, role, status and at least one criteria item are mandatory. Use the
  task and run values above exactly: the run is %s, and a block naming any other
  run is rejected.
- status is exactly one of: complete | blocked | partial. Blocked is a
  legitimate answer, and a better one than a plausible guess.
- Each criterion is one line: - id: <id>   met: true|false   evidence: "..."
- Write exactly one %s block. Prose before it is kept for the human reading
  the timeline; nothing after it is read.
`
