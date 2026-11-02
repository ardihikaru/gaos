package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path"
	"regexp"
	"sort"
	"strings"
)

// The pre-dispatch egress pass. T-126, requirement L-4, rule BR-32.
//
// BRD §8.12.2 states the requirement: "A pre-dispatch egress pass runs on every
// context slice: secret and credential detection, path allowlists and denylists
// per product, and size limits. It runs BEFORE the model sees anything, and a
// positive detection refuses the dispatch rather than reporting it afterwards."
//
// The reason it refuses rather than reports is in the same row: "The safety
// dimension in domain E scores what already happened. A secret scored at 40 has
// already left the building." R-11 puts it in the risk register as the one way
// GAOS can harm a user badly without any component failing -- correct
// behaviour, unreviewed content, irreversible send.
//
// Everything in this file is PURE. No clock, no random, no I/O, no lock. Three
// reasons, all load-bearing:
//
//   - What this produces becomes events, and fold.go's first rule is that the
//     reducer is pure. A verdict that varied with the clock would fold one way
//     during the run and another way during the replay of it.
//   - A security control you cannot table-test is a security control nobody
//     trusts. Every case below is a literal in egress_test.go.
//   - A pass that read the filesystem would be screening something other than
//     what was frozen. The packet is the subject; the disk is not.

// ---------- the packet ----------

// The part kinds. Only KindRepo is governed by the path allowlist, and that
// distinction is not cosmetic -- see EgressPolicy.AllowPaths.
const (
	KindTask     = "task"     // .gaos/packet/task.md     -- objective + criteria
	KindRules    = "rules"    // .gaos/packet/rules.md    -- domain rules to score against
	KindContext  = "context"  // .gaos/packet/context/... -- the frozen slice, verbatim
	KindContract = "contract" // .gaos/packet/handoff.md  -- the shape the run must produce
	KindHandoff  = "handoff"  // .gaos/handoff/OUTPUT.md  -- declared, empty, required
	KindRepo     = "repo"     // the operator's own files: the largest egress there is
)

// PacketPart is one file the model would be given.
type PacketPart struct {
	Path string
	Kind string
	Body string
}

// Packet is the frozen task packet of BRD §7.6, before it has been screened.
// The layout it describes:
//
//	workspace/
//	  .gaos/packet/task.md      <- objective + criteria
//	  .gaos/packet/context/...  <- the frozen slice, verbatim
//	  .gaos/packet/rules.md     <- domain rules to score against
//	  .gaos/handoff/OUTPUT.md   <- declared, empty, required
//
// Cites is the provenance half, and it travels WITH the packet rather than
// beside it so that the pass can cut both together. See Screen step 2.
type Packet struct {
	TaskID string
	// RunID and Role are what the handoff must echo. They were only ever
	// inside the contract's prose; the gateway adapter renders its own
	// contract (the handoff travels in the reply, D-057) and validates the
	// echoed run id, so it needs them as values rather than as text.
	RunID string
	Role  string
	// SessionID is the session this attempt resumes ("" = open a new one),
	// and SessionReason why a new one opens (D-056). Not part of the digest:
	// they say where the turn runs, not what the model is given.
	SessionID     string
	SessionReason string
	// Answer is the operator's answer to the question the previous attempt
	// asked (M15), which the gateway adapter sends through ai-manager's answer
	// route. Not part of the digest either: the answer reaches the model as
	// the packet's answer part, which is.
	Answer    *PacketAnswer
	DeskID    string
	Workspace string
	Parts     []PacketPart
	Cites     []Citation
}

// PacketAnswer is what the answer route needs: the question and the choice.
type PacketAnswer struct {
	QuestionID string
	Choice     int
	Text       string
}

func (p Packet) Clone() Packet {
	c := p
	if p.Answer != nil {
		a := *p.Answer
		c.Answer = &a
	}
	c.Parts = append([]PacketPart(nil), p.Parts...)
	c.Cites = append([]Citation(nil), p.Cites...)
	return c
}

// Target is where the packet would go. The egress pass needs it for one
// question only -- L-6's no-egress mode -- and deliberately knows nothing else
// about the provider, because a pass that knew provider shapes would be leaking
// them upward in exactly the way BR-33 forbids.
type Target struct {
	Provider string // "anthropic", "local", ...
	Local    bool   // true when the model runs on the operator's own host
}

// ---------- the policy ----------

// EgressPolicy is the `egress:` block of BRD §7.6.1's config/model.yaml:
//
//	egress:                               # domain L applies in BOTH modes, identically
//	  redact:        [secrets, credentials, env-files]
//	  allow_paths:   ["src/**", "tests/**"]
//	  local_only:    false                # true = no-egress mode (L-6)
//
// The block is NOT per-mode. §7.6.1 is explicit: "the pre-dispatch check in L-4
// runs identically whether a subprocess or an HTTP request is about to carry
// the slice out." That is why this is a plain value handed to a pure function
// rather than anything an adapter owns or configures.
type EgressPolicy struct {
	// Redact names the armed detector classes: secrets, credentials, env-files.
	//
	// EMPTY MEANS ALL THREE, not none. A detector list nobody wrote is a
	// scanner somebody forgot to turn on, and the safe default for a forgotten
	// security control is on. TestAnEmptyPolicyStillRunsEveryDetector pins it.
	Redact []string

	// AllowPaths and DenyPaths are L-4's "path allowlists and denylists per
	// product".
	//
	// EMPTY AllowPaths MEANS NO PATH RESTRICTION -- the opposite default to
	// Redact, deliberately. An allowlist nobody wrote cannot be a list of files
	// somebody meant to exclude, whereas a detector nobody armed is a check
	// somebody meant to have. The two defaults differ and a reader will assume
	// they match, so both are tested.
	//
	// They govern parts of KindRepo and nothing else. .gaos/packet/task.md is
	// GAOS's own writing, and the documented allowlist ["src/**","tests/**"]
	// matches neither it nor rules.md -- so applying the list to the whole
	// packet would refuse every dispatch ever made, on a default that looks
	// entirely reasonable.
	AllowPaths []string
	DenyPaths  []string

	// LocalOnly is L-6's no-egress mode: "a product may be configured so that
	// only a locally-hosted model may be routed to, and any routing decision
	// that would leave the host fails the task instead of downgrading quietly."
	LocalOnly bool

	// MaxBytes and MaxPartBytes are L-4's "size limits", in BYTES of the
	// assembled slice after redaction -- not tokens.
	//
	// Bytes rather than tokens for two reasons: a token count needs a
	// model-specific vocabulary, which is I/O and a provider shape leaking
	// above the adapter (BR-33); and bytes are what actually crosses the wire.
	//
	// Two numbers because one 200 KB file and two hundred 1 KB files are
	// different risks. Zero means unlimited, on either.
	MaxBytes     int
	MaxPartBytes int
}

// The detector classes, matching the `redact:` vocabulary in §7.6.1.
const (
	ClassSecrets     = "secrets"
	ClassCredentials = "credentials"
	ClassEnvFiles    = "env-files"
)

// armed reports whether a class is switched on. An empty Redact arms everything
// -- see the field comment.
func (p EgressPolicy) armed(class string) bool {
	if len(p.Redact) == 0 {
		return true
	}
	for _, c := range p.Redact {
		if strings.EqualFold(strings.TrimSpace(c), class) {
			return true
		}
	}
	return false
}

// ---------- the verdict ----------

// The refusal rules. They are values rather than sentences because the
// DispatchRefused payload carries one, and a reducer -- or the budget guard
// that will reuse this event under BR-35 -- must be able to read the reason
// without parsing English.
const (
	RuleSecret     = "secret"
	RuleLocalOnly  = "local-only"
	RuleSize       = "size"
	RuleEmptySlice = "empty-slice"
	RulePathEscape = "path-escape"
)

// Redaction is one part the pass cut out of the slice.
type Redaction struct {
	Path string
	Rule string
	Why  string
}

// Finding records WHERE a detector fired, and never WHAT it matched.
//
// This is the most important field-level decision in the file. A refusal
// becomes a DispatchRefused event; the event stream is append-only and cannot
// be rewritten (BR-04); the timeline is rendered into a browser. A Finding
// carrying the matched text would take a credential that never left the host
// and write it permanently into the record whose entire purpose is to prove the
// credential never left the host.
//
// Offset is enough for a human to go and look at the file themselves, which is
// the only thing anyone actually needs.
type Finding struct {
	Path     string
	Detector string
	Class    string
	Offset   int
}

// EgressVerdict is the whole result of a pass: what it would let out, what it
// cut, and -- if it refused -- why.
type EgressVerdict struct {
	Allowed    bool
	Rule       string // set iff !Allowed
	Reason     string // one sentence; becomes the DispatchRefused payload
	Redactions []Redaction
	Findings   []Finding
	Bytes      int    // size of what would go out, AFTER redaction
	Digest     string // sha256 over the surviving parts, first 16 hex chars
}

// ---------- the clearance handle ----------

// Cleared is a packet that has been through the egress pass. Screen is the only
// function that returns one.
//
// This is store.go's readOnly trick pointed the other way. readOnly narrows
// what a caller may DO with a store; Cleared narrows what a caller may PASS to
// an adapter. Both refuse to be a check somebody remembers to run:
//
//	Dispatch(ctx, pkt)      // does not compile -- a Packet is not a Cleared
//	Dispatch(ctx, cleared)  // the only shape that does
//
// BR-32 says "nothing reaches a model provider without passing the pre-dispatch
// egress check first". A rule phrased as an order of calls holds until the
// second caller is written. Phrased as a type, the second caller cannot be
// written wrong, because the only route from a Packet to an Adapter runs
// through Screen -- Screen being the only thing that manufactures the argument
// Dispatch demands.
//
// THE HONEST LIMIT, stated so nobody over-trusts it: this is package main, so
// `Cleared{}` is a composite literal any file here could write. That is exactly
// the limit readOnly has -- a handler can always reach past s.mut() to s.store.
// The standard both meet is "unforgeable by FORGETTING", and
// TestNothingOutsideTheEgressPassConstructsACleared walks this package's syntax
// tree so that the deliberate version is a failing test rather than a code
// review somebody has to remember to do.
type Cleared struct {
	packet Packet
	digest string
	bytes  int
}

// Packet returns a copy. The adapter must not be able to edit what was
// screened: a packet mutated after clearance is a packet nothing screened.
func (c Cleared) Packet() Packet    { return c.packet.Clone() }
func (c Cleared) Workspace() string { return c.packet.Workspace }
func (c Cleared) Digest() string    { return c.digest }
func (c Cleared) Bytes() int        { return c.bytes }

// Cites is the provenance of what SURVIVED, which is not the same list the
// packet was assembled with. A copy, for the reason Packet() is a copy.
func (c Cleared) Cites() []Citation { return append([]Citation(nil), c.packet.Cites...) }

// empty reports the zero value. Dispatch checks it, and the check is a belt
// rather than the braces: if the type discipline held, it is unreachable.
func (c Cleared) empty() bool { return c.digest == "" }

// EgressRefusal is BR-32's "a detection refuses the dispatch".
//
// It satisfies ErrRejected so every handler that already turns a rejection into
// a toast keeps working -- and it carries Rule, the machine-readable half,
// because the DispatchRefused payload needs a value the reducer can read
// without parsing a sentence.
type EgressRefusal struct {
	Rule   string
	Reason string
}

func (e *EgressRefusal) Error() string        { return e.Reason }
func (e *EgressRefusal) Is(target error) bool { return target == ErrRejected }

// ---------- the detectors ----------

type detector struct {
	name  string
	class string
	re    *regexp.Regexp
}

// The detector table. Compiled once, at package load, because a MustCompile in
// a hot path is a panic waiting for the first slice that reaches it.
//
// Ordering is load-bearing in one place: anthropic-key precedes openai-key, and
// reportFindings keeps the first class per (path, offset), so `sk-ant-...` is
// reported once as the Anthropic key it is rather than twice.
//
// The list is deliberately short. Every pattern here is one whose false
// NEGATIVE would be a credential leaving the host, and whose false positive
// costs one refused dispatch the operator can see the reason for. That trade is
// the whole point of L-4 and it is meant to be lopsided.
var detectors = []detector{
	{"private-key-block", ClassSecrets, regexp.MustCompile(
		`-----BEGIN (?:RSA |EC |DSA |OPENSSH |PGP )?PRIVATE KEY-----`)},
	{"jwt", ClassSecrets, regexp.MustCompile(
		`\beyJ[A-Za-z0-9_\-]{8,}\.[A-Za-z0-9_\-]{8,}\.[A-Za-z0-9_\-]{8,}\b`)},
	// The assignment form. \S{6,} after the separator is what keeps a BRD line
	// reading `API_KEY = <redacted>` from firing: a placeholder is short, and a
	// real key is not.
	{"env-assignment", ClassSecrets, regexp.MustCompile(
		`(?im)^[ \t]*(?:export[ \t]+)?[A-Z0-9_]*(?:PASSWORD|PASSWD|SECRET|TOKEN|API_?KEY|PRIVATE_KEY|ACCESS_KEY)[A-Z0-9_]*[ \t]*[:=][ \t]*\S{6,}`)},

	{"anthropic-key", ClassCredentials, regexp.MustCompile(`\bsk-ant-[A-Za-z0-9_\-]{16,}`)},
	{"openai-key", ClassCredentials, regexp.MustCompile(`\bsk-(?:proj-)?[A-Za-z0-9]{20,}`)},
	{"aws-access-key-id", ClassCredentials, regexp.MustCompile(`\b(?:AKIA|ASIA|AGPA|AIDA)[0-9A-Z]{16}\b`)},
	{"github-token", ClassCredentials, regexp.MustCompile(`\bgh[pousr]_[A-Za-z0-9]{36,}\b`)},
	{"google-api-key", ClassCredentials, regexp.MustCompile(`\bAIza[0-9A-Za-z_\-]{35}\b`)},
	{"slack-token", ClassCredentials, regexp.MustCompile(`\bxox[abprs]-[0-9A-Za-z\-]{10,}\b`)},
	{"basic-auth-url", ClassCredentials, regexp.MustCompile(`\b[a-z][a-z0-9+.\-]*://[^\s/:@]+:[^\s/@]{3,}@`)},
}

// envFileBases are the filenames whose CONTENT nobody needs to inspect: the
// name is the finding. §8.12.1's credentials row says a credential in a context
// slice is "a safety-veto defect, not a leak to be reported afterwards", and
// the cheapest way to honour that is to never read the file in the first place.
var envFileBases = []string{
	".env", ".netrc", ".npmrc", ".git-credentials", "credentials",
}

var envFileGlobs = []string{
	".env.*", "*.pem", "*.key", "*.p12", "*.pfx", "id_rsa*", "id_ed25519*", "id_ecdsa*",
}

func isEnvFile(p string) bool {
	base := path.Base(p)
	for _, b := range envFileBases {
		if base == b {
			return true
		}
	}
	for _, g := range envFileGlobs {
		if ok, _ := path.Match(g, base); ok {
			return true
		}
	}
	return false
}

// ---------- glob matching ----------

// matchGlob is path.Match plus "**".
//
// path.Match alone cannot express §7.6.1's "src/**": its * never crosses a
// separator, so "src/**" matches "src/a" and nothing beneath it -- silently,
// with no error, no panic, and a policy that looks configured while protecting
// nothing. That is the worst shape a security control can fail in, which is why
// this gets its own function and its own test rather than a comment at a call
// site.
func matchGlob(pattern, name string) bool {
	return globParts(strings.Split(pattern, "/"), strings.Split(name, "/"))
}

func globParts(pat, name []string) bool {
	for len(pat) > 0 {
		if pat[0] == "**" {
			// ** consumes zero or more segments. Try each split point.
			rest := pat[1:]
			if len(rest) == 0 {
				return true
			}
			for i := 0; i <= len(name); i++ {
				if globParts(rest, name[i:]) {
					return true
				}
			}
			return false
		}
		if len(name) == 0 {
			return false
		}
		if ok, err := path.Match(pat[0], name[0]); err != nil || !ok {
			return false
		}
		pat, name = pat[1:], name[1:]
	}
	return len(name) == 0
}

func matchAny(patterns []string, name string) bool {
	for _, p := range patterns {
		if matchGlob(p, name) {
			return true
		}
	}
	return false
}

// ---------- the pass ----------

// Screen is the pre-dispatch egress pass. L-4, T-126, BR-32.
//
// Three returns rather than two, and the error is the point. The verdict is
// needed on BOTH paths -- a slice that cleared may still have been cut down,
// and L-5 wants that on the timeline either way -- while the error is what
// makes the refusal impossible to walk past with the usual `if err != nil`.
//
// On refusal the Cleared is the zero value: there is nothing to dispatch.
//
// The order below is PRECEDENCE, not sequence. Every check is evaluated, then
// the reported reason is chosen by a fixed ranking -- secret, path-escape,
// local-only, size, empty-slice -- so the sentence the operator reads is a
// function of the packet rather than of the order the checks happened to run in.
//
// Secret ranks first because it is the one that has to be fixed before
// anything else matters. Path-escape ranks second because it means the packet
// ASSEMBLER is wrong, which is a bug rather than a policy outcome: an operator
// told "there is nothing left to send" would go and widen their allowlist,
// which is both useless and the wrong direction. That is a determinism property -- two runs on the
// same packet must produce byte-identical events -- and a usability one: tell
// them about the credential, not about the byte count.
func Screen(pol EgressPolicy, tgt Target, p Packet) (Cleared, EgressVerdict, error) {
	v := EgressVerdict{}

	// 1. Path normalisation. A part that escapes the workspace is a refusal,
	//    never a redaction: it means the packet assembler is wrong, and
	//    quietly dropping the part would hide that.
	var escaped string
	kept := make([]PacketPart, 0, len(p.Parts))
	for _, part := range p.Parts {
		clean := path.Clean(part.Path)
		if clean == ".." || strings.HasPrefix(clean, "../") || strings.HasPrefix(clean, "/") {
			if escaped == "" {
				escaped = part.Path
			}
			continue
		}
		part.Path = clean
		kept = append(kept, part)
	}

	// 2. Path rules, over KindRepo parts only. A hit DROPS the part and the
	//    pass continues: the allowlist's job is to bound what leaves, not to
	//    abort. That is the difference between L-4's "allowlists" and its
	//    "positive detection".
	survivors := make([]PacketPart, 0, len(kept))
	for _, part := range kept {
		if part.Kind != KindRepo {
			survivors = append(survivors, part)
			continue
		}
		switch {
		case len(pol.DenyPaths) > 0 && matchAny(pol.DenyPaths, part.Path):
			v.Redactions = append(v.Redactions, Redaction{
				Path: part.Path, Rule: "deny-path",
				Why: "matched a denylist pattern for this product",
			})
		case pol.armed(ClassEnvFiles) && isEnvFile(part.Path):
			v.Redactions = append(v.Redactions, Redaction{
				Path: part.Path, Rule: "env-file",
				Why: "a credential-bearing filename is never read, let alone sent",
			})
		case len(pol.AllowPaths) > 0 && !matchAny(pol.AllowPaths, part.Path):
			v.Redactions = append(v.Redactions, Redaction{
				Path: part.Path, Rule: "allow-path",
				Why: "outside the configured path allowlist",
			})
		case pol.MaxPartBytes > 0 && len(part.Body) > pol.MaxPartBytes:
			v.Redactions = append(v.Redactions, Redaction{
				Path: part.Path, Rule: "part-size",
				Why: fmt.Sprintf("%d bytes exceeds the %d-byte per-file limit", len(part.Body), pol.MaxPartBytes),
			})
		default:
			survivors = append(survivors, part)
		}
	}

	// 3. Content detectors, over the SURVIVORS, every kind.
	//
	//    Over the survivors and not over everything, deliberately: a key inside
	//    a dropped .env is a redaction, because the file never left. The same
	//    key pasted into a domain rule is a refusal, because that one is about
	//    to leave. The distinction is the whole reason step 2 runs first.
	v.Findings = reportFindings(pol, survivors)

	// 4. Size, over the survivors.
	for _, part := range survivors {
		v.Bytes += len(part.Body)
	}

	// 5. Digest, over the survivors, in path order so it does not depend on
	//    assembly order. 04-execution.sql declares ContextSlice.digest for
	//    exactly this: proving two runs saw the same context without re-reading
	//    every edge.
	v.Digest = digestOf(survivors)

	sort.Slice(v.Redactions, func(i, j int) bool { return v.Redactions[i].Path < v.Redactions[j].Path })

	// ---- precedence ----
	switch {
	case len(v.Findings) > 0:
		f := v.Findings[0]
		v.Rule, v.Reason = RuleSecret, fmt.Sprintf(
			"detector %s matched %s at byte %d; L-4 refuses the dispatch rather than scoring the leak afterwards",
			f.Detector, f.Path, f.Offset)
	case pol.LocalOnly && !tgt.Local:
		v.Rule, v.Reason = RuleLocalOnly, fmt.Sprintf(
			"no-egress mode is set for this product and %q is not a locally-hosted model; L-6 fails the task rather than downgrading quietly",
			tgt.Provider)
	case escaped != "":
		v.Rule, v.Reason = RulePathEscape, fmt.Sprintf(
			"packet part %q resolves outside the workspace; the assembler is wrong and dropping it quietly would hide that",
			escaped)
	case pol.MaxBytes > 0 && v.Bytes > pol.MaxBytes:
		v.Rule, v.Reason = RuleSize, fmt.Sprintf(
			"the slice is %d bytes against a %d-byte limit; truncating it would make the frozen packet a lie about what the model received",
			v.Bytes, pol.MaxBytes)
	case len(survivors) == 0:
		v.Rule, v.Reason = RuleEmptySlice, "every part of this packet was redacted; there is nothing left to send"
	default:
		v.Allowed = true
	}

	if !v.Allowed {
		return Cleared{}, v, &EgressRefusal{Rule: v.Rule, Reason: v.Reason}
	}

	c := p.Clone()
	c.Parts = survivors
	c.Cites = citationsFor(p.Cites, survivors)
	return Cleared{packet: c, digest: v.Digest, bytes: v.Bytes}, v, nil
}

// citationsFor keeps the provenance of the parts that survived, and drops the
// rest.
//
// This is the honesty of the audit edge in one function. USES_CONTEXT answers
// "what was this attempt given"; a citation kept for a part the pass redacted
// answers it wrongly, and wrongly in the direction that matters -- it would say
// a rule reached the model when the file carrying it was cut on the way out.
//
// Paths are compared after path.Clean because step 1 cleans the parts, and a
// citation written against the assembler's spelling of a path would otherwise
// silently fail to match its own part.
func citationsFor(cites []Citation, survivors []PacketPart) []Citation {
	if len(cites) == 0 {
		return nil
	}
	live := make(map[string]bool, len(survivors))
	for _, part := range survivors {
		live[part.Path] = true
	}
	out := make([]Citation, 0, len(cites))
	for _, c := range cites {
		if live[path.Clean(c.Path)] {
			out = append(out, c)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// reportFindings runs the armed detectors and returns hits in a stable order.
//
// Stable because two runs over the same packet must produce byte-identical
// events: the sort is what lets fold.go's order-stability rule survive contact
// with a map-free but still order-sensitive scan.
func reportFindings(pol EgressPolicy, parts []PacketPart) []Finding {
	var out []Finding
	seen := map[string]bool{} // path\x00offset -- first detector wins

	for _, part := range parts {
		for _, d := range detectors {
			if !pol.armed(d.class) {
				continue
			}
			for _, loc := range d.re.FindAllStringIndex(part.Body, -1) {
				key := fmt.Sprintf("%s\x00%d", part.Path, loc[0])
				if seen[key] {
					continue
				}
				seen[key] = true
				out = append(out, Finding{
					Path: part.Path, Detector: d.name, Class: d.class, Offset: loc[0],
				})
			}
		}
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].Path != out[j].Path {
			return out[i].Path < out[j].Path
		}
		if out[i].Offset != out[j].Offset {
			return out[i].Offset < out[j].Offset
		}
		return out[i].Detector < out[j].Detector
	})
	return out
}

// digestOf hashes the surviving parts in path order. Sixteen hex characters is
// the same width the model document uses for a ContextSlice digest, and it is
// an identity check rather than a security boundary -- nobody is defending
// against a chosen-prefix attack on their own audit trail.
func digestOf(parts []PacketPart) string {
	ordered := append([]PacketPart(nil), parts...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].Path < ordered[j].Path })

	h := sha256.New()
	for _, p := range ordered {
		fmt.Fprintf(h, "%s\x00%d\x00", p.Path, len(p.Body))
		h.Write([]byte(p.Body))
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}
