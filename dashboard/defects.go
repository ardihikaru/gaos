package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// Defect intake (D-091, docs/designs/gaos-defect-autofix.md §3-§5).
//
// The QA tool reports a defect of a case; GAOS records it once per key, plans a
// triage card and follows the record through its states. Everything in a report
// is untrusted text: the title and the excerpt are stripped, capped and
// screened, reach an agent only as a context part, and never appear in an event
// or a log line. The repository a report names is never trusted: only the ones
// the product owns are kept.

// The defect states (§5.2).
const (
	DefectStateOpen    = "open"
	DefectStateTriaged = "triaged"
	DefectStateFixing  = "fixing"
	DefectStateFixed   = "fixed"
	DefectStateClosed  = "closed"
)

// The defect kinds the QA tool reports (§3).
const (
	DefectKindXfail = "xfail"
	DefectKindFail  = "fail"
	DefectKindXpass = "xpass"
)

// The rules a defect move or event names.
const (
	RuleDefectSuperseded = "autofix-superseded"
	RuleDefectFixed      = "autofix-fixed"
	RuleDefectVerdict    = "autofix-verdict"
	RuleDefectRegression = "autofix-regression"
	RuleDefectPlan       = "autofix-plan"
	RuleDefectClosed     = "autofix-closed"
)

// The limits of §4.1.
const (
	defectMaxItems    = 50
	defectMaxBody     = 256 << 10
	defectMaxNewKeys  = 50
	defectMaxTitle    = 200
	defectMaxExcerpt  = 4 << 10
	defectMaxRepos    = 8
	defectMaxOps      = 5
	defectExcerptDays = 30
)

var (
	defectIDRe = regexp.MustCompile(`^DEF-\d{1,6}$`)
	defectRepo = regexp.MustCompile(`^[A-Za-z0-9_.\-]{1,100}/[A-Za-z0-9_.\-]{1,100}$`)
	defectOpRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.\-/]{0,80}$`)
)

// Defect is one defect key of a product (§5.1). Key is the vertex id.
type Defect struct {
	Key             string
	ProductID       string
	DefectID        string
	CaseID          string
	Kind            string
	Level           string
	AutomationType  string
	Backend         string
	TargetRepos     []string
	Operations      []string
	Spec            string
	Title           string
	Excerpt         string
	ExcerptWithheld bool
	FirstSeen       int64
	LastSeen        int64
	RunRef          string
	Gen             int
	Regression      bool
	Difficulty      string
	Class           string
	Repo            string
	Files           []string
	Reason          string
	State           string
	SupersededBy    string
	TriageTask      string
	FixTask         string
	FixPR           string
	Context         DefectContext
}

// Clone deep-copies a record.
func (d Defect) Clone() Defect {
	d.TargetRepos = append([]string(nil), d.TargetRepos...)
	d.Operations = append([]string(nil), d.Operations...)
	d.Files = append([]string(nil), d.Files...)
	d.Context = d.Context.Clone()
	return d
}

// Ref is the id a person reads: the defect id, else the case id.
func (d Defect) Ref() string {
	if d.DefectID != "" {
		return d.DefectID
	}
	return d.CaseID
}

// FixPR is the draft pull request GAOS opened for a defect (§9.1).
type FixPR struct {
	ID         string
	ProductID  string
	DefectKey  string
	Repo       string
	Number     int
	URL        string
	State      string
	CreatedAt  int64
	UpdatedAt  int64
	TaskID     string
	Difficulty string
	Before     string
	After      string
	Branch     string
	BaseSHA    string
	CommitSHA  string
	WaSent     bool
	Reply      string
}

// The pull-request record states.
const (
	FixPRPreparing = "preparing"
	FixPRDraft     = "draft"
	FixPRReady     = "ready"
	FixPRMerged    = "merged"
	FixPRClosed    = "closed"
)

// DefectItem is one reported defect (§4.1).
type DefectItem struct {
	CaseID         string   `json:"caseId"`
	DefectID       string   `json:"defectId"`
	Kind           string   `json:"kind"`
	Level          string   `json:"level"`
	AutomationType string   `json:"automationType"`
	Backend        string   `json:"backend"`
	TargetRepos    []string `json:"targetRepos"`
	Operations     []string `json:"operations"`
	Spec           string   `json:"spec"`
	Title          string   `json:"title"`
	Excerpt        string   `json:"excerpt"`
	RunRef         string   `json:"runRef"`
	RunAt          int64    `json:"runAt"`
}

// DefectReportIn is a batch.
type DefectReportIn struct {
	Source string
	Items  []DefectItem
	now    int64 // tests only; zero reads the clock
}

// DefectReportOut is the answer of §4.1. NeedContext names the records whose
// triage card still needs its source slice; the caller builds it outside any lock.
type DefectReportOut struct {
	Applied     int        `json:"applied"`
	Unchanged   int        `json:"unchanged"`
	Rejected    []QaReject `json:"rejected"`
	NeedContext []string   `json:"-"`
}

// validateDefectItem is the pure half of §4.1's per-item rules. The answer is a
// short code, "" when valid.
func validateDefectItem(it DefectItem, now int64) string {
	switch {
	case !qaCaseID.MatchString(it.CaseID):
		return "bad-case-id"
	case it.Kind != DefectKindXfail && it.Kind != DefectKindFail && it.Kind != DefectKindXpass:
		return "bad-kind"
	case it.Kind != DefectKindFail && !defectIDRe.MatchString(it.DefectID):
		return "bad-defect-id"
	case it.Kind == DefectKindFail && it.DefectID != "" && !defectIDRe.MatchString(it.DefectID):
		return "bad-defect-id"
	case it.AutomationType != "be" && it.AutomationType != "e2e":
		return "bad-automation-type"
	case it.Backend != "" && it.Backend != "graphql" && it.Backend != "rest":
		return "bad-backend"
	case !qaLevels[it.Level]:
		return "bad-level"
	case it.Spec == "" || validateSpec(it.Spec) != "":
		return "bad-spec"
	case len(it.TargetRepos) > defectMaxRepos:
		return "too-many-repos"
	case len(it.Operations) > defectMaxOps:
		return "too-many-operations"
	case utf8.RuneCountInString(it.Title) > defectMaxTitle:
		return "title-too-long"
	case byteLen(it.Excerpt) > defectMaxExcerpt:
		return "excerpt-too-long"
	case validateRunRef(it.RunRef) != "":
		return "bad-run-ref"
	case it.RunAt < 0 || it.RunAt > now+qaFutureSlack:
		return "bad-run-at"
	}
	for _, r := range it.TargetRepos {
		if !defectRepo.MatchString(r) {
			return "bad-repo"
		}
	}
	for _, o := range it.Operations {
		if !defectOpRe.MatchString(o) {
			return "bad-operation"
		}
	}
	return ""
}

// stripBR53 applies BR-53's stripping to untrusted text: backticks, angle
// brackets, line breaks and every other control character are removed (a line
// break becomes a space, so words do not run together).
func stripBR53(s string) string {
	var sb strings.Builder
	for _, r := range s {
		switch {
		case r == '`' || r == '<' || r == '>':
		case r == '\n' || r == '\r' || r == '\t':
			sb.WriteRune(' ')
		case unicode.IsControl(r), unicode.Is(unicode.Cf, r):
		default:
			sb.WriteRune(r)
		}
	}
	return strings.TrimSpace(sb.String())
}

// capRunes cuts a string to n runes.
func capRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n])
}

// capBytes cuts a string to at most n bytes on a rune boundary.
func capBytes(s string, n int) string {
	if len(s) <= n {
		return s
	}
	s = s[:n]
	for !utf8.ValidString(s) && len(s) > 0 {
		s = s[:len(s)-1]
	}
	return s
}

// screenDefectText strips, caps and screens the two untrusted fields. A hit of
// the egress detectors drops the text, keeps the item and says so.
func screenDefectText(title, excerpt string) (t, e string, withheld bool) {
	t = capRunes(stripBR53(title), defectMaxTitle)
	e = capBytes(stripBR53(excerpt), defectMaxExcerpt)
	if screenBody(title) || screenBody(t) {
		t, withheld = "", true
	}
	if screenBody(excerpt) || screenBody(e) {
		e, withheld = "", true
	}
	return t, e, withheld
}

// ownedRepos keeps the entries of the report the product owns, without repeats.
func ownedRepos(owned, named []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, r := range named {
		o, n, ok := ghOwnerRepo(r)
		if !ok || !productOwns(owned, o, n) || seen[strings.ToLower(r)] {
			continue
		}
		seen[strings.ToLower(r)] = true
		out = append(out, r)
	}
	return out
}

// defectFinal reports whether a state is one a regression reopens.
func defectFinal(state string) bool { return state == DefectStateFixed || state == DefectStateClosed }

// newKeysToday counts the product's records first seen on now's UTC day.
func (s *MemStore) newKeysTodayLocked(productID string, now int64) int {
	n := 0
	for _, d := range s.defects {
		if d.ProductID == productID && d.FirstSeen/86400 == now/86400 {
			n++
		}
	}
	return n
}

// defectEventLocked writes one defect event. The payload names the key, the
// kind or state and a rule, never the title or the excerpt.
func (s *MemStore) defectEventLocked(t EventType, productID, text, kind string, p EventPayload) Event {
	if idx := s.deskIdxByID(productID); idx >= 0 {
		base := deskPayload(s.desks[idx])
		base.Task, base.Reason, base.Rule, base.To, base.Actor = p.Task, p.Reason, p.Rule, p.To, p.Actor
		p = base
	}
	return s.emit(t, productID, text, kind, p)
}

// setDefectStateLocked moves a record and writes DefectStateChanged.
func (s *MemStore) setDefectStateLocked(d *Defect, to, rule, actor string) Event {
	from := d.State
	d.State = to
	p := EventPayload{Task: d.Key, Reason: from + "→" + to, Rule: rule, To: to, Actor: actor}
	text := fmt.Sprintf("defect %s: %s → %s (%s)", d.Ref(), from, to, rule)
	if actor != "" {
		text += " by " + actor
	}
	return s.defectEventLocked(DefectStateChanged, d.ProductID, text, KindInfo, p)
}

// cancelDefectCardsLocked cancels the open cards of a record, every
// generation. A card with a live turn is left to finish.
func (s *MemStore) cancelDefectCardsLocked(d *Defect, rule string) []Event {
	var evs []Event
	prefix := "defect:" + d.Key + ":"
	for i := range s.tasks {
		t := &s.tasks[i]
		if !strings.HasPrefix(t.DedupKey, prefix) {
			continue
		}
		switch t.Status {
		case StatusDone, StatusFailed, StatusCanceled, StatusDispatched, StatusRunning:
			continue
		}
		if e, ok := s.moveTaskLocked(t.ID, StatusCanceled, rule); ok {
			evs = append(evs, e)
		}
	}
	return evs
}

// ReportDefects applies a batch from the QA tool (§4.2). A bad item is
// rejected with a reason and the rest apply.
func (s *MemStore) ReportDefects(ctx context.Context, productID string, in DefectReportIn) (DefectReportOut, Result, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m := s.masterPtr(productID)
	if m == nil {
		return DefectReportOut{}, Result{}, ErrNotFound
	}
	if s.deskIdxByID(productID) < 0 {
		return DefectReportOut{}, Result{}, ErrNotFound
	}
	now := in.now
	if now == 0 {
		now = time.Now().Unix()
	}
	out := DefectReportOut{Rejected: []QaReject{}}
	var evs []Event
	changed := map[string]bool{}
	var keys []string
	touch := func(d *Defect) {
		if !changed[d.Key] {
			changed[d.Key] = true
			keys = append(keys, d.Key)
		}
	}
	rej := func(it DefectItem, reason string) {
		out.Rejected = append(out.Rejected, QaReject{CaseID: it.CaseID, Reason: reason})
	}
	repeat := func(d *Defect, it DefectItem) {
		if it.RunAt > d.LastSeen {
			d.LastSeen = it.RunAt
		}
		if now > d.LastSeen {
			d.LastSeen = now
		}
		d.RunRef = it.RunRef
		touch(d)
		out.Unchanged++
	}
	for _, it := range in.Items {
		if why := validateDefectItem(it, now); why != "" {
			rej(it, why)
			continue
		}
		if _, ok := s.qa[productID][it.CaseID]; !ok {
			rej(it, "unknown-case")
			continue
		}
		switch it.Kind {
		case DefectKindXpass:
			d := s.defects[productID+"/"+it.DefectID]
			if d == nil || defectFinal(d.State) {
				out.Unchanged++
				continue
			}
			d.LastSeen, d.RunRef = max(d.LastSeen, now), it.RunRef
			evs = append(evs, s.cancelDefectCardsLocked(d, RuleDefectFixed)...)
			evs = append(evs, s.setDefectStateLocked(d, DefectStateFixed, RuleDefectFixed, ""))
			touch(d)
			out.Applied++
		case DefectKindXfail:
			key := productID + "/" + it.DefectID
			if d := s.defects[key]; d != nil {
				repeat(d, it)
				continue
			}
			if s.newKeysTodayLocked(productID, now) >= defectMaxNewKeys {
				rej(it, "defect-limit")
				continue
			}
			// A later xfail takes over an open fail record of the same case.
			for _, o := range s.defects {
				if o.ProductID == productID && o.Kind == DefectKindFail && o.CaseID == it.CaseID &&
					o.SupersededBy == "" && !defectFinal(o.State) {
					o.SupersededBy = key
					evs = append(evs, s.cancelDefectCardsLocked(o, RuleDefectSuperseded)...)
					evs = append(evs, s.setDefectStateLocked(o, DefectStateClosed, RuleDefectSuperseded, ""))
					touch(o)
				}
			}
			d, e := s.newDefectLocked(m, key, it, now)
			evs = append(evs, e...)
			touch(d)
			out.Applied++
			out.NeedContext = append(out.NeedContext, key)
		case DefectKindFail:
			if cur := s.defectByCaseLocked(productID, it.CaseID); cur != nil {
				if !defectFinal(cur.State) {
					repeat(cur, it)
					continue
				}
				evs = append(evs, s.reopenDefectLocked(m, cur, it, now)...)
				touch(cur)
				out.Applied++
				out.NeedContext = append(out.NeedContext, cur.Key)
				continue
			}
			key := productID + "/" + it.CaseID
			if d := s.defects[key]; d != nil {
				repeat(d, it)
				continue
			}
			if s.newKeysTodayLocked(productID, now) >= defectMaxNewKeys {
				rej(it, "defect-limit")
				continue
			}
			d, e := s.newDefectLocked(m, key, it, now)
			evs = append(evs, e...)
			touch(d)
			out.Applied++
			out.NeedContext = append(out.NeedContext, key)
		}
	}
	if len(keys) == 0 && len(evs) == 0 {
		return out, Result{}, nil
	}
	res := Result{Floor: s.floor(), NewEvents: evs}
	sort.Strings(keys)
	for _, k := range keys {
		res.Defects = append(res.Defects, s.defects[k].Clone())
	}
	return out, res, nil
}

// defectByCaseLocked is the product's live record of a case, any key space: the
// most recently seen one that no record superseded.
func (s *MemStore) defectByCaseLocked(productID, caseID string) *Defect {
	var best *Defect
	for _, d := range s.defects {
		if d.ProductID != productID || d.CaseID != caseID || d.SupersededBy != "" {
			continue
		}
		if best == nil || d.LastSeen > best.LastSeen || (d.LastSeen == best.LastSeen && d.Key < best.Key) {
			best = d
		}
	}
	return best
}

// newDefectLocked records a new key and plans its triage card.
func (s *MemStore) newDefectLocked(m *Master, key string, it DefectItem, now int64) (*Defect, []Event) {
	title, excerpt, withheld := screenDefectText(it.Title, it.Excerpt)
	d := &Defect{
		Key: key, ProductID: m.ID, DefectID: it.DefectID, CaseID: it.CaseID, Kind: it.Kind,
		Level: it.Level, AutomationType: it.AutomationType, Backend: it.Backend,
		TargetRepos: ownedRepos(m.Repos, it.TargetRepos), Operations: append([]string(nil), it.Operations...),
		Spec: it.Spec, Title: title, Excerpt: excerpt, ExcerptWithheld: withheld,
		FirstSeen: now, LastSeen: now, RunRef: it.RunRef, Gen: 1, State: DefectStateOpen,
	}
	s.defects[key] = d
	evs := []Event{s.defectEventLocked(DefectReported, m.ID,
		fmt.Sprintf("defect reported: %s (%s)", d.Ref(), d.Kind), KindInfo,
		EventPayload{Task: key, Rule: d.Kind, To: DefectStateOpen, Reason: d.Kind})}
	_, planned := s.planDefectCardLocked(d, "triage", StatusPending, "", LabelDefectTriage)
	evs = append(evs, planned...)
	return d, evs
}

// reopenDefectLocked is the regression of §4.2: the record returns to open with
// a higher generation, a new triage card, and a fix card that will never be
// automatic. It does not spend a new-key slot.
func (s *MemStore) reopenDefectLocked(m *Master, d *Defect, it DefectItem, now int64) []Event {
	_, excerpt, withheld := screenDefectText("", it.Excerpt)
	from := d.State
	d.Gen++
	d.Regression = true
	d.Difficulty, d.Class, d.Repo, d.Reason = "", "", "", ""
	d.Files = nil
	d.TriageTask, d.FixTask, d.FixPR = "", "", ""
	d.Context = DefectContext{}
	d.Excerpt, d.ExcerptWithheld = excerpt, withheld
	d.LastSeen, d.RunRef = max(d.LastSeen, now), it.RunRef
	d.TargetRepos = ownedRepos(m.Repos, it.TargetRepos)
	d.State = from // setDefectStateLocked records the move from it
	evs := []Event{s.setDefectStateLocked(d, DefectStateOpen, RuleDefectRegression, "")}
	_, planned := s.planDefectCardLocked(d, "triage", StatusPending, "", LabelDefectTriage)
	return append(evs, planned...)
}

// Defects are a product's records, as copies, ordered by key.
func (s *MemStore) Defects(ctx context.Context, productID string) ([]Defect, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []Defect
	for _, d := range s.defects {
		if d.ProductID == productID {
			out = append(out, d.Clone())
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out, nil
}

// DefectByKey is one record, as a copy.
func (s *MemStore) DefectByKey(ctx context.Context, productID, key string) (Defect, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	d := s.defects[key]
	if d == nil || d.ProductID != productID {
		return Defect{}, ErrNotFound
	}
	return d.Clone(), nil
}

// CloseDefect is an operator's close (§5.2). ref is a defect id or a case id of
// the product; a record of another product is not found.
func (s *MemStore) CloseDefect(ctx context.Context, productID, ref, actor string) (Result, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.masterPtr(productID) == nil {
		return Result{}, ErrNotFound
	}
	d := s.defects[productID+"/"+ref]
	if d == nil {
		d = s.defectByCaseLocked(productID, ref)
	}
	if d == nil || d.ProductID != productID {
		return Result{}, ErrNotFound
	}
	if d.State == DefectStateClosed {
		return Result{}, nil
	}
	evs := s.cancelDefectCardsLocked(d, RuleDefectClosed)
	evs = append(evs, s.setDefectStateLocked(d, DefectStateClosed, RuleDefectClosed, actorAlias(actor)))
	return Result{Floor: s.floor(), NewEvents: evs, Defects: []Defect{d.Clone()}, Toast: "Defect closed."}, nil
}

// SetDefectContext stores the source slice a triage or fix turn reuses.
func (s *MemStore) SetDefectContext(ctx context.Context, productID, key string, c DefectContext) (Result, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	d := s.defects[key]
	if d == nil || d.ProductID != productID {
		return Result{}, ErrNotFound
	}
	if n := c.Size(); n > defectContextMax {
		return Result{}, reject(fmt.Sprintf("the context is %d bytes; the limit is %d", n, defectContextMax))
	}
	d.Context = c.Clone()
	return Result{Defects: []Defect{d.Clone()}}, nil
}

// FixPRs are a product's pull-request records, as copies, by id.
func (s *MemStore) FixPRs(ctx context.Context, productID string) ([]FixPR, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []FixPR
	for _, f := range s.fixPRs {
		if f.ProductID == productID {
			out = append(out, f)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// UpsertFixPR writes a pull-request record. It carries no event: the opened and
// state-changed events are written by the caller's own mutation (phase 5).
func (s *MemStore) UpsertFixPR(ctx context.Context, f FixPR) (Result, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.masterPtr(f.ProductID) == nil {
		return Result{}, ErrNotFound
	}
	if f.ID == "" {
		return Result{}, reject("a pull-request record needs an id")
	}
	s.fixPRs[f.ID] = f
	return Result{FixPRs: []FixPR{f}}, nil
}

// restoreDefects loads the records read back from the graph. Boot only.
func (s *MemStore) restoreDefects(ds []Defect, fs []FixPR) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, d := range ds {
		c := d.Clone()
		s.defects[d.Key] = &c
	}
	for _, f := range fs {
		s.fixPRs[f.ID] = f
	}
}

// AutoFixOf is a product's auto-fix configuration.
func (s *MemStore) AutoFixOf(ctx context.Context, productID string) (AutoFix, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	m := s.masterPtr(productID)
	if m == nil {
		return AutoFix{}, ErrNotFound
	}
	return m.AutoFix.Clone(), nil
}

// SetAutoFix replaces a product's configuration and writes AutoFixConfigured.
// The event names the actor's alias, the new values (never the template text)
// and the kind of GitHub token in use. The preconditions for turning it on are
// checked by the route, not here.
func (s *MemStore) SetAutoFix(ctx context.Context, productID string, a AutoFix, actor, tokenKind string) (Result, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m := s.masterPtr(productID)
	if m == nil {
		return Result{}, ErrNotFound
	}
	a = a.Clone()
	a.FixCap, a.TriageCap = a.Caps()
	m.AutoFix = a
	fix, triage := a.Caps()
	tpl := "default"
	if a.PRTemplate != "" {
		tpl = "custom"
	}
	p := EventPayload{Actor: actorAlias(actor), Rule: "autofix-config", Reason: fmt.Sprintf(
		"enabled=%t fixCap=%d triageCap=%d allowed=%d forbidden=%d test=%d template=%s ci=%t token=%s",
		a.Enabled, fix, triage, len(a.AllowedGlobs), len(a.ForbiddenGlobs), len(a.TestGlobs), tpl, a.CIConfirmed, tokenKind)}
	ev := s.defectEventLocked(AutoFixConfigured, productID, "auto-fix configured by "+p.Actor, KindInfo, p)
	c := m.Clone()
	return Result{Master: &c, NewEvents: []Event{ev}, Toast: "Auto-fix settings saved."}, nil
}

// ---------------------------------------------------------------- the route

// limitText names a body limit for an error message.
func limitText(n int64) string {
	switch {
	case n%(1<<20) == 0:
		return strconv.FormatInt(n>>20, 10) + " MiB"
	case n%(1<<10) == 0:
		return strconv.FormatInt(n>>10, 10) + " KiB"
	}
	return strconv.FormatInt(n, 10) + " bytes"
}

type apiDefectsBody struct {
	Source string       `json:"source"`
	Items  []DefectItem `json:"items"`
}

// apiQaDefects is POST /v1/products/{id}/qa/defects (§4.1). It is a new route
// with a strict decoder, so qa/results never has to change for a QA tool that
// is one release ahead.
func (s *server) apiQaDefects(w http.ResponseWriter, r *http.Request) {
	scope, cl, ok := s.apiGate(w, r, scopeQA)
	if !ok {
		return
	}
	id, ok := s.apiProduct(w, r, scope, cl)
	if !ok {
		return
	}
	release, ok := s.beginWrite(w, scope, cl)
	if !ok {
		return
	}
	defer release()
	var body apiDefectsBody
	if !apiDecodeLimit(w, r, &body, defectMaxBody) {
		return
	}
	if len(body.Items) > defectMaxItems {
		apiErr(w, http.StatusRequestEntityTooLarge, "too-large", "a batch holds at most "+strconv.Itoa(defectMaxItems)+" items")
		return
	}
	if body.Source != "push" {
		apiErr(w, http.StatusBadRequest, "bad-source", `source is "push"`)
		return
	}
	out, _, err := s.store.ReportDefects(r.Context(), id, DefectReportIn{Source: body.Source, Items: body.Items})
	if err != nil {
		apiStoreErr(w, err)
		return
	}
	if len(out.NeedContext) > 0 && s.codeSrc != nil {
		// Outside every lock, and off the request: the searches may wait on GitHub.
		go func(keys []string) {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
			defer cancel()
			s.buildDefectContexts(ctx, id, keys)
		}(out.NeedContext)
	}
	apiJSON(w, http.StatusOK, out)
}

// buildDefectContexts builds the source slice of each record and stores it. A
// failed search blocks the triage card; it never reads as "nothing found".
func (s *server) buildDefectContexts(ctx context.Context, productID string, keys []string) {
	for _, k := range keys {
		if err := buildAndStoreDefectContext(ctx, s.store, s.codeSrc, productID, k, ctxSleep); err != nil {
			log.Printf("defects: the source slice of a record could not be built: %s", defectErrClass(err))
		}
	}
}
