package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// The handoff document. BR-30, D-9, and -- since D-050 -- the only authority on
// what an invocation actually did.
//
// BRD §7.6 calls it a contract rather than a convention: "an agent that writes
// prose where a parser expects a field has failed the attempt." The declared
// shape, from the same section:
//
//	--- # front matter: machine-read, all fields mandatory
//	task:        T-1042
//	run:         R-1042-2
//	role:        backend
//	status:      complete | blocked | partial
//	criteria:
//	  - id: AC-7   met: true   evidence: "tests/pricing_test.go::TestRounding"
//	artifacts:
//	  - path: "src/pricing.go"     kind: code
//	blockers:
//	  - "AC-8 contradicts domain rule DR-3; needs a decision, not a retry"
//	handoff:
//	  next_role:  qa
//	  must_know:  "Rounding is half-even by DR-3."
//	usage:                       # omit entirely if the invocation reported nothing
//	  tokens_in:  184320
//	  tokens_out: 9104
//	  basis:      measured | estimated
//	---
//
// WHY A HAND-WRITTEN SCANNER RATHER THAN A YAML LIBRARY, and the reason is not
// "stdlib only": this is not YAML. It is one closed document shape with six
// keys, two lists of inline key/value records and one optional block. A general
// parser would accept documents the contract does not define, which is the
// opposite of what BR-30 asks for -- and every document it accepted that GAOS
// did not mean would be a handoff shaped around one tool's output format, which
// §7.6.3 names as the first step in losing two-mode interchangeability.

// The three statuses the contract permits.
const (
	HandoffComplete = "complete"
	HandoffBlocked  = "blocked"
	HandoffPartial  = "partial"
)

// HandoffPath is where the document must be, relative to the workspace. It is
// declared and empty before the invocation starts, per the §7.6 packet layout.
const HandoffPath = ".gaos/handoff/OUTPUT.md"

type HandoffCriterion struct {
	ID       string
	Met      bool
	Evidence string
}

type HandoffArtifact struct {
	Path string
	Kind string
}

type HandoffUsage struct {
	TokensIn  int64
	TokensOut int64
	Basis     string
}

type Handoff struct {
	Task     string
	Run      string
	Role     string
	Status   string
	Criteria []HandoffCriterion

	Artifacts []HandoffArtifact
	Blockers  []string
	NextRole  string
	MustKnow  string

	// Usage is nil when the invocation reported nothing. §7.6 says to omit the
	// block entirely in that case, so nil is a legal document and not an error
	// -- it means costBasis is unavailable, never estimated.
	Usage *HandoffUsage
}

// Met reports whether every acceptance criterion was satisfied. Nothing in this
// package calls it yet; the reviewer of M5 will.
func (h Handoff) Met() bool {
	for _, c := range h.Criteria {
		if !c.Met {
			return false
		}
	}
	return len(h.Criteria) > 0
}

// ReadHandoff is the one function in this file that touches a disk.
//
// Absent, no front matter, unparseable, or missing a mandatory field: all four
// are ONE failure with ONE signature (BR-30). They are one failure because they
// are one failure to the retry loop -- a second signature would split a class
// the guard treats as a single thing, and the guard is what makes the loop
// terminate.
func ReadHandoff(workspace string) (Handoff, error) {
	b, err := os.ReadFile(filepath.Join(workspace, HandoffPath))
	if err != nil {
		return Handoff{}, fmt.Errorf("%s: %w", SigHandoffMissing, err)
	}
	return ParseHandoff(b)
}

// ParseHandoff reads the front matter of the declared shape.
func ParseHandoff(b []byte) (Handoff, error) {
	fm, ok := frontMatter(string(b))
	if !ok {
		return Handoff{}, fmt.Errorf("%s: no `---` front matter block", SigHandoffMissing)
	}

	var h Handoff
	var section string // "", "criteria", "artifacts", "blockers", "handoff", "usage"

	for _, raw := range strings.Split(fm, "\n") {
		line := strings.TrimRight(raw, " \t\r")
		if line == "" || strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		indented := strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t")
		trimmed := strings.TrimSpace(stripComment(line))
		if trimmed == "" {
			continue
		}

		// A list item belongs to whatever section is open.
		if strings.HasPrefix(trimmed, "- ") {
			item := strings.TrimSpace(trimmed[2:])
			switch section {
			case "criteria":
				c, err := parseCriterion(item)
				if err != nil {
					return Handoff{}, err
				}
				h.Criteria = append(h.Criteria, c)
			case "artifacts":
				h.Artifacts = append(h.Artifacts, HandoffArtifact{
					Path: unquote(inlineField(item, "path")),
					Kind: unquote(inlineField(item, "kind")),
				})
			case "blockers":
				h.Blockers = append(h.Blockers, unquote(item))
			default:
				return Handoff{}, fmt.Errorf("%s: list item outside a known block: %q", SigHandoffMissing, item)
			}
			continue
		}

		key, val, found := strings.Cut(trimmed, ":")
		if !found {
			return Handoff{}, fmt.Errorf("%s: line is neither a key nor a list item: %q", SigHandoffMissing, trimmed)
		}
		key = strings.TrimSpace(key)
		val = unquote(strings.TrimSpace(val))

		// An indented key belongs to the open block; a flush-left one opens a
		// new section and closes whatever was open.
		if indented {
			switch section {
			case "handoff":
				switch key {
				case "next_role":
					h.NextRole = val
				case "must_know":
					h.MustKnow = val
				}
			case "usage":
				if h.Usage == nil {
					h.Usage = &HandoffUsage{}
				}
				switch key {
				case "tokens_in":
					h.Usage.TokensIn, _ = strconv.ParseInt(val, 10, 64)
				case "tokens_out":
					h.Usage.TokensOut, _ = strconv.ParseInt(val, 10, 64)
				case "basis":
					h.Usage.Basis = val
				}
			}
			continue
		}

		switch key {
		case "task":
			h.Task, section = val, ""
		case "run":
			h.Run, section = val, ""
		case "role":
			h.Role, section = val, ""
		case "status":
			h.Status, section = val, ""
		case "criteria", "artifacts", "blockers", "handoff", "usage":
			section = key
		default:
			section = ""
		}
	}

	return h, validateHandoff(h)
}

// validateHandoff enforces the contract's "all fields mandatory".
//
// Lenient about what it does not know and strict about what it does: an extra
// key a future tool emits is ignored, a missing mandatory one is the whole
// attempt failed. That asymmetry is deliberate -- BR-30 is about GAOS being
// able to READ the document, not about policing what else is in it.
func validateHandoff(h Handoff) error {
	var missing []string
	if h.Task == "" {
		missing = append(missing, "task")
	}
	if h.Run == "" {
		missing = append(missing, "run")
	}
	if h.Role == "" {
		missing = append(missing, "role")
	}
	if h.Status == "" {
		missing = append(missing, "status")
	}
	if len(h.Criteria) == 0 {
		missing = append(missing, "criteria")
	}
	if len(missing) > 0 {
		return fmt.Errorf("%s: mandatory field(s) absent: %s", SigHandoffMissing, strings.Join(missing, ", "))
	}

	switch h.Status {
	case HandoffComplete, HandoffBlocked, HandoffPartial:
	default:
		return fmt.Errorf("%s: status %q is not one of complete|blocked|partial", SigHandoffMissing, h.Status)
	}

	if h.Usage != nil && h.Usage.Basis != "" {
		switch h.Usage.Basis {
		case CostMeasured, CostEstimated:
		default:
			return fmt.Errorf("%s: usage.basis %q is not one of measured|estimated", SigHandoffMissing, h.Usage.Basis)
		}
	}
	return nil
}

// frontMatter returns the text between the opening and closing `---`.
func frontMatter(s string) (string, bool) {
	// "\ufeff" written as an escape rather than as the character itself: a
	// literal BOM in a Go source file is an error in the middle of one, and a
	// tool that emits a BOM is exactly the case this line exists to survive.
	s = strings.TrimLeft(s, "\ufeff \t\r\n")
	if !strings.HasPrefix(s, "---") {
		return "", false
	}
	rest := s[3:]
	if i := strings.IndexByte(rest, '\n'); i >= 0 {
		rest = rest[i+1:]
	} else {
		return "", false
	}
	for i, line := range strings.Split(rest, "\n") {
		if strings.TrimSpace(line) == "---" {
			return strings.Join(strings.Split(rest, "\n")[:i], "\n"), true
		}
	}
	return "", false
}

// parseCriterion reads `id: AC-7   met: true   evidence: "..."`, the one inline
// record shape the contract uses.
func parseCriterion(item string) (HandoffCriterion, error) {
	id := unquote(inlineField(item, "id"))
	if id == "" {
		return HandoffCriterion{}, fmt.Errorf("%s: criterion has no id: %q", SigHandoffMissing, item)
	}
	return HandoffCriterion{
		ID:       id,
		Met:      strings.EqualFold(inlineField(item, "met"), "true"),
		Evidence: unquote(inlineField(item, "evidence")),
	}, nil
}

// inlineField pulls `name:` out of an inline record, stopping at the next
// `word:` that is not inside quotes.
func inlineField(item, name string) string {
	i := strings.Index(item, name+":")
	if i < 0 {
		return ""
	}
	// Only a field start: preceded by nothing or by whitespace.
	if i > 0 && item[i-1] != ' ' && item[i-1] != '\t' {
		return ""
	}
	rest := strings.TrimLeft(item[i+len(name)+1:], " \t")

	if len(rest) > 0 && (rest[0] == '"' || rest[0] == '\'') {
		q := rest[0]
		if j := strings.IndexByte(rest[1:], q); j >= 0 {
			return rest[:j+2]
		}
		return rest
	}
	// Unquoted: up to the next `word:` field start.
	for j := 0; j < len(rest); j++ {
		if rest[j] != ':' {
			continue
		}
		k := j
		for k > 0 && rest[k-1] != ' ' && rest[k-1] != '\t' {
			k--
		}
		if k > 0 {
			return strings.TrimSpace(rest[:k])
		}
	}
	return strings.TrimSpace(rest)
}

// stripComment removes a trailing ` # ...` that is not inside quotes.
func stripComment(s string) string {
	var qc byte
	for i := 0; i < len(s); i++ {
		switch {
		case qc != 0:
			if s[i] == qc {
				qc = 0
			}
		case s[i] == '"' || s[i] == '\'':
			qc = s[i]
		case s[i] == '#' && (i == 0 || s[i-1] == ' ' || s[i-1] == '\t'):
			return s[:i]
		}
	}
	return s
}

func unquote(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}
