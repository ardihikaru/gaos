package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Creating the draft pull request (D-091, design §8.3). GAOS does it, in the
// dashboard process: the write token never enters a workspace or a packet. The
// agent's reply is persisted on the FixPR record before the first write, every
// step is idempotent and keyed by the branch, and GAOS never marks a pull
// request ready, merges, force-updates or pushes to a default branch.

// The labels every auto-fix pull request carries.
var fixPRLabels = []string{"ai-assisted", "defect-auto-fix"}

var slugRe = regexp.MustCompile(`[^a-z0-9]+`)

// fixBranch is gaos/fix-<slug>: the lower-cased defect or case id, at most 60
// characters, with -<gen> appended for a later generation.
func fixBranch(d Defect) string {
	slug := strings.Trim(slugRe.ReplaceAllString(strings.ToLower(d.Ref()), "-"), "-")
	if len(slug) > 60 {
		slug = strings.Trim(slug[:60], "-")
	}
	if slug == "" {
		slug = "defect"
	}
	if d.Gen > 1 {
		slug += "-" + strconv.Itoa(d.Gen)
	}
	return fixRefPrefix + slug
}

// fixPRID is the record's id.
func fixPRID(d Defect) string {
	return "fx-" + sanitizeIDPart(d.Key) + "-" + strconv.Itoa(max(d.Gen, 1))
}

// fixCommitSubject is `fix(<first path segment>): <summary> (<defect id>)`.
func fixCommitSubject(plan fixPlan, summary, ref string) string {
	seg := strings.SplitN(plan.Files[0].Path, "/", 2)[0]
	if !strings.Contains(plan.Files[0].Path, "/") {
		seg = strings.TrimSuffix(seg, path.Ext(seg))
	}
	return fmt.Sprintf("fix(%s): %s (%s)", seg, prSummaryLine(summary), prInlineText(ref, 40))
}

// fixCommitMessage carries the trailers adoption looks for.
func fixCommitMessage(subject, key string) string {
	return subject + "\n\n" + fixTrailer(key) + "\nAI-Assisted: true\n"
}

func fixTrailer(key string) string { return "GAOS-Defect: " + key }

func hasTrailer(message, key string) bool {
	for _, l := range strings.Split(message, "\n") {
		if strings.TrimSpace(l) == fixTrailer(key) {
			return true
		}
	}
	return false
}

// fixEligible is §5.3's record-based rule. The answer is "" when a pull request
// may be opened, else why not. It looks at the record and the product's
// configuration, never at a label, whoever dispatched the card.
func fixEligible(d Defect, taskID string, gen int, af AutoFix, tokenKind string) string {
	switch {
	case d.FixTask != taskID:
		return "the record names another fix card"
	case d.Difficulty != "easy":
		return "the verdict is not easy"
	case d.AutomationType != "be":
		return "the defect is not a backend defect"
	case d.Regression:
		return "the defect is a regression"
	case d.Gen != gen:
		return "the card is of an older generation"
	case d.State != DefectStateTriaged:
		return "the defect is " + d.State
	case !af.Enabled:
		return "auto-fix is off"
	case autoFixPreconditions(af, tokenKind) != nil:
		return "the auto-fix preconditions do not hold"
	}
	for _, r := range d.TargetRepos {
		if strings.EqualFold(r, d.Repo) {
			return ""
		}
	}
	return "the repository is not a kept target"
}

// defectFixer is the after-run hook of a fixer card and the boot job.
type defectFixer struct {
	st        Store
	gh        *ghWriter // nil: the write token is not configured
	tokenKind string
	notify    func(ctx context.Context, t TaskRecord, kind, text string)
	logf      func(string, ...any)

	mu   sync.Mutex
	busy map[string]bool
}

func (p *defectFixer) claim(key string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.busy == nil {
		p.busy = map[string]bool{}
	}
	if p.busy[key] {
		return false
	}
	p.busy[key] = true
	return true
}

func (p *defectFixer) release(key string) {
	p.mu.Lock()
	delete(p.busy, key)
	p.mu.Unlock()
}

// afterRun reads a finished fixer's reply. A card that is not eligible ends in
// Review with its reply (the edits) stored as the run's artifact: no pull
// request, whatever the card was labelled.
func (p *defectFixer) afterRun(ctx context.Context, t TaskRecord, reply string) {
	if t.Role != "fixer" || t.Status != StatusReview {
		return
	}
	key, gen, job, ok := parseDefectCardKey(t.DedupKey)
	if !ok || job != defectJobFix {
		return
	}
	if !p.claim(key) {
		return
	}
	defer p.release(key)
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	d, err := p.st.DefectByKey(ctx, t.DeskID, key)
	if err != nil {
		return
	}
	af, err := p.st.AutoFixOf(ctx, t.DeskID)
	if err != nil {
		return
	}
	if why := fixEligible(d, t.ID, gen, af, p.tokenKind); why != "" || p.gh == nil {
		p.logf("autofix: %s ends in review without a pull request", t.ID)
		return
	}
	fr, err := parseFix(reply)
	if err != nil {
		code := "fix-missing"
		var fe *fixError
		if errors.As(err, &fe) {
			code = fe.Code
		}
		p.logf("autofix: unreadable fix reply: %s", code)
		_, _ = p.st.ResolvePRCard(ctx, t.DeskID, t.ID, StatusBlocked, BlockUnreadable)
		return
	}
	p.run(ctx, t, d, af, fr, FixPR{})
}

// block ends the card BLOCKED with a reason and closes the record, if any.
func (p *defectFixer) block(ctx context.Context, t TaskRecord, rec FixPR, reason, detail string) {
	p.logf("autofix: %s blocked %s (%s)", t.ID, reason, detail)
	_, _ = p.st.ResolvePRCard(ctx, t.DeskID, t.ID, StatusBlocked, reason)
	p.closeRecord(ctx, rec)
}

func (p *defectFixer) closeRecord(ctx context.Context, rec FixPR) {
	if rec.ID != "" && rec.State == FixPRPreparing {
		rec.State, rec.UpdatedAt = FixPRClosed, autoFixNow()
		_, _ = p.st.UpsertFixPR(ctx, rec)
	}
}

// failed classifies an error: GAOS's own refusals block the card, anything else
// is GitHub refusing a write and fails it github-<class>.
func (p *defectFixer) failed(ctx context.Context, t TaskRecord, rec FixPR, err error) {
	var fb *fixBlock
	switch {
	case errors.As(err, &fb):
		p.block(ctx, t, rec, fb.Reason, fb.Detail)
	case errors.Is(err, errDraftUnsupported):
		p.block(ctx, t, rec, BlockDraftUnsupported, "draft")
	default:
		class := ghErrorClass(err)
		p.logf("autofix: %s failed github-%s", t.ID, class)
		_, _ = p.st.ResolvePRCard(ctx, t.DeskID, t.ID, StatusFailed, "github-"+class)
		p.closeRecord(ctx, rec)
	}
}

// run is §8.3. rec is the stored record of a resumed run, or the zero value.
func (p *defectFixer) run(ctx context.Context, t TaskRecord, d Defect, af AutoFix, fr FixReply, rec FixPR) {
	owner, repo, ok := ghOwnerRepo(d.Repo)
	if !ok || p.gh == nil {
		p.block(ctx, t, rec, BlockUnreadable, "no repository")
		return
	}
	packet := map[string]DefectContextFile{}
	for _, f := range d.Context.Files {
		if strings.EqualFold(f.Repo, d.Repo) {
			packet[f.Path] = f
		}
	}
	base, head, err := p.gh.head(ctx, owner, repo, af.BaseBranch)
	if err != nil {
		p.failed(ctx, t, rec, err)
		return
	}
	plan, err := checkFix(fr, packet, af, func(fp string) (ghTreeEntry, error) {
		return p.gh.treeEntry(ctx, owner, repo, head, fp)
	})
	if err != nil {
		p.failed(ctx, t, rec, err)
		return
	}
	if screenBody(fr.Summary + "\n" + fr.Before + "\n" + fr.After) {
		p.block(ctx, t, rec, BlockEgress, "the pull-request text matched a secret detector")
		return
	}
	branch := fixBranch(d)
	wfs, unreadable, err := p.gh.workflows(ctx, owner, repo, head)
	if err != nil {
		p.failed(ctx, t, rec, err)
		return
	}
	if len(unreadable) > 0 {
		p.block(ctx, t, rec, BlockCISecrets, "a workflow could not be read")
		return
	}
	changed := make([]string, 0, len(plan.Files))
	for _, f := range plan.Files {
		changed = append(changed, f.Path)
	}
	if wf, risky := ciSecretsRisk(wfs, branch, base, changed); risky {
		p.block(ctx, t, rec, BlockCISecrets, wf)
		return
	}

	// The parsed reply is stored before the first write, so a restart never pays
	// for a second agent turn.
	now := autoFixNow()
	if rec.ID == "" {
		stored, _ := json.Marshal(fr)
		rec = FixPR{ID: fixPRID(d), ProductID: d.ProductID, DefectKey: d.Key, Repo: d.Repo, State: FixPRPreparing,
			CreatedAt: now, TaskID: t.ID, Difficulty: d.Difficulty, Branch: branch, BaseSHA: head, Base: baseIfValid(base),
			Before: capBytes(stripBR53(fr.Before), 1<<10), After: capBytes(stripBR53(fr.After), 1<<10), Reply: string(stored)}
	}
	rec.UpdatedAt = now
	if _, err := p.st.UpsertFixPR(ctx, rec); err != nil {
		p.logf("autofix: the record could not be stored: %v", err)
		return
	}

	subject := fixCommitSubject(plan, fr.Summary, d.Ref())
	in := prBodyIn{Defect: d.Ref(), Title: d.Title, Summary: fr.Summary, Before: fr.Before, After: fr.After,
		Limits: fmt.Sprintf("%d file(s); %d changed line(s) outside tests (cap %d), %d in tests (cap %d).",
			len(plan.Files), plan.NonTest, fixMaxNonTest, plan.TestLines, fixMaxTest)}
	for _, f := range plan.Files {
		in.Files = append(in.Files, f.Path)
	}

	created := false
	if rec.Number == 0 {
		headCommit, err := p.gh.commit(ctx, owner, repo, head)
		if err != nil {
			p.failed(ctx, t, rec, err)
			return
		}
		items := make([]ghTreeItem, 0, len(plan.Files))
		for _, f := range plan.Files {
			blob, err := p.gh.createBlob(ctx, owner, repo, f.New)
			if err != nil {
				p.failed(ctx, t, rec, err)
				return
			}
			items = append(items, ghTreeItem{Path: f.Path, Mode: f.Mode, Type: "blob", SHA: blob})
		}
		tree, err := p.gh.createTree(ctx, owner, repo, headCommit.Tree, items)
		if err != nil {
			p.failed(ctx, t, rec, err)
			return
		}
		commit, err := p.gh.createCommit(ctx, owner, repo, fixCommitMessage(subject, d.Key), tree, head)
		if err != nil {
			p.failed(ctx, t, rec, err)
			return
		}
		commit, created, err = p.adoptOrCreate(ctx, owner, repo, branch, commit, tree, d.Key)
		if err != nil {
			p.failed(ctx, t, rec, err)
			return
		}
		rec.CommitSHA = commit
		num, url, found, err := p.gh.findPR(ctx, owner, repo, branch)
		if err == nil && !found {
			num, url, err = p.gh.createPR(ctx, owner, repo, subject, branch, base, renderPRBody(af.PRTemplate, in))
		}
		if err != nil {
			if created {
				if derr := p.gh.deleteRef(ctx, owner, repo, branch); derr != nil {
					p.logf("autofix: the branch of %s could not be deleted", t.ID)
				}
			}
			p.failed(ctx, t, rec, err)
			return
		}
		rec.Number, rec.URL = num, url
		rec.UpdatedAt = autoFixNow()
		_, _ = p.st.UpsertFixPR(ctx, rec)
		// The pull request exists: a label or comment that fails is logged, and
		// the branch is kept (deleting it would close the pull request).
		if err := p.gh.addComment(ctx, owner, repo, num, prCommentBody(in)); err != nil {
			p.logf("autofix: the comment of %s#%d could not be posted: %s", rec.Repo, num, ghErrorClass(err))
		}
	}
	if err := p.gh.addLabels(ctx, owner, repo, rec.Number, fixPRLabels); err != nil {
		p.logf("autofix: the labels of %s#%d could not be set: %s", rec.Repo, rec.Number, ghErrorClass(err))
	}
	if _, err := p.st.OpenFixPR(ctx, d.ProductID, t.ID, rec); err != nil {
		p.logf("autofix: the opened pull request could not be recorded: %v", err)
		return
	}
	if p.notify != nil {
		p.notify(ctx, t, "fix:pr", d.ProductID+" · auto-fix draft PR "+d.Ref()+": "+rec.URL)
		rec.State, rec.WaSent, rec.UpdatedAt = FixPRDraft, true, autoFixNow()
		_, _ = p.st.UpsertFixPR(ctx, rec)
	}
}

// adoptOrCreate creates the branch, or adopts one that is already there only
// when its head commit carries our trailer and its tree equals ours. A ref is
// never moved or overwritten. created says this run made the ref.
func (p *defectFixer) adoptOrCreate(ctx context.Context, owner, repo, branch, commit, tree, key string) (string, bool, error) {
	for attempt := 0; attempt < 2; attempt++ {
		sha, exists, err := p.gh.refSHA(ctx, owner, repo, branch)
		if err != nil {
			return "", false, err
		}
		if exists {
			cm, err := p.gh.commit(ctx, owner, repo, sha)
			if err != nil {
				return "", false, err
			}
			if !hasTrailer(cm.Message, key) || cm.Tree != tree {
				return "", false, &fixBlock{BlockBranchExists, "the branch is not ours"}
			}
			return sha, false, nil
		}
		err = p.gh.createRef(ctx, owner, repo, branch, commit)
		if err == nil {
			return commit, true, nil
		}
		if !errors.Is(err, errRefExists) {
			return "", false, err
		}
	}
	return "", false, &fixBlock{BlockBranchExists, "the branch appeared while it was created"}
}

// resumeFixPRs is the boot job (§8.3): every `preparing` record runs again from
// its stored reply, and a fixer card in review with no record is read again from
// its stored reply artifact.
func (p *defectFixer) resumeFixPRs(ctx context.Context) {
	f, err := p.st.Snapshot(ctx)
	if err != nil || f.Stats.Replay || p.gh == nil {
		return
	}
	for _, desk := range f.Desks {
		pid := desk.ID
		recs, _ := p.st.FixPRs(ctx, pid)
		have := map[string]bool{}
		for _, r := range recs {
			have[r.TaskID] = true
			if r.State != FixPRPreparing {
				continue
			}
			p.resumeOne(ctx, pid, r)
		}
		cards, _ := p.st.PRCards(ctx, pid)
		for _, c := range cards {
			if c.Role != "fixer" || c.Status != StatusReview || have[c.ID] {
				continue
			}
			conv, err := p.st.Conversation(ctx, pid, c.ID)
			if err != nil || len(conv.Attempts) == 0 {
				continue
			}
			p.afterRun(ctx, c, conv.Attempts[len(conv.Attempts)-1].Reply)
		}
	}
}

func (p *defectFixer) resumeOne(ctx context.Context, pid string, r FixPR) {
	if !p.claim(r.DefectKey) {
		return
	}
	defer p.release(r.DefectKey)
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	cards, _ := p.st.PRCards(ctx, pid)
	var card *TaskRecord
	for i := range cards {
		if cards[i].ID == r.TaskID {
			card = &cards[i]
		}
	}
	d, err := p.st.DefectByKey(ctx, pid, r.DefectKey)
	af, err2 := p.st.AutoFixOf(ctx, pid)
	var fr FixReply
	if card == nil || err != nil || err2 != nil || json.Unmarshal([]byte(r.Reply), &fr) != nil || card.Status != StatusReview {
		p.closeRecord(ctx, r)
		return
	}
	_, gen, _, _ := parseDefectCardKey(card.DedupKey)
	if why := fixEligible(d, card.ID, gen, af, p.tokenKind); why != "" {
		p.logf("autofix: a stored fix of %s is no longer eligible", card.ID)
		p.closeRecord(ctx, r)
		return
	}
	p.run(ctx, *card, d, af, fr, r)
}

// baseIfValid keeps a base branch name only if it looks like one: a name read from GitHub is
// stored, shown and put in a comment, so it is checked first (design §6.4).
func baseIfValid(b string) string {
	if prRefOK(b) {
		return b
	}
	return ""
}
