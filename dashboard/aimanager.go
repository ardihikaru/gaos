package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// The ai-manager client. D-055: GAOS no longer runs a model tool; it asks
// ai-manager to run one turn inside the product's workspace, and ai-manager
// hands it to ai-claude-cli. This file knows ai-manager's wire shapes and
// nothing above the adapter does.
//
// Every turn is ASYNC with a deterministic X-Request-ID: ai-manager treats a
// repeated POST with the same id as a no-op, so a request whose 202 was lost
// can be sent again without starting a second turn, and the same id is what a
// restarted orchestrator polls (D-060). Contract: architecture/gateway.html.

// Typed answers, one per row of the gateway's error mapping.
var (
	ErrTurnRunning       = errors.New("ai-manager: another turn of this session is running (409)")
	ErrSessionGone       = errors.New("ai-manager: the session is expired or unknown (410)")
	ErrWorkspaceInactive = errors.New("ai-manager: the workspace is not enabled for chat (403/423)")
	ErrRequestUnknown    = errors.New("ai-manager: the request is unknown or expired (404)")
	ErrGatewayTransient  = errors.New("ai-manager: the gateway or its worker failed (5xx)")
)

// AIMCredentials are the two secrets, read from files and never logged.
// Chat is the ai-stack Basic login; Admin is the bearer for provisioning only.
type AIMCredentials struct {
	Login, Secret string
	AdminKey      string
}

// LoadAIMCredentials reads the Basic login from a JSON file of the shape
// ~/.config/ai-stack/basic-auth.json ({"login_id","secret_key"}) and the
// admin key, if a path is given, from a file holding just the key.
func LoadAIMCredentials(chatFile, adminFile string) (AIMCredentials, error) {
	var c AIMCredentials
	b, err := os.ReadFile(chatFile)
	if err != nil {
		return c, fmt.Errorf("GAOS_AIM_CHAT_AUTH_FILE: %w", err)
	}
	var f struct {
		Login  string `json:"login_id"`
		Secret string `json:"secret_key"`
	}
	if json.Unmarshal(b, &f) != nil || f.Login == "" || f.Secret == "" {
		return c, fmt.Errorf("GAOS_AIM_CHAT_AUTH_FILE %s: want {\"login_id\",\"secret_key\"}", chatFile)
	}
	c.Login, c.Secret = f.Login, f.Secret
	if adminFile != "" {
		k, err := os.ReadFile(adminFile)
		if err != nil {
			return c, fmt.Errorf("GAOS_AIM_ADMIN_KEY_FILE: %w", err)
		}
		c.AdminKey = strings.TrimSpace(string(k))
	}
	return c, nil
}

// AIMClient is a stdlib HTTP client of ai-manager.
type AIMClient struct {
	Base  string // e.g. https://ai-manager.mfardiansyah.id
	Creds AIMCredentials
	HTTP  *http.Client
}

// ChatRequest is the body of POST /v1/chat that GAOS sends.
type ChatRequest struct {
	Prompt string `json:"prompt"`
	// Provider is the vendor (M16): "" for Claude, which ai-manager pins
	// sessions to by default, so a Claude turn's body is what it was before.
	Provider    string `json:"provider,omitempty"`
	Model       string `json:"model,omitempty"`
	NewSession  bool   `json:"new_session,omitempty"`
	SessionID   string `json:"session_id,omitempty"`
	WorkspaceID string `json:"workspace_id,omitempty"`
	// AskUser lets the agent end its turn with an ai-question block (D-064);
	// Caller tags the question so GET /v1/questions can filter GAOS's own.
	AskUser bool   `json:"ask_user,omitempty"`
	Caller  string `json:"caller,omitempty"`
}

// ChatReply is the part of ai-manager's reply GAOS reads.
type ChatReply struct {
	Text         string          `json:"text"`
	Provider     string          `json:"provider"`
	Model        string          `json:"model"`
	InputTokens  int64           `json:"input_tokens"`
	OutputTokens int64           `json:"output_tokens"`
	CostUSD      *float64        `json:"cost_usd"` // nil = not reported
	FinishReason string          `json:"finish_reason"`
	SessionID    string          `json:"session_id"`
	WorkspaceID  string          `json:"workspace_id"`
	Metadata     json.RawMessage `json:"metadata"`
	// Question is set when the agent ended the turn asking the operator; the
	// finish reason is then "awaiting_input" (M15).
	Question *GatewayQuestion `json:"question,omitempty"`
}

// ObservedContext reports metadata.context.workspace: what the worker's
// workspace offered Claude Code for this turn. Nil when the reply has none.
func (r ChatReply) ObservedContext() *ObservedContext {
	var m struct {
		C struct {
			W *ObservedContext `json:"workspace"`
		} `json:"context"`
	}
	json.Unmarshal(r.Metadata, &m)
	return m.C.W
}

// SessionWarning reports metadata.session_warning: the turn was not saved.
func (r ChatReply) SessionWarning() string {
	var m struct {
		W string `json:"session_warning"`
	}
	json.Unmarshal(r.Metadata, &m)
	return m.W
}

func (c *AIMClient) client() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 30 * time.Second}
}

// do sends one request with the Basic login. The credentials never appear in
// an error: errors name the method, the path and the status only (TC-284).
func (c *AIMClient) do(ctx context.Context, method, path, requestID string, body any) (int, []byte, error) {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return 0, nil, err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(c.Base, "/")+path, rd)
	if err != nil {
		return 0, nil, fmt.Errorf("ai-manager %s %s: %w", method, path, err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth(c.Creds.Login, c.Creds.Secret)
	if requestID != "" {
		req.Header.Set("X-Request-ID", requestID)
	}
	resp, err := c.client().Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("%w: %s %s unreachable", ErrGatewayTransient, method, path)
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	return resp.StatusCode, out, nil
}

// Submit sends a turn asynchronously. 202 (or 200 for a request ai-manager
// already holds) means accepted; anything else is a typed error.
func (c *AIMClient) Submit(ctx context.Context, requestID string, r ChatRequest) error {
	status, body, err := c.do(ctx, http.MethodPost, "/v1/chat", requestID, r)
	if err != nil {
		return err
	}
	if status == http.StatusAccepted || status == http.StatusOK {
		return nil
	}
	return statusError(status, body)
}

// Result polls a submitted turn once. done=false while it is still running.
func (c *AIMClient) Result(ctx context.Context, requestID string) (ChatReply, bool, error) {
	status, body, err := c.do(ctx, http.MethodGet, "/v1/requests/"+url.PathEscape(requestID)+"/result", "", nil)
	if err != nil {
		return ChatReply{}, false, err
	}
	switch status {
	case http.StatusAccepted:
		return ChatReply{}, false, nil
	case http.StatusOK:
		var r ChatReply
		if json.Unmarshal(body, &r) != nil {
			return ChatReply{}, true, fmt.Errorf("%w: the result is not a chat reply", ErrGatewayTransient)
		}
		return r, true, nil
	case http.StatusNotFound:
		return ChatReply{}, true, ErrRequestUnknown
	}
	return ChatReply{}, true, statusError(status, body)
}

// statusError maps a status to its typed error, keeping ai-manager's own
// {"error": ...} sentence — which never carries a credential — as detail.
func statusError(status int, body []byte) error {
	var e struct {
		Error string `json:"error"`
	}
	json.Unmarshal(body, &e)
	var base error
	switch {
	case status == http.StatusConflict:
		base = ErrTurnRunning
	case status == http.StatusGone:
		base = ErrSessionGone
	case status == http.StatusForbidden || status == http.StatusLocked:
		base = ErrWorkspaceInactive
	case status >= 500:
		base = ErrGatewayTransient
	default:
		return fmt.Errorf("ai-manager answered %d: %s", status, e.Error)
	}
	if e.Error == "" {
		return base
	}
	return fmt.Errorf("%w: %s", base, e.Error)
}

// Providers lists the providers ai-manager has available, for the boot smoke
// test.
func (c *AIMClient) Providers(ctx context.Context) ([]string, error) {
	all, err := c.ProviderInfo(ctx)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, x := range all {
		if x.Available {
			out = append(out, x.Name)
		}
	}
	return out, nil
}

// ProviderInfo is GET /v1/providers in full: every provider, available or
// not, with the models it accepts (M16: the vendors pane's model picker).
func (c *AIMClient) ProviderInfo(ctx context.Context) ([]VendorInfo, error) {
	status, body, err := c.do(ctx, http.MethodGet, "/v1/providers", "", nil)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, statusError(status, body)
	}
	var p struct {
		Providers []struct {
			Name         string   `json:"name"`
			Available    bool     `json:"available"`
			DefaultModel string   `json:"default_model"`
			Models       []string `json:"models"`
		} `json:"providers"`
	}
	if json.Unmarshal(body, &p) != nil {
		return nil, fmt.Errorf("ai-manager /v1/providers: unexpected reply")
	}
	out := make([]VendorInfo, 0, len(p.Providers))
	for _, x := range p.Providers {
		out = append(out, VendorInfo{Name: x.Name, Available: x.Available, DefaultModel: x.DefaultModel, Models: x.Models})
	}
	return out, nil
}
