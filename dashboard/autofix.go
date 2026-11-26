package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// Auto-fix: the switch, the caps and the automatic dispatch (D-091, design §6).
//
// The scheduler never dispatches a defect card, in any mode. This loop does, for
// a product whose operator switched auto-fix on, through the same function the
// board's dispatch uses (board.go dispatch), so every gate applies; its claim
// re-checks the switch and the cap under the lock that takes the move.

// autoFixTick is how often the loop looks for work.
const autoFixTick = 15 * time.Second

// RuleFixPR names the actor of the move that ends a fix card with its pull request.
const RuleFixPR = "autofix-pr"

// autoFixNow is the clock of the caps; a test replaces it.
var autoFixNow = func() int64 { return time.Now().Unix() }

// testMarkers: a TestGlobs entry must contain one (§6.1).
var testMarkers = []string{"_test.", ".test.", ".spec.", "/test/", "/tests/"}

// globOK is a syntactically acceptable glob: relative, no `..`, no control.
func globOK(g string) bool {
	if g == "" || len(g) > 200 || strings.HasPrefix(g, "/") || strings.ContainsAny(g, "\\") {
		return false
	}
	for _, r := range g {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	for _, seg := range strings.Split(g, "/") {
		if seg == ".." {
			return false
		}
	}
	return true
}

// validateAutoFix is §6.1's validation. Enabling needs globs, a confirmed CI, a
// good template and the write token; the other rules hold whether it is on or off.
func validateAutoFix(a AutoFix, tokenKind string) error {
	if a.FixCap < 0 || a.FixCap > autoFixFixCapMax {
		return reject(fmt.Sprintf("The fix cap is between 1 and %d (0 for the default %d).", autoFixFixCapMax, autoFixFixCapDefault))
	}
	if a.TriageCap < 0 || a.TriageCap > autoFixTriageCapMax {
		return reject(fmt.Sprintf("The triage cap is between 1 and %d (0 for the default %d).", autoFixTriageCapMax, autoFixTriageCapDefault))
	}
	for _, set := range [][]string{a.AllowedGlobs, a.ForbiddenGlobs, a.TestGlobs} {
		if len(set) > 50 {
			return reject("A list holds at most 50 globs.")
		}
		for _, g := range set {
			if !globOK(g) {
				return reject("A glob is not a relative path pattern without `..`.")
			}
		}
	}
	for _, g := range a.TestGlobs {
		marked := false
		for _, m := range testMarkers {
			marked = marked || strings.Contains(g, m)
		}
		if g == "*" || g == "**" || !marked {
			return reject("Every test glob must contain a test marker (_test., .test., .spec., /test/, /tests/) and may not be * or **.")
		}
	}
	if len(a.AllowedBases) > 8 {
		return reject("At most 8 allowed base branches.")
	}
	for _, b := range a.AllowedBases {
		if !baseBranchRe.MatchString(b) || strings.Contains(b, "..") {
			return reject("An allowed base is not a valid branch name.")
		}
	}
	if len(a.AutomationRepos) > 4 || !validTargetRepos(a.AutomationRepos) {
		return reject("Each automation repository must be owner/repo, at most 4.")
	}
	if a.PRTemplate != "" {
		if len(a.PRTemplate) > 8<<10 {
			return reject("The pull-request template is longer than 8 KiB.")
		}
		if !templateHasSlots(a.PRTemplate) {
			return reject("The pull-request template needs the slots {{before}} and {{after}}.")
		}
		if screenBody(a.PRTemplate) {
			return reject("The pull-request template matched a secret detector.")
		}
	}
	if a.Enabled {
		if err := autoFixPreconditions(a, tokenKind); err != nil {
			return err
		}
	}
	return nil
}

// baseBranchRe is what a base branch may look like.
var baseBranchRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]{0,99}$`)

// validTargetRepos reports whether every entry is an owner/repo pair.
func validTargetRepos(rs []string) bool {
	if len(rs) > 8 {
		return false
	}
	for _, r := range rs {
		if _, _, ok := ghOwnerRepo(r); !ok {
			return false
		}
	}
	return true
}

// autoFixPreconditions are the conditions a product must meet for auto-fix to
// be on. They are re-checked before a pull request, whoever dispatched the card.
func autoFixPreconditions(a AutoFix, tokenKind string) error {
	switch {
	case len(a.AllowedGlobs) == 0:
		return reject("Auto-fix needs at least one allowed path.")
	case len(a.TestGlobs) == 0:
		return reject("Auto-fix needs at least one test glob.")
	case a.BaseBranch != "" && !baseBranchRe.MatchString(a.BaseBranch):
		return reject("The base branch is not a valid branch name.")
	case !validTargetRepos(a.TargetRepos):
		return reject("Each target repository must be owner/repo, at most 8.")
	case !a.CIConfirmed:
		return reject("Confirm that the repositories' CI runs no secrets on gaos/fix-* branches.")
	case tokenKind == "":
		return reject("Auto-fix needs the GitHub write token (GAOS_GITHUB_WRITE_TOKEN_FILE).")
	}
	return nil
}

// ---------------------------------------------------------------- dispatch

// autoClaimLocked is the claim of the automatic path. s.mu is held. It admits
// only the two labels the loop dispatches, a fix card only when the record says
// it is an easy backend defect, the switch, and the daily cap of its job.
func (s *MemStore) autoClaimLocked(productID string, t *TaskRecord, stamp bool) error {
	m := s.masterPtr(productID)
	if m == nil || !m.AutoFix.Enabled {
		return reject("auto-fix is off for " + productID)
	}
	key, _, job, ok := parseDefectCardKey(t.DedupKey)
	if !ok {
		return reject("only a defect card is dispatched automatically")
	}
	switch {
	case job == defectJobTriage && hasLabel(t.Labels, LabelDefectTriage):
	case job == defectJobFix && hasLabel(t.Labels, LabelDefectEasy):
		d := s.defects[key]
		if d == nil || d.ProductID != productID || d.FixTask != t.ID || d.Difficulty != "easy" ||
			d.AutomationType != "be" || d.Regression {
			return reject("the record of " + t.ID + " is not an easy backend defect; a person moves it")
		}
	default:
		return reject("only defect:triage and defect:easy cards are dispatched automatically")
	}
	fixCap, triageCap := m.AutoFix.Caps()
	limit := triageCap
	if job == defectJobFix {
		limit = fixCap
	}
	day := autoFixNow() / 86400
	n := 0
	for i := range s.tasks {
		o := &s.tasks[i]
		if o.DeskID != productID || o.ID == t.ID || o.AutoAt == 0 || o.AutoAt/86400 != day {
			continue
		}
		if _, _, j, ok := parseDefectCardKey(o.DedupKey); ok && j == job {
			n++
		}
	}
	if n >= limit {
		return reject(fmt.Sprintf("%s cap %d/%d until 00:00 UTC", job, n, limit))
	}
	if stamp {
		t.AutoAt = autoFixNow()
	}
	return nil
}

// AutoDispatch dispatches one READY defect card for the loop. A refusal leaves
// the card READY, except an egress refusal, which blocks it autofix-egress.
func (s *MemStore) AutoDispatch(ctx context.Context, productID, taskID string) (Result, error) {
	return s.dispatch(ctx, productID, taskID, MoveAutoFix, func(t *TaskRecord, stamp bool) error {
		return s.autoClaimLocked(productID, t, stamp)
	})
}

// AutoDispatch is persisted like an operator's move: the TaskMoved and the
// stamped task are written in one transaction.
func (s *ArcadeStore) AutoDispatch(ctx context.Context, productID, taskID string) (Result, error) {
	return s.dispatchPersisted(ctx, func() (Result, error) { return s.proj.AutoDispatch(ctx, productID, taskID) })
}

// dispatchPersisted runs a dispatch WITHOUT the store's write lock and then writes its result.
//
// A dispatch runs the egress pass, and a refusal there records itself through the projection's
// write hook, which takes the write lock (persistBackground). Under mutate that is the same lock
// held by the caller: the first refused dispatch froze every later mutation, an operator's board
// move included (found live, 2026-11-26: a goroutine dump showed mutate -> dispatch ->
// refuseDispatch -> persistBackground waiting on itself, and an answer sent to the inbox hung
// with it). The projection applies the move under its own lock; the lock taken here only orders
// the write, like the runner's own background writes.
func (s *ArcadeStore) dispatchPersisted(ctx context.Context, apply func() (Result, error)) (Result, error) {
	r, err := apply()
	if err != nil {
		return Result{}, err
	}
	if err := s.persistBackground(ctx, r); err != nil {
		return Result{}, err
	}
	return r, nil
}

func (readOnly) AutoDispatch(context.Context, string, string) (Result, error) {
	return Result{}, reject(replayRefusal)
}

// autoFixLoop looks for work every 15 seconds until ctx ends.
func autoFixLoop(ctx context.Context, st Store, src codeSource) {
	tk := time.NewTicker(autoFixTick)
	defer tk.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tk.C:
			autoFixPass(ctx, st, src)
		}
	}
}

var (
	afRefusalMu   sync.Mutex
	afRefusalLast = map[string]string{}
)

// logAutoFixRefusal says why a ready defect card was not dispatched, once per card and reason: a
// refusal here used to be silent, and a card waited in ready for twenty minutes with no trace
// (found live). The reason is the gate's own sentence, which carries no secret.
func logAutoFixRefusal(cardID, reason string) {
	afRefusalMu.Lock()
	defer afRefusalMu.Unlock()
	if afRefusalLast[cardID] == reason {
		return
	}
	afRefusalLast[cardID] = reason
	log.Printf("autofix: %s is ready but not dispatched: %s", cardID, capRunes(reason, 200))
}

// autoFixPass is one pass. It answers how many cards it dispatched. The source
// slice of a card is built here, outside any lock, before the dispatch.
func autoFixPass(ctx context.Context, st Store, src codeSource) int {
	f, err := st.Snapshot(ctx)
	if err != nil || f.Stats.Replay {
		return 0
	}
	n := 0
	for _, d := range f.Desks {
		af, err := st.AutoFixOf(ctx, d.ID)
		if err != nil || !af.Enabled {
			continue
		}
		cards, err := st.PRCards(ctx, d.ID)
		if err != nil {
			continue
		}
		sort.Slice(cards, func(i, j int) bool { return cards[i].ID < cards[j].ID })
		for _, c := range cards {
			key, _, job, ok := parseDefectCardKey(c.DedupKey)
			if !ok || c.Status != StatusReady {
				continue
			}
			if !(job == defectJobTriage && hasLabel(c.Labels, LabelDefectTriage)) &&
				!(job == defectJobFix && hasLabel(c.Labels, LabelDefectEasy)) {
				continue
			}
			if job == defectJobTriage {
				if err := buildAndStoreDefectContext(ctx, st, src, d.ID, key, ctxSleep); err != nil {
					log.Printf("autofix: the source slice of a record could not be built: %s", defectErrClass(err))
					continue
				}
			}
			if job == defectJobFix {
				// A retarget's fix card: the base it reads must still be allowed (revoking a base
				// refuses it for every later attempt), and its source slice is read from that base.
				if rec, err := st.DefectByKey(ctx, d.ID, key); err == nil && rec.BaseOverride != "" {
					if !containsFold(af.AllowedBases, rec.BaseOverride) {
						_, _ = st.ResolvePRCard(ctx, d.ID, c.ID, StatusBlocked, BlockBaseNotAllowed)
						log.Printf("autofix: %s blocked %s", c.ID, BlockBaseNotAllowed)
						continue
					}
					if !rec.Context.Built {
						if err := buildAndStoreDefectContext(ctx, st, src, d.ID, key, ctxSleep); err != nil {
							log.Printf("autofix: the source slice of a retarget could not be built: %s", defectErrClass(err))
							continue
						}
					}
				}
			}
			if _, err := st.AutoDispatch(ctx, d.ID, c.ID); err != nil {
				if !errors.Is(err, ErrRejected) {
					log.Printf("autofix: a dispatch failed: %v", err)
				} else {
					logAutoFixRefusal(c.ID, err.Error())
				}
				continue
			}
			n++
			break // one live turn per product (BR-38)
		}
	}
	return n
}

// ---------------------------------------------------------------- the fixer's packet

// fixerPartsLocked is the packet of a fixer card: the defect's facts, the
// triager's verdict and the source files, pinned to the head they were read at.
func (s *MemStore) fixerPartsLocked(t TaskRecord) []PacketPart {
	if t.Role != "fixer" || !strings.HasPrefix(t.DedupKey, "defect:") {
		return nil
	}
	var d *Defect
	for _, x := range s.defects {
		if x.FixTask == t.ID {
			d = x
			break
		}
	}
	if d == nil {
		return nil
	}
	var sb strings.Builder
	sb.WriteString(renderDefectPacket(*d))
	fmt.Fprintf(&sb, "\n## The triage verdict\n\nDifficulty: %s\nClass: %s\nRepository: %s\nFiles: %s\nReason: %s\n",
		d.Difficulty, d.Class, d.Repo, strings.Join(d.Files, ", "), d.Reason)
	// A retarget card also gets the fix made on the old base, as data (design §6.4).
	if d.BaseOverride != "" {
		if old, ok := s.fixPRs[d.FixPR]; ok {
			sb.WriteString(previousFixSection(old.Base, old))
		}
	}
	return []PacketPart{{Path: ".gaos/packet/context/defect.md", Kind: KindContext, Body: sb.String()}}
}

// ---------------------------------------------------------------- the opened pull request

// OpenFixPR records a draft pull request GAOS opened: the FixPR record, the
// FixPROpened event, the defect's move to fixing and the end of the fix card,
// in one transaction.
func (s *MemStore) OpenFixPR(ctx context.Context, productID, taskID string, f FixPR) (Result, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.masterPtr(productID) == nil {
		return Result{}, ErrNotFound
	}
	t := s.taskByIDLocked(taskID)
	d := s.defects[f.DefectKey]
	if t == nil || t.DeskID != productID || t.Role != "fixer" || d == nil || d.ProductID != productID || d.FixTask != taskID {
		return Result{}, ErrNotFound
	}
	f.ProductID, f.TaskID, f.State = productID, taskID, FixPRDraft
	f.UpdatedAt = autoFixNow()
	s.fixPRs[f.ID] = f
	// A pull request a retarget opened marks the one it replaced, in the same transaction (design §6.4).
	var replaced []FixPR
	if old, ok := s.fixPRs[f.RetargetedFrom]; ok && f.RetargetedFrom != "" && old.ProductID == productID && old.RetargetedTo == "" {
		old.RetargetedTo = f.ID
		s.fixPRs[old.ID] = old
		replaced = append(replaced, old)
	}
	d.FixPR = f.ID
	// The pull request exists whatever happened to the card meanwhile, so a card
	// that is no longer in review does not stop the record.
	evs, err := s.resolveCardLocked(t, StatusDone, "fix-pr-opened", RuleFixPR)
	if err != nil {
		evs = nil
	}
	evs = append(evs, s.defectEventLocked(FixPROpened, productID,
		fmt.Sprintf("draft pull request opened for defect %s: %s#%d", d.Ref(), f.Repo, f.Number), KindInfo,
		EventPayload{Task: d.Key, Rule: RuleFixPR, To: FixPRDraft, Reason: fmt.Sprintf("%s#%d", f.Repo, f.Number)}))
	if d.State == DefectStateTriaged {
		evs = append(evs, s.setDefectStateLocked(d, DefectStateFixing, RuleFixPR, ""))
	}
	return Result{Floor: s.floor(), NewEvents: evs, Defects: []Defect{d.Clone()}, FixPRs: append([]FixPR{f}, replaced...)}, nil
}

func (s *ArcadeStore) OpenFixPR(ctx context.Context, productID, taskID string, f FixPR) (Result, error) {
	return s.mutate(ctx, func() (Result, error) { return s.proj.OpenFixPR(ctx, productID, taskID, f) })
}

func (readOnly) OpenFixPR(context.Context, string, string, FixPR) (Result, error) {
	return Result{}, reject(replayRefusal)
}
