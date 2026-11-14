package main

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// The QA tool's door into the inbox (D-078, docs/designs/qa-inbox-api.md).
//
//	POST /v1/products/{id}/inbox                 ask the operator a question
//	GET  /v1/products/{id}/inbox/{externalId}    poll for the decision
//
// Both take the QA token (or the admin one), and the QA token reaches only the
// products GAOS_API_QA_PRODUCTS names; any other product is a 404. The answer
// is also pushed to the QA tool (inbox_worker.go); this GET is what keeps
// working when the push does not.

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

func (s *server) apiInboxAsk(w http.ResponseWriter, r *http.Request) {
	scope, ok := s.apiGate(w, r, scopeQA)
	if !ok {
		return
	}
	id, ok := s.apiProduct(w, r, scope)
	if !ok {
		return
	}
	release, ok := s.beginWrite(w, scope)
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
	item, created, _, err := s.store.AskExternal(r.Context(), id, ask)
	if err != nil {
		apiStoreErr(w, err)
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	apiJSON(w, status, apiInboxCreated{ID: item.ItemID, ExternalID: item.ExternalID, Status: item.Status, Created: created})
}

// apiInboxState is what the QA tool polls. The optional fields are absent while
// they have no value, so "choice" is there only once somebody chose.
type apiInboxState struct {
	ExternalID  string `json:"externalId"`
	Status      string `json:"status"`
	Choice      int    `json:"choice,omitempty"`
	OptionLabel string `json:"optionLabel,omitempty"`
	Text        string `json:"text,omitempty"`
	AskedAt     int64  `json:"askedAt"`
	AnsweredAt  int64  `json:"answeredAt,omitempty"`
	ExpiresAt   int64  `json:"expiresAt"`
}

func (s *server) apiInboxGet(w http.ResponseWriter, r *http.Request) {
	scope, ok := s.apiGate(w, r, scopeQA)
	if !ok {
		return
	}
	id, ok := s.apiProduct(w, r, scope)
	if !ok {
		return
	}
	ext := r.PathValue("externalId")
	if !apiInboxID.MatchString(ext) {
		apiErr(w, http.StatusNotFound, "not-found", "no such inbox item")
		return
	}
	x, err := s.store.ExternalGet(r.Context(), id, ext)
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
	return apiInboxState{ExternalID: x.ExternalID, Status: x.Status, Choice: x.Choice,
		OptionLabel: x.OptionLabel(), Text: x.Text, AskedAt: x.AskedAt, AnsweredAt: x.AnsweredAt, ExpiresAt: x.Deadline}
}
