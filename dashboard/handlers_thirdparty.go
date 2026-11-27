package main

import (
	"errors"
	"net/http"
)

// handleSaveThirdParty is POST /products/{id}/third-party (D-104 §3.2), a form of kind, enabled
// and productCode. It is restricted to the auto-fix admins: no session is 401, another account
// 403, a second enabled binding of the kind 409.
func (s *server) handleSaveThirdParty(w http.ResponseWriter, r *http.Request) {
	ss, ok := sessionOf(r)
	if !ok || ss.Email == "" {
		writeBoardJSON(w, http.StatusUnauthorized, map[string]string{"error": "sign in to the back office"})
		return
	}
	if !autoFixAdmin(ss.Email) {
		writeBoardJSON(w, http.StatusForbidden, map[string]string{"error": "third-party settings are restricted to the accounts listed for auto-fix"})
		return
	}
	id := r.PathValue("id")
	r.Body = http.MaxBytesReader(w, r.Body, 8<<10)
	if err := r.ParseForm(); err != nil {
		writeBoardJSON(w, http.StatusBadRequest, map[string]string{"error": "the form could not be read"})
		return
	}
	link := ThirdPartyLink{Kind: r.FormValue("kind"), Enabled: formBool(r.FormValue("enabled")), ProductCode: r.FormValue("productCode")}
	res, err := s.mut(r).SetThirdParty(r.Context(), id, link, ss.Email)
	if errors.Is(err, ErrConflict) {
		writeBoardJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}
	if err != nil {
		s.autoFixError(w, r, err)
		return
	}
	if s.tm != nil {
		s.tm.checks.clear() // a result cached under the old binding no longer holds
		s.tm.people.Kick()
	}
	if wantsHTMX(r) {
		s.renderBOView(w, r, res, "integrations")
		return
	}
	writeBoardJSON(w, http.StatusOK, map[string]string{"ok": "third-party binding saved"})
}
