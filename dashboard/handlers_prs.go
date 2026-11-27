package main

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// The retarget route (D-092, docs/designs/gaos-pr-overview.md §6.2). A back-office POST: the gate's
// session and origin checks apply; the route adds the admin list, the record checks, the one GitHub
// read, and then the store mutation, which repeats every check under its lock.

// branchChecker answers whether a branch exists in a repository: the read client.
type branchChecker interface {
	branchExists(ctx context.Context, owner, repo, name string) (bool, error)
}

// retargetFail answers a refusal as a toast with the status of the design's table.
func (s *server) retargetFail(w http.ResponseWriter, r *http.Request, status int, msg string) {
	v, _ := s.newView(r)
	v.Toast = msg
	w.WriteHeader(status)
	s.rend.RenderWithChrome(w, "empty", v)
}

// retargetPrechecks are the record checks that need no lock (design §6.2 step 4), run before the one
// GitHub read so a stale tab cannot make GAOS call GitHub about a pull request that cannot change.
func retargetPrechecks(m Master, f FixPR, base string) error {
	switch {
	case !strings.HasPrefix(f.Branch, fixRefPrefix):
		return retargetRefusal(http.StatusUnprocessableEntity, "GAOS only changes the base of pull requests it opened (branch gaos/fix-…).")
	case f.State != FixPRDraft && f.State != FixPRReady:
		return retargetRefusal(http.StatusUnprocessableEntity, "This pull request is closed. Nothing to change.")
	case f.RetargetedTo != "":
		return retargetRefusal(http.StatusConflict, "This pull request was already retargeted (see the newer one). Open that one to retarget it again.")
	case f.Base == "":
		return retargetRefusal(http.StatusUnprocessableEntity, "The base branch of this pull request is not known yet. GAOS reads it within 5 minutes; try again then.")
	case strings.EqualFold(f.Base, base):
		return retargetRefusal(http.StatusUnprocessableEntity, "That is already the base.")
	}
	owner, repo, ok := ghOwnerRepo(f.Repo)
	if !ok || !productOwns(defectOwnedRepos(m), owner, repo) {
		return retargetRefusal(http.StatusUnprocessableEntity, "This repository is not one of the product's.")
	}
	return nil
}

// handleRetarget is POST /products/{id}/prs/{fixPRID}/retarget (form field base).
func (s *server) handleRetarget(w http.ResponseWriter, r *http.Request) {
	ss, ok := sessionOf(r)
	if !ok || ss.Email == "" {
		writeBoardJSON(w, http.StatusUnauthorized, map[string]string{"error": "sign in to the back office"})
		return
	}
	if !autoFixAdmin(ss.Email) {
		s.retargetFail(w, r, http.StatusForbidden, "Only accounts on the auto-fix admin list can change a base branch. Ask an admin, or ask to be added.")
		return
	}
	id, fixID := r.PathValue("id"), r.PathValue("fixPRID")
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	if err := r.ParseForm(); err != nil {
		s.retargetFail(w, r, http.StatusBadRequest, "The form could not be read.")
		return
	}
	base := strings.TrimSpace(r.PostFormValue("base"))
	if !baseBranchRe.MatchString(base) || strings.Contains(base, "..") {
		s.retargetFail(w, r, http.StatusUnprocessableEntity, "That is not a valid branch name.")
		return
	}
	ctx := r.Context()
	recs, err := s.store.FixPRs(ctx, id)
	if err != nil {
		s.retargetFail(w, r, http.StatusNotFound, "No such pull request.")
		return
	}
	var rec FixPR
	found := false
	for _, f := range recs {
		if f.ID == fixID {
			rec, found = f, true
		}
	}
	m, merr := s.store.Master(ctx, id)
	if !found || merr != nil {
		s.retargetFail(w, r, http.StatusNotFound, "No such pull request.")
		return
	}
	if err := retargetPrechecks(m, rec, base); err != nil {
		var re *RetargetError
		if errors.As(err, &re) {
			s.retargetFail(w, r, re.Status, re.Msg)
			return
		}
		s.retargetFail(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	if s.branches == nil {
		s.retargetFail(w, r, http.StatusUnprocessableEntity, "GitHub is not configured for reading, so the branch cannot be checked.")
		return
	}
	owner, repo, _ := ghOwnerRepo(rec.Repo)
	exists, err := s.branches.branchExists(ctx, owner, repo, base)
	switch {
	case err != nil:
		s.retargetFail(w, r, http.StatusBadGateway, "GitHub cannot be reached; try again in a minute.")
		return
	case !exists:
		s.retargetFail(w, r, http.StatusUnprocessableEntity, "That branch does not exist in "+rec.Repo+".")
		return
	}
	res, err := s.mut(r).RetargetFixPR(ctx, id, fixID, base, ss.Email, s.writeTokenKind)
	if err != nil {
		var re *RetargetError
		switch {
		case errors.As(err, &re):
			s.retargetFail(w, r, re.Status, re.Msg)
		case errors.Is(err, ErrNotFound):
			s.retargetFail(w, r, http.StatusNotFound, "No such pull request.")
		default:
			s.retargetFail(w, r, http.StatusUnprocessableEntity, err.Error())
		}
		return
	}
	s.renderBOView(w, r, res, "prs")
}

// handleCloseFixPR is POST /products/{id}/prs/{fixPRID}/close (D-108, design §5.3): an auto-fix
// admin closes, on GitHub, an open fix pull request the Auto-fix page lists as a duplicate. The
// gate's session and origin checks apply; a replay refuses before any GitHub call; GitHub is read
// first, so a pull request already closed or merged is recorded as it is; the store re-checks the
// finding under its lock. The branch is kept and the defect is never moved.
func (s *server) handleCloseFixPR(w http.ResponseWriter, r *http.Request) {
	ss, ok := sessionOf(r)
	if !ok || ss.Email == "" {
		writeBoardJSON(w, http.StatusUnauthorized, map[string]string{"error": "sign in to the back office"})
		return
	}
	if !autoFixAdmin(ss.Email) {
		s.retargetFail(w, r, http.StatusForbidden, "Only accounts on the auto-fix admin list can close a pull request.")
		return
	}
	mut := s.mut(r)
	if _, replay := mut.(readOnly); replay {
		s.retargetFail(w, r, http.StatusConflict, "This view is a replay; nothing is written.")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	if err := r.ParseForm(); err != nil {
		s.retargetFail(w, r, http.StatusBadRequest, "The form could not be read.")
		return
	}
	ctx := r.Context()
	id, fixID := r.PathValue("id"), r.PathValue("fixPRID")
	recs, err := s.store.FixPRs(ctx, id)
	m, merr := s.store.Master(ctx, id)
	var rec FixPR
	for _, f := range recs {
		if f.ID == fixID {
			rec = f
		}
	}
	if err != nil || merr != nil || rec.ID == "" {
		s.retargetFail(w, r, http.StatusNotFound, "No such pull request.")
		return
	}
	defs := map[string]Defect{}
	if ds, err := s.store.Defects(ctx, id); err == nil {
		for _, d := range ds {
			defs[d.Key] = d
		}
	}
	listed := false
	for _, dup := range afDuplicates(recs, defs) {
		listed = listed || dup.Close == fixID
	}
	switch {
	case !listed:
		s.retargetFail(w, r, http.StatusConflict, "This pull request is not a duplicate any more. Reload the page.")
		return
	case !closableFix(m, rec):
		s.retargetFail(w, r, http.StatusUnprocessableEntity, "GAOS only closes pull requests it opened (branch gaos/fix-…).")
		return
	case s.fixWriter == nil:
		s.retargetFail(w, r, http.StatusUnprocessableEntity, "The GitHub write token is not configured.")
		return
	}
	owner, repo, _ := ghOwnerRepo(rec.Repo)
	ghFail := func(err error) {
		switch c := ghErrorClass(err); c {
		case "token", "refused":
			s.retargetFail(w, r, http.StatusBadGateway, "GitHub refused the request ("+c+"); check the write token.")
		default:
			s.retargetFail(w, r, http.StatusBadGateway, "GitHub cannot be reached; try again in a minute.")
		}
	}
	state, err := s.fixWriter.prState(ctx, owner, repo, rec.Number)
	if err != nil {
		ghFail(err)
		return
	}
	ref := rec.Repo + "#" + strconv.Itoa(rec.Number)
	var res Result
	if state != "open" {
		res, err = mut.AdvanceFixPR(ctx, id, FixPR{ID: rec.ID, State: state, UpdatedAt: autoFixNow()})
		res.Toast = ref + " was already " + state + " on GitHub."
	} else {
		if err := s.fixWriter.closePR(ctx, owner, repo, rec.Number); err != nil {
			ghFail(err)
			return
		}
		res, err = mut.CloseFixPR(ctx, id, rec.ID, RuleDedup, ss.Email)
		res.Toast = "Closed " + ref + "."
	}
	if err != nil {
		var re *RetargetError
		if errors.As(err, &re) {
			s.retargetFail(w, r, re.Status, re.Msg)
			return
		}
		s.retargetFail(w, r, http.StatusConflict, err.Error())
		return
	}
	// The page the admin keeps sight of: the Auto-fix view of the posted filter.
	q := url.Values{}
	for _, k := range []string{"product", "state", "difficulty", "page"} {
		if v := r.PostFormValue(k); v != "" {
			q.Set(k, v)
		}
	}
	r.URL.RawQuery = q.Encode()
	s.renderBOView(w, r, res, "autofix")
}
