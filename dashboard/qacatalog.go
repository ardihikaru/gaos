package main

import (
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"
)

// The QA catalog (docs/designs/hisv3-qa-sync.md, D-072, D-073).
//
// A product's test cases, owned field by field. GAOS writes the PLANNING half
// (what the case is, what is decided about it); the QA tools write the
// AUTOMATION half (is there a script, did the last run pass). The halves are
// disjoint, so a push and a pull never conflict; inside the automation half the
// newest run wins (BR-51).
//
// The catalog is not part of the event fold and it never writes the task graph
// (BR-54): a card moves because an operator or the scheduler moved it, not
// because a result arrived.

// ---------- the closed vocabularies (copied from the source repo's catalog rules) ----------

const (
	DecisionPlanned    = "planned"
	DecisionBlocked    = "blocked"
	DecisionManualOnly = "manual-only"
	DecisionWont       = "wont"

	// StatusAutomated is a derived status; it is never stored and never written.
	StatusAutomatedCase = "automated"
)

var (
	qaPolarities = map[string]bool{"+": true, "-": true}
	qaLevels     = map[string]bool{"API": true, "GQL": true, "WEB": true, "E2E": true}
	qaPris       = map[string]bool{"P0": true, "P1": true, "P2": true, "P3": true}
	qaDecisions  = map[string]bool{DecisionPlanned: true, DecisionBlocked: true, DecisionManualOnly: true, DecisionWont: true}
	qaResults    = map[string]bool{"pass": true, "fail": true, "skipped": true}
	qaReasonCats = map[string]bool{"defect": true, "not-needed": true, "blocked": true, "unsafe": true, "manual-only": true, "deferred": true, "other": true}

	qaCaseID    = regexp.MustCompile(`^TC-[A-Z]+-[0-9]{3}$`)
	qaEpicCode  = regexp.MustCompile(`^[A-Z]{2,8}$`)
	qaUserStory = regexp.MustCompile(`^US-[A-Z]+-[0-9]{2}$`)
	qaModuleKey = regexp.MustCompile(`^[A-D][0-9]{2}$`)
	qaFeatureID = regexp.MustCompile(`^[A-D][0-9]{2}\.[0-9]{2,3}(\.[0-9]{2})?$`)
	qaPrintable = regexp.MustCompile(`^[\x20-\x7e]*$`)
)

// Caps, in bytes.
const (
	qaMaxScenario = 600
	qaMaxSpec     = 240
	qaMaxRunRef   = 120
	qaMaxReason   = 240
	qaMaxFeatures = 12
	qaMaxCursor   = 128
	qaMaxNote     = 200
	qaMaxRejected = 20

	// qaMaxItems bounds every batch (BR-53). 200 cases is about 600 statements
	// under the store's one lock.
	qaMaxItems = 200

	// qaFutureSlack is how far past "now" a run may claim to have happened. One
	// item with a millisecond timestamp must not lock a case forever.
	qaFutureSlack = 300
)

// ---------- the records ----------

// QaCase is one test case. The JSON shape is the API's (design §4.7).
type QaCase struct {
	CaseID    string   `json:"caseId"`
	Epic      string   `json:"epic"`
	Module    string   `json:"module"`
	Features  []string `json:"features"`
	UserStory string   `json:"userStory"`
	Scenario  string   `json:"scenario"`
	Polarity  string   `json:"polarity"`
	Level     string   `json:"level"`
	Pri       string   `json:"pri"`
	Decision  string   `json:"decision"`
	Reason    string   `json:"reason"`

	// Status is derived and output-only.
	Status string `json:"status"`

	Implemented bool   `json:"implemented"`
	Spec        string `json:"spec"`
	Result      string `json:"result"`
	RunAt       int64  `json:"runAt"`
	RunRef      string `json:"runRef"`

	// CatRev is the product's CatHead at the planning change that last touched
	// this case. Output-only; the API pages by it.
	CatRev int `json:"catRev"`

	ProductID string `json:"-"`
}

// EffectiveStatus is "automated" when the case is planned and has a script;
// otherwise the decision. A parked case with a script is not "automated" --
// it is needs-decision (see NeedsDecision).
func (c QaCase) EffectiveStatus() string {
	if c.Decision == DecisionPlanned && c.Implemented {
		return StatusAutomatedCase
	}
	return c.Decision
}

// NeedsDecision is a script that exists for a case the planner parked. GAOS
// never flips the decision itself ("an agent never silently drops or weakens a
// case").
func (c QaCase) NeedsDecision() bool { return c.Implemented && c.Decision != DecisionPlanned }

func (c QaCase) out() QaCase {
	c.Status = c.EffectiveStatus()
	c.Features = append([]string{}, c.Features...)
	return c
}

func (c QaCase) clone() QaCase {
	c.Features = append([]string(nil), c.Features...)
	return c
}

// QaModule maps a module key to the GAOS story that stands for it. It is the
// seed's idempotency record: Story has no key of its own, and widening it would
// touch every positional literal in the fixtures.
type QaModule struct {
	Key   string `json:"key"`
	Title string `json:"title"`
	Story string `json:"story"`
}

// QaEpic is one catalog epic, a planning grouping of cases and one card.
type QaEpic struct {
	Code    string `json:"code"`
	Title   string `json:"title"`
	Service string `json:"service"`
	Source  string `json:"source"`
	Module  string `json:"module"`
	Story   string `json:"story"`
}

// QaSyncSide is what one direction of the outbound sync last did.
type QaSyncSide struct {
	At        int64    `json:"at"`
	OK        bool     `json:"ok"`
	Note      string   `json:"note"`
	Items     int      `json:"items"`
	Cursor    string   `json:"cursor"`
	Rejected  []string `json:"rejected,omitempty"`
	RejectedN int      `json:"rejectedN,omitempty"`
}

// QaState rides on the Product record as a document (no DDL), like the vendor
// policy.
type QaState struct {
	Modules []QaModule `json:"modules"`
	Epics   []QaEpic   `json:"epics"`
	CatHead int        `json:"catHead"`
	Pull    QaSyncSide `json:"pull"`
	Push    QaSyncSide `json:"push"`
}

func (q QaState) Clone() QaState {
	c := q
	c.Modules = append([]QaModule(nil), q.Modules...)
	c.Epics = append([]QaEpic(nil), q.Epics...)
	c.Pull.Rejected = append([]string(nil), q.Pull.Rejected...)
	c.Push.Rejected = append([]string(nil), q.Push.Rejected...)
	return c
}

// ---------- requests and answers ----------

// QaReject names one refused item and why. The reason is a short code.
type QaReject struct {
	CaseID string `json:"caseId"`
	Reason string `json:"reason"`
}

type QaImportIn struct {
	Seed    bool       `json:"seed"`
	Modules []QaModule `json:"modules"`
	Epics   []QaEpic   `json:"epics"`
	Cases   []QaCase   `json:"cases"`
}

type QaImportOut struct {
	Created   int        `json:"created"`
	Updated   int        `json:"updated"`
	Unchanged int        `json:"unchanged"`
	Rejected  []QaReject `json:"rejected"`
	CatHead   int        `json:"catHead"`
}

// QaResultItem is one automation fact. An absent field is untouched.
type QaResultItem struct {
	CaseID      string  `json:"caseId"`
	Implemented *bool   `json:"implemented"`
	Spec        *string `json:"spec"`
	Result      *string `json:"result"`
	RunAt       *int64  `json:"runAt"`
	RunRef      *string `json:"runRef"`
}

type QaResultsIn struct {
	Source string         `json:"source"`
	Force  bool           `json:"force"`
	Items  []QaResultItem `json:"items"`
}

type QaResultsOut struct {
	Applied   int        `json:"applied"`
	Unchanged int        `json:"unchanged"`
	Rejected  []QaReject `json:"rejected"`
	CatHead   int        `json:"catHead"`
}

type QaQuery struct {
	After int
	Limit int
	Epic  string
}

type QaPage struct {
	Items   []QaCase `json:"items"`
	Next    *int     `json:"next"`
	CatHead int      `json:"catHead"`
}

// QaEpicRoll is one epic's roll-up. Regressed: a case of the epic whose last
// run failed.
type QaEpicRoll struct {
	QaEpic
	Total       int  `json:"total"`
	Automated   int  `json:"automated"`
	Planned     int  `json:"planned"`
	Blocked     int  `json:"blocked"`
	ManualOnly  int  `json:"manualOnly"`
	Wont        int  `json:"wont"`
	Pass        int  `json:"pass"`
	Fail        int  `json:"fail"`
	Regressed   bool `json:"regressed"`
	CoveragePct int  `json:"coveragePct"`
}

type QaTotals struct {
	Cases       int `json:"cases"`
	Automated   int `json:"automated"`
	Planned     int `json:"planned"`
	Blocked     int `json:"blocked"`
	ManualOnly  int `json:"manualOnly"`
	Wont        int `json:"wont"`
	Positive    int `json:"positive"`
	Negative    int `json:"negative"`
	Pass        int `json:"pass"`
	Fail        int `json:"fail"`
	Skipped     int `json:"skipped"`
	CoveragePct int `json:"coveragePct"`
}

type QaSummary struct {
	ProductID     string       `json:"productId"`
	Name          string       `json:"name"`
	Totals        QaTotals     `json:"totals"`
	Epics         []QaEpicRoll `json:"epics"`
	Regressed     []string     `json:"regressed"`
	NeedsDecision []string     `json:"needsDecision"`
	NeedsMore     int          `json:"needsDecisionMore"`
	Pull          QaSyncSide   `json:"pull"`
	Push          QaSyncSide   `json:"push"`
	CatHead       int          `json:"catHead"`
}

// ---------- validation ----------

func byteLen(s string) int { return len([]byte(s)) }

// checkRelPath refuses an absolute path and any ".." segment.
func checkRelPath(p string) bool {
	if p == "" {
		return true
	}
	if strings.HasPrefix(p, "/") || strings.HasPrefix(p, `\`) || strings.Contains(p, `\`) {
		return false
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == ".." {
			return false
		}
	}
	return qaPrintable.MatchString(p)
}

// validatePlanning checks the planning half of a case. The answer is a short
// code ("" is valid), the same code the API returns in rejected[].
func validatePlanning(c QaCase) string {
	switch {
	case !qaCaseID.MatchString(c.CaseID):
		return "bad-case-id"
	case !qaEpicCode.MatchString(c.Epic):
		return "bad-epic"
	case c.Module != "" && !qaModuleKey.MatchString(c.Module):
		return "bad-module"
	case c.UserStory != "" && !qaUserStory.MatchString(c.UserStory):
		return "bad-user-story"
	case len(c.Features) > qaMaxFeatures:
		return "too-many-features"
	case strings.TrimSpace(c.Scenario) == "":
		return "empty-scenario"
	case byteLen(c.Scenario) > qaMaxScenario:
		return "scenario-too-long"
	case !qaPolarities[c.Polarity]:
		return "bad-polarity"
	case !qaLevels[c.Level]:
		return "bad-level"
	case !qaPris[c.Pri]:
		return "bad-pri"
	case !qaDecisions[c.Decision]:
		return "bad-decision"
	}
	for _, f := range c.Features {
		if !qaFeatureID.MatchString(f) {
			return "bad-feature"
		}
	}
	// A planned case needs no reason, but the source catalog's planned rows may
	// carry a note (deferred:..., for one); when one is there it is held to the
	// same shape as every other reason.
	if c.Decision == DecisionPlanned && c.Reason == "" {
		return ""
	}
	if byteLen(c.Reason) > qaMaxReason {
		return "reason-too-long"
	}
	cat, detail, ok := strings.Cut(c.Reason, ":")
	if !ok || !qaReasonCats[cat] || strings.TrimSpace(detail) == "" {
		return "bad-reason"
	}
	return ""
}

func validateSpec(spec string) string {
	if byteLen(spec) > qaMaxSpec || !checkRelPath(spec) {
		return "bad-spec"
	}
	return ""
}

func validateRunRef(ref string) string {
	if byteLen(ref) > qaMaxRunRef || !qaPrintable.MatchString(ref) {
		return "bad-run-ref"
	}
	return ""
}

func validateModule(m QaModule) string {
	switch {
	case !qaModuleKey.MatchString(m.Key):
		return "bad-module"
	case strings.TrimSpace(m.Title) == "" || byteLen(m.Title) > 200:
		return "bad-module-title"
	case byteLen(m.Story) > 20 || !qaPrintable.MatchString(m.Story):
		return "bad-module-story"
	}
	return ""
}

func validateEpic(e QaEpic) string {
	switch {
	case !qaEpicCode.MatchString(e.Code):
		return "bad-epic"
	case strings.TrimSpace(e.Title) == "" || byteLen(e.Title) > 200:
		return "bad-epic-title"
	case byteLen(e.Service) > 60 || byteLen(e.Source) > 240 || !checkRelPath(e.Source):
		return "bad-epic-source"
	case e.Module != "" && !qaModuleKey.MatchString(e.Module):
		return "bad-module"
	}
	return ""
}

// validCursor is the pull cursor's shape: short, printable, no query syntax
// surprises (the client also builds the URL with url.Values).
func validCursor(c string) bool { return byteLen(c) <= qaMaxCursor && qaPrintable.MatchString(c) }

func samePlanning(a, b QaCase) bool {
	if a.Epic != b.Epic || a.Module != b.Module || a.UserStory != b.UserStory || a.Scenario != b.Scenario ||
		a.Polarity != b.Polarity || a.Level != b.Level || a.Pri != b.Pri || a.Decision != b.Decision ||
		a.Reason != b.Reason || len(a.Features) != len(b.Features) {
		return false
	}
	for i := range a.Features {
		if a.Features[i] != b.Features[i] {
			return false
		}
	}
	return true
}

// ---------- the roll-up ----------

func rollUp(m Master, cases []QaCase) QaSummary {
	sum := QaSummary{ProductID: m.ID, CatHead: m.Qa.CatHead, Pull: m.Qa.Pull, Push: m.Qa.Push,
		Epics: []QaEpicRoll{}, Regressed: []string{}, NeedsDecision: []string{}}
	rolls := map[string]*QaEpicRoll{}
	order := []string{}
	for _, e := range m.Qa.Epics {
		r := &QaEpicRoll{QaEpic: e}
		rolls[e.Code] = r
		order = append(order, e.Code)
	}
	for _, c := range cases {
		r := rolls[c.Epic]
		if r == nil {
			r = &QaEpicRoll{QaEpic: QaEpic{Code: c.Epic, Title: c.Epic}}
			rolls[c.Epic] = r
			order = append(order, c.Epic)
		}
		sum.Totals.Cases++
		r.Total++
		if c.Polarity == "+" {
			sum.Totals.Positive++
		} else {
			sum.Totals.Negative++
		}
		switch c.EffectiveStatus() {
		case StatusAutomatedCase:
			sum.Totals.Automated++
			r.Automated++
		case DecisionPlanned:
			sum.Totals.Planned++
			r.Planned++
		case DecisionBlocked:
			sum.Totals.Blocked++
			r.Blocked++
		case DecisionManualOnly:
			sum.Totals.ManualOnly++
			r.ManualOnly++
		case DecisionWont:
			sum.Totals.Wont++
			r.Wont++
		}
		switch c.Result {
		case "pass":
			sum.Totals.Pass++
			r.Pass++
		case "fail":
			sum.Totals.Fail++
			r.Fail++
		case "skipped":
			sum.Totals.Skipped++
		}
		if c.NeedsDecision() {
			if len(sum.NeedsDecision) < qaMaxItems {
				sum.NeedsDecision = append(sum.NeedsDecision, c.CaseID)
			} else {
				sum.NeedsMore++
			}
		}
	}
	for _, code := range order {
		r := rolls[code]
		r.Regressed = r.Fail > 0
		if open := r.Total - r.Wont; open > 0 {
			r.CoveragePct = r.Automated * 100 / open
		}
		if r.Regressed {
			sum.Regressed = append(sum.Regressed, r.Code)
		}
		sum.Epics = append(sum.Epics, *r)
	}
	if open := sum.Totals.Cases - sum.Totals.Wont; open > 0 {
		sum.Totals.CoveragePct = sum.Totals.Automated * 100 / open
	}
	sort.Strings(sum.NeedsDecision)
	return sum
}

// qaEventText is the one-line text of the RecordUpdated a QA write emits.
func qaEventText(product, what string, parts ...string) string {
	return fmt.Sprintf("%s: %s · %s", product, what, strings.Join(parts, ", "))
}

// decodeStrict reads exactly one JSON value and refuses unknown fields, so a
// results item that names a planning field is a decode error rather than a
// field silently ignored (BR-51).
func decodeStrict(r io.Reader, v any) error {
	dec := json.NewDecoder(r)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return err
	}
	if dec.More() {
		return fmt.Errorf("trailing data after the JSON value")
	}
	return nil
}

// sortDeskIDs orders product ids by (length, id), which is the seed order for
// p-1..p-9 and puts p-10 after p-9; a plain string sort puts it before p-2.
func sortDeskIDs(ids []string) {
	sort.SliceStable(ids, func(i, j int) bool {
		if len(ids[i]) != len(ids[j]) {
			return len(ids[i]) < len(ids[j])
		}
		return ids[i] < ids[j]
	})
}
