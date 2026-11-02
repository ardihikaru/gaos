package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// M15, "Needs you" (D-064). An agent that needs a human decision ends its turn
// with an ai-question block; ai-manager parses it, stores it in the temp bucket
// and returns it on the reply. The answer goes back through
// POST /v1/questions/{id}/answer, which ai-manager turns into the next turn of
// the same session under the request id GAOS chose -- so the answer turn is
// polled at /v1/requests/{id}/result exactly like any other.

// FinishAwaitingInput is the finish reason of a turn that ended on a question.
const FinishAwaitingInput = "awaiting_input"

// The operator's four choices: three the agent offered, the first of them
// recommended, and their own words.
const (
	ChoiceRecommended = 1
	ChoiceOther       = 4
)

var (
	ErrQuestionAnswered = errors.New("ai-manager: the question was already answered (409)")
	ErrQuestionExpired  = errors.New("ai-manager: the question has expired (410)")
)

// QuestionOption is one of the three options an agent offers.
type QuestionOption struct {
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
}

// GatewayQuestion is the question as ai-manager returns it on a reply.
type GatewayQuestion struct {
	ID       string           `json:"id"`
	Question string           `json:"question"`
	Context  string           `json:"context,omitempty"`
	Options  []QuestionOption `json:"options"`
}

// AnswerRequest is the body of POST /v1/questions/{id}/answer. Prompt carries
// GAOS's reply contract, so the resumed turn still ends in a gaos-handoff.
type AnswerRequest struct {
	Choice int    `json:"choice"`
	Text   string `json:"text,omitempty"`
	Prompt string `json:"prompt,omitempty"`
}

// validAnswer is the rule both ends enforce: a choice of 1..4, and words when
// the choice is the operator's own.
func validAnswer(choice int, text string) error {
	if choice < ChoiceRecommended || choice > ChoiceOther {
		return reject(fmt.Sprintf("Choice %d is not one of the four; pick 1, 2, 3 or 4.", choice))
	}
	if choice == ChoiceOther && strings.TrimSpace(text) == "" {
		return reject("Your own answer needs words; option 4 cannot be empty.")
	}
	return nil
}

// SubmitAnswer answers a question and starts the resumed turn under requestID.
// A retry with the same request id is accepted again and starts nothing new.
func (c *AIMClient) SubmitAnswer(ctx context.Context, questionID, requestID string, a AnswerRequest) error {
	path := "/v1/questions/" + url.PathEscape(questionID) + "/answer"
	status, body, err := c.do(ctx, http.MethodPost, path, requestID, a)
	if err != nil {
		return err
	}
	switch status {
	case http.StatusAccepted, http.StatusOK:
		return nil
	case http.StatusConflict:
		return ErrQuestionAnswered
	case http.StatusGone:
		return ErrQuestionExpired
	case http.StatusNotFound:
		return ErrRequestUnknown
	}
	return statusError(status, body)
}
