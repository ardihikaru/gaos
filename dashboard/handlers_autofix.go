package main

import (
	"errors"
	"net/http"
	"net/url"
	"os"
	"strings"
)

// The auto-fix back-office routes (D-091, design §5.2, §6.1): the product's
// configuration, restricted to the accounts in GAOS_AUTOFIX_ADMINS, and the
// operator's close of a defect. The Auto-fix view itself is a later phase.

// autoFixAdmin reports whether an account may change a product's auto-fix
// configuration. The file named by GAOS_AUTOFIX_ADMINS lists one address per line;
// it must be mode 600, and a missing or loose file means nobody (OQ-37).
func autoFixAdmin(email string) bool {
	path := strings.TrimSpace(os.Getenv("GAOS_AUTOFIX_ADMINS"))
	if path == "" || email == "" {
		return false
	}
	st, err := os.Stat(path)
	if err != nil || !st.Mode().IsRegular() || st.Mode().Perm()&0o077 != 0 {
		return false
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "#") && strings.EqualFold(line, email) {
			return true
		}
	}
	return false
}

// formLines reads a textarea: one entry per line, trimmed, without repeats.
func formLines(v string) []string {
	var out []string
	seen := map[string]bool{}
	for _, l := range strings.Split(strings.ReplaceAll(v, "\r\n", "\n"), "\n") {
		if l = strings.TrimSpace(l); l != "" && !seen[l] {
			seen[l] = true
			out = append(out, l)
		}
	}
	return out
}

func formBool(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "on", "1", "true", "yes":
		return true
	}
	return false
}

// autoFixFromForm reads the configuration form. A missing field is empty: the
// form replaces the whole record.
func autoFixFromForm(r *http.Request) (AutoFix, error) {
	atoi := func(k string) (int, error) {
		v := strings.TrimSpace(r.PostFormValue(k))
		if v == "" {
			return 0, nil
		}
		n := atoiDefault(v, -1)
		if n < 0 {
			return 0, reject("A cap is a whole number.")
		}
		return n, nil
	}
	fix, err := atoi("fixCap")
	if err != nil {
		return AutoFix{}, err
	}
	triage, err := atoi("triageCap")
	if err != nil {
		return AutoFix{}, err
	}
	review, err := atoi("reviewFixCap")
	if err != nil {
		return AutoFix{}, err
	}
	return AutoFix{
		Enabled: formBool(r.PostFormValue("enabled")), FixCap: fix, TriageCap: triage,
		AllowedGlobs: formLines(r.PostFormValue("allowedGlobs")), ForbiddenGlobs: formLines(r.PostFormValue("forbiddenGlobs")),
		TestGlobs: formLines(r.PostFormValue("testGlobs")), PRTemplate: strings.TrimSpace(r.PostFormValue("prTemplate")),
		CIConfirmed: formBool(r.PostFormValue("ciConfirmed")),
		BaseBranch:  strings.TrimSpace(r.PostFormValue("baseBranch")),
		TargetRepos: formLines(r.PostFormValue("targetRepos")),
		// An omitted field clears the list, like every field of this form (the whole record is replaced).
		AllowedBases:    formLines(r.PostFormValue("allowedBases")),
		AutomationRepos: formLines(r.PostFormValue("automationRepos")),
		// D-102: the review fix's branch patterns and its daily cap.
		ReviewFixHeads: formLines(r.PostFormValue("reviewFixHeads")),
		ReviewFixCap:   review,
	}, nil
}

func (s *server) autoFixError(w http.ResponseWriter, r *http.Request, err error) {
	if wantsHTMX(r) {
		s.fail(w, r, err)
		return
	}
	if errors.Is(err, ErrNotFound) {
		writeBoardJSON(w, http.StatusNotFound, map[string]string{"error": "no such record"})
		return
	}
	writeBoardJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
}

// handleSaveAutoFix is POST /products/{id}/autofix (a form). Turning auto-fix on
// is refused 422 unless §6.1's preconditions hold.
func (s *server) handleSaveAutoFix(w http.ResponseWriter, r *http.Request) {
	ss, ok := sessionOf(r)
	if !ok || ss.Email == "" {
		writeBoardJSON(w, http.StatusUnauthorized, map[string]string{"error": "sign in to the back office"})
		return
	}
	if !autoFixAdmin(ss.Email) {
		writeBoardJSON(w, http.StatusForbidden, map[string]string{"error": "auto-fix settings are restricted to the accounts listed for it"})
		return
	}
	id := r.PathValue("id")
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	if err := r.ParseForm(); err != nil {
		writeBoardJSON(w, http.StatusBadRequest, map[string]string{"error": "the form could not be read"})
		return
	}
	a, err := autoFixFromForm(r)
	if err == nil {
		err = validateAutoFix(a, s.writeTokenKind)
	}
	if err != nil {
		s.autoFixError(w, r, err)
		return
	}
	kind := s.writeTokenKind
	if kind == "" {
		kind = "none"
	}
	res, err := s.mut(r).SetAutoFix(r.Context(), id, a, ss.Email, kind)
	if err != nil {
		s.autoFixError(w, r, err)
		return
	}
	if wantsHTMX(r) {
		s.renderBOAfter(w, r, res, id, "board")
		return
	}
	writeBoardJSON(w, http.StatusOK, map[string]string{"ok": "auto-fix settings saved"})
}

// handleCloseDefect is POST /products/{id}/defects/{ref}/close: an operator
// closes a defect by its defect id or case id. A record of another product is 404.
func (s *server) handleCloseDefect(w http.ResponseWriter, r *http.Request) {
	ss, ok := sessionOf(r)
	if !ok || ss.Email == "" {
		writeBoardJSON(w, http.StatusUnauthorized, map[string]string{"error": "sign in to the back office"})
		return
	}
	id, ref := r.PathValue("id"), r.PathValue("ref")
	res, err := s.mut(r).CloseDefect(r.Context(), id, ref, ss.Email, autoFixAdmin(ss.Email))
	if errors.Is(err, ErrUnitHeld) {
		s.failStatus(w, r, err, http.StatusConflict)
		return
	}
	if err != nil {
		s.autoFixError(w, r, err)
		return
	}
	if wantsHTMX(r) {
		s.renderBOAfter(w, r, res, id, "board")
		return
	}
	writeBoardJSON(w, http.StatusOK, map[string]string{"ok": "defect closed"})
}

// handleSaveUnitChecks is POST /products/{id}/autofix/unit (D-105 §4.1): an auto-fix
// admin saves a product's unit checks. 401 without a session, 403 for anyone else,
// then the 64 KiB body; a refusal is 422 like the auto-fix settings.
func (s *server) handleSaveUnitChecks(w http.ResponseWriter, r *http.Request) {
	ss, ok := sessionOf(r)
	if !ok || ss.Email == "" {
		writeBoardJSON(w, http.StatusUnauthorized, map[string]string{"error": "sign in to the back office"})
		return
	}
	if !autoFixAdmin(ss.Email) {
		writeBoardJSON(w, http.StatusForbidden, map[string]string{"error": "unit checks are restricted to the accounts listed for auto-fix"})
		return
	}
	id := r.PathValue("id")
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	if err := r.ParseForm(); err != nil {
		writeBoardJSON(w, http.StatusBadRequest, map[string]string{"error": "the form could not be read"})
		return
	}
	u := UnitChecks{Enabled: formBool(r.PostFormValue("enabled")), Workflow: strings.TrimSpace(r.PostFormValue("workflow")),
		CheckName: strings.TrimSpace(r.PostFormValue("checkName")), Bases: formLines(r.PostFormValue("bases"))}
	if v := strings.TrimSpace(r.PostFormValue("dailyCap")); v != "" {
		u.DailyCap = atoiDefault(v, -1)
	}
	var err error
	switch {
	case u.Enabled && s.codeSrc == nil:
		err = reject("GitHub is not configured; unit checks need it.")
	case u.Enabled && s.btConsumer == nil:
		err = reject("Brain Town is not connected; no merge would reach GAOS.")
	}
	var res Result
	if err == nil {
		res, err = s.mut(r).SetUnitChecks(r.Context(), id, u, ss.Email)
	}
	if err != nil {
		s.autoFixError(w, r, err)
		return
	}
	if wantsHTMX(r) {
		v := s.applyResult(r, res)
		r.URL.RawQuery = "product=" + url.QueryEscape(id) // the page the admin keeps sight of
		s.sheet(w, r, v, "autofix", "", "")
		if v.BO != nil && v.BO.AutoFix != nil {
			for i := range v.BO.AutoFix.Units {
				if v.BO.AutoFix.Units[i].ProductID == id {
					v.BO.AutoFix.Units[i].Open = true
					v.BO.AutoFix.UnitSaved = id
				}
			}
		}
		s.rend.RenderWithChrome(w, "bo-main-oob", v)
		return
	}
	writeBoardJSON(w, http.StatusOK, map[string]string{"ok": "unit checks saved"})
}
