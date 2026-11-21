package main

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// The QA tool's door into the inbox (D-078, docs/designs/qa-inbox-api.md).
//
//	POST /v1/products/{id}/inbox                        ask the operator a question
//	GET  /v1/products/{id}/inbox/{externalId}           poll for the decision
//	POST /v1/products/{id}/inbox/{externalId}/answer    answer it yourself (D-083)
//
// All three take a client token (the QA token, or one from the clients file);
// the first two also take the admin one, which asks as the QA tool. A client
// reaches only its own products (any other is a 404) and only the items it
// asked. The QA tool's answers are also pushed to it (inbox_worker.go); this
// GET is what keeps working when the push does not, and any other client polls.

const (
	inboxIDMax       = 80
	inboxQuestionMax = 500
	inboxContextMax  = 4000
	inboxTitleMax    = 120
	inboxLabelMax    = 60
	inboxDescMax     = 200
)

// apiInboxID is the QA tool's id. It starts with a letter or digit so it can
// never be "." or "..", which a URL path would swallow.
var apiInboxID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,79}$`)

type apiInboxOption struct {
	Label       string `json:"label"`
	Description string `json:"description"`
}

type apiInboxBody struct {
	ExternalID     string           `json:"externalId"`
	Question       string           `json:"question"`
	Context        string           `json:"context"`
	Options        []apiInboxOption `json:"options"`
	ExpiresInHours *int             `json:"expiresInHours"`
	Title          string           `json:"title"`
}

// inboxText cleans text a human will read: CRLF becomes LF, control characters
// and bidirectional overrides are dropped (a tab and a newline stay), the ends
// are trimmed. Templates escape HTML; this keeps what is stored printable.
func inboxText(s string) string {
	s = strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\r", "\n")
	s = strings.Map(func(r rune) rune {
		switch {
		case r == '\n' || r == '\t':
			return r
		case r == utf8.RuneError, unicode.IsControl(r):
			return -1
		case r >= 0x202A && r <= 0x202E, r >= 0x2066 && r <= 0x2069, r == 0x200E, r == 0x200F:
			return -1
		}
		return r
	}, s)
	return strings.TrimSpace(s)
}

func runesOver(s string, n int) bool { return utf8.RuneCountInString(s) > n }

// buildInboxAsk validates and sanitises a body. The second result is an apiErr
// code and reason, empty when the body is good.
func buildInboxAsk(b apiInboxBody) (ExternalAsk, string, string) {
	if !apiInboxID.MatchString(b.ExternalID) {
		return ExternalAsk{}, "bad-external-id", "externalId is 1 to 80 characters of A-Z a-z 0-9 . _ : - and starts with a letter or digit"
	}
	q := inboxText(b.Question)
	switch {
	case q == "":
		return ExternalAsk{}, "bad-question", "question is required"
	case runesOver(q, inboxQuestionMax):
		return ExternalAsk{}, "bad-question", "question is at most 500 characters"
	}
	ctx := inboxText(b.Context)
	if runesOver(ctx, inboxContextMax) {
		return ExternalAsk{}, "bad-context", "context is at most 4000 characters"
	}
	title := strings.Join(strings.Fields(inboxText(b.Title)), " ")
	if runesOver(title, inboxTitleMax) {
		return ExternalAsk{}, "bad-title", "title is at most 120 characters"
	}
	hours := 0
	if b.ExpiresInHours != nil {
		hours = *b.ExpiresInHours
		if hours < 1 || hours > externalMaxHours {
			return ExternalAsk{}, "bad-expiry", "expiresInHours is 1 to 720"
		}
	}
	var opts []QuestionOption
	switch n := len(b.Options); {
	case n == 0:
		opts = defaultExternalOptions()
	case n < 2 || n > 3:
		return ExternalAsk{}, "bad-options", "options holds 2 or 3 items, or is left out for Approve / Reject / Needs changes"
	default:
		for _, o := range b.Options {
			l := strings.Join(strings.Fields(inboxText(o.Label)), " ")
			d := strings.Join(strings.Fields(inboxText(o.Description)), " ")
			if l == "" || runesOver(l, inboxLabelMax) {
				return ExternalAsk{}, "bad-options", "an option label is 1 to 60 characters"
			}
			if runesOver(d, inboxDescMax) {
				return ExternalAsk{}, "bad-options", "an option description is at most 200 characters"
			}
			opts = append(opts, QuestionOption{Label: l, Description: d})
		}
	}
	return ExternalAsk{ExternalID: b.ExternalID, Title: title, Question: q, Context: ctx,
		Options: opts, ExpiresInHours: hours}, "", ""
}

type apiInboxCreated struct {
	ID         string `json:"id"`
	ExternalID string `json:"externalId"`
	Status     string `json:"status"`
	Created    bool   `json:"created"`
}

// askerOf is who an ask is recorded as: the calling client, or -- for the
// admin token, which has no client -- the QA tool, as before D-083.
func askerOf(cl *apiClient) (name, label string) {
	if cl == nil || cl.Name == ClientQATools {
		return ClientQATools, ExternalSource
	}
	return cl.Name, cl.Name
}

// ownsItem is the owner rule (D-083): a client reads and answers only what it
// asked; the admin token reads anything.
func ownsItem(cl *apiClient, x ExternalItem) bool {
	return cl == nil || effectiveAsker(x.QuestionPayload) == cl.Name
}

func (s *server) apiInboxAsk(w http.ResponseWriter, r *http.Request) {
	scope, cl, ok := s.apiGate(w, r, scopeClient)
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
	var body apiInboxBody
	if !apiDecode(w, r, &body) {
		return
	}
	// Egress screen: nothing that looks like a secret reaches the inbox, the
	// event stream or the operator's screen. The same detector as the create door.
	raw, _ := json.Marshal(body)
	var tree any
	_ = json.Unmarshal(raw, &tree)
	if hit := secretInStrings(tree); hit != "" {
		apiErr(w, http.StatusBadRequest, "secret-in-body", "the body matches the secret detector "+hit+"; nothing was written")
		return
	}
	ask, code, why := buildInboxAsk(body)
	if code != "" {
		apiErr(w, http.StatusBadRequest, code, why)
		return
	}
	// The detector ran on the raw body; sanitising can JOIN a split secret (a token with a
	// bidi mark inside it), so the cleaned text is screened again.
	clean := []any{ask.Title, ask.Question, ask.Context}
	for _, o := range ask.Options {
		clean = append(clean, o.Label, o.Description)
	}
	if hit := secretInStrings(clean); hit != "" {
		apiErr(w, http.StatusBadRequest, "secret-in-body", "the cleaned text matches the secret detector "+hit+"; nothing was written")
		return
	}
	ask.AskedBy, ask.Label = askerOf(cl)
	item, created, _, err := s.store.AskExternal(r.Context(), id, ask)
	if err != nil {
		if errors.Is(err, ErrIDTaken) {
			apiErr(w, http.StatusConflict, "id-taken", err.Error())
			return
		}
		apiStoreErr(w, err)
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	apiJSON(w, status, apiInboxCreated{ID: item.ItemID, ExternalID: item.ExternalID, Status: item.Status, Created: created})
}

// apiInboxState is what a client polls, and the body of every answer outcome
// that names an item. The optional fields are absent while they have no value,
// so "choice" is there only once somebody chose; "source" is always present,
// null until an answer recorded one (D-083).
type apiInboxState struct {
	ExternalID  string     `json:"externalId"`
	Status      string     `json:"status"`
	Choice      int        `json:"choice,omitempty"`
	OptionLabel string     `json:"optionLabel,omitempty"`
	Text        string     `json:"text,omitempty"`
	AskedAt     int64      `json:"askedAt"`
	AnsweredAt  int64      `json:"answeredAt,omitempty"`
	ExpiresAt   int64      `json:"expiresAt"`
	Source      *apiSource `json:"source"`
}

func (s *server) apiInboxGet(w http.ResponseWriter, r *http.Request) {
	scope, cl, ok := s.apiGate(w, r, scopeClient)
	if !ok {
		return
	}
	id, ok := s.apiProduct(w, r, scope, cl)
	if !ok {
		return
	}
	ext := r.PathValue("externalId")
	if !apiInboxID.MatchString(ext) {
		apiErr(w, http.StatusNotFound, "not-found", "no such inbox item")
		return
	}
	x, err := s.store.ExternalGet(r.Context(), id, ext)
	if err == nil && !ownsItem(cl, x) {
		err = ErrNotFound
	}
	if err != nil {
		if err == ErrNotFound {
			apiErr(w, http.StatusNotFound, "not-found", "no such inbox item")
			return
		}
		apiStoreErr(w, err)
		return
	}
	apiJSON(w, http.StatusOK, inboxState(x))
}

func inboxState(x ExternalItem) apiInboxState {
	st := apiInboxState{ExternalID: x.ExternalID, Status: x.Status, Choice: x.Choice,
		OptionLabel: x.OptionLabel(), Text: x.Text, AskedAt: x.AskedAt, AnsweredAt: x.AnsweredAt, ExpiresAt: x.Deadline}
	if x.Status == ExtAnswered {
		st.Source = apiSourceOf(x.AnsweredBy)
	}
	return st
}

// ---------- answering (D-083) ----------

// apiInboxActor is who, at the client, gave the answer. Reported, not verified.
type apiInboxActor struct {
	Username string `json:"username"`
	Display  string `json:"display"`
}

type apiInboxAnswerBody struct {
	Choice int           `json:"choice"`
	Text   string        `json:"text"`
	Actor  apiInboxActor `json:"actor"`
}

// apiActorHandle is a client's handle for the person who answered.
var apiActorHandle = regexp.MustCompile(`^[A-Za-z0-9._@+-]{1,64}$`)

const apiActorDisplayMax = 64

// apiStateWith is an item's state with an error code and reason on top: the
// 409 and 410 bodies. The state has neither key, so nothing collides.
func apiStateWith(x ExternalItem, code, reason string) map[string]any {
	raw, _ := json.Marshal(inboxState(x))
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	m["error"], m["reason"] = code, reason
	return m
}

// apiInboxAnswer is POST /v1/products/{id}/inbox/{externalId}/answer: a client
// answers an item it asked (D-083, BR-57). The source is the client's record;
// only the actor's handle comes from the body. First answer wins; an identical
// retry by the same client is 200 with the first answer and writes nothing.
func (s *server) apiInboxAnswer(w http.ResponseWriter, r *http.Request) {
	scope, cl, ok := s.apiGate(w, r, scopeClient)
	if !ok {
		return
	}
	if cl == nil {
		apiErr(w, http.StatusForbidden, "forbidden", "the admin token is not a client; answer in the back office")
		return
	}
	id, ok := s.apiProduct(w, r, scope, cl)
	if !ok {
		return
	}
	ext := r.PathValue("externalId")
	if !apiInboxID.MatchString(ext) {
		apiErr(w, http.StatusNotFound, "not-found", "no such inbox item")
		return
	}
	release, ok := s.beginWrite(w, scope, cl)
	if !ok {
		return
	}
	defer release()
	var body apiInboxAnswerBody
	if !apiDecode(w, r, &body) {
		return
	}
	raw, _ := json.Marshal(body)
	var tree any
	_ = json.Unmarshal(raw, &tree)
	if hit := secretInStrings(tree); hit != "" {
		apiErr(w, http.StatusBadRequest, "secret-in-body", "the body matches the secret detector "+hit+"; nothing was written")
		return
	}
	text := inboxText(body.Text)
	user := inboxText(body.Actor.Username)
	display := strings.Join(strings.Fields(inboxText(body.Actor.Display)), " ")
	if hit := secretInStrings([]any{text, user, display}); hit != "" {
		apiErr(w, http.StatusBadRequest, "secret-in-body", "the cleaned text matches the secret detector "+hit+"; nothing was written")
		return
	}
	if !apiActorHandle.MatchString(user) {
		apiErr(w, http.StatusBadRequest, "bad-actor", "actor.username is 1 to 64 of A-Z a-z 0-9 . _ @ + -")
		return
	}
	if s.namesOperator(user) {
		apiErr(w, http.StatusBadRequest, "bad-actor", "actor.username may not name the operator")
		return
	}
	if runesOver(display, apiActorDisplayMax) {
		apiErr(w, http.StatusBadRequest, "bad-actor", "actor.display is at most 64 characters")
		return
	}
	if runesOver(text, externalTextMax) {
		apiErr(w, http.StatusUnprocessableEntity, "bad-text", "An answer is at most 4000 characters.")
		return
	}
	by := AnswerSource{Kind: cl.Kind, Client: cl.Name, Origin: cl.Origin, Actor: user}
	_, err := s.store.AnswerExternal(r.Context(), id, ext, ExternalAnswer{Choice: body.Choice, Text: text, By: by})
	if ac, closed := asAnswerClosed(err); closed {
		x := ac.Item
		if ac.Expired {
			apiJSON(w, http.StatusGone, apiStateWith(x, "expired", "That question expired before it was answered."))
			return
		}
		if a := x.AnsweredBy; a != nil && a.Client == cl.Name && a.Actor == user && x.Choice == body.Choice && x.Text == text {
			apiJSON(w, http.StatusOK, inboxState(x)) // the same answer again: nothing is written
			return
		}
		apiJSON(w, http.StatusConflict, apiStateWith(x, "already_answered", answeredSentence(apiSourceOf(x.AnsweredBy).source())))
		return
	}
	switch {
	case err == nil:
	case errors.Is(err, ErrNotFound):
		apiErr(w, http.StatusNotFound, "not-found", "no such inbox item")
		return
	case errors.Is(err, ErrRejected):
		apiErr(w, http.StatusUnprocessableEntity, "bad-choice", err.Error())
		return
	default:
		apiStoreErr(w, err)
		return
	}
	// The origin is the client's registered host, not where the call came from;
	// the forwarded chain is logged as a lead (Traefik keeps a received
	// X-Forwarded-For only from the tunnel and appends its own peer).
	log.Printf("api: inbox answer %s/%s by %s peer=%s xff=%q", id, ext, cl.Name, r.RemoteAddr,
		qaTrim(inboxText(r.Header.Get("X-Forwarded-For")), 200))
	s.inbox.Kick() // the QA tool's own items are pushed; never wait for it
	x, err := s.store.ExternalGet(r.Context(), id, ext)
	if err != nil {
		apiStoreErr(w, err)
		return
	}
	apiJSON(w, http.StatusOK, inboxState(x))
}

// namesOperator says a reported handle poses as the back office: "operator",
// or a back-office account's email.
func (s *server) namesOperator(user string) bool {
	if strings.EqualFold(user, "operator") {
		return true
	}
	if s.auth == nil {
		return false
	}
	e, ok := canonEmail(user)
	return ok && s.auth.accounts[e] != nil
}
