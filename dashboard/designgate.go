package main

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// The design gate. M17, T-184…T-188, D-068 and D-069.
//
// What this file is: the platform half of the Elephant-Goldfish Model, in Go.
// A design is written by a task of kind `design`, attacked by a task of kind
// `review` that holds nothing of the author's, and the implementation waits for
// the review to resolve.
//
// The one thing D-069 settled that everything else follows from: **the
// isolation boundary is the packet, not the workspace.** The reviewer's
// workspace is the product's own, holding the published Blueprint whole —
// because those standing files ARE the rubric, and a reviewer that cannot see
// the domain rules cannot judge a design against them. What is withheld is the
// author's session state, and it is withheld at the packet.
//
// The second thing D-069 settled: **the gate is the planner's own DEPENDS_ON
// edge.** The implement task depends on the review task, and the scheduler
// already holds a task whose dependency is not DONE (scheduler.go:363-376).
// No new task status, no new blocking reason, no new agent role.
//
// What this file deliberately does NOT do: screen what comes back. A reply is
// stored verbatim (runner.go:195-203) and this change does not alter that.
// D-069 clause 10 records the limit — the egress pass is a backstop, not the
// cure, and inbound screening is separate work.

// ---------------------------------------------------------------- the kinds

// The task kinds. Three values, closed, and the same shape as the DDL regexp
// in 14-design-gate.sql.
//
// `implement` is the default and the answer for every task that is neither a
// design nor a review, so a Task written before this migration (no kind at
// all) is an implement task.
const (
	TaskKindImplement = "implement"
	TaskKindDesign    = "design"
	TaskKindReview    = "review"
)

// taskKind normalises a task's kind.
//
// An absent kind is an implement task: a task planned before the migration has
// no kind, and the alternative is a task the planner and the scheduler
// disagree about.
//
// An UNKNOWN kind is NOT an implement task. It fails closed: the fail-open
// direction here is contamination, not silence — an unrecognised kind would
// take the implement branch, which is the branch that carries the previous
// handoff, the failure lesson and the operator's answer.
func taskKind(kind string) string {
	switch kind {
	case TaskKindDesign:
		return TaskKindDesign
	case TaskKindReview:
		return TaskKindReview
	case TaskKindImplement, "":
		// An ABSENT kind is an implement task: a Task planned before the
		// migration has no kind, and the alternative is a task the planner and
		// the scheduler disagree about.
		return TaskKindImplement
	default:
		// An UNKNOWN kind is neither. It is not an implement task, because
		// the fail-open direction here is contamination, and it is not a
		// review, because nothing says it was ever gated.
		return ""
	}
}

// knownTaskKind reports whether a kind is one of the three. A task whose kind
// is not known is refused at dispatch rather than treated as an implement
// task.
func knownTaskKind(kind string) bool {
	return kind == "" || kind == TaskKindImplement || kind == TaskKindDesign || kind == TaskKindReview
}

// The packet part kinds this milestone adds.
//
// KindDesign is the only one: the findings travel back as KindContext, which
// already exists, and the rubric travels as KindRules, which already exists.
//
// The two reply contracts, and the fences that mark them. The handoff's is
// `gaos-handoff` (handoff_reply.go:24); these follow the same shape.
const (
	DesignFence = "gaos-design"
	ReviewFence = "gaos-review"
)

// The failure signatures the two new contracts can add. Each is one row of the
// error mapping, beside the ones gateway_contract already knows.
const (
	// SigDesignMissing: a design task's reply carried no readable design
	// block. The attempt failed, exactly as a handoff-less attempt fails.
	SigDesignMissing = "design-missing"
	// SigReviewMissing: a review task's reply carried no readable review
	// block. A failed review is not a verdict (D-068 clause 7).
	SigReviewMissing = "review-missing"
)

// ---------------------------------------------------------------- the design

// Design is a parsed gaos-design block: the identity the parser checks, and
// the document the reviewer is given, verbatim.
type Design struct {
	Task string
	Run  string
	Role string

	// Document is the text after the front matter. It is never re-rendered:
	// the reviewer must see what the designer wrote, not GAOS's parse of it.
	Document string
}

// ExtractDesign reads the design out of a reply. It takes the LAST gaos-design
// block, and refuses a block that names another task or another run.
//
// The run check is not pedantry: D-056 makes a retry resume its session, so
// the model can see and repeat the block it wrote for the previous attempt.
// The handoff contract guards the same way (handoff_reply.go:87-94), and a
// design artifact that silently came from the previous attempt would be a
// design nobody re-read.
func ExtractDesign(reply, taskID, runID string) (Design, error) {
	blocks := fenceBlocks(reply, DesignFence)
	if len(blocks) == 0 {
		return Design{}, fmt.Errorf("%s: the reply has no %s block", SigDesignMissing, fence+DesignFence)
	}
	last := blocks[len(blocks)-1]
	d, err := ParseDesign([]byte(last))
	if err != nil {
		return Design{}, err
	}
	if runID != "" && d.Run != runID {
		return Design{}, fmt.Errorf("%s: the design names run %q, and this run is %q", SigDesignMissing, d.Run, runID)
	}
	if taskID != "" && d.Task != taskID {
		return Design{}, fmt.Errorf("%s: the design names task %q, and the brief named %q", SigDesignMissing, d.Task, taskID)
	}
	return d, nil
}

// ParseDesign reads one design block: front matter, then the document.
//
// The front matter is mandatory and carries the identity. The document is
// everything after it, so a document may contain `---` of its own (a
// horizontal rule, a YAML block) without confusing the parser: only the
// FIRST closing `---` ends the front matter.
//
// A hand-written scanner, not a general front-matter parser, for the same
// reason handoff.go has one: the shape is closed, and a general parser would
// accept documents the contract does not define.
func ParseDesign(b []byte) (Design, error) {
	fm, body, ok := splitBlock(string(b))
	if !ok {
		return Design{}, fmt.Errorf("%s: no `---` front matter block", SigDesignMissing)
	}
	d := Design{Document: body}
	for _, raw := range strings.Split(fm, "\n") {
		key, val, found := strings.Cut(strings.TrimSpace(stripComment(raw)), ":")
		if !found {
			continue
		}
		switch strings.TrimSpace(key) {
		case "task":
			d.Task = unquote(strings.TrimSpace(val))
		case "run":
			d.Run = unquote(strings.TrimSpace(val))
		case "role":
			d.Role = unquote(strings.TrimSpace(val))
		}
	}
	var missing []string
	if d.Task == "" {
		missing = append(missing, "task")
	}
	if d.Run == "" {
		missing = append(missing, "run")
	}
	if len(missing) > 0 {
		return Design{}, fmt.Errorf("%s: mandatory field(s) absent: %s", SigDesignMissing, strings.Join(missing, ", "))
	}
	if strings.TrimSpace(d.Document) == "" {
		return Design{}, fmt.Errorf("%s: the design carries no document", SigDesignMissing)
	}
	// A design document that quotes a contract example would put a second
	// block of a reserved tag inside the reply, and the last one wins. The
	// design body is refused rather than the quote honoured.
	if tag := reservedFenceIn(d.Document); tag != "" {
		return Design{}, fmt.Errorf("%s: the design document carries a %s block, which is reserved", SigDesignMissing, fence+tag)
	}
	return d, nil
}

// ---------------------------------------------------------------- the review

// The severities a finding may carry: the three labels the general lenses
// use. `blocker` is the one that holds the gate.
const (
	SeverityBlocker = "blocker"
	SeverityMajor   = "major"
	SeverityNit     = "nit"
)

// The verdict. DERIVED from the findings, never declared by the reviewer: a
// declared verdict can disagree with the findings it sits next to, and then two
// things are true at once. It is RECORDED on the review artifact, so the
// register can assert it.
const (
	VerdictAccept  = "accept"
	VerdictIterate = "iterate"
)

// ReviewFinding is one finding: severity, scope and note.
//
// There is no disposition field, and that is deliberate. The four dispositions
// (docs/rules/gaos-design-review.md) are the OPERATOR's triage of a reviewer's
// finding; a reviewer contesting its own finding is nonsense, and the gate
// counts blockers without them.
//
// The note is one bounded line saying what is wrong. A finding that carried
// only a location would tell the implementer where to look and nothing else,
// and the note is the reviewer's own words rather than the text it complains
// about: a finding records WHERE, and the note says why, without quoting.
type ReviewFinding struct {
	Severity string
	Scope    string
	Note     string
}

// Review is a parsed gaos-review block.
type Review struct {
	Task       string
	Run        string
	Role       string
	NoFindings bool
	Findings   []ReviewFinding
}

// OpenBlockers counts the findings that hold the gate.
func (r Review) OpenBlockers() int {
	n := 0
	for _, f := range r.Findings {
		if f.Severity == SeverityBlocker {
			n++
		}
	}
	return n
}

// Verdict is the review's outcome, derived: an open Blocker means iterate.
func (r Review) Verdict() string {
	if r.OpenBlockers() > 0 {
		return VerdictIterate
	}
	return VerdictAccept
}

// ExtractReview reads the review out of a reply. It takes the LAST
// gaos-review block and refuses a block that names another task or another
// run, for the same reason ExtractDesign does.
func ExtractReview(reply, taskID, runID string) (Review, error) {
	blocks := fenceBlocks(reply, ReviewFence)
	if len(blocks) == 0 {
		return Review{}, fmt.Errorf("%s: the reply has no %s block", SigReviewMissing, fence+ReviewFence)
	}
	last := blocks[len(blocks)-1]
	r, err := ParseReview([]byte(last))
	if err != nil {
		return Review{}, err
	}
	if runID != "" && r.Run != runID {
		return Review{}, fmt.Errorf("%s: the review names run %q, and this run is %q", SigReviewMissing, r.Run, runID)
	}
	if taskID != "" && r.Task != taskID {
		return Review{}, fmt.Errorf("%s: the review names task %q, and the brief named %q", SigReviewMissing, r.Task, taskID)
	}
	return r, nil
}

// ParseReview reads one review block.
//
// The `findings:` key is mandatory and may be empty: a review that found
// nothing is a legitimate answer, and the empty list is how it says so. But
// an empty list must ALSO carry `no-findings: true`, because a review that
// found nothing and a review block the model did not finish are otherwise the
// same reply — and the second one must not open the gate.
func ParseReview(b []byte) (Review, error) {
	fm, _, ok := splitBlock(string(b))
	if !ok {
		return Review{}, fmt.Errorf("%s: no `---` front matter block", SigReviewMissing)
	}
	var r Review
	section := ""
	sawFindings := false
	for _, raw := range strings.Split(fm, "\n") {
		line := strings.TrimRight(raw, " \t\r")
		if line == "" || strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		if strings.HasPrefix(strings.TrimSpace(line), "- ") {
			if section != "findings" {
				return Review{}, fmt.Errorf("%s: list item outside the findings block: %q", SigReviewMissing, strings.TrimSpace(line))
			}
			item := strings.TrimSpace(strings.TrimSpace(line)[2:])
			f := ReviewFinding{
				Severity: unquote(inlineField(item, "severity")),
				Scope:    unquote(inlineField(item, "scope")),
				Note:     unquote(inlineField(item, "note")),
			}
			if f.Severity == "" {
				return Review{}, fmt.Errorf("%s: a finding has no severity: %q", SigReviewMissing, item)
			}
			switch f.Severity {
			case SeverityBlocker, SeverityMajor, SeverityNit:
			default:
				return Review{}, fmt.Errorf("%s: severity %q is not one of blocker|major|nit", SigReviewMissing, f.Severity)
			}
			if !validScope(f.Scope) {
				return Review{}, fmt.Errorf("%s: %q is not a scope: a path, an optional :line, and nothing else", SigReviewMissing, f.Scope)
			}
			if f.Note == "" {
				return Review{}, fmt.Errorf("%s: a finding has no note: %q", SigReviewMissing, item)
			}
			// The note is the free-text channel into the implementer's prompt,
			// so it is held to the same standard as the scope: one bounded line,
			// no control characters, and no reserved fence a reviewer could use
			// to smuggle a contract block into a reply.
			if len([]rune(f.Note)) > 300 || strings.ContainsAny(f.Note, "\n\r\x00") {
				return Review{}, fmt.Errorf("%s: the note of a finding is one bounded line", SigReviewMissing)
			}
			if tag := reservedFenceIn(f.Note); tag != "" {
				return Review{}, fmt.Errorf("%s: the note of a finding carries a %s block, which is reserved",
					SigReviewMissing, fence+tag)
			}
			r.Findings = append(r.Findings, f)
			continue
		}
		key, val, found := strings.Cut(strings.TrimSpace(stripComment(line)), ":")
		if !found {
			return Review{}, fmt.Errorf("%s: line is neither a key nor a list item: %q", SigReviewMissing, line)
		}
		key = strings.TrimSpace(key)
		val = unquote(strings.TrimSpace(val))
		switch key {
		case "task":
			r.Task, section = val, ""
		case "run":
			r.Run, section = val, ""
		case "role":
			r.Role, section = val, ""
		case "no-findings":
			if val != "true" {
				return Review{}, fmt.Errorf("%s: no-findings is %q, and it is either true or absent", SigReviewMissing, val)
			}
			r.NoFindings = true
			section = ""
		case "findings":
			section, sawFindings = "findings", true
		default:
			section = ""
		}
	}
	var missing []string
	if r.Task == "" {
		missing = append(missing, "task")
	}
	if r.Run == "" {
		missing = append(missing, "run")
	}
	if !sawFindings {
		missing = append(missing, "findings")
	}
	if len(missing) > 0 {
		return Review{}, fmt.Errorf("%s: mandatory field(s) absent: %s", SigReviewMissing, strings.Join(missing, ", "))
	}
	if len(r.Findings) == 0 && !r.NoFindings {
		return Review{}, fmt.Errorf("%s: the findings are empty and no-findings is not true; an empty list must say it", SigReviewMissing)
	}
	return r, nil
}

// validScope checks a finding's scope: a path, an optional `:line`, and
// nothing else.
//
// The scope is persisted in the review artifact and forwarded into the
// implementer's prompt, so it is the one free-text field a steered reviewer
// could use to carry prose. A path must look like a path -- it must have an
// extension or a directory separator -- and a line must be a number.
func validScope(scope string) bool {
	if scope == "" || len([]rune(scope)) > 200 {
		return false
	}
	if strings.ContainsAny(scope, "\n\r\t") {
		return false
	}
	if strings.ContainsAny(scope, " \t") {
		return false
	}
	path, line := scope, ""
	if i := strings.LastIndex(scope, ":"); i >= 0 {
		path, line = scope[:i], scope[i+1:]
		if line == "" {
			return false
		}
		if strings.Contains(path, ":") {
			// A Windows drive letter or a second segment: not a path:line.
			return false
		}
		n, err := strconv.Atoi(line)
		if err != nil || n <= 0 {
			return false
		}
	}
	if path == "" || strings.HasPrefix(path, "/") {
		return false
	}
	// No traversal: the scope names a place in the tree, and a scope that can
	// climb out of it is not a place in the tree.
	if strings.Contains(path, "..") {
		return false
	}
	// A path, not a sentence: it has an extension or it is in a directory.
	if !strings.Contains(path, "/") && !strings.Contains(path, ".") {
		return false
	}
	return true
}

// ---------------------------------------------------------------- the gate

// The gate's reasons, and the rules the moves carry.
//
// The reason is written on the task's own record (TaskRecord.BlockReason);
// the rule is the actor, and it travels in the TaskMoved event's Rule
// (runner.go:382-383). They are different things and both are needed: the
// reason says what is holding the task, the rule says who acted.
const (
	ReasonDesignUnreviewed = "design-unreviewed"
	ReasonDesignBlocked    = "design-blocked"
	ReasonReviewFailed     = "review-failed"
	ReasonDesignMissing    = "design-missing"
	ReasonDesignScreened   = "design-screened-out"
	ReasonReviewScreened   = "review-screened-out"

	RuleDesignDelivered = "design-delivered"
	RuleReviewCleared   = "review-cleared"
	RuleDesignBlocked   = "design-blocked"
	RuleReviewFailed    = "review-failed"
	RuleGateUnwired     = "gate-unwired"
	RuleDesignScreened  = "design-screened"
	RuleReviewScreened  = "review-screened"
)

// The gate's state on a task that depends on a review.
const (
	GateUnreviewed = "unreviewed"
	GateCleared    = "cleared"
	GateBlocked    = "blocked"
	GateFailed     = "failed"
)

// gateState is the gate's verdict on a task, read off the graph.
//
// It is a pure function of the task's dependencies and their statuses, so
// two replicas reading the same graph reach the same answer -- the same
// property the scheduler has (D-1).
func (s *MemStore) gateStateLocked(taskID string) (string, bool) {
	t := s.taskByIDLocked(taskID)
	if t == nil {
		return "", false
	}
	reviewID := s.gateReviewDepLocked(t)
	if reviewID == "" {
		return "", false
	}
	review := s.taskByIDLocked(reviewID)
	if review == nil {
		return GateUnreviewed, true
	}
	switch review.Status {
	case StatusDone:
		// The gate's OWN record, not the task's status. An operator can
		// approve a card, and an approval is not a verdict: a review task that
		// reached DONE without a recorded finding is a review that never ran,
		// and the implementation stays held.
		if art, ok := s.reviewArtifactFor(reviewID); ok &&
			strings.Contains(art.Body, "verdict: "+VerdictAccept) {
			return GateCleared, true
		}
		return GateFailed, true
	case StatusBlocked:
		if review.BlockReason == ReasonReviewFailed {
			return GateFailed, true
		}
		return GateBlocked, true
	default:
		return GateUnreviewed, true
	}
}

// gateReviewDepLocked is the review task an implementation task depends on.
//
// "The review task the implementation depends on" is found by the DEPENDS_ON
// edge, not by a field: the edge is the gate (D-069 clause 7), and a second
// carrier could disagree with it.
func (s *MemStore) gateReviewDepLocked(t *TaskRecord) string {
	for _, dep := range t.Deps {
		if d := s.taskByIDLocked(dep); d != nil && taskKind(d.Kind) == TaskKindReview {
			return dep
		}
	}
	return ""
}

// gateDesignDepLocked is the design task a review task depends on.
func (s *MemStore) gateDesignDepLocked(t *TaskRecord) string {
	for _, dep := range t.Deps {
		if d := s.taskByIDLocked(dep); d != nil && taskKind(d.Kind) == TaskKindDesign {
			return dep
		}
	}
	return ""
}

// taskByIDLocked finds one task. Caller holds the lock.
func (s *MemStore) taskByIDLocked(id string) *TaskRecord {
	for i := range s.tasks {
		if s.tasks[i].ID == id {
			return &s.tasks[i]
		}
	}
	return nil
}

// taskIsKindLocked reports whether a task is of one kind. Caller holds the
// lock.
func (s *MemStore) taskIsKindLocked(id, kind string) bool {
	t := s.taskByIDLocked(id)
	return t != nil && taskKind(t.Kind) == kind
}

// designArtifactFor is the design a task produced: the artifact of kind
// `design` whose task id is the design task's.
//
// The artifact is found by the task, not by a field on the review, so the
// review and the design are joined by the same DEPENDS_ON edge the gate uses.
func (s *MemStore) designArtifactFor(taskID string) (ArtifactRecord, bool) {
	return s.artifactOfKindFor(taskID, ArtifactDesign)
}

// reviewArtifactFor is the gate's record of what the reviewer found.
func (s *MemStore) reviewArtifactFor(taskID string) (ArtifactRecord, bool) {
	return s.artifactOfKindFor(taskID, ArtifactReview)
}

// artifactOfKindFor is the newest artifact of one kind produced by one task.
func (s *MemStore) artifactOfKindFor(taskID, kind string) (ArtifactRecord, bool) {
	var best ArtifactRecord
	found := false
	for _, a := range s.artifacts {
		if a.TaskID != taskID || a.Kind != kind {
			continue
		}
		if !found || a.CreatedAt > best.CreatedAt || (a.CreatedAt == best.CreatedAt && a.ID > best.ID) {
			best, found = a, true
		}
	}
	return best, found
}

// renderReviewArtifact is the gate's record, as Markdown.
//
// The verdict is derived and the findings carry where and why -- never the
// matched text, which is the rule egress.go states for its own findings
// (egress.go:222-238). The drift line names the Blueprint version the design
// was written under when it is not the version the review runs under.
func renderReviewArtifact(taskID, runID string, r Review, designPin, reviewPin int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Design review — %s\n\n", taskID)
	fmt.Fprintf(&b, "verdict: %s\n", r.Verdict())
	fmt.Fprintf(&b, "open blockers: %d\n", r.OpenBlockers())
	if designPin > 0 && reviewPin > 0 && designPin != reviewPin {
		fmt.Fprintf(&b, "drift: this design was written under Blueprint version %d; the review runs under version %d\n",
			designPin, reviewPin)
	}
	if len(r.Findings) == 0 {
		b.WriteString("\nno findings\n")
		return b.String()
	}
	b.WriteString("\n")
	for _, f := range r.Findings {
		fmt.Fprintf(&b, "- %s  %s  %s\n", f.Severity, f.Scope, f.Note)
	}
	return b.String()
}

// ------------------------------------------------------------ the shared bits

// reservedFenceIn reports the first reserved contract fence inside a body.
//
// A design document that quotes a contract example would nest a block of a
// reserved tag inside the reply, and the last block of that tag wins. The
// design body is refused rather than the quote honoured.
func reservedFenceIn(body string) string {
	for _, tag := range []string{HandoffFence, DesignFence, ReviewFence} {
		if strings.Contains(body, fence+tag) {
			return tag
		}
	}
	return ""
}

// splitBlock splits a block into its front matter and the text after it.
//
// The front matter is the text between the opening `---` and the FIRST closing
// `---`; everything after that closing line is the body. The body may contain
// `---` of its own, because only the first one ends the front matter.
//
// It is frontMatter (handoff.go:237) with the offset it threw away, because
// the design block needs what comes AFTER the front matter and frontMatter
// only answers what is inside it.
func splitBlock(s string) (fm, body string, ok bool) {
	s = strings.TrimLeft(s, "\ufeff \t\r\n")
	if !strings.HasPrefix(s, "---") {
		return "", "", false
	}
	rest := s[3:]
	if i := strings.IndexByte(rest, '\n'); i >= 0 {
		rest = rest[i+1:]
	} else {
		return "", "", false
	}
	parts := strings.Split(rest, "\n")
	for i, line := range parts {
		if strings.TrimSpace(line) == "---" {
			return strings.Join(parts[:i], "\n"), strings.Join(parts[i+1:], "\n"), true
		}
	}
	return "", "", false
}

// ------------------------------------------------------------ the gate's acts

// gateOutcome is the gate's decision about a finished task: where it goes,
// the rule that says who acted, and the reason written on the record.
type gateOutcome struct {
	to     string
	rule   string
	reason string
}

// gateVerdict is the gate's decision about a finished design or review task.
//
// It returns false for every other kind, because every other task keeps
// today's behaviour: its run ends in REVIEW and the operator approves it.
//
// Caller holds the lock.
func (s *MemStore) gateVerdict(taskID, runID string, att Attempt) (gateOutcome, bool) {
	t := s.taskByIDLocked(taskID)
	if t == nil {
		return gateOutcome{}, false
	}
	switch taskKind(t.Kind) {
	case TaskKindDesign:
		d, err := ExtractDesign(att.Reply, taskID, runID)
		if err != nil {
			// No design document. The attempt's own failure handling says
			// FAILED; the gate has nothing to add.
			return gateOutcome{}, false
		}
		// Screened BEFORE it is recorded. The egress pass screens what leaves;
		// an artifact is what stays, and the store is append-only (BR-04), so a
		// credential in a design document would be written permanently and the
		// refusal would come afterwards -- when the reviewer has already been
		// refused the design.
		if !s.screenArtifactBody(".gaos/packet/design.md", d.Document) {
			// A refusal is a RECORDED outcome, not a silent no-op. A design
			// the screening refused leaves no artifact, and a task with no
			// artifact and no reason looks exactly like a task that finished.
			return gateOutcome{to: StatusBlocked, rule: RuleDesignScreened, reason: ReasonDesignScreened}, true
		}
		s.recordHandoff(ArtifactRecord{
			ID: runID + "-design", Kind: ArtifactDesign,
			Title: "design of " + runID, TaskID: taskID, DeskID: t.DeskID,
			RunID: runID, Attempt: attemptOfRun(s.runs, runID),
			Body: d.Document, CreatedAt: time.Now().Unix(),
		})
		return gateOutcome{to: StatusDone, rule: RuleDesignDelivered}, true

	case TaskKindReview:
		// A review with no design has nothing to attack. If the design task
		// produced no artifact, the reviewer was handed the rubric and a
		// contract telling it to attack a document that is not there, and an
		// empty finding list would clear the gate on nothing.
		designID := ""
		if rt := s.taskByIDLocked(taskID); rt != nil {
			designID = s.gateDesignDepLocked(rt)
		}
		if designID == "" {
			return gateOutcome{to: StatusBlocked, rule: RuleReviewFailed, reason: ReasonDesignMissing}, true
		}
		if _, ok := s.designArtifactFor(designID); !ok {
			return gateOutcome{to: StatusBlocked, rule: RuleReviewFailed, reason: ReasonDesignMissing}, true
		}
		r, err := ExtractReview(att.Reply, taskID, runID)
		if err != nil {
			// A failed review is not a verdict (D-068 clause 7): the task goes
			// to BLOCKED with reason review-failed, and it consumes no retry,
			// no escalation and no failure signature (BR-34). The operator's
			// retry is the only thing that counts one.
			return gateOutcome{to: StatusBlocked, rule: RuleReviewFailed, reason: ReasonReviewFailed}, true
		}
		designPin, reviewPin := s.blueprintPinsLocked(taskID)
		body := renderReviewArtifact(taskID, runID, r, designPin, reviewPin)
		if !s.screenArtifactBody(".gaos/packet/context/findings.md", body) {
			return gateOutcome{to: StatusBlocked, rule: RuleReviewScreened, reason: ReasonReviewScreened}, true
		}
		s.recordHandoff(ArtifactRecord{
			ID: runID + "-review", Kind: ArtifactReview,
			Title: "review of " + runID, TaskID: taskID, DeskID: t.DeskID,
			RunID: runID, Attempt: attemptOfRun(s.runs, runID),
			Body: body, CreatedAt: time.Now().Unix(),
		})
		if r.OpenBlockers() > 0 {
			return gateOutcome{to: StatusBlocked, rule: RuleDesignBlocked, reason: ReasonDesignBlocked}, true
		}
		return gateOutcome{to: StatusDone, rule: RuleReviewCleared}, true
	}
	return gateOutcome{}, false
}

// gateReleaseLocked moves the implementation tasks the gate is holding.
//
// A review that clears releases them to READY, so the scheduler may dispatch
// them and their next packet carries the findings. A review that blocks leaves
// them BLOCKED, and rewrites the reason -- the reason is a field, and a
// same-status move is refused (runner.go:379), so it is written on the record
// rather than moved.
//
// Caller holds the lock.
func (s *MemStore) gateReleaseLocked(reviewTaskID, reviewTo string) []Event {
	var out []Event
	// The release is conditioned on the GATE'S OWN STATE, not on the gate's
	// intention and not on the review task's status alone. A review whose move
	// was refused -- FAILED, say -- has not resolved, and an implementation
	// released on the strength of an intention is an implementation that ran
	// with no review. `gateStateLocked` is the single reading of the gate, and
	// it reads the review's RECORD: a review task that reached DONE without a
	// recorded finding is a review that never ran.
	for i := range s.tasks {
		t := &s.tasks[i]
		if taskKind(t.Kind) != TaskKindImplement {
			continue
		}
		if s.gateReviewDepLocked(t) != reviewTaskID {
			continue
		}
		// The gate's own reading of THIS task's gate, which is the only
		// reading that knows both the review's status and the record the
		// gate itself wrote.
		state, _ := s.gateStateLocked(t.ID)
		switch state {
		case GateCleared:
			if m, ok := s.moveTaskLocked(t.ID, StatusReady, RuleReviewCleared); ok {
				out = append(out, m)
			}
			t.BlockReason = ""
		case GateUnreviewed:
			t.BlockReason = ReasonDesignUnreviewed
		default:
			t.BlockReason = ReasonDesignBlocked
		}
	}
	return out
}

// blueprintPinsLocked is the Blueprint version a design was written under and
// the one the review runs under.
//
// The pin is not a new field: the run carries it (runner.go:114) and the
// artifact carries its RunID, so it is a join. The drift report compares the
// two -- a design written under a superseded Blueprint is a design whose rules
// have moved under it.
//
// Caller holds the lock.
func (s *MemStore) blueprintPinsLocked(reviewTaskID string) (designPin, reviewPin int) {
	t := s.taskByIDLocked(reviewTaskID)
	if t == nil {
		return 0, 0
	}
	if designID := s.gateDesignDepLocked(t); designID != "" {
		if art, ok := s.designArtifactFor(designID); ok {
			designPin = s.blueprintPinForRunLocked(art.RunID)
		}
	}
	for _, r := range s.runs {
		if r.TaskID == reviewTaskID && r.BlueprintVersion > 0 {
			reviewPin = r.BlueprintVersion
			break
		}
	}
	return designPin, reviewPin
}

// blueprintPinForRunLocked is the Blueprint version a run was pinned to.
func (s *MemStore) blueprintPinForRunLocked(runID string) int {
	for _, r := range s.runs {
		if r.ID == runID {
			return r.BlueprintVersion
		}
	}
	return 0
}

// screenArtifactBody runs the egress detectors over a body before it is
// recorded.
//
// The egress pass screens what LEAVES the host. An artifact is what STAYS, and
// the store is append-only (BR-04), so a credential in a design document would
// be written permanently and the refusal would come afterwards -- when the
// reviewer has already been refused the design, and the credential is already
// in the record whose whole purpose is to prove nothing leaked.
//
// The detectors are the same ones the egress pass arms; only the timing
// differs.
func (s *MemStore) screenArtifactBody(path, body string) bool {
	return len(reportFindings(s.egress, []PacketPart{{Path: path, Body: body}})) == 0
}

// gateHoldsLocked reports whether the design gate is holding a task, and the
// review it is waiting for.
//
// It is the board's answer to "may somebody unblock this by hand?", and the
// answer for a gated task is no until the review has resolved: the gate
// releases the task because the review cleared, and a card moved by hand
// releases it for no reason at all.
//
// Caller holds the lock.
func (s *MemStore) gateHoldsLocked(t *TaskRecord) (bool, string) {
	if t == nil || taskKind(t.Kind) != TaskKindImplement {
		return false, ""
	}
	reviewID := s.gateReviewDepLocked(t)
	if reviewID == "" {
		return false, ""
	}
	review := s.taskByIDLocked(reviewID)
	if review == nil || review.Status == StatusDone {
		return false, ""
	}
	return true, reviewID
}

// unmetDepsLocked lists the prerequisites of a task that are not DONE.
//
// It is the same rule the scheduler applies (scheduler.go:363-376), read
// through one helper, so the scheduler's route and the operator's route cannot
// disagree about what "waiting" means.
//
// Caller holds the lock.
func (s *MemStore) unmetDepsLocked(taskID string) []string {
	t := s.taskByIDLocked(taskID)
	if t == nil {
		return nil
	}
	status := make(map[string]string, len(s.tasks))
	for i := range s.tasks {
		status[s.tasks[i].ID] = s.tasks[i].Status
	}
	return unmetDeps(TaskNode{ID: t.ID, Deps: t.Deps}, status)
}
